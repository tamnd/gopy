package objects

import "testing"

// PEP 626 location-table fixtures. Each entry is hand-encoded in the
// 4-bit-code format the writer in compile/assemble_locations.go
// produces. End-to-end parity with the writer lives in
// vm/positions_test.go (which can import compile without cycling);
// here we keep just enough byte-level fixtures to pin the decoder
// against drift in the bit layout itself.

func TestCoLinesEmpty(t *testing.T) {
	if got := CoLines(nil); got != nil {
		t.Errorf("CoLines(nil) = %v, want nil", got)
	}
	if got := CoLines(&Code{}); got != nil {
		t.Errorf("CoLines(empty) = %v, want nil", got)
	}
}

// TestCoLinesShortForm: a short-form record (code 0, length 1,
// columns 5..12 on the firstlineno).
func TestCoLinesShortForm(t *testing.T) {
	// entry start: 0x80 | (0<<3) | 0 = 0x80
	// payload: ((5&7)<<4) | (12-5) = 0x57
	c := &Code{
		Firstlineno: 5,
		Linetable:   []byte{0x80, 0x57},
	}
	got := CoLines(c)
	want := []LineEntry{{Start: 0, End: 2, Line: 5}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestCoLinesOneLineForm: oneline form (line delta 1, columns 10/20).
func TestCoLinesOneLineForm(t *testing.T) {
	// entry start: 0x80 | ((10+1)<<3) | (2-1) = 0xd9
	c := &Code{
		Firstlineno: 1,
		Linetable:   []byte{0xd9, 10, 20},
	}
	pos := CoPositions(c)
	if len(pos) != 1 {
		t.Fatalf("len = %d, want 1", len(pos))
	}
	if pos[0].Line != 2 || pos[0].Column != 10 || pos[0].EndColumn != 20 {
		t.Errorf("got %+v, want line=2 col=10 endcol=20", pos[0])
	}
	if pos[0].Start != 0 || pos[0].End != 4 {
		t.Errorf("got span [%d,%d), want [0,4)", pos[0].Start, pos[0].End)
	}
}

// TestCoLinesNoneForm: none form pins line=-1 and stops the cursor
// from advancing.
func TestCoLinesNoneForm(t *testing.T) {
	// entry start: 0x80 | (15<<3) | 0 = 0xf8
	c := &Code{
		Firstlineno: 7,
		Linetable:   []byte{0xf8},
	}
	got := CoLines(c)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Line != -1 {
		t.Errorf("none-form line = %d, want -1", got[0].Line)
	}
}

// TestCoLinesNoColumnForm: code 13, signed varint line delta.
func TestCoLinesNoColumnForm(t *testing.T) {
	// entry start: 0x80 | (13<<3) | 0 = 0xe8
	// signed varint zigzag(2) = 4 -> 0x04
	c := &Code{
		Firstlineno: 5,
		Linetable:   []byte{0xe8, 0x04},
	}
	got := CoLines(c)
	if len(got) != 1 || got[0].Line != 7 {
		t.Errorf("got %+v, want line=7", got)
	}
}

// TestCoLinesLongForm: long form (signed line delta + 3 unsigned
// varints for end-line, col+1, endcol+1). Pins multi-line spans.
func TestCoLinesLongForm(t *testing.T) {
	// entry start 0x80 | (14<<3) | 0 = 0xf0
	// signed line-delta zigzag(1) = 2
	// end-line delta 1, col+1 = 1, endcol+1 = 9
	c := &Code{
		Firstlineno: 4,
		Linetable:   []byte{0xf0, 2, 1, 1, 9},
	}
	pos := CoPositions(c)
	if len(pos) != 1 {
		t.Fatalf("len = %d", len(pos))
	}
	if pos[0].Line != 5 || pos[0].EndLine != 6 || pos[0].Column != 0 || pos[0].EndColumn != 8 {
		t.Errorf("long-form: %+v", pos[0])
	}
}

// TestCoAddr2LocationLookup: precomputed-entry lookup pinpoints a
// span by byte offset.
func TestCoAddr2LocationLookup(t *testing.T) {
	c := &Code{
		Firstlineno: 5,
		Linetable:   []byte{0x80, 0x57}, // span [0, 2), short form
	}
	got, ok := CoAddr2Location(c, 1)
	if !ok {
		t.Fatal("expected hit at addr=1")
	}
	if got.Line != 5 {
		t.Errorf("line = %d, want 5", got.Line)
	}
	if _, ok := CoAddr2Location(c, 5); ok {
		t.Error("addr=5 must miss (out of range)")
	}
}
