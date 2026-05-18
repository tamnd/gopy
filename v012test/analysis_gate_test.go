// Analysis-pass gate. The forward pass currently lands two pieces:
// the orchestrator (Analyze, three-phase) and the cleanup pass
// (removeUnneededUops). Both run end-to-end inside Optimize, so the
// gate exercises them through the public install path. The
// per-opcode abstract interpretation table is gated on #430 and the
// dict / type watcher arm on #445; this gate is the floor those
// follow-ups raise.
package v012test

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/optimizer"
	"github.com/tamnd/gopy/state"
)

// TestAnalysisGateCleanupRunsInOptimize asserts that Optimize routes
// the trace through Analyze. The cleanup pass replaces the leading
// _SET_IP / _CHECK_VALIDITY pair with NOPs once it can prove no
// escape has happened since _START_EXECUTOR; prepareForExecution
// then drops the NOPs. A trace produced from the projector that
// emits both rows up front should come back without them.
func TestAnalysisGateCleanupRunsInOptimize(t *testing.T) {
	co := loopBytecode()
	interp := &state.Interpreter{JIT: true}

	exec, status := optimizer.Optimize(interp, nil, co, 0, 0, 0)
	if status != 1 {
		t.Fatalf("Optimize status = %d, want 1", status)
	}
	if exec == nil {
		t.Fatal("Optimize returned nil executor")
	}

	for i := 0; i < len(exec.Trace); i++ {
		op := exec.Trace[i].Opcode()
		if op == optimizer.UopNop {
			t.Errorf("trace[%d] = _NOP; cleanup pass should have compacted it out", i)
		}
		if op == optimizer.UopSetIp {
			// _SET_IP between _START_EXECUTOR and the first
			// escaping op should be NOP'd by removeUnneededUops
			// and then dropped by prepareForExecution.
			if i < findFirstEscape(exec.Trace) {
				t.Errorf("trace[%d] = _SET_IP before any escape; cleanup pass missed it", i)
			}
		}
	}
}

// TestAnalysisGateBenignBailFromOptimizeUops covers the
// out-of-space short-circuit: a code object with a stack size that
// blows past the abstract-interp limit makes optimizeUops return 0,
// which bubbles up as a benign bail from Optimize. The projector
// itself succeeds so we know the bail came from analysis.
func TestAnalysisGateBenignBailFromOptimizeUops(t *testing.T) {
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			byte(compile.CACHE), 0,
		},
		Stacksize: 1 << 20,
		Consts:    []any{nil},
	}
	interp := &state.Interpreter{JIT: true}

	exec, status := optimizer.Optimize(interp, nil, co, 0, 0, 0)
	if status != 0 {
		t.Errorf("Optimize status = %d, want 0 (benign bail)", status)
	}
	if exec != nil {
		t.Errorf("Optimize returned non-nil executor on bail: %v", exec)
	}
	if op := compile.Opcode(co.Code[0]); op == compile.ENTER_EXECUTOR {
		t.Error("install site flipped to ENTER_EXECUTOR despite analysis bail")
	}
}

// findFirstEscape returns the index of the first uop in the trace
// whose metadata flags include FlagEscapes. Returns len(trace) if
// no escape exists, which makes the SET_IP-before-escape check
// vacuous (the test does not enforce on traces with no escapes).
func findFirstEscape(trace []optimizer.UOPInstruction) int {
	for i := 0; i < len(trace); i++ {
		name := optimizer.UopNames[trace[i].Opcode()]
		if name == "" {
			continue
		}
		if optimizer.UopMeta[name].Flags&optimizer.FlagEscapes != 0 {
			return i
		}
	}
	return len(trace)
}
