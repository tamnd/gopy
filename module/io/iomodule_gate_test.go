// Gate tests for the _io module factory functions: open(), open_code(),
// text_encoding(). CPython: Modules/_io/_iomodule.c lines 199..517.

package io_test

import (
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateIOOpen(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "open.bin")
	gate.Compare(t, cpy, gopy, loadScript(t, "iomodule_open.py"), path)
}
