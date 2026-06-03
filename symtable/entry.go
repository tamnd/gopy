package symtable

import (
	"github.com/tamnd/gopy/ast"
)

// Entry is the per-block symbol table state. It mirrors
// PySTEntryObject field-for-field so that algorithms ported from
// symtable.c read line-for-line. The visit pass populates Symbols /
// Varnames / Children plus the boolean attribute flags; the analyze
// pass rewrites Symbols to embed each name's resolved Scope.
//
// CPython: Include/internal/pycore_symtable.h:L88 PySTEntryObject
type Entry struct {
	// ID is the AST node pointer used as the dict key in
	// st_blocks. Go uses the AST node value directly; this field
	// preserves the ordering for diagnostics.
	ID int

	// Type is the kind of namespace this entry represents.
	Type Block

	// Name is the block name. Module is "top"; functions and
	// classes use the def / class statement name; comprehensions
	// use "<listcomp>" / "<setcomp>" / "<dictcomp>" / "<genexpr>";
	// lambdas use "<lambda>".
	Name string

	// Symbols maps each name appearing in the block to its packed
	// SymbolFlags. The visit pass writes only DEF_* / USE bits;
	// the analyze pass ORs in the resolved Scope at bit 12.
	Symbols map[string]SymbolFlags

	// SymbolOrder records the first-insertion order of the keys in
	// Symbols. CPython's ste_symbols is a dict, so symtable.py's
	// get_identifiers() reports names in the order they were added;
	// a Go map loses that, so the order is tracked alongside.
	//
	// CPython: Include/internal/pycore_symtable.h:90 ste_symbols (dict)
	SymbolOrder []string

	// Varnames records function parameter names in declaration
	// order. Used by the codegen to lay out fastlocals.
	Varnames []string

	// Children is the ordered list of nested blocks discovered by
	// the visit pass. The analyze pass drops inlined comprehension
	// children and splices their grandchildren into our list.
	Children []*Entry

	// Directives stores (name, location) tuples for `global` and
	// `nonlocal` statements; consulted to point error messages at
	// the right line.
	Directives []Directive

	// MangledNames is the set of identifiers (currently only
	// PEP 695 type parameter names inside a class scope) that
	// receive private name mangling on lookup.
	MangledNames map[string]struct{}

	// ScopeInfo is set during visits inside type-variable blocks;
	// reproduces ste_scope_info ("a TypeVar default", etc).
	ScopeInfo string

	// Nested is true when this block is lexically inside a
	// FunctionBlock-like ancestor (function / annotation / type
	// alias / type params / type variable). Used to decide whether
	// names may resolve to Free.
	Nested bool

	// Generator marks a function block that contains a `yield` or
	// `yield from`.
	Generator bool

	// Coroutine marks an async def, or a module under
	// PyCF_ALLOW_TOP_LEVEL_AWAIT, that contains an `await`.
	Coroutine bool

	// AnnotationsUsed is true if any annotation appears in this
	// block. Consulted by the compiler to emit __annotations__
	// scaffolding at class scope.
	AnnotationsUsed bool

	// Comprehension tags this block as the body of a list, set,
	// dict, or generator comprehension. NoComprehension otherwise.
	Comprehension ComprehensionType

	// Varargs is true for functions declared with `*args`.
	Varargs bool

	// Varkeywords is true for functions declared with `**kwargs`.
	Varkeywords bool

	// ReturnsValue is true when `return expr` (with a value)
	// appears in this function block.
	ReturnsValue bool

	// NeedsClassClosure is set on class scopes that observe a
	// `__class__` reference from a method.
	NeedsClassClosure bool

	// NeedsClassDict is set on class scopes that observe a
	// `__classdict__` reference from a method body.
	NeedsClassDict bool

	// CompInlined marks a comprehension entry whose body has been
	// inlined into the parent scope by the analyze pass.
	CompInlined bool

	// CompIterTarget is set transiently while visiting a
	// comprehension target so addDef can stamp DefCompIter.
	CompIterTarget bool

	// CanSeeClassScope is set on annotation, type alias, and type
	// params blocks immediately enclosed by a class so name
	// resolution can mediate through that class.
	CanSeeClassScope bool

	// HasDocstring is true for module / function / class blocks
	// whose body opens with a string literal.
	HasDocstring bool

	// Method is true for FunctionBlocks defined directly inside a
	// ClassBlock. Drives the codegen choice of LOAD_METHOD.
	Method bool

	// HasConditionalAnnotations is set on blocks where a
	// conditionally-executed annotation appeared (PEP 649).
	HasConditionalAnnotations bool

	// InConditionalBlock is a transient flag set by the visit pass
	// while inside a conditional annotation context.
	InConditionalBlock bool

	// InUnevaluatedAnnotation is set transiently while visiting an
	// annotation that will not be evaluated under PEP 563.
	InUnevaluatedAnnotation bool

	// CompIterExpr counts how many comprehension iterable
	// expressions enclose the current visit point. CPython tracks
	// this so it can reject walrus inside a comprehension's
	// outermost iterable.
	CompIterExpr int

	// AnnotationBlock is the lazy AnnotationBlock attached to a
	// function or class for PEP 649 deferred annotation
	// evaluation. Nil when the block carries no annotations.
	AnnotationBlock *Entry

	// Loc is the source location of the def / class / comprehension
	// that opened this block.
	Loc ast.Pos
}

// Directive records the position of a `global` / `nonlocal` statement
// for later "x is used prior to global declaration" error reporting.
//
// CPython: Python/symtable.c:L1791 Py_BuildValue("(Niiii)", ...)
type Directive struct {
	Name string
	Loc  ast.Pos
}

// IsFunctionLike reports whether the block participates in name
// binding the way a function does (its locals can be cell variables).
//
// CPython: Python/symtable.c:L566 _PyST_IsFunctionLike
func (e *Entry) IsFunctionLike() bool {
	switch e.Type {
	case FunctionBlock, AnnotationBlock, TypeVariableBlock,
		TypeAliasBlock, TypeParametersBlock:
		return true
	}
	return false
}

// GetSymbol returns the packed SymbolFlags for name, or zero if name
// is not recorded in this block.
//
// CPython: Python/symtable.c:L535 _PyST_GetSymbol
func (e *Entry) GetSymbol(name string) SymbolFlags {
	return e.Symbols[name]
}

// GetScope returns the resolved Scope for name, or zero if the
// analyze pass has not stamped one (e.g. unknown name).
//
// CPython: Python/symtable.c:L555 _PyST_GetScope
func (e *Entry) GetScope(name string) Scope {
	return e.Symbols[name].Scope()
}

// SetSymbol writes flags for name, recording first-insertion order in
// SymbolOrder so iteration matches CPython's insertion-ordered
// ste_symbols dict. Updating an existing name keeps its position.
//
// CPython: Python/symtable.c:L1498 symtable_add_def_helper (PyDict_SetItem)
func (e *Entry) SetSymbol(name string, flags SymbolFlags) {
	if _, present := e.Symbols[name]; !present {
		e.SymbolOrder = append(e.SymbolOrder, name)
	}
	e.Symbols[name] = flags
}

// OrderedSymbols returns the symbol names in first-insertion order.
//
// CPython: Python/symtable.c:L535 ste_symbols dict key order
func (e *Entry) OrderedSymbols() []string {
	return e.SymbolOrder
}
