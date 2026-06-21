// Port of cpython/Python/codegen.c. Walks each scope's AST and emits
// instructions into an instruction Sequence. The driver walks the
// symtable top-down and invokes Codegen once per scope.
//
// CPython: Python/codegen.c

package compile

import (
	"fmt"
	"slices"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// Intrinsic ids for CALL_INTRINSIC_1. CPython:
// Include/internal/pycore_intrinsics.h INTRINSIC_*. Numbering matches
// CPython byte-for-byte so the VM dispatch table indexes line up with
// the eval loop's CALL_INTRINSIC_1 reader.
const (
	intrinsicPrint              int32 = 1
	intrinsicStopIterationError int32 = 3
	intrinsicAsyncGenWrap       int32 = 4
	intrinsicTypeVar            int32 = 7
	intrinsicParamSpec          int32 = 8
	intrinsicTypeVarTuple       int32 = 9
	intrinsicSubscriptGeneric   int32 = 10
)

// Intrinsic ids for CALL_INTRINSIC_2. CPython:
// Include/internal/pycore_intrinsics.h INTRINSIC_2_*.
const (
	intrinsicPrepReraiseStar        int32 = 1
	intrinsicTypeVarWithBound       int32 = 2
	intrinsicTypeVarWithConstraints int32 = 3
	intrinsicSetFunctionTypeParams  int32 = 4
	intrinsicSetTypeParamDefault    int32 = 5
)

// Operands for LOAD_SPECIAL. CPython:
// Include/internal/pycore_ceval.h:L380 SPECIAL___ENTER__ etc.
const (
	specialEnter  int32 = 0
	specialExit   int32 = 1
	specialAEnter int32 = 2
	specialAExit  int32 = 3
)

// Operands for RESUME. CPython:
// Include/internal/pycore_compile.h RESUME_AT_FUNC_START etc.
const (
	resumeAtFuncStart    int32 = 0
	resumeAfterYield     int32 = 1
	resumeAfterYieldFrom int32 = 2
	resumeAfterAwait     int32 = 3
)

// CO_* flags. CPython: Include/cpython/code.h.
const (
	CoOptimized          uint32 = 0x0001
	CoNewLocals          uint32 = 0x0002
	CoVarargs            uint32 = 0x0004
	CoVarkeywords        uint32 = 0x0008
	CoNested             uint32 = 0x0010
	CoGenerator          uint32 = 0x0020
	CoCoroutine          uint32 = 0x0080
	CoIterableCoroutine  uint32 = 0x0100
	CoAsyncGenerator     uint32 = 0x0200
	CoNoMonitoringEvents uint32 = 0x2000000
	CoHasDocstring       uint32 = 0x4000000
	CoMethod             uint32 = 0x8000000
)

// Unit is the per-scope handoff codegen produces. The flowgraph
// optimizes Seq in place and the assembler packs the result into a
// Code object.
//
// CPython: Python/compile.c compiler_unit
type Unit struct {
	Name      string
	Qualname  string
	ScopeType symtable.Block
	// Ste is the symtable Entry this unit was entered from. Kept so
	// compiler_set_qualname's force_global lookup can resolve the new
	// scope's name against the parent entry's symbol scopes.
	//
	// CPython: Python/compile.c compiler_unit.u_ste
	Ste                 *symtable.Entry
	Argcount            int
	PosOnlyArgCount     int
	KwOnlyArgCount      int
	FirstLineno         int
	Flags               uint32
	Seq                 *Sequence
	Consts              []any
	Names               []string
	VarNames            []string
	FreeVars            []string
	CellVars            []string
	FastHidden          map[string]bool
	DeferredAnnotations []deferredAnnotation
	// NextConditionalAnnotationIndex is the counter for conditional annotation
	// indices, incremented each time a new conditional annotation is recorded.
	// Mirrors CPython's Python/compile.c:670 u_next_conditional_annotation_index.
	NextConditionalAnnotationIndex int
	// InConditionalBlock is the nesting depth of conditional control-flow blocks
	// (if/for/while/match/try/with). Annotations inside conditional blocks get
	// a non-(-1) CondIdx so the __annotate__ function can guard their inclusion.
	// Mirrors CPython's Python/compile.c:64 u_in_conditional_block.
	InConditionalBlock int
	// InInlinedComp is the nesting depth of inlined comprehensions whose
	// body is currently being emitted into this unit. Used to compute the
	// in_class_block flag (a comprehension directly in a class body
	// isolates its locals, but one nested inside another inlined comp does
	// not re-isolate).
	//
	// CPython: Python/compile.c:70 compiler_unit.u_in_inlined_comp
	InInlinedComp int
	// Private is the enclosing class name used for PEP 8 private name
	// mangling. Set when entering a class scope and inherited by nested
	// function / comprehension scopes so `self.__x` inside a method
	// resolves the same attribute as `self._ClassName__x`.
	//
	// CPython: Python/compile.c compiler_unit.u_private
	Private string
	// StaticAttributes collects attribute names assigned via `self.X = ...`
	// inside any nested method of a class body. The class body emitter
	// sorts these into a tuple and stores it as __static_attributes__ on
	// the class, used by the runtime for slot-skipping optimization.
	//
	// CPython: Python/compile.c compiler_unit.u_static_attributes
	StaticAttributes map[string]bool
}

// deferredAnnotation records one PEP 649 annotation pending emission
// at end-of-block. Filled by codegen_expr_ann.go.
//
// CPython: Python/codegen.c:L737 codegen_deferred_annotations_body
type deferredAnnotation struct {
	Name  string
	Value ast.Expr
	Loc   ast.Pos
	// CondIdx is the conditional annotation index (-1 = unconditional).
	// At the annotation site the compiler emits SET_ADD to track which
	// indices were actually reached; emitAnnotateBody uses it to guard
	// each annotation with CONTAINS_OP.
	//
	// CPython: Python/compile.c:63 u_conditional_annotation_indices
	CondIdx int
}

// Compiler is the long-lived driver state shared by every Codegen
// call within one Compile invocation.
//
// CPython: Python/compile.c compiler
// compilerRecursionLimit mirrors CPython Python/symtable.c C_RECURSION_LIMIT
// used via ENTER_RECURSIVE / Py_EnterRecursiveCall during compilation.
const compilerRecursionLimit = 500

type Compiler struct {
	Filename string
	Optimize int
	Future   *future.Features
	Symtable *symtable.Table

	// recursionRemaining guards visitExpr / visitStmt against circular ASTs.
	// CPython: Python/compile.c (via Py_EnterRecursiveCall in codegen.c)
	recursionRemaining int

	// units stacks active scopes during the recursive descent. The
	// top-of-stack is the scope currently being codegen'd; nested
	// function bodies push a fresh unit, emit, and pop.
	units []*Unit

	// constCache deduplicates constants across the current unit.
	// Cleared on each enterScope. CPython: Python/compile.c
	// compiler_add_const uses const_cache + consts list.
	constCache map[any]int

	// nameCache, varnameCache, freeCache, cellCache: same idea for
	// the per-pool dedup. CPython uses PyDict.
	nameCache    map[string]int
	varnameCache map[string]int
	freeCache    map[string]int
	cellCache    map[string]int

	// scope is the symtable Entry for the unit on top of units. Set
	// by enterScope and used by every visitor for name resolution.
	scope *symtable.Entry

	// fblocks is the per-unit frame block stack. Cleared on each
	// enterScope.
	fblocks []fblock

	// disableWarning suppresses compile-time SyntaxWarnings while
	// non-zero. The exception-path copy of a finally body is compiled a
	// second time (under a FINALLY_END fblock), so without this guard a
	// warning inside the finally clause would be emitted twice. Pushing
	// the FINALLY_END fblock bumps this counter; popping it restores it.
	//
	// CPython: Python/compile.c:106 compiler.c_disable_warning
	disableWarning int

	// interactive marks compile-mode='single' (REPL / doctest). When
	// set, expression-statements at module nest level emit
	// CALL_INTRINSIC_1 INTRINSIC_PRINT so each result reaches
	// sys.displayhook, even when the statement sits inside a compound
	// block such as `with` or `if`.
	//
	// CPython: Python/codegen.c codegen_stmt_expr (c->c_interactive &&
	// c->c_nestlevel <= 1 fires PRINT_EXPR)
	interactive bool

	// saveNestedSeqs makes leaveScope hang each finished scope's
	// instruction sequence off its parent unit's Seq.Nested, so a
	// caller can walk the recursive pre-flowgraph instruction tree.
	// Only the _testinternalcapi.compiler_codegen helper sets it; the
	// normal compile path leaves it false (nested scopes flow through
	// the const pool as *Unit instead).
	//
	// CPython: Python/compile.c:103 compiler.c_save_nested_seqs
	saveNestedSeqs bool
}

// NewCompiler builds a fresh driver. Symtable must already be built
// over mod.
//
// CPython: Python/compile.c new_compiler
func NewCompiler(filename string, optimize int, ff *future.Features, st *symtable.Table) *Compiler {
	return &Compiler{
		Filename:           filename,
		Optimize:           optimize,
		Future:             ff,
		Symtable:           st,
		recursionRemaining: compilerRecursionLimit,
	}
}

// Codegen emits instructions for one scope. The caller drives the
// walk; codegen does not recurse into nested scopes itself (the
// driver pushes a new unit and calls Codegen again).
//
// CPython: Python/codegen.c _PyCodegen_Module / _PyCodegen_FunctionBody
func (c *Compiler) Codegen(sc *symtable.Entry, mod ast.Mod) (*Unit, error) {
	c.enterScope(sc)
	defer c.leaveScope()

	// Module scope opens with RESUME RESUME_AT_FUNC_START. CPython
	// builds loc = LOCATION(lineno, lineno, 0, 0) where lineno is the
	// scope's firstlineno (1 for the toplevel module), then for
	// COMPILE_SCOPE_MODULE overrides loc.lineno to 0 so dis prints
	// `0 RESUME 0` ahead of the body. The override leaves end_lineno
	// at firstlineno, which is what the long-form location entry on
	// the linetable encodes (line_delta=-1, end_line_delta=1, ...).
	//
	// CPython: Python/codegen.c:647 codegen_enter_scope (the
	// ADDOP_I(c, loc, RESUME, RESUME_AT_FUNC_START) after
	// _PyCompile_EnterScope, with loc.lineno = 0 for module scope).
	resumeLoc := ast.Pos{Lineno: 0, EndLineno: c.unit().FirstLineno, ColOffset: 0, EndColOffset: 0}
	c.addOpI(RESUME, resumeAtFuncStart, resumeLoc)

	// Module scope plants ANNOTATIONS_PLACEHOLDER right after RESUME. The
	// deferred __annotate__ build (stashed via SetAnnotationsCode) is spliced
	// in at this marker by cfgFromSequence, so it lands after RESUME and
	// before the body's BUILD_SET/STORE __conditional_annotations__ prologue.
	// CPython plants it unconditionally and drops it when no stash exists; we
	// only plant it when the module carries (always-conditional) annotations,
	// which is exactly when a stash is produced. The spliced-out result is
	// byte-identical either way.
	//
	// CPython: Python/codegen.c:659 codegen_enter_scope (COMPILE_SCOPE_MODULE)
	if sc.HasConditionalAnnotations {
		c.addOp(ANNOTATIONS_PLACEHOLDER, resumeLoc)
	}

	switch m := mod.(type) {
	case *ast.Module:
		if err := c.visitModule(m); err != nil {
			return nil, err
		}
	case *ast.Expression:
		if err := c.visitExpressionMod(m); err != nil {
			return nil, err
		}
	case *ast.Interactive:
		if err := c.visitInteractive(m); err != nil {
			return nil, err
		}
	case *ast.FunctionType:
		// FunctionType is a typing-only mod; never compiled to bytecode.
		return nil, fmt.Errorf("compile: FunctionType has no bytecode form")
	default:
		return nil, fmt.Errorf("compile: unknown mod kind %T", mod)
	}

	return c.unit(), nil
}

// enterScope pushes a fresh Unit onto the stack and resets the
// per-scope caches. Mirrors codegen_enter_scope.
//
// CPython: Python/codegen.c:L648 codegen_enter_scope
//
//nolint:gocyclo // mirrors codegen_enter_scope per-scope dispatch
func (c *Compiler) enterScope(sc *symtable.Entry) {
	name := sc.Name
	// Symtable uses "top" as the internal name for the module-level
	// entry; CPython's codegen replaces it with "<module>" before
	// stamping it into the toplevel Code object.
	//
	// CPython: Python/codegen.c:917 _PyCodegen_EnterAnonymousScope
	if sc.Type == symtable.ModuleBlock {
		name = "<module>"
	}
	// firstlineno follows the call site that pushes the scope. CPython
	// passes `1` for the toplevel module scope via
	// _PyCodegen_EnterAnonymousScope (Python/codegen.c:917) and the
	// statement's lineno for nested scopes. The symtable Entry does
	// not record a Loc for the module block, so its sc.Loc.Lineno is
	// zero; clamp to 1 here so co_firstlineno matches CPython for
	// freshly compiled modules.
	firstLine := sc.Loc.Lineno
	if sc.Type == symtable.ModuleBlock {
		firstLine = 1
	}
	u := &Unit{
		Name:        name,
		Qualname:    buildQualname(c.units, name, sc.Type),
		ScopeType:   sc.Type,
		Ste:         sc,
		FirstLineno: firstLine,
		Seq:         &Sequence{},
		FastHidden:  map[string]bool{},
	}
	// Inherit u_private from the parent unit. Class scopes override
	// this below so a class body's private name is its own class name.
	// Mirrors compiler_enter_scope's private-parameter threading.
	//
	// CPython: Python/compile.c compiler_enter_scope (u_private init)
	if len(c.units) > 0 {
		u.Private = c.units[len(c.units)-1].Private
	}
	if sc.Type == symtable.ClassBlock {
		u.Private = sc.Name
	}
	switch sc.Type {
	case symtable.ModuleBlock:
		u.Flags = 0
		// PyCF_ALLOW_TOP_LEVEL_AWAIT: a module whose body awaits is a
		// coroutine. A module can never be a generator (yield at module
		// scope is a syntax error), so the coroutine bit stands alone.
		//
		// CPython: Python/codegen.c compute_code_flags (ste_coroutine on
		// the module entry sets CO_COROUTINE)
		if sc.Coroutine {
			u.Flags |= CoCoroutine
		}
	case symtable.FunctionBlock:
		u.Flags = CoOptimized | CoNewLocals
		if sc.Method {
			u.Flags |= CoMethod
		}
		// An async generator is both a coroutine and a generator; set
		// CO_ASYNC_GENERATOR in that case. CPython: compute_code_flags.
		switch {
		case sc.Generator && sc.Coroutine:
			u.Flags |= CoAsyncGenerator
		case sc.Generator:
			u.Flags |= CoGenerator
		case sc.Coroutine:
			u.Flags |= CoCoroutine
		}
	case symtable.ClassBlock:
		u.Flags = 0
		u.StaticAttributes = map[string]bool{}
	case symtable.AnnotationBlock:
		// PEP 649 __annotate__ runs as a regular function: optimized
		// fast locals, fresh f_locals. Without these flags the inner
		// code object lacks the CO_NEWLOCALS contract, so its single
		// `format` parameter never lands in LOCALS[0] and the first
		// LOAD_FAST inside the body trips on an unbound slot.
		//
		// CPython: Python/codegen.c codegen_setup_annotations_scope
		// (the SCOPE_TYPE_FUNCTION flags it asks for)
		u.Flags = CoOptimized | CoNewLocals
	case symtable.TypeParametersBlock,
		symtable.TypeVariableBlock,
		symtable.TypeAliasBlock:
		// PEP 695 synthetic function scopes: the outer "<generic
		// parameters of X>" wrapper, per-TypeVar bound / default
		// thunks, and the body of `type X = ...`. CPython enters all
		// three through codegen_enter_scope with
		// COMPILE_SCOPE_ANNOTATIONS, which sets CO_OPTIMIZED |
		// CO_NEWLOCALS (and the nested flag below).
		//
		// CPython: Python/codegen.c:648 codegen_enter_scope
		// (COMPILE_SCOPE_ANNOTATIONS branch)
		u.Flags = CoOptimized | CoNewLocals
	}
	// CoNested: any scope nested inside a non-module scope. Mirrors
	// CPython's compute_code_flags COMPILER_FLAGS_NESTED check.
	if len(c.units) > 0 && c.units[len(c.units)-1].ScopeType != symtable.ModuleBlock {
		u.Flags |= CoNested
	}
	c.units = append(c.units, u)
	c.scope = sc
	c.fblocks = nil
	c.constCache = map[any]int{}
	c.nameCache = map[string]int{}
	c.varnameCache = map[string]int{}
	c.freeCache = map[string]int{}
	c.cellCache = map[string]int{}
	// Pre-allocate CellVars / FreeVars slots in a deterministic order so
	// the outer scope's emitClosure (which pushes cells in the same order)
	// and the cfg pipeline's insert_prefix_instructions (which emits one
	// MAKE_CELL per cell symbol) agree on slot indices. Mirrors CPython's
	// compiler_enter_scope dictbytype(CELL/FREE) population: cell symbols
	// land in u_cellvars, free symbols in u_freevars, both alphabetically.
	//
	// CPython: Python/compile.c compiler_enter_scope (dictbytype CELL/FREE)
	var cellNames []string
	var freeNames []string
	for name, flags := range sc.Symbols {
		// dictbytype(ste_symbols, CELL, DEF_COMP_CELL, 0): a name lands in
		// u_cellvars when its scope is CELL or it is an inlined-comprehension
		// cell (DEF_COMP_CELL). The hidden loop variable of an inlined
		// comprehension that a nested function captures is a cell in the
		// enclosing code object even though the name's own scope resolved to
		// a global/name binding (e.g. a same-named global referenced
		// elsewhere in the parent), so it must be pre-allocated here so deref
		// offsets stay stable regardless of emission order.
		//
		// CPython: Python/compile.c:605 u_cellvars = dictbytype(..., CELL, DEF_COMP_CELL, 0)
		if flags.Scope() == symtable.Cell || flags&symtable.DefCompCell != 0 {
			cellNames = append(cellNames, name)
			continue
		}
		switch flags.Scope() {
		case symtable.Free:
			freeNames = append(freeNames, name)
		default:
			// DEF_FREE_CLASS: a class scope can have a name that is
			// LOCAL to the class yet also referenced as Free by a
			// nested method. The class itself doesn't make a cell
			// (the local binding wins inside the class body), but
			// the nested method's closure tuple still needs to
			// pull the value from the enclosing function. CPython
			// handles this by listing the name in u_freevars even
			// when its scope is LOCAL, so dict_lookup_arg finds
			// the cell index.
			//
			// CPython: Python/compile.c:641 compiler_enter_scope
			// (dictbytype(symbols, FREE, DEF_FREE_CLASS, ...))
			if flags&symtable.DefFreeClass != 0 {
				freeNames = append(freeNames, name)
			}
		}
	}
	sortStrings(cellNames)
	sortStrings(freeNames)
	// __conditional_annotations__ is forced into u_cellvars (after the
	// sorted cells) when the scope tracks conditional annotations, even
	// where the symtable left the name GLOBAL. That happens at module
	// scope: the generated __annotate__ reads the set via LOAD_GLOBAL, so
	// the name never goes free and never resolves to CELL, yet the code
	// object still needs the cell so MAKE_CELL runs at the prologue. Class
	// scopes already resolve it to CELL above (the annotate body uses
	// LOAD_DEREF), so this add is a no-op there, matching DictAddObj.
	//
	// CPython: Python/compile.c:630 compiler_enter_scope (DictAddObj cellvars)
	if sc.HasConditionalAnnotations && !slices.Contains(cellNames, "__conditional_annotations__") {
		cellNames = append(cellNames, "__conditional_annotations__")
	}
	for _, name := range cellNames {
		u.CellVars = append(u.CellVars, name)
		c.cellCache[name] = len(u.CellVars) - 1
	}
	for _, name := range freeNames {
		u.FreeVars = append(u.FreeVars, name)
		c.freeCache[name] = len(u.FreeVars) - 1
	}
}

// buildQualname assembles co_qualname from the parent unit stack and
// the new scope's name. Top-level scopes get just the name; scopes
// nested inside a class get "Outer.Name"; scopes nested inside a
// function get "Outer.<locals>.Name". Mirrors CPython's
// compiler_set_qualname.
//
// Annotation, type-params, type-alias, and type-variable scopes are
// transparent: they do not appear in the qualname chain. CPython
// treats them all as COMPILE_SCOPE_ANNOTATIONS (which is skipped) in
// codegen_enter_scope, so a class Foo[T] has qualname "Foo", not
// "<generic parameters of Foo>.<locals>.Foo".
//
// CPython: Python/compile.c:L644 compiler_set_qualname
func buildQualname(stack []*Unit, name string, scopeType symtable.Block) string {
	if len(stack) == 0 {
		return name
	}
	// Walk backwards past transparent scopes to find the real parent.
	parent := stack[len(stack)-1]
	for isTransparentScope(parent.ScopeType) {
		stack = stack[:len(stack)-1]
		if len(stack) == 0 {
			return name
		}
		parent = stack[len(stack)-1]
	}
	// force_global: when the new scope is a function or class whose name
	// resolves to GLOBAL_EXPLICIT in the parent symtable entry (a `global`
	// declaration of that name in the parent), the qualname is just the
	// bare name with no parent prefix. CPython asserts the scope is never
	// GLOBAL_IMPLICIT here.
	//
	// CPython: Python/compile.c:259 compiler_set_qualname (force_global)
	if scopeType == symtable.FunctionBlock || scopeType == symtable.ClassBlock {
		if parent.Ste != nil {
			mangled := symtable.Mangle(parent.Private, name)
			if parent.Ste.GetScope(mangled) == symtable.GlobalExplicit {
				return name
			}
		}
	}
	if parent.ScopeType == symtable.ModuleBlock {
		return name
	}
	if parent.ScopeType == symtable.ClassBlock {
		return parent.Qualname + "." + name
	}
	// Annotation scopes use "." not ".<locals>." even when the parent
	// is a function scope. CPython: Python/compile.c:644
	// compiler_set_qualname (COMPILE_SCOPE_ANNOTATIONS branch).
	if scopeType == symtable.AnnotationBlock {
		return parent.Qualname + "." + name
	}
	return parent.Qualname + ".<locals>." + name
}

// isTransparentScope reports whether a scope type is invisible in
// qualname computation. These scopes (PEP 695 generics, PEP 649
// annotations) are entered as COMPILE_SCOPE_ANNOTATIONS in CPython,
// which compiler_set_qualname skips when building co_qualname.
//
// CPython: Python/compile.c:244 (COMPILE_SCOPE_ANNOTATIONS check)
func isTransparentScope(t symtable.Block) bool {
	switch t {
	case symtable.AnnotationBlock,
		symtable.TypeParametersBlock,
		symtable.TypeVariableBlock,
		symtable.TypeAliasBlock:
		return true
	}
	return false
}

// leaveScope pops the top unit. The unit is still reachable through
// the slice the caller saved before the pop; CPython models this as
// a return value from compiler_exit_scope.
//
// CPython: Python/codegen.c:L660 compiler_exit_scope
func (c *Compiler) leaveScope() {
	if len(c.units) == 0 {
		return
	}
	child := c.units[len(c.units)-1]
	c.units = c.units[:len(c.units)-1]
	if len(c.units) > 0 {
		// scope tracking only matters for the active unit. The
		// driver re-enters the parent scope explicitly.
		c.scope = nil
		// _PyCompile_ExitScope appends the finished child sequence to
		// the parent under c_save_nested_seqs, in scope-exit order.
		//
		// CPython: Python/compile.c:719 _PyCompile_ExitScope
		if c.saveNestedSeqs && child != nil && child.Seq != nil {
			c.units[len(c.units)-1].Seq.AddNested(child.Seq)
		}
	}
}

// unit returns the unit on top of the stack.
//
// CPython: Python/compile.c c->u (top-of-stack accessor)
func (c *Compiler) unit() *Unit {
	if len(c.units) == 0 {
		return nil
	}
	return c.units[len(c.units)-1]
}

// seq returns the active instruction sequence.
//
// CPython: Python/compile.c INSTR_SEQUENCE(c)
func (c *Compiler) seq() *Sequence {
	return c.unit().Seq
}

// loc returns the position of any AST node, or the zero Pos.
//
// CPython: Python/codegen.c LOC macro
func loc(n any) ast.Pos {
	if n == nil {
		return ast.Pos{}
	}
	if pos, ok := n.(interface{ Position() ast.Pos }); ok {
		return pos.Position()
	}
	return ast.Pos{}
}
