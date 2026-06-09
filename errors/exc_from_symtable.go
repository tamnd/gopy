package errors

import (
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/symtable"
)

// SyntaxFromSymtable builds a populated SyntaxError instance from the
// symtable-level structured record. CPython raises symtable errors
// through PyErr_RangedSyntaxLocationObject which sets filename / lineno
// / offset on the SyntaxError instance; mirror that by passing the
// 2-arg (msg, info) form so the member descriptors expose .filename,
// .lineno, .offset to Python user code (test_scope.check_syntax_error
// reads .lineno / .offset).
//
// CPython: Python/symtable.c PyErr_RangedSyntaxLocationObject
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func SyntaxFromSymtable(se *symtable.SyntaxError) *Exception {
	filename := objects.None()
	if se.Filename != "" {
		filename = objects.NewStr(se.Filename)
	}
	lineno := objects.None()
	if se.Pos.Lineno > 0 {
		lineno = objects.NewInt(int64(se.Pos.Lineno))
	}
	// ast Pos.ColOffset is 0-indexed; SyntaxError.offset is 1-indexed.
	//
	// CPython: Python/symtable.c symtable_error_set passes col_offset+1
	// to PyErr_RangedSyntaxLocationObject.
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
	out, err := objects.Call(PyExc_SyntaxError, objects.NewTuple(args), nil)
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
