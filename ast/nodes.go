package ast

// Hand-written foundation pieces for the AST. The full set of node
// types is generated from cpython/Parser/Python.asdl into nodes_gen.go;
// this file holds only the shape that does not appear in the asdl
// (Pos and the docstring helper).
//
// CPython: Parser/Python.asdl + Python/ast.c

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
