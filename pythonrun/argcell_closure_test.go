// Regression test for the arg-cell + free-var layout drift that
// blocked the test_tokenize panel gate. functools.singledispatch's
// register builds a code object where the first param ("cls") is
// also a cellvar (closed over by an inner lambda), and the function
// has multiple free vars on top. fix_cell_offsets merges the
// arg-cell into the local slot, so co_nlocalsplus drops below
// len(varnames)+len(cellvars)+len(freevars). The frame layout must
// follow the compacted size or LOAD_DEREF lands on the un-populated
// arg-cell.
//
// CPython: Python/flowgraph.c:3711 build_cellfixedoffsets
// CPython: Python/flowgraph.c:3843 fix_cell_offsets
// CPython: Objects/codeobject.c:389 get_localsplus_counts

package pythonrun

import (
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
)

// TestArgCellWithFreeVarsExecutes mirrors the
// singledispatch.<locals>.register shape: the outer scope captures
// several names (free vars from register's POV), and register itself
// takes an arg that an inner lambda closes over (cls becomes an
// arg-cell with kind CO_FAST_LOCAL|CO_FAST_CELL). Pre-fix, calling
// register would surface
// `LOAD_DEREF: <unknown> slot N not a cell ... got <nil>` because
// the frame allocated one extra slot for the duplicated cls cellvar.
func TestArgCellWithFreeVarsExecutes(t *testing.T) {
	src := `
def outer():
    a = 1
    b = 2
    c = 3
    d = 4
    e = 5
    def register(cls, func=None):
        if func is None:
            return lambda f: register(cls, f)
        return (cls, func, a, b, c, d, e)
    return register

reg = outer()
result = reg("CLS", "FUNC")
`
	ts := state.NewThread()
	globals := objects.NewDict()
	if _, err := RunString(ts, src, "<argcell>", parser.ModeFile, globals, nil); err != nil {
		t.Fatalf("RunString: %v", err)
	}
	v, err := globals.GetItem(objects.NewStr("result"))
	if err != nil || v == nil {
		t.Fatalf("result not bound: %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("result type = %T, want *objects.Tuple", v)
	}
	if tup.Len() != 7 {
		t.Fatalf("result len = %d, want 7", tup.Len())
	}
	for i, want := range []int64{0, 0, 1, 2, 3, 4, 5} {
		if i < 2 {
			// slots 0/1 are the cls / func args, just spot-check non-nil
			if tup.Item(i) == nil {
				t.Errorf("result[%d] is nil", i)
			}
			continue
		}
		n, ok := tup.Item(i).(*objects.Int)
		if !ok {
			t.Fatalf("result[%d] = %T, want *Int", i, tup.Item(i))
		}
		got, ok := n.Int64()
		if !ok {
			t.Errorf("result[%d] not an int64", i)
			continue
		}
		if got != want {
			t.Errorf("result[%d] = %d, want %d", i, got, want)
		}
	}
}

// TestArgCellRecursiveLambdaCalls hits the lambda branch directly:
// register(cls) with no func returns a lambda that closes over
// register itself plus the arg-cell cls. Invoking the lambda must
// re-enter register cleanly.
func TestArgCellRecursiveLambdaCalls(t *testing.T) {
	src := `
def make():
    cache = []
    def register(cls, func=None):
        if func is None:
            return lambda f: register(cls, f)
        cache.append((cls, func))
        return func
    return register, cache

reg, cache = make()
reg("CLS")("FUNC")
result = cache[0]
`
	ts := state.NewThread()
	globals := objects.NewDict()
	if _, err := RunString(ts, src, "<argcell-lambda>", parser.ModeFile, globals, nil); err != nil {
		t.Fatalf("RunString: %v", err)
	}
	v, err := globals.GetItem(objects.NewStr("result"))
	if err != nil || v == nil {
		t.Fatalf("result not bound: %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("result type = %T, want *objects.Tuple", v)
	}
	if tup.Len() != 2 {
		t.Fatalf("result len = %d, want 2", tup.Len())
	}
}
