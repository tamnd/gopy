package errors

import (
	"io"
	"strings"

	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/traceback"
)

// excDisplayMessage routes through tp_str so KeyError and other types
// that override BaseException.__str__ format their args correctly.
//
// CPython: Objects/exceptions.c:L226 BaseException_str dispatch
func excDisplayMessage(exc *Exception) string {
	if exc.ExcType != nil && exc.ExcType.Str != nil {
		s, err := exc.ExcType.Str(exc)
		if err == nil {
			return s
		}
	}
	return exc.Message()
}

// FormatException returns the multi-line rendering of exc, including
// any chained __cause__ / __context__ exceptions. Mirrors
// `traceback.format_exception`.
//
// CPython: Python/traceback.c:L1129 _PyErr_Display
func FormatException(exc *Exception) string {
	if exc == nil {
		return ""
	}
	var b strings.Builder
	writeChain(&b, exc)
	return b.String()
}

func writeChain(b *strings.Builder, exc *Exception) {
	switch {
	case exc.Cause != nil:
		writeChain(b, exc.Cause)
		b.WriteString("\nThe above exception was the direct cause of the following exception:\n\n")
	case exc.Context != nil && !exc.Suppress:
		writeChain(b, exc.Context)
		b.WriteString("\nDuring handling of the above exception, another exception occurred:\n\n")
	}
	b.WriteString(traceback.FormatException(exc.TB, exc.TypeName(), excDisplayMessage(exc)))
}

// Print writes FormatException of the current exception to w and
// clears the slot. Mirrors PyErr_Print.
//
// CPython: Python/pythonrun.c:L656 PyErr_Print
func Print(ts *state.Thread, w io.Writer) {
	exc := Occurred(ts)
	if exc == nil {
		return
	}
	_, _ = io.WriteString(w, FormatException(exc))
	Clear(ts)
}
