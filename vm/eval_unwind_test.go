package vm

import (
	"testing"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	parsererrors "github.com/tamnd/gopy/parser/errors"
)

// TestSynthesizeStructuredSyntaxError pins that a *parsererrors.SyntaxError
// returned by the parser surfaces on the Python side as a SyntaxError
// instance whose msg / filename / lineno / offset / text / end_lineno /
// end_offset member descriptors are all populated. Without the
// errors.As branch in synthesizeException these attributes default to
// None because the prefix-table path only knows the message string.
//
// CPython: Parser/pegen_errors.c:317 _PyPegen_raise_error_known_location
// CPython: Objects/exceptions.c:2713 SyntaxError_init
func TestSynthesizeStructuredSyntaxError(t *testing.T) {
	se := &parsererrors.SyntaxError{
		Kind: parsererrors.KindSyntax,
		Pos: parsererrors.Pos{
			Lineno:  3,
			ColOff:  5,
			EndLine: 3,
			EndCol:  9,
		},
		Filename: "demo.py",
		Message:  "invalid syntax",
		Text:     "x = +",
	}
	exc := synthesizeException(se)
	if exc.ExcType != pyerrors.PyExc_SyntaxError {
		t.Fatalf("ExcType = %v, want SyntaxError", exc.ExcType.Name)
	}
	if exc.SyntaxErr == nil {
		t.Fatal("SyntaxErr state is nil; members would all read None")
	}
	want := map[string]any{
		"msg":        "invalid syntax",
		"filename":   "demo.py",
		"lineno":     int64(3),
		"offset":     int64(6),
		"text":       "x = +",
		"end_lineno": int64(3),
		"end_offset": int64(10),
	}
	checkSyntaxField(t, exc, "msg", want["msg"])
	checkSyntaxField(t, exc, "filename", want["filename"])
	checkSyntaxField(t, exc, "lineno", want["lineno"])
	checkSyntaxField(t, exc, "offset", want["offset"])
	checkSyntaxField(t, exc, "text", want["text"])
	checkSyntaxField(t, exc, "end_lineno", want["end_lineno"])
	checkSyntaxField(t, exc, "end_offset", want["end_offset"])
}

// TestSynthesizeStructuredIndentationError checks that the
// Kind field routes to IndentationError. TabError follows the same
// branch and is implicit in the switch.
func TestSynthesizeStructuredIndentationError(t *testing.T) {
	se := &parsererrors.SyntaxError{
		Kind:     parsererrors.KindIndentation,
		Pos:      parsererrors.Pos{Lineno: 1},
		Filename: "x.py",
		Message:  "unexpected indent",
	}
	exc := synthesizeException(se)
	if exc.ExcType != pyerrors.PyExc_IndentationError {
		t.Fatalf("ExcType = %v, want IndentationError", exc.ExcType.Name)
	}
}

func checkSyntaxField(t *testing.T, exc *pyerrors.Exception, name string, want any) {
	t.Helper()
	got, err := objects.GetAttr(exc, objects.NewStr(name))
	if err != nil {
		t.Fatalf("%s: getattr err = %v", name, err)
	}
	switch w := want.(type) {
	case string:
		s, err := objects.Str(got)
		if err != nil {
			t.Fatalf("%s: str() = %v", name, err)
		}
		if s != w {
			t.Errorf("%s = %q, want %q", name, s, w)
		}
	case int64:
		i, ok := got.(*objects.Int)
		if !ok {
			t.Fatalf("%s: got %T, want *objects.Int", name, got)
		}
		n, _ := i.Int64()
		if n != w {
			t.Errorf("%s = %d, want %d", name, n, w)
		}
	}
}
