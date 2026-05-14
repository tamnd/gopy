// Gate tests for _io.IncrementalNewlineDecoder pinned against
// CPython 3.14.5. CPython: Modules/_io/textio.c lines 246..635.

package io_test

import (
	"testing"

	"github.com/tamnd/gopy/test/gate"
)

func TestGateIncrementalNewlineDecoderUniversal(t *testing.T) {
	cpy := gate.FindCPython(t)
	if cpy == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopy := gate.BuildGopy(t)
	gate.Compare(t, cpy, gopy, loadScript(t, "inldecoder_universal.py"))
}
