// Gate tests for _io.BufferedReader / BufferedWriter / BufferedRandom
// pinned against CPython 3.14.5.
// CPython: Modules/_io/bufferedio.c
// Scripts live under gatedata/ and are embedded at build time.

package io_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateBufferedReader(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "br.bin")
	if err := os.WriteFile(path, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gate.Compare(t, cpy, gopy, loadScript(t, "bufferedio_reader.py"), path)
}

func TestGateBufferedWriter(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "bw.bin")
	gate.Compare(t, cpy, gopy, loadScript(t, "bufferedio_writer.py"), path)
}

func TestGateBufferedRandom(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	path := filepath.Join(t.TempDir(), "brnd.bin")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gate.Compare(t, cpy, gopy, loadScript(t, "bufferedio_random.py"), path)
}
