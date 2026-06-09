package errors

import (
	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/objects"
	parsererrors "github.com/tamnd/gopy/parser/errors"
)

// SyntaxFromParser builds a populated SyntaxError instance from the
// parser-level structured record. The exception is constructed with
// the canonical 2-arg signature SyntaxError(msg, (filename, lineno,
// offset, text, end_lineno, end_offset)) so the member descriptors
// registered in exc_syntax.go expose the location to Python user code.
//
// IndentationError / TabError get the same shape because the
// SyntaxError_init member layout is shared across the subclass chain.
//
// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func SyntaxFromParser(se *parsererrors.SyntaxError) *Exception {
	typ := PyExc_SyntaxError
	switch se.Kind {
	case parsererrors.KindIndentation:
		typ = PyExc_IndentationError
	case parsererrors.KindTab:
		typ = PyExc_TabError
	case parsererrors.KindIncompleteInput:
		// CPython: Objects/exceptions.c _IncompleteInputError (SyntaxError subclass)
		typ = PyExc_IncompleteInputError
	}
	filename := objects.None()
	if se.Filename != "" {
		filename = objects.NewStr(se.Filename)
	}
	text := objects.None()
	if se.Text != "" {
		// CPython stores SyntaxError.text as a proper str. When the
		// offending source line has non-utf-8 bytes the tokenizer
		// captures them raw; CPython's PySys_FormatStderr and the
		// constructor paths route the bytes through
		// PyUnicode_DecodeUTF8 with errors='replace' so each invalid
		// byte lands as U+FFFD.
		//
		// CPython: Objects/unicodeobject.c:4756 PyUnicode_DecodeUTF8Stateful
		decoded, _, derr := codecs.Decode([]byte(se.Text), "utf-8", "replace")
		if derr == nil {
			text = objects.NewStr(decoded)
		} else {
			text = objects.NewStr(se.Text)
		}
	}
	// SyntaxError.offset and .end_offset are 1-indexed (CPython:
	// pegen_errors.c:258 col_offset + 1). ColOff is 0-based (same as
	// ast.col_offset), so we add 1 and allow column 0 (col_offset=-1 is
	// the sentinel for "unknown" in CPython; -1 means truly absent).
	offset := objects.None()
	if se.Pos.ColOff >= 0 {
		offset = objects.NewInt(int64(se.Pos.ColOff + 1))
	}
	endLineno := objects.None()
	if se.Pos.EndLine > 0 {
		endLineno = objects.NewInt(int64(se.Pos.EndLine))
	}
	endOffset := objects.None()
	if se.Pos.EndCol >= 0 {
		endOffset = objects.NewInt(int64(se.Pos.EndCol + 1))
	}
	lineno := objects.None()
	if se.Pos.Lineno > 0 {
		lineno = objects.NewInt(int64(se.Pos.Lineno))
	}
	info := objects.NewTuple([]objects.Object{
		filename, lineno, offset, text, endLineno, endOffset,
	})
	args := []objects.Object{objects.NewStr(se.Message), info}
	out, err := objects.Call(typ, objects.NewTuple(args), nil)
	if err != nil {
		return New(typ, objects.NewTuple([]objects.Object{
			objects.NewStr(se.Message),
		}))
	}
	exc, _ := out.(*Exception)
	if exc == nil {
		return New(typ, objects.NewTuple([]objects.Object{
			objects.NewStr(se.Message),
		}))
	}
	// Populate _metadata so traceback.py's _find_keyword_typos can suggest
	// "Did you mean X?" corrections for keyword-like names in SyntaxErrors.
	// Format matches CPython's Py_BuildValue("(iiN)", ...) call.
	//
	// CPython: Python/errors.c:902 _PyErr_SetSyntaxErrorMetadata
	// CPython: Lib/traceback.py:793 _find_keyword_typos
	if se.SourceText != "" && exc.SyntaxErr != nil {
		exc.SyntaxErr.Metadata = objects.NewTuple([]objects.Object{
			objects.NewInt(int64(se.LastStmtLineno)),
			objects.NewInt(int64(se.LastStmtColOff)),
			objects.NewStr(se.SourceText),
		})
	}
	return exc
}
