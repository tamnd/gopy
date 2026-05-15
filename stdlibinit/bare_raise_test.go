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

// TestBareRaiseReraisesHandled exercises RAISE_VARARGS oparg=0. A bare
// `raise` inside an except clause must re-raise the currently-handled
// exception, which PUSH_EXC_INFO parked in tstate->exc_info on entry to
// the handler. Before the fix the VM unconditionally returned
// "No active exception to re-raise" because case 0 ignored the
// thread-state slot.
//
// CPython: Python/bytecodes.c:1648 RAISE_VARARGS oparg==0
func TestBareRaiseReraisesHandled(t *testing.T) {
	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := `
try:
    try:
        raise ValueError("boom")
    except ValueError:
        raise
except ValueError as e:
    print("caught:", type(e).__name__, str(e))
`
	if _, err := pythonrun.RunString(ts, src, "<bare-raise>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "caught: ValueError boom"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
