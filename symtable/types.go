// Package symtable ports CPython's Python/symtable.c into Go.
//
// Two passes drive the package: a block-visit pass over the AST that
// records raw def/use facts on per-scope Entry objects, and an analysis
// pass that resolves each name to one of Local / GlobalExplicit /
// GlobalImplicit / Free / Cell.
//
// CPython: Python/symtable.c, Include/internal/pycore_symtable.h
package symtable

// Block names the kind of namespace an Entry represents. The numeric
// order tracks pycore_symtable.h _Py_block_ty so that ports of error
// messages and dispatch tables can mirror the C source line by line.
//
// CPython: Include/internal/pycore_symtable.h:L13 _Py_block_ty
type Block int

const (
	// FunctionBlock is a def or lambda body.
	FunctionBlock Block = iota
	// ClassBlock is a class body.
	ClassBlock
	// ModuleBlock is the top-level module body.
	ModuleBlock
	// AnnotationBlock is the implicit scope around an annotation
	// expression for PEP 649 deferred evaluation. Inert under PEP 563.
	AnnotationBlock
	// TypeAliasBlock is the implicit scope around `type X = ...`.
	TypeAliasBlock
	// TypeParametersBlock is the implicit scope around `[T, U]` on
	// a generic def or class.
	TypeParametersBlock
	// TypeVariableBlock is the implicit scope around the bound,
	// constraint, or default of a single TypeVar / ParamSpec /
	// TypeVarTuple.
	TypeVariableBlock
)

// String returns the CPython enum name.
//
// CPython: Include/internal/pycore_symtable.h _Py_block_ty enum names
func (b Block) String() string {
	switch b {
	case FunctionBlock:
		return "FunctionBlock"
	case ClassBlock:
		return "ClassBlock"
	case ModuleBlock:
		return "ModuleBlock"
	case AnnotationBlock:
		return "AnnotationBlock"
	case TypeAliasBlock:
		return "TypeAliasBlock"
	case TypeParametersBlock:
		return "TypeParametersBlock"
	case TypeVariableBlock:
		return "TypeVariableBlock"
	}
	return "?"
}

// ComprehensionType marks which comprehension flavor (if any) a
// FunctionBlock encodes.
//
// CPython: Include/internal/pycore_symtable.h:L38 _Py_comprehension_ty
type ComprehensionType int

const (
	// NoComprehension is the default for non-comprehension blocks.
	NoComprehension ComprehensionType = iota
	// ListComprehension marks a `[expr for ...]` body block.
	ListComprehension
	// DictComprehension marks a `{k: v for ...}` body block.
	DictComprehension
	// SetComprehension marks a `{expr for ...}` body block.
	SetComprehension
	// GeneratorExpression marks a `(expr for ...)` body block.
	GeneratorExpression
)

// Scope is the resolved disposition of a name inside a block. Values
// match the CPython numeric constants so that bit-packed symbol flags
// stay byte-equal across a port boundary.
//
// CPython: Include/internal/pycore_symtable.h:L180
type Scope int

const (
	// Local is a name bound in this block by assignment / parameter /
	// import.
	Local Scope = 1
	// GlobalExplicit is a name declared by a `global` statement.
	GlobalExplicit Scope = 2
	// GlobalImplicit is a name treated as global because no binding
	// covers it.
	GlobalImplicit Scope = 3
	// Free is a name bound in some enclosing function block.
	Free Scope = 4
	// Cell is a local that is captured by an enclosed block as a Free
	// variable.
	Cell Scope = 5
)

// String returns the CPython enum name.
//
// CPython: Include/internal/pycore_symtable.h scope-tag macro names
func (s Scope) String() string {
	switch s {
	case Local:
		return "LOCAL"
	case GlobalExplicit:
		return "GLOBAL_EXPLICIT"
	case GlobalImplicit:
		return "GLOBAL_IMPLICIT"
	case Free:
		return "FREE"
	case Cell:
		return "CELL"
	}
	return "?"
}

// SymbolFlags packs CPython's per-name DEF_* / USE bits in the low 12
// bits and the resolved Scope in bits [12,16). The numeric layout is
// fixed by Include/internal/pycore_symtable.h: bit-for-bit
// compatibility lets a future bytecode marshaller round-trip flags
// against a CPython produced .pyc.
//
// CPython: Include/internal/pycore_symtable.h:L156
type SymbolFlags uint32

// DEF_* and USE bits.
//
// CPython: Include/internal/pycore_symtable.h:L158
const (
	// DefGlobal records a `global` statement covering this name.
	DefGlobal SymbolFlags = 1
	// DefLocal records an assignment to this name in the block.
	DefLocal SymbolFlags = 2
	// DefParam records this name as a function parameter.
	DefParam SymbolFlags = 4
	// DefNonlocal records a `nonlocal` statement covering this name.
	DefNonlocal SymbolFlags = 8
	// Use records that this name is read in the block.
	Use SymbolFlags = 0x10
	// DefFreeClass marks a free variable that aliases a class-scope
	// name; consulted to emit DEREF / CLASS_DEREF correctly.
	DefFreeClass SymbolFlags = 0x40
	// DefImport marks a binding produced by `import`.
	DefImport SymbolFlags = 0x80
	// DefAnnot marks that the name carries an annotation in this
	// block. Only used to flag class-level annotations into the
	// __annotations__ dict.
	DefAnnot SymbolFlags = 0x100
	// DefCompIter marks a comprehension iteration variable; used to
	// enforce the walrus-no-rebind rule.
	DefCompIter SymbolFlags = 0x200
	// DefTypeParam marks a PEP 695 type parameter.
	DefTypeParam SymbolFlags = 0x400
	// DefCompCell marks a cell variable in an inlined comprehension.
	DefCompCell SymbolFlags = 0x800
)

// DefBound is the CPython mask for "name is bound in this block".
//
// CPython: Include/internal/pycore_symtable.h:L170
const DefBound = DefLocal | DefParam | DefImport

// ScopeOffset is the bit position of the resolved Scope inside
// SymbolFlags. CPython packs the analyzer output into bits [12,16).
//
// CPython: Include/internal/pycore_symtable.h:L176
const ScopeOffset = 12

// ScopeMask carves the four-bit slot reserved for Scope.
//
// CPython: Include/internal/pycore_symtable.h:L177
const ScopeMask SymbolFlags = DefGlobal | DefLocal | DefParam | DefNonlocal

// Scope returns the resolved Scope packed into f, or zero if the
// analyze pass has not yet stamped a value.
//
// CPython: Include/internal/pycore_symtable.h SCOPE_MASK extract
func (f SymbolFlags) Scope() Scope {
	return Scope((f >> ScopeOffset) & ScopeMask)
}

// Defs returns just the DEF_* / USE bits, stripping the packed Scope.
//
// CPython: Include/internal/pycore_symtable.h DEF_BOUND mask
func (f SymbolFlags) Defs() SymbolFlags {
	return f & ((1 << ScopeOffset) - 1)
}
