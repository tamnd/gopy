// Gate tests for the _io._IOBase / _io._RawIOBase contract, pinned by
// running the same Python script through both CPython 3.14 and the
// gopy binary and asserting byte-equal output. Co-located with the
// iobase port so future changes land alongside the gate that protects
// them. Scripts live under gatedata/ and are embedded at build time.
//
// The scripts run against BytesIO (a concrete _IOBase subclass that
// gopy fully implements) rather than bare _io._IOBase. CPython lets
// Python code subclass _IOBase directly and inherit readline /
// readlines / iter through the type method table; gopy's IOBase
// methods are bound by a custom getattro that does not yet flow
// through user subclasses, so a `class Src(_io._RawIOBase): ...`
// gate would catch a runtime-wide gap, not an iobase port gap. The
// in-process Go tests in iobase_test.go cover those abstract-base
// behaviors directly.

package io_test

import (
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateBytesIOClosedRaises(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "iobase_closed_raises.py"))
}

func TestGateBytesIOReadline(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "iobase_readline.py"))
}

func TestGateBytesIOIter(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "iobase_iter.py"))
}
