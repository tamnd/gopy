// CPython: Parser/pegen_errors.c _PyPegen_raise_error /
// _PyPegen_raise_error_known_location. The C code threads
// PyObject* exception types through; gopy lifts them onto a Kind
// enum and a *SyntaxError value the caller can inspect.

package errors

import "fmt"

// Kind names the SyntaxError subclass the parser would have raised
// in CPython. The compile pipeline lifts this onto the matching
// Go error type.
//
// CPython: Parser/pegen_errors.c errtype argument family
type Kind int

// Kind values. Default is plain SyntaxError; the indent / tab
// variants line up with IndentationError and TabError.
const (
	KindSyntax Kind = iota
	KindIndentation
	KindTab
	KindMemory
	KindOverflow
	KindUnicode
	KindValue
)

// Pos pairs a 1-based line with a 0-based byte column. Negative
// EndCol means "end of line", matching CPython's -1 sentinel.
//
// CPython: Parser/pegen.h Token col_offset / end_col_offset
type Pos struct {
	Lineno  int
	ColOff  int
	EndLine int
	EndCol  int
}

// SyntaxError is the structured record the parser hands back. The
// pegen runtime promotes the first one set on a Parser into the
// caller's *SyntaxError.
//
// CPython: Include/cpython/pyerrors.h PySyntaxErrorObject
type SyntaxError struct {
	Kind     Kind
	Pos      Pos
	Filename string
	Message  string
	Text     string
}

// Error renders the bare message. The "File ..., line N" envelope
// is added by the caller because gopy keeps filename and line out
// of the message body.
func (e *SyntaxError) Error() string { return e.Message }

// Build is the worker behind every raise_*. It formats msg with
// args, stamps the location and filename, and returns the record.
//
// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
func Build(kind Kind, filename string, pos Pos, msg string, args ...any) *SyntaxError {
	return &SyntaxError{
		Kind:     kind,
		Pos:      pos,
		Filename: filename,
		Message:  fmt.Sprintf(msg, args...),
	}
}

// Raise builds a plain SyntaxError at pos.
//
// CPython: Parser/pegen_errors.c:228 _PyPegen_raise_error
func Raise(filename string, pos Pos, msg string, args ...any) *SyntaxError {
	return Build(KindSyntax, filename, pos, msg, args...)
}

// RaiseIndent builds an IndentationError.
//
// CPython: Parser/pegen.h RAISE_INDENTATION_ERROR
func RaiseIndent(filename string, pos Pos, msg string, args ...any) *SyntaxError {
	return Build(KindIndentation, filename, pos, msg, args...)
}

// RaiseTab builds a TabError.
//
// CPython: Parser/pegen_errors.c:103 E_TABSPACE arm
func RaiseTab(filename string, pos Pos, msg string, args ...any) *SyntaxError {
	return Build(KindTab, filename, pos, msg, args...)
}

// RaiseRange is the explicit-span builder used when the second-pass
// invalid grammar pinpoints a token range.
//
// CPython: Parser/pegen.h RAISE_SYNTAX_ERROR_KNOWN_RANGE
func RaiseRange(filename string, start, end Pos, msg string, args ...any) *SyntaxError {
	pos := Pos{
		Lineno:  start.Lineno,
		ColOff:  start.ColOff,
		EndLine: end.EndLine,
		EndCol:  end.EndCol,
	}
	return Build(KindSyntax, filename, pos, msg, args...)
}
