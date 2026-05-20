// Verifies the Tier-2 log shape stays byte-stable with the
// CPython peer in Python/gopy_tier2_trace.c so the two streams can
// be diffed line-by-line during body-translator work.

package optimizer

import (
	"bytes"
	"strings"
	"testing"
)

func TestTier2LogShapeMatchesCPython(t *testing.T) {
	var buf bytes.Buffer
	tier2LogOnce.Do(func() {})
	tier2LogSink = &buf
	t.Cleanup(func() { tier2LogSink = nil })

	tier2Log("project_add", "idx=%d uop=%s oparg=%d operand0=%d target=%d",
		3, "_LOAD_FAST", 1, uint64(0), uint32(7))
	tier2Log("dispatch", "idx=%d uop=%s oparg=%d target=%d operand0=%d",
		0, "_START_EXECUTOR", 0, uint32(0), uint64(0))
	tier2Log("exit", "kind=%s idx=%d uop=%s", "exit", 4, "_EXIT_TRACE")

	got := buf.String()
	wantPrefixes := []string{
		"T2 project_add idx=3 uop=_LOAD_FAST oparg=1 operand0=0 target=7\n",
		"T2 dispatch idx=0 uop=_START_EXECUTOR oparg=0 target=0 operand0=0\n",
		"T2 exit kind=exit idx=4 uop=_EXIT_TRACE\n",
	}
	for _, want := range wantPrefixes {
		if !strings.Contains(got, want) {
			t.Errorf("missing line\n  want: %q\n  got:  %q", want, got)
		}
	}
}

func TestTier2LogDisabledByDefault(t *testing.T) {
	saved := tier2LogSink
	tier2LogSink = nil
	t.Cleanup(func() { tier2LogSink = saved })

	if tier2LogEnabled() {
		t.Fatalf("tier2LogEnabled() = true with sink=nil, want false")
	}
	tier2Log("dispatch", "idx=%d", 0)
}
