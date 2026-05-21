// Per-opcode profile, gated by GOPY_STATS env. Mirrors CPython's
// Py_STATS build flag, but env-driven so the default-off case stays
// zero-cost.
//
// CPython: Include/cpython/pystats.h:60 OpcodeStats + Python/specialize.c:144
// print_spec_stats. The CPython infrastructure is a build-time flag
// (Py_STATS); gopy uses an env var because Go has no equivalent of
// preprocessor-driven dead-code elimination.

package vm

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/tamnd/gopy/compile"
)

// OpcodeStats mirrors CPython OpcodeStats minus SpecializationStats
// (the specializer package tracks those separately).
//
// CPython: Include/cpython/pystats.h:60
type OpcodeStats struct {
	ExecutionCount uint64
	PairCount      [256]uint64
}

// statsTable is the gopy-global opcode profile. Allocated only when
// GOPY_STATS=1 was set before InitStatsFromEnv ran.
//
// CPython: Include/cpython/pystats.h:182 PyAPI_DATA(PyStats*) _Py_stats.
var (
	statsTable   *[256]OpcodeStats
	statsTableMu sync.Mutex
)

// InitStatsFromEnv enables the profile when GOPY_STATS=1.
// Safe to call multiple times; second call is a no-op.
func InitStatsFromEnv() {
	if os.Getenv("GOPY_STATS") != "1" {
		return
	}
	statsTableMu.Lock()
	defer statsTableMu.Unlock()
	if statsTable == nil {
		statsTable = new([256]OpcodeStats)
	}
}

// init mirrors CPython where Py_STATS is set by the build, not by
// runtime configuration. Reading the env at package-init time avoids
// having to wire vm.InitStatsFromEnv through lifecycle.initInterpMain
// (which would create a vm <- lifecycle import cycle).
func init() {
	InitStatsFromEnv()
}

// StatsEnabled reports whether the per-opcode profile is collecting.
func StatsEnabled() bool { return statsTable != nil }

// ResetStats zeroes the per-opcode counters. Used by tests.
func ResetStats() {
	statsTableMu.Lock()
	defer statsTableMu.Unlock()
	if statsTable != nil {
		*statsTable = [256]OpcodeStats{}
	}
}

// EnableStatsForTest force-enables the table outside the env path.
// Tests call this; the regular runtime hits InitStatsFromEnv only.
func EnableStatsForTest() {
	statsTableMu.Lock()
	defer statsTableMu.Unlock()
	if statsTable == nil {
		statsTable = new([256]OpcodeStats)
	}
}

// DisableStatsForTest is the matching teardown for EnableStatsForTest.
func DisableStatsForTest() {
	statsTableMu.Lock()
	defer statsTableMu.Unlock()
	statsTable = nil
}

// SnapshotOpcodeStats returns a copy of the table for testing. Cheap
// because OpcodeStats is plain data; the copy avoids the test
// inspecting the live counters that dispatch is updating.
func SnapshotOpcodeStats() [256]OpcodeStats {
	statsTableMu.Lock()
	defer statsTableMu.Unlock()
	if statsTable == nil {
		return [256]OpcodeStats{}
	}
	return *statsTable
}

// DumpStats writes the profile in the same key=value shape CPython
// emits from print_spec_stats. Zero-count opcodes are skipped.
//
// CPython: Python/specialize.c:144 print_spec_stats
func DumpStats(w io.Writer) {
	if statsTable == nil {
		fmt.Fprintln(w, "# GOPY_STATS not enabled at process start")
		return
	}
	for op := 0; op < 256; op++ {
		if statsTable[op].ExecutionCount == 0 {
			continue
		}
		name := opcodeStatsName(op)
		fmt.Fprintf(w, "opcode[%s].execution_count : %d\n", name, statsTable[op].ExecutionCount)
		for j := 0; j < 256; j++ {
			if statsTable[op].PairCount[j] == 0 {
				continue
			}
			fmt.Fprintf(w, "opcode[%s].pair_count[%s] : %d\n",
				name, opcodeStatsName(j), statsTable[op].PairCount[j])
		}
	}
}

func opcodeStatsName(op int) string {
	name := compile.Opcode(op).Name()
	if name == "" {
		return fmt.Sprintf("OP_%d", op)
	}
	return name
}

// DumpStatsToFile writes the profile to $GOPY_STATS_FILE if set.
// Called from lifecycle shutdown. No-op when GOPY_STATS is off.
//
// CPython: Python/specialize.c:386 _Py_PrintSpecializationStats
func DumpStatsToFile() error {
	path := os.Getenv("GOPY_STATS_FILE")
	if path == "" || statsTable == nil {
		return nil
	}
	f, err := os.Create(path) //nolint:gosec // GOPY_STATS_FILE is a developer-supplied stats path; opening it is the entire contract.
	if err != nil {
		return err
	}
	defer f.Close()
	DumpStats(f)
	return nil
}

// recordOpcode bumps execution_count[op] and pair_count[lastOp][op].
// Both reads run under the GIL like CPython's INSTRUCTION_STATS, so
// no atomics are required.
//
// CPython: Python/ceval_macros.h:63 INSTRUCTION_STATS
func (e *evalState) recordOpcode(op compile.Opcode) {
	if statsTable == nil {
		return
	}
	statsTable[op].ExecutionCount++
	statsTable[e.lastOpcode].PairCount[op]++
	e.lastOpcode = op
}
