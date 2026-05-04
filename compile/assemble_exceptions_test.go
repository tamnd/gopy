package compile

import (
	"bytes"
	"testing"
)

// TestWriteExcItemSmall: values under 64 fit in a single byte; the
// msb argument lights up bit 7 only on the entry-start byte.
func TestWriteExcItemSmall(t *testing.T) {
	got := writeExcItem(nil, 5, entryStartBit)
	want := []byte{0x80 | 5}
	if !bytes.Equal(got, want) {
		t.Errorf("writeExcItem(5, msb) = % x, want % x", got, want)
	}
}

// TestWriteExcItemTwoByte: 64 should produce a continuation byte plus
// a final byte.
func TestWriteExcItemTwoByte(t *testing.T) {
	got := writeExcItem(nil, 64, 0)
	// 64 = 0b1_000000: first byte = (1>>0&0x3f)|0x40 = 0x41
	// (high 6 bits = 1, continuation set), then low 6 bits = 0.
	want := []byte{0x41, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("writeExcItem(64) = % x, want % x", got, want)
	}
}

// TestAssembleExceptionTableEmpty: a sequence with no handlers
// returns no bytes.
func TestAssembleExceptionTableEmpty(t *testing.T) {
	seq := &Sequence{Instrs: []Instr{
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
	}}
	if got := assembleExceptionTable(seq); len(got) != 0 {
		t.Errorf("empty handlers expected no table, got % x", got)
	}
}

// TestAssembleExceptionTableSingle: one run of two instructions
// guarded by a handler at index 2 produces exactly one record. The
// record's first byte must have bit 7 set.
func TestAssembleExceptionTableSingle(t *testing.T) {
	h := ExceptHandlerInfo{Label: 2, StartDepth: 1, PreserveLasti: 0}
	seq := &Sequence{Instrs: []Instr{
		{Op: NOP, Handler: h},
		{Op: NOP, Handler: h},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
	}}
	got := assembleExceptionTable(seq)
	if len(got) == 0 {
		t.Fatal("expected non-empty table")
	}
	if got[0]&entryStartBit == 0 {
		t.Errorf("first byte missing entry-start bit: % x", got)
	}
	starts := 0
	for _, b := range got {
		if b&entryStartBit != 0 {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("expected 1 entry start, got %d (% x)", starts, got)
	}
}

// TestAssembleExceptionTableTwoEntries: alternating handlers produce
// two records.
func TestAssembleExceptionTableTwoEntries(t *testing.T) {
	h1 := ExceptHandlerInfo{Label: 4, StartDepth: 1}
	h2 := ExceptHandlerInfo{Label: 5, StartDepth: 1}
	seq := &Sequence{Instrs: []Instr{
		{Op: NOP, Handler: h1},
		{Op: NOP, Handler: h2},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
	}}
	got := assembleExceptionTable(seq)
	starts := 0
	for _, b := range got {
		if b&entryStartBit != 0 {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("expected 2 entries, got %d (% x)", starts, got)
	}
}

// TestAssembleExceptionTableLasti: preserve_lasti bit threads into
// the depth_lasti field.
func TestAssembleExceptionTableLasti(t *testing.T) {
	h := ExceptHandlerInfo{Label: 1, StartDepth: 2, PreserveLasti: 1}
	seq := &Sequence{Instrs: []Instr{
		{Op: NOP, Handler: h},
		{Op: NOP, Handler: ExceptHandlerInfo{Label: -1}},
	}}
	got := assembleExceptionTable(seq)
	if len(got) < 4 {
		t.Fatalf("table too short: % x", got)
	}
	// Last byte is depth_lasti = ((2-1-1)<<1) | 1 = 1. Small enough
	// to live in a single varint byte with no continuation.
	if got[len(got)-1] != 1 {
		t.Errorf("depth_lasti tail = %d, want 1 (full % x)", got[len(got)-1], got)
	}
}
