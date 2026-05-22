package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// SyntaxError is the codegen-time error raised by visitor passes for
// program-level mistakes that CPython surfaces as PyExc_SyntaxError
// from inside compile.c. The VM's unwind layer lifts a *SyntaxError
// into a Python SyntaxError instance with the filename, line, and
// column populated from Pos.
//
// CPython: Python/compile.c:1191 _PyCompile_Error raises
// PyExc_SyntaxError via _PyErr_RaiseSyntaxError with the message and
// location captured by LOC(node).
type SyntaxError struct {
	Msg      string
	Filename string
	Pos      ast.Pos
}

// Error renders the message with a "SyntaxError:" prefix so that the
// VM's prefix-based unwind table promotes a bare *SyntaxError into a
// real Python SyntaxError, mirroring how symtable.SyntaxError surfaces.
func (e *SyntaxError) Error() string { return "SyntaxError: " + e.Msg }

// errorAt builds a *SyntaxError at the given AST position. The format
// string follows printf rules; %s replaces CPython's %U (PyUnicode)
// conversion since identifiers reach codegen as Go strings.
//
// CPython: Python/compile.c:1191 _PyCompile_Error
func (c *Compiler) errorAt(pos ast.Pos, format string, args ...any) error {
	return &SyntaxError{
		Msg:      fmt.Sprintf(format, args...),
		Filename: c.Filename,
		Pos:      pos,
	}
}
