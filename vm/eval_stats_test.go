// Gate for the per-opcode profile. CPython has no direct test for
// Py_STATS (the infrastructure is reflective; the build flag is what
// users gate on). gopy needs the gate because the counter sites are
// hot-path code and must stay zero-cost when statsTable is nil.

package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestEvalStatsDisabledByDefault locks the default-off contract.
// Bench harnesses that do not set GOPY_STATS must not pay the counter
// cost.
//
// CPython parity: Py_STATS undefined means INSTRUCTION_STATS compiles
// to ((void)0) (Python/ceval_macros.h:70).
func TestEvalStatsDisabledByDefault(t *testing.T) {
	if StatsEnabled() {
		t.Fatal("StatsEnabled true with no GOPY_STATS set: counter sites pay overhead by default")
	}
}

// TestEvalStatsCountsExecutedOpcodes runs a tiny program with stats
// force-enabled and asserts the counters reflect the actual dispatch
// trace. Mirrors what `python -X showrefcount` and the Py_STATS dump
// would produce, except gopy emits per-opcode counts via DumpStats.
func TestEvalStatsCountsExecutedOpcodes(t *testing.T) {
	EnableStatsForTest()
	defer DisableStatsForTest()
	ResetStats()

	// LOAD_CONST 0; RETURN_VALUE
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(42)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	if _, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict()); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}

	snap := SnapshotOpcodeStats()
	if snap[compile.LOAD_CONST].ExecutionCount == 0 {
		t.Fatalf("LOAD_CONST execution_count = 0, want > 0")
	}
	if snap[compile.RETURN_VALUE].ExecutionCount == 0 {
		t.Fatalf("RETURN_VALUE execution_count = 0, want > 0")
	}
	// pair_count[LOAD_CONST][RETURN_VALUE] should fire exactly once
	// since the program is LOAD_CONST then RETURN_VALUE.
	if got := snap[compile.LOAD_CONST].PairCount[compile.RETURN_VALUE]; got == 0 {
		t.Fatalf("pair_count[LOAD_CONST][RETURN_VALUE] = 0, want > 0")
	}
}

// TestDumpStatsFormatMatchesCPython locks the print format.
// CPython emits `opcode[NAME].execution_count : N` (one space around
// the colon). Any future format drift breaks downstream parsers that
// CPython users already run against Py_STATS output.
//
// CPython: Python/specialize.c:171 PRINT_STAT macro
func TestDumpStatsFormatMatchesCPython(t *testing.T) {
	EnableStatsForTest()
	defer DisableStatsForTest()
	ResetStats()

	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(1)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	if _, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict()); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}

	var buf bytes.Buffer
	DumpStats(&buf)
	out := buf.String()
	want := "opcode[LOAD_CONST].execution_count : "
	if !strings.Contains(out, want) {
		t.Fatalf("dump missing %q\n--- got ---\n%s", want, out)
	}
	wantPair := "opcode[LOAD_CONST].pair_count[RETURN_VALUE] : "
	if !strings.Contains(out, wantPair) {
		t.Fatalf("dump missing %q\n--- got ---\n%s", wantPair, out)
	}
}
