package specialize

import (
	"encoding/binary"
	"testing"

	"github.com/tamnd/gopy/compile"
)

// build assembles a synthetic bytecode buffer with an op followed by
// nCaches zero-filled cache codeunits, then a NOP terminator. Caller
// passes the cache count explicitly so we can also exercise mismatch
// paths.
func build(op compile.Opcode, nCaches int) []byte {
	buf := make([]byte, 2*(1+nCaches+1))
	buf[0] = byte(op)
	// last codeunit is NOP so Quicken's size-1 stop is observable
	buf[len(buf)-2] = byte(compile.NOP)
	return buf
}

// TestQuickenStampsAdaptiveCounter pins the counter shape that
// CPython writes into a fresh adaptive cache slot.
func TestQuickenStampsAdaptiveCounter(t *testing.T) {
	cases := []struct {
		name string
		op   compile.Opcode
	}{
		{"LOAD_GLOBAL", compile.LOAD_GLOBAL},
		{"BINARY_OP", compile.BINARY_OP},
		{"LOAD_ATTR", compile.LOAD_ATTR},
		{"STORE_ATTR", compile.STORE_ATTR},
		{"CALL", compile.CALL},
		{"CALL_KW", compile.CALL_KW},
		{"COMPARE_OP", compile.COMPARE_OP},
		{"CONTAINS_OP", compile.CONTAINS_OP},
		{"FOR_ITER", compile.FOR_ITER},
		{"SEND", compile.SEND},
		{"STORE_SUBSCR", compile.STORE_SUBSCR},
		{"LOAD_SUPER_ATTR", compile.LOAD_SUPER_ATTR},
		{"TO_BOOL", compile.TO_BOOL},
		{"UNPACK_SEQUENCE", compile.UNPACK_SEQUENCE},
	}
	want := AdaptiveCounterWarmup().ValueAndBackoff
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := build(c.op, CacheCount(c.op))
			Quicken(buf, true)
			got := binary.LittleEndian.Uint16(buf[2:])
			if got != want {
				t.Fatalf("counter slot: got 0x%04x want 0x%04x",
					got, want)
			}
		})
	}
}

// TestQuickenJumpBackward pins the JUMP_BACKWARD seed.
func TestQuickenJumpBackward(t *testing.T) {
	buf := build(compile.JUMP_BACKWARD, CacheCount(compile.JUMP_BACKWARD))
	Quicken(buf, true)
	got := binary.LittleEndian.Uint16(buf[2:])
	want := InitialJumpBackoffCounter().ValueAndBackoff
	if got != want {
		t.Fatalf("JUMP_BACKWARD seed: got 0x%04x want 0x%04x",
			got, want)
	}
}

// TestQuickenPopJumpHistory pins the 0x5555 branch-hint seed.
func TestQuickenPopJumpHistory(t *testing.T) {
	for _, op := range []compile.Opcode{
		compile.POP_JUMP_IF_FALSE,
		compile.POP_JUMP_IF_TRUE,
		compile.POP_JUMP_IF_NONE,
		compile.POP_JUMP_IF_NOT_NONE,
	} {
		t.Run(op.Name(), func(t *testing.T) {
			buf := build(op, CacheCount(op))
			Quicken(buf, true)
			got := binary.LittleEndian.Uint16(buf[2:])
			if got != 0x5555 {
				t.Fatalf("branch hint slot: got 0x%04x want 0x5555",
					got)
			}
		})
	}
}

// TestQuickenDisabledCounters pins the unreachable-sentinel path
// CPython takes when enable_counters is false.
func TestQuickenDisabledCounters(t *testing.T) {
	buf := build(compile.LOAD_GLOBAL, CacheCount(compile.LOAD_GLOBAL))
	Quicken(buf, false)
	got := binary.LittleEndian.Uint16(buf[2:])
	want := InitialUnreachableBackoffCounter().ValueAndBackoff
	if got != want {
		t.Fatalf("disabled counter slot: got 0x%04x want 0x%04x",
			got, want)
	}
}

// TestQuickenSkipsNonAdaptive ensures Quicken leaves opcodes with no
// cache untouched. NOP has no cache; following bytes must stay zero.
func TestQuickenSkipsNonAdaptive(t *testing.T) {
	buf := []byte{
		byte(compile.NOP), 0,
		byte(compile.LOAD_CONST), 0,
		byte(compile.NOP), 0,
	}
	Quicken(buf, true)
	for i, b := range buf {
		if b != 0 {
			if i == 0 || i == 2 || i == 4 {
				continue
			}
			t.Fatalf("byte %d: got 0x%02x want 0", i, b)
		}
	}
}

// TestQuickenIdempotent matches CPython's expectation that repeated
// quickening is a no-op.
func TestQuickenIdempotent(t *testing.T) {
	buf := build(compile.LOAD_GLOBAL, CacheCount(compile.LOAD_GLOBAL))
	Quicken(buf, true)
	first := make([]byte, len(buf))
	copy(first, buf)
	Quicken(buf, true)
	for i := range buf {
		if buf[i] != first[i] {
			t.Fatalf("byte %d drifted on second pass: 0x%02x -> 0x%02x",
				i, first[i], buf[i])
		}
	}
}

// TestQuickenSkipsTrailingAdaptive: the last codeunit cannot have a
// cache (out of bounds for the slot). Quicken must not write past
// the buffer if an adaptive opcode lands at the last position.
func TestQuickenSkipsTrailingAdaptive(t *testing.T) {
	buf := []byte{
		byte(compile.NOP), 0,
		byte(compile.LOAD_GLOBAL), 0,
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Quicken panicked at trailing adaptive op: %v", r)
		}
	}()
	Quicken(buf, true)
}

// TestQuickenSkipsCacheBytes confirms Quicken jumps past the cache
// bytes after stamping. Two adaptive opcodes back-to-back: the
// second sits at codeunit 1+caches and must also be stamped.
func TestQuickenSkipsCacheBytes(t *testing.T) {
	a := compile.LOAD_GLOBAL
	b := compile.BINARY_OP
	bufLen := 2 * (1 + CacheCount(a) + 1 + CacheCount(b) + 1)
	buf := make([]byte, bufLen)
	buf[0] = byte(a)
	buf[2*(1+CacheCount(a))] = byte(b)
	buf[bufLen-2] = byte(compile.NOP)
	Quicken(buf, true)

	want := AdaptiveCounterWarmup().ValueAndBackoff
	gotA := binary.LittleEndian.Uint16(buf[2:])
	gotB := binary.LittleEndian.Uint16(buf[2*(1+CacheCount(a)+1):])
	if gotA != want || gotB != want {
		t.Fatalf("two-op stamp: gotA=0x%04x gotB=0x%04x want 0x%04x",
			gotA, gotB, want)
	}
}
