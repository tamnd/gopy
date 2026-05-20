// Dispatch-loop micro-benchmarks. Each bench runs a tight bytecode
// loop so the dispatcher cost dominates the body. Used by the
// D-series to measure per-arm wins before pushing the full
// pyperformance suite.
//
// Run with: go test -run=^$ -bench=. ./vm/...
//
// CPython parity: micro-benches like these are typically captured via
// pyperf-timeit on a hand-crafted loop; the gopy equivalent lives in
// bench/ for end-to-end runs, but the per-arm cost needs a faster
// turn-around so the D-phases can iterate.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// BenchmarkLoadConstReturn measures the smallest dispatch pair:
// LOAD_CONST followed by RETURN_VALUE. Establishes a floor; every
// later D-phase should leave this no slower.
func BenchmarkLoadConstReturn(b *testing.B) {
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(0)},
		Stacksize: 4,
	}
	ts := state.NewThread()
	g := objects.NewDict()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EvalCode(ts, co, g, g); err != nil {
			b.Fatalf("EvalCode: %v", err)
		}
	}
}

// BenchmarkLoadFastReturn measures the path that hot loops walk:
// LOAD_FAST 0 then RETURN_VALUE. This is the single hottest pair in
// CPython's pyperformance profile (Python/bytecodes.c LOAD_FAST), so
// it is the right yardstick for D5's inlining work.
func BenchmarkLoadFastReturn(b *testing.B) {
	var bc []byte
	// We need the slot populated, so prime it with LOAD_CONST/STORE_FAST,
	// then the measured pair.
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.STORE_FAST, 0)
	bc = emitWithCache(bc, compile.LOAD_FAST, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := &objects.Code{
		Code:      bc,
		Consts:    []any{int64(7)},
		Varnames:  []string{"x"},
		Stacksize: 4,
	}
	ts := state.NewThread()
	g := objects.NewDict()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EvalCode(ts, co, g, g); err != nil {
			b.Fatalf("EvalCode: %v", err)
		}
	}
}
