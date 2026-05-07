// End-to-end coverage for __slots__: a `class C: __slots__ = (...)`
// source compiles, runs through __build_class__, and produces a class
// whose instances reject non-slot attribute writes.

package vm

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
)

func TestSlotsAssignThroughSource(t *testing.T) {
	src := "class C:\n" +
		"  __slots__ = ('x', 'y')\n" +
		"c = C()\n" +
		"c.x = 7\n" +
		"c.y = 9\n" +
		"r = c.x + c.y\n"
	g := runClassSrc(t, src)
	r := lookupName(t, g, "r")
	n, _ := r.(*objects.Int).Int64()
	if n != 16 {
		t.Fatalf("r = %d, want 16", n)
	}
}

func TestSlotsRejectNonSlotAttributeThroughSource(t *testing.T) {
	src := "class C:\n" +
		"  __slots__ = ('x',)\n" +
		"c = C()\n" +
		"c.y = 1\n"
	err := runClassSrcExpectErr(t, src)
	if err == nil || !strings.Contains(err.Error(), "AttributeError") {
		t.Fatalf("err = %v, want AttributeError", err)
	}
}

func TestSlotsExplicitDictThroughSource(t *testing.T) {
	src := "class C:\n" +
		"  __slots__ = ('x', '__dict__')\n" +
		"c = C()\n" +
		"c.x = 1\n" +
		"c.y = 2\n" +
		"r = c.x + c.y\n"
	g := runClassSrc(t, src)
	r := lookupName(t, g, "r")
	n, _ := r.(*objects.Int).Int64()
	if n != 3 {
		t.Fatalf("r = %d, want 3", n)
	}
}

// runClassSrcExpectErr runs the same pipeline as runClassSrc but
// returns the eval error instead of failing the test, so the caller
// can assert on it.
func runClassSrcExpectErr(t *testing.T, src string) error {
	t.Helper()
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	if src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, "<slots_test>", parser.ModeFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cco, err := compile.Compile(mod, "<slots_test>", 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	co := liftCompileCode(cco)
	ts := state.NewThread()
	_, runErr := EvalCode(ts, co, g, nil)
	return runErr
}
