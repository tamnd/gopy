// Gate tests for _io.TextIOWrapper pinned against CPython 3.14.5.
// CPython: Modules/_io/textio.c
//
// textio is the largest and least-complete file in module/io: write
// works end-to-end but several read paths are still partial. The gates
// here cover what already lines up; expand them as the port advances.
// Scripts live under gatedata/ and are embedded at build time.

package io_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateTextIOWrapperWriteRead(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "tw.txt")
	gate.Compare(t, cpy, gopy, loadScript(t, "textiowrapper_write_read.py"), path)
}

func TestGateTextIOWrapperReadlines(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "twr.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gate.Compare(t, cpy, gopy, loadScript(t, "textiowrapper_readlines.py"), path)
}
