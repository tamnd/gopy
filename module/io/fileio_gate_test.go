// Gate tests for _io.FileIO pinned against CPython 3.14.5.
// CPython: Modules/_io/fileio.c
// Scripts live under gatedata/ and are embedded at build time. Each
// FileIO script reads its target path from sys.argv[1] so the same
// Python source runs unchanged under CPython and gopy.

package io_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestGateFileIORead(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := writeTempFile(t, "src.bin", []byte("abcdef\nghi\n"))
	gate.Compare(t, cpy, gopy, loadScript(t, "fileio_read.py"), path)
}

func TestGateFileIOWriteThenRead(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "out.bin")
	gate.Compare(t, cpy, gopy, loadScript(t, "fileio_write_then_read.py"), path)
}

func TestGateFileIOSeekTell(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := writeTempFile(t, "seek.bin", []byte("0123456789"))
	gate.Compare(t, cpy, gopy, loadScript(t, "fileio_seek_tell.py"), path)
}
