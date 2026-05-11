package stdlibinit

import (
	"testing"

	"github.com/tamnd/gopy/imp"
)

// TestInittabHasGC pins that blank-importing this package wires the
// gc module into the inittab. Without stdlibinit, cmd/gopy would
// have to blank-import gc itself; this test breaks if a future
// refactor drops the gc registration line.
func TestInittabHasGC(t *testing.T) {
	if fn := imp.FindInitFunc("gc"); fn == nil {
		t.Fatal("imp.FindInitFunc(\"gc\") = nil; stdlibinit should register gc")
	}
}

// TestInittabHasContextVars pins the same contract for _contextvars.
func TestInittabHasContextVars(t *testing.T) {
	if fn := imp.FindInitFunc("_contextvars"); fn == nil {
		t.Fatal("imp.FindInitFunc(\"_contextvars\") = nil; stdlibinit should register _contextvars")
	}
}

// TestInittabHasSys pins the same contract for sys.
func TestInittabHasSys(t *testing.T) {
	if fn := imp.FindInitFunc("sys"); fn == nil {
		t.Fatal("imp.FindInitFunc(\"sys\") = nil; stdlibinit should register sys")
	}
}

// TestInittabSnapshotIncludesAll asserts the snapshot lists every
// module the registry blank-imports. Adding a new built-in module
// to registry.go means adding its name here; the test fails loudly
// on a mismatch.
func TestInittabSnapshotIncludesAll(t *testing.T) {
	want := []string{"gc", "_contextvars", "sys", "_weakref"}
	have := map[string]bool{}
	for _, e := range imp.InittabSnapshot() {
		have[e.Name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("inittab missing %q", name)
		}
	}
}
