package errors

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// SyntaxFromCompile builds a populated SyntaxError instance from the
// codegen-level structured record. Mirrors SyntaxFromSymtable: CPython
// raises codegen errors through _PyErr_RaiseSyntaxError which sets
// filename / lineno / offset on the SyntaxError instance; we pass the
// 2-arg (msg, info) form so member descriptors expose .filename,
// .lineno, .offset to user code.
//
// CPython: Python/compile.c:1191 _PyCompile_Error
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func SyntaxFromCompile(se *compile.SyntaxError) *Exception {
	filename := objects.None()
	if se.Filename != "" {
		filename = objects.NewStr(se.Filename)
	}
	lineno := objects.None()
	if se.Pos.Lineno > 0 {
		lineno = objects.NewInt(int64(se.Pos.Lineno))
	}
	// ast Pos.ColOffset is 0-indexed; SyntaxError.offset is 1-indexed.
	// CPython: Python/compile.c:1200 _PyErr_RaiseSyntaxError passes
	// loc.col_offset + 1.
	offset := objects.None()
	if se.Pos.ColOffset >= 0 {
		offset = objects.NewInt(int64(se.Pos.ColOffset + 1))
	}
	endLineno := objects.None()
	if se.Pos.EndLineno > 0 {
		endLineno = objects.NewInt(int64(se.Pos.EndLineno))
	}
	endOffset := objects.None()
	if se.Pos.EndColOffset >= 0 {
		endOffset = objects.NewInt(int64(se.Pos.EndColOffset + 1))
	}
	info := objects.NewTuple([]objects.Object{
		filename, lineno, offset, objects.None(), endLineno, endOffset,
	})
	args := []objects.Object{objects.NewStr(se.Msg), info}
	out, err := PyExc_SyntaxError.Call(PyExc_SyntaxError, args, nil)
	if err != nil {
		return New(PyExc_SyntaxError, objects.NewTuple([]objects.Object{
			objects.NewStr(se.Msg),
		}))
	}
	if exc, ok := out.(*Exception); ok {
		return exc
	}
	return New(PyExc_SyntaxError, objects.NewTuple([]objects.Object{
		objects.NewStr(se.Msg),
	}))
}
