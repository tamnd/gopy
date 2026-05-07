package errors

import "github.com/tamnd/gopy/objects"

// SyntaxError carries the structured payload the parser raises:
// filename, lineno, offset, text, end_lineno, end_offset, plus the
// optional msg. CPython exposes these as named attributes; the
// Go-side struct keeps them next to the underlying Exception so a
// caller can inspect them without reaching into args[1].
//
// CPython: Objects/exceptions.c:2898 ComplexExtendsException SyntaxError
type SyntaxErrorInfo struct {
	Msg       objects.Object
	Filename  objects.Object
	Lineno    int
	Offset    int
	Text      objects.Object
	EndLineno int
	EndOffset int
}

// IndentationError fires for any block-structure mismatch (the parser
// raises it instead of SyntaxError when the offending token is INDENT
// or DEDENT). TabError is a stricter form that only fires when the
// mix is between tabs and spaces; CPython routes the -tt flag through
// it.
//
// CPython: Objects/exceptions.c:2906 MiddlingExtendsException IndentationError
// CPython: Objects/exceptions.c:2913 MiddlingExtendsException TabError
var (
	PyExc_SyntaxError          = newExcType("SyntaxError", []*objects.Type{PyExc_Exception})
	PyExc_IndentationError     = newExcType("IndentationError", []*objects.Type{PyExc_SyntaxError})
	PyExc_TabError             = newExcType("TabError", []*objects.Type{PyExc_IndentationError})
	PyExc_IncompleteInputError = newExcType("_IncompleteInputError", []*objects.Type{PyExc_SyntaxError})
)
