// Package future ports cpython/Python/future.c. It walks a Module AST
// past its docstring and collects __future__ imports into a Features
// bitmask plus the location of the last __future__ statement.
//
// Spec: notes/Spec/1600/1620_gopy_compile_pipeline.md
package future

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// Features is the gopy equivalent of CPython's _PyFutureFeatures.
//
// CPython: Include/internal/pycore_symtable.h _PyFutureFeatures
type Features struct {
	Bits     uint32
	Location ast.Pos
}

// CO_FUTURE_* flag bits. Numeric values match
// cpython/Include/cpython/code.h byte-for-byte so that a code object
// produced by gopy carries flags interchangeable with CPython.
//
// CPython: Include/cpython/code.h:L135
const (
	Division        uint32 = 0x20000
	AbsoluteImport  uint32 = 0x40000
	WithStatement   uint32 = 0x80000
	PrintFunction   uint32 = 0x100000
	UnicodeLiterals uint32 = 0x200000
	BarryAsBdfl     uint32 = 0x400000
	GeneratorStop   uint32 = 0x800000
	Annotations     uint32 = 0x1000000
)

// Feature name strings recognized inside `from __future__ import X`.
//
// CPython: Include/cpython/compile.h:L37
const (
	nameNestedScopes    = "nested_scopes"
	nameGenerators      = "generators"
	nameDivision        = "division"
	nameAbsoluteImport  = "absolute_import"
	nameWithStatement   = "with_statement"
	namePrintFunction   = "print_function"
	nameUnicodeLiterals = "unicode_literals"
	nameBarryAsBdfl     = "barry_as_FLUFL"
	nameGeneratorStop   = "generator_stop"
	nameAnnotations     = "annotations"
)

// SyntaxError carries the same shape as the Python SyntaxError that
// CPython raises from this module: a message plus the source range.
//
// CPython: Python/future.c PyErr_RangedSyntaxLocationObject
type SyntaxError struct {
	Msg      string
	Filename string
	Pos      ast.Pos
}

func (e *SyntaxError) Error() string {
	return e.Msg
}

// FromAST walks mod and returns the collected __future__ feature set.
// Walks Module and Interactive bodies; any other top-level shape
// returns a zero Features cleanly. Mirrors _PyFuture_FromAST.
//
// CPython: Python/future.c:L109 _PyFuture_FromAST
func FromAST(mod ast.Mod, filename string) (*Features, error) {
	ff := &Features{Location: ast.NoPos}
	body, ok := moduleBody(mod)
	if !ok {
		return ff, nil
	}
	if err := parse(ff, body, filename); err != nil {
		return nil, err
	}
	return ff, nil
}

// moduleBody returns the body slice for Module/Interactive forms.
// For Expression and FunctionType (the eval/typing forms) the C code
// short-circuits: there can be no __future__ statement there.
func moduleBody(mod ast.Mod) (ast.Seq[ast.Stmt], bool) {
	switch m := mod.(type) {
	case *ast.Module:
		return m.Body, true
	case *ast.Interactive:
		return m.Body, true
	}
	return nil, false
}

// parse mirrors future_parse: skip the optional docstring then
// consume __future__ imports until any other statement.
//
// CPython: Python/future.c:L64 future_parse
func parse(ff *Features, body ast.Seq[ast.Stmt], filename string) error {
	n := body.Len()
	if n == 0 {
		return nil
	}
	i := 0
	if ast.IsDocString(body.Get(0)) {
		i = 1
	}
	for ; i < n; i++ {
		s := body.Get(i)
		imp, ok := s.(*ast.ImportFrom)
		if !ok {
			return nil
		}
		if imp.Level != 0 || imp.Module != "__future__" {
			return nil
		}
		if err := checkFeatures(ff, imp, filename); err != nil {
			return err
		}
		ff.Location = imp.Pos
	}
	return nil
}

// checkFeatures mirrors future_check_features: validate every alias
// name in `from __future__ import X, Y, Z`, OR-in the bitmask for
// the activating ones, and reject "braces" / unknown names.
//
// CPython: Python/future.c:L8 future_check_features
func checkFeatures(ff *Features, imp *ast.ImportFrom, filename string) error {
	for i := 0; i < imp.Names.Len(); i++ {
		name := imp.Names.Get(i)
		switch name.Name {
		case nameNestedScopes,
			nameGenerators,
			nameDivision,
			nameAbsoluteImport,
			nameWithStatement,
			namePrintFunction,
			nameUnicodeLiterals,
			nameGeneratorStop:
			// Recognized but no-op: the feature is unconditionally on.
		case nameBarryAsBdfl:
			ff.Bits |= BarryAsBdfl
		case nameAnnotations:
			ff.Bits |= Annotations
		case "braces":
			return &SyntaxError{
				Msg:      "not a chance",
				Filename: filename,
				Pos:      name.Pos,
			}
		default:
			return &SyntaxError{
				Msg:      fmt.Sprintf("future feature %.100s is not defined", name.Name),
				Filename: filename,
				Pos:      name.Pos,
			}
		}
	}
	return nil
}
