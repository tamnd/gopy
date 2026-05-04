// Port of cpython/Python/codegen.c. Walks each scope's AST and emits
// instructions into an instruction Sequence. The driver walks the
// symtable top-down and invokes Codegen once per scope.
//
// CPython: Python/codegen.c

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// Intrinsic ids for CALL_INTRINSIC_1. CPython:
// Include/internal/pycore_intrinsics.h INTRINSIC_*. Add more as the
// visitors that emit them land.
const (
	intrinsicPrint int32 = 1
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
	CoOptimized         uint32 = 0x0001
	CoNewLocals         uint32 = 0x0002
	CoVarargs           uint32 = 0x0004
	CoVarkeywords       uint32 = 0x0008
	CoNested            uint32 = 0x0010
	CoGenerator         uint32 = 0x0020
	CoCoroutine         uint32 = 0x0100
	CoIterableCoroutine uint32 = 0x0200
	CoAsyncGenerator    uint32 = 0x0400
	CoHasDocstring      uint32 = 0x4000000
	CoMethod            uint32 = 0x8000000
)

// Unit is the per-scope handoff codegen produces. The flowgraph
// optimizes Seq in place and the assembler packs the result into a
// Code object.
//
// CPython: Python/compile.c compiler_unit
type Unit struct {
	Name                string
	Qualname            string
	ScopeType           symtable.Block
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
}

// deferredAnnotation records one PEP 649 annotation pending emission
// at end-of-block. Filled by codegen_expr_ann.go.
//
// CPython: Python/codegen.c:L737 codegen_deferred_annotations_body
type deferredAnnotation struct {
	Name  string
	Value ast.Expr
	Loc   ast.Pos
}

// Compiler is the long-lived driver state shared by every Codegen
// call within one Compile invocation.
//
// CPython: Python/compile.c compiler
type Compiler struct {
	Filename string
	Optimize int
	Future   *future.Features
	Symtable *symtable.Table

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
}

// NewCompiler builds a fresh driver. Symtable must already be built
// over mod.
//
// CPython: Python/compile.c new_compiler
func NewCompiler(filename string, optimize int, ff *future.Features, st *symtable.Table) *Compiler {
	return &Compiler{
		Filename: filename,
		Optimize: optimize,
		Future:   ff,
		Symtable: st,
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
func (c *Compiler) enterScope(sc *symtable.Entry) {
	u := &Unit{
		Name:        sc.Name,
		ScopeType:   sc.Type,
		FirstLineno: sc.Loc.Lineno,
		Seq:         &Sequence{},
		FastHidden:  map[string]bool{},
	}
	switch sc.Type {
	case symtable.ModuleBlock:
		u.Flags = 0
	case symtable.FunctionBlock:
		u.Flags = CoOptimized | CoNewLocals
		if sc.Method {
			u.Flags |= CoMethod
		}
		if sc.Generator {
			u.Flags |= CoGenerator
		}
		if sc.Coroutine {
			u.Flags |= CoCoroutine
		}
	case symtable.ClassBlock:
		u.Flags = 0
	}
	c.units = append(c.units, u)
	c.scope = sc
	c.fblocks = nil
	c.constCache = map[any]int{}
	c.nameCache = map[string]int{}
	c.varnameCache = map[string]int{}
	c.freeCache = map[string]int{}
	c.cellCache = map[string]int{}
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
	c.units = c.units[:len(c.units)-1]
	if len(c.units) > 0 {
		// scope tracking only matters for the active unit. The
		// driver re-enters the parent scope explicitly.
		c.scope = nil
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
