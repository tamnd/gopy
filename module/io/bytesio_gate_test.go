// Gate tests for _io.BytesIO pinned against CPython 3.14.5.
// CPython: Modules/_io/bytesio.c
// Scripts live under gatedata/ and are embedded at build time.

package io_test

import (
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateBytesIOReadWrite(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "bytesio_readwrite.py"))
}

func TestGateBytesIOTruncate(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "bytesio_truncate.py"))
}

func TestGateBytesIOReadlineLimit(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "bytesio_readline_limit.py"))
}
