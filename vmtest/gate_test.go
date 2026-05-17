// Cross-cut gate for the v0.6 VM. Rounds source through
// parser -> compile -> vm.Eval and pins the produced value (or the
// raised error) against a Go-side expectation.
//
// CPython parity comes online once the curated cpython_smoke panel
// (#168) wires up a side-by-side runner. Until then this gate is the
// shape-level check: each subtest exercises one bytecode shape that
// the hand-written panel has to handle.
//
// CPython: Lib/test/test_compile.py + Lib/test/test_grammar.py
// (subset chosen for v0.6 dispatch coverage)

package vmtest

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// runSource pipes src through the v0.6 stack: parse -> compile -> eval.
// Returns the result of the toplevel module (None on success, or the
// terminal RETURN_VALUE for an expression-mode source).
func runSource(t *testing.T, src string, mode parser.Mode) (objects.Object, error) {
	t.Helper()
	mod, err := parser.ParseString(src, "<gate>", mode)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, "<gate>", 0)
	if err != nil {
		return nil, err
	}
	co := liftCode(cco)
	ts := state.NewThread()
	g := objects.NewDict()
	return vm.EvalCode(ts, co, g, nil)
}

// liftCode adapts compile.Code (the v0.5 marshal-target shape) into
// objects.Code (the runtime shape the VM consumes). Both have the same
// information; the rename collapses once the objects.Code port replaces
// compile.Code (1687).
func liftCode(c *compile.Code) *objects.Code {
	return &objects.Code{
		Code:           c.Code,
		Consts:         c.Consts,
		Names:          c.Names,
		Varnames:       c.VarNames,
		Freevars:       c.FreeVars,
		Cellvars:       c.CellVars,
		Stacksize:      c.Stacksize,
		ExceptionTable: c.ExceptionTable,
	}
}

// shouldSkipUnimplemented returns true when err is one we expect during
// the rolling v0.6 panel buildout. The gate's purpose is to grow as
// arms land, so a t.Skip on ErrNotImplemented (or on a parser that is
// still missing rule bodies, since the PEG generator is a separate
// 1640-block) is the right shape.
func shouldSkipUnimplemented(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, vm.ErrNotImplemented) {
		return true
	}
	return strings.Contains(err.Error(), "generated rule bodies not yet emitted")
}

func TestGateEvalConstantExpression(t *testing.T) {
	v, err := runSource(t, "42", parser.ModeEval)
	if shouldSkipUnimplemented(err) {
		t.Skipf("dispatch panel incomplete: %v", err)
	}
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", v)
	}
	if n, _ := got.Int64(); n != 42 {
		t.Errorf("got %d, want 42", n)
	}
}

func TestGateEvalArithmetic(t *testing.T) {
	v, err := runSource(t, "1 + 2", parser.ModeEval)
	if shouldSkipUnimplemented(err) {
		t.Skipf("dispatch panel incomplete: %v", err)
	}
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", v)
	}
	if n, _ := got.Int64(); n != 3 {
		t.Errorf("got %d, want 3", n)
	}
}

func TestGateEvalIfElse(t *testing.T) {
	src := "1 if True else 2"
	v, err := runSource(t, src, parser.ModeEval)
	if shouldSkipUnimplemented(err) {
		t.Skipf("dispatch panel incomplete: %v", err)
	}
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", v)
	}
	if n, _ := got.Int64(); n != 1 {
		t.Errorf("got %d, want 1", n)
	}
}

func TestGateEvalListBuild(t *testing.T) {
	v, err := runSource(t, "[1, 2, 3]", parser.ModeEval)
	if shouldSkipUnimplemented(err) {
		t.Skipf("dispatch panel incomplete: %v", err)
	}
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	list, ok := v.(*objects.List)
	if !ok {
		t.Fatalf("got %T, want *objects.List", v)
	}
	if got := list.Len(); got != 3 {
		t.Fatalf("len = %d, want 3", got)
	}
	for i, want := range []int64{1, 2, 3} {
		iv, ok := list.Item(i).(*objects.Int)
		if !ok {
			t.Fatalf("list[%d]: got %T, want *objects.Int", i, list.Item(i))
		}
		got, _ := iv.Int64()
		if got != want {
			t.Errorf("list[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestGateEvalFString(t *testing.T) {
	v, err := runSource(t, `f"x={1+2}"`, parser.ModeEval)
	if shouldSkipUnimplemented(err) {
		t.Skipf("dispatch panel incomplete: %v", err)
	}
	if err != nil {
		t.Fatalf("Eval err: %v", err)
	}
	got, gerr := objects.Str(v)
	if gerr != nil {
		t.Fatalf("Str: %v", gerr)
	}
	if got != "x=3" {
		t.Errorf("got %q, want %q", got, "x=3")
	}
}
