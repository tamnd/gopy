package traceback_test

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/traceback"
)

func TestPushFormatOldestFirst(t *testing.T) {
	tb := traceback.New(traceback.Entry{File: "a.py", Line: 1, Name: "f"})
	tb = traceback.Push(tb, traceback.Entry{File: "b.py", Line: 2, Name: "g"})
	tb = traceback.Push(tb, traceback.Entry{File: "c.py", Line: 3, Name: "h"})
	out := traceback.Format(tb)
	idxA := strings.Index(out, "a.py")
	idxC := strings.Index(out, "c.py")
	if idxA < 0 || idxC < 0 || idxA >= idxC {
		t.Fatalf("oldest must come first; got:\n%s", out)
	}
}

func TestFormatExceptionEmptyMessage(t *testing.T) {
	out := traceback.FormatException(nil, "ValueError", "")
	if out != "ValueError\n" {
		t.Fatalf("got %q", out)
	}
}
