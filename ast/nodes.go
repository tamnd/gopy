package ast

// Hand-written subset of the AST node types. The full set is generated
// from cpython/Parser/Python.asdl into nodes_gen.go; this file holds
// the foundation pieces that other packages (future, symtable,
// compile) consume before the generator lands.
//
// Each struct mirrors the asdl product/sum type 1:1, with a Pos
// trailer for the lineno/col_offset/end_lineno/end_col_offset that
// CPython carries on every node via SRC_LOCATION_FROM_AST.
//
// CPython: Parser/Python.asdl

// Pos is the source position quadruple. CPython uses
// _Py_SourceLocation; lineno=-1 marks "no position".
//
// CPython: Include/internal/pycore_location.h _Py_SourceLocation
type Pos struct {
	Lineno       int
	ColOffset    int
	EndLineno    int
	EndColOffset int
}

// NoPos is the sentinel CPython spells (_Py_SourceLocation){-1,-1,-1,-1}.
var NoPos = Pos{-1, -1, -1, -1}

// Mod is the asdl mod sum type. CPython: mod_ty.
type Mod interface {
	isMod()
}

// Module is `Module(stmt* body, type_ignore* type_ignores)`.
type Module struct {
	Body        Seq[Stmt]
	TypeIgnores Seq[*TypeIgnore]
}

func (*Module) isMod() {}

// Interactive is `Interactive(stmt* body)`.
type Interactive struct {
	Body Seq[Stmt]
}

func (*Interactive) isMod() {}

// Expression is `Expression(expr body)`.
type Expression struct {
	Body Expr
}

func (*Expression) isMod() {}

// FunctionType is `FunctionType(expr* argtypes, expr returns)`.
type FunctionType struct {
	ArgTypes Seq[Expr]
	Returns  Expr
}

func (*FunctionType) isMod() {}

// TypeIgnore is `TypeIgnore(int lineno, string tag)`.
type TypeIgnore struct {
	Lineno int
	Tag    string
}

// Stmt is the asdl stmt sum type. CPython: stmt_ty.
type Stmt interface {
	isStmt()
	Position() Pos
}

// Expr is the asdl expr sum type. CPython: expr_ty.
type Expr interface {
	isExpr()
	Position() Pos
}

// ImportFrom is `ImportFrom(identifier? module, alias* names, int? level)`.
type ImportFrom struct {
	Module string // "" means relative-only import
	Names  Seq[*Alias]
	Level  int
	Pos
}

func (*ImportFrom) isStmt() {}

// Position returns the source location quadruple.
func (s *ImportFrom) Position() Pos { return s.Pos }

// Alias is `alias(identifier name, identifier? asname)` plus its own
// position quadruple.
type Alias struct {
	Name   string
	AsName string
	Pos
}

// ExprStmt is `Expr(expr value)`. Used to wrap a top-level expression
// like a docstring constant.
type ExprStmt struct {
	Value Expr
	Pos
}

func (*ExprStmt) isStmt() {}

// Position returns the source location quadruple.
func (s *ExprStmt) Position() Pos { return s.Pos }

// Constant is `Constant(constant value, string? kind)`. The Value is
// any of the marshallable Python constants. Validation of Value is
// performed by ast.Validate.
type Constant struct {
	Value any
	Kind  string
	Pos
}

func (*Constant) isExpr() {}

// Position returns the source location quadruple.
func (e *Constant) Position() Pos { return e.Pos }

// IsDocString reports whether stmt is the bare-string-literal form
// recognized by _PyAST_GetDocString. Mirrors the C predicate: an
// ExprStmt whose value is a string-typed Constant.
//
// CPython: Python/ast.c _PyAST_GetDocString
func IsDocString(s Stmt) bool {
	es, ok := s.(*ExprStmt)
	if !ok {
		return false
	}
	c, ok := es.Value.(*Constant)
	if !ok {
		return false
	}
	_, ok = c.Value.(string)
	return ok
}
