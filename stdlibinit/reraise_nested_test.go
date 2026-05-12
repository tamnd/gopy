package stdlibinit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"

	_ "github.com/tamnd/gopy/stdlibinit"
)

// runReraiseScript boots gopy and runs src. The captured stdout is
// the gate output.
func runReraiseScript(t *testing.T, src, want string) {
	t.Helper()
	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	if _, err := pythonrun.RunString(ts, src, "<reraise>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// gate F.2a: when a nested `try` raises an exception whose type the
// inner `except` doesn't handle, the outer handler must still see the
// real exception type. Before the RERAISE fix, the vm flattened the
// reraised exception into a plain Exception via Errorf("%s",
// repr(exc)), so the outer `except KeyError` never matched. enum's
// _proto_member.__set_name__ hits this exact pattern: the inner
// `except TypeError` doesn't catch the KeyError from
// `enum_class._value2member_map_[value]`, so propagation past the inner
// handler must preserve the KeyError type.
//
// CPython: Python/bytecodes.c:1429 RERAISE
func TestReraiseNestedPreservesType(t *testing.T) {
	runReraiseScript(t, `
try:
    try:
        raise KeyError("strict")
    except TypeError:
        print("inner type")
except KeyError as e:
    print("outer caught:", type(e).__name__, repr(e))
print("done")
`, "outer caught: KeyError KeyError('strict')\ndone")
}

// gate F.2b: same pattern, but the inner block raises through a dict
// subscript (the failure path inside enum.py:287).
func TestReraiseNestedDictSubscript(t *testing.T) {
	runReraiseScript(t, `
d = {}
try:
    try:
        x = d["missing"]
    except TypeError:
        print("inner type")
except KeyError as e:
    print("outer caught:", type(e).__name__)
print("done")
`, "outer caught: KeyError\ndone")
}
