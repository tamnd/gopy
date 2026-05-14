// Gate tests for _io.StringIO pinned against CPython 3.14.5.
// CPython: Modules/_io/stringio.c
// Scripts live under gatedata/ and are embedded at build time.

package io_test

import (
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateStringIOReadWrite(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "stringio_readwrite.py"))
}

func TestGateStringIOReadline(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "stringio_readline.py"))
}

func TestGateStringIOClosedRaises(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "stringio_closed_raises.py"))
}
