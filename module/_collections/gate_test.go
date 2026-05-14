// Gate tests for the _collections module: deque sequence ops + __reduce__,
// defaultdict __or__ / __ior__ / __reduce__.
//
// CPython: Modules/_collectionsmodule.c

package _collections_test

import (
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateDequeOps(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "deque_ops.py"))
}

func TestGateDefaultdictOps(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "defaultdict_ops.py"))
}
