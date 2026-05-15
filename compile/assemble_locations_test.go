package compile

import (
	"bytes"
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestWriteLocShort: single-line span, columns under 80, span under
// 16 produces a 2-byte short-form record.
func TestWriteLocShort(t *testing.T) {
	got := writeLocationInfoShortForm(nil, 1, 5, 12)
	// entry start: 0x80 | (0<<3) | 0 = 0x80
	// payload: ((5&7)<<4) | (12-5) = (5<<4)|7 = 0x57
	want := []byte{0x80, 0x57}
	if !bytes.Equal(got, want) {
		t.Errorf("short-form bytes = % x, want % x", got, want)
	}
}

// TestWriteLocOneline: line delta 1, columns 10..20 picks the oneline
// form (3 bytes).
func TestWriteLocOneline(t *testing.T) {
	got := writeLocationInfoOnelineForm(nil, 2, 1, 10, 20)
	// entry start: 0x80 | ((10+1)<<3) | (2-1) = 0x80 | 0x58 | 1 = 0xd9
	want := []byte{0xd9, 10, 20}
	if !bytes.Equal(got, want) {
		t.Errorf("oneline bytes = % x, want % x", got, want)
	}
}

// TestWriteLocNone: produces a 1-byte entry with code 15 set.
func TestWriteLocNone(t *testing.T) {
	got := writeLocationInfoNone(nil, 1)
	// 0x80 | (15<<3) | 0 = 0x80 | 0x78 = 0xf8
	want := []byte{0xf8}
	if !bytes.Equal(got, want) {
		t.Errorf("none bytes = % x, want % x", got, want)
	}
}

// TestWriteLocNoColumn: code 13 plus signed varint line delta.
func TestWriteLocNoColumn(t *testing.T) {
	got := writeLocationInfoNoColumn(nil, 1, 2)
	// 0x80 | (13<<3) | 0 = 0x80 | 0x68 = 0xe8
	// signed varint zigzag(2) = 4 -> 0x04
	want := []byte{0xe8, 4}
	if !bytes.Equal(got, want) {
		t.Errorf("no-column bytes = % x, want % x", got, want)
	}
}

// TestWriteLocLong: full 4-varint long form for a multi-line span.
func TestWriteLocLong(t *testing.T) {
	loc := ast.Pos{Lineno: 5, ColOffset: 0, EndLineno: 6, EndColOffset: 8}
	got := writeLocationInfoLongForm(nil, loc, 4, 1)
	// entry start 0x80 | (14<<3) | 0 = 0xf0
	// signed line-delta zigzag(1) = 2
	// end-line delta = 1
	// col+1 = 1
	// endcol+1 = 9
	want := []byte{0xf0, 2, 1, 1, 9}
	if !bytes.Equal(got, want) {
		t.Errorf("long bytes = % x, want % x", got, want)
	}
}

// TestWriteLocEntryDispatch: dispatcher picks the smallest form for
// each input.
func TestWriteLocEntryDispatch(t *testing.T) {
	cases := []struct {
		name      string
		loc       ast.Pos
		cursor    int
		wantFirst byte
	}{
		{"none", ast.Pos{Lineno: noLineno, EndLineno: noLineno, ColOffset: -1, EndColOffset: -1}, 1, 0xf8},
		{"short", ast.Pos{Lineno: 5, EndLineno: 5, ColOffset: 0, EndColOffset: 4}, 5, 0x80},
		{"oneline", ast.Pos{Lineno: 6, EndLineno: 6, ColOffset: 90, EndColOffset: 100}, 5, 0xd8},
		{"no-column", ast.Pos{Lineno: 5, EndLineno: 5, ColOffset: -1, EndColOffset: -1}, 5, 0xe8},
		{"long", ast.Pos{Lineno: 5, EndLineno: 7, ColOffset: 0, EndColOffset: 4}, 5, 0xf0},
	}
	for _, c := range cases {
		got, _ := writeLocationInfoEntry(nil, c.loc, c.cursor, 1)
		if got[0] != c.wantFirst {
			t.Errorf("%s: first byte = %#x, want %#x (full=% x)", c.name, got[0], c.wantFirst, got)
		}
	}
}

// TestEmitLocationSplitsLongSpans: spans over 8 codeunits split into
// 8-codeunit chunks because the entry start length field is only 3
// bits.
func TestEmitLocationSplitsLongSpans(t *testing.T) {
	loc := ast.Pos{Lineno: 1, EndLineno: 1, ColOffset: 0, EndColOffset: 4}
	got, _ := assembleEmitLocation(nil, loc, 1, 20)
	// 20 = 8 + 8 + 4 -> three entries, each starts with bit 7 set.
	starts := 0
	for _, b := range got {
		if b&0x80 != 0 {
			starts++
		}
	}
	if starts != 3 {
		t.Errorf("expected 3 entry starts, got %d in % x", starts, got)
	}
}

// TestAssembleLineTableEmpty: empty sequence produces an empty table.
func TestAssembleLineTableEmpty(t *testing.T) {
	seq := &Sequence{}
	got := assembleLocationInfo(seq, 1)
	if len(got) != 0 {
		t.Errorf("empty seq line table = % x, want empty", got)
	}
}

// TestAssembleLineTableCoalesces: adjacent instructions with the same
// location share one entry.
func TestAssembleLineTableCoalesces(t *testing.T) {
	loc := ast.Pos{Lineno: 1, EndLineno: 1, ColOffset: 0, EndColOffset: 4}
	seq := &Sequence{Instrs: []Instr{
		{Op: NOP, Loc: loc},
		{Op: NOP, Loc: loc},
		{Op: NOP, Loc: loc},
	}}
	got := assembleLocationInfo(seq, 1)
	starts := 0
	for _, b := range got {
		if b&0x80 != 0 {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("expected 1 entry for coalesced run, got %d (% x)", starts, got)
	}
}
