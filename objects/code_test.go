package objects

import "testing"

func TestCoLinesEmpty(t *testing.T) {
	if got := CoLines(nil); got != nil {
		t.Errorf("CoLines(nil) = %v, want nil", got)
	}
	if got := CoLines(&Code{}); got != nil {
		t.Errorf("CoLines(empty) = %v, want nil", got)
	}
}

func TestCoLinesGolden(t *testing.T) {
	c := &Code{
		Firstlineno: 10,
		// Three spans: bytes 0..3 -> line 10, 4..7 -> line 11, 8..11 -> line 13
		Linetable: []byte{
			0x04, 0x00, // span 4 bytes, delta 0
			0x04, 0x01, // span 4 bytes, delta +1
			0x04, 0x02, // span 4 bytes, delta +2
		},
	}
	got := CoLines(c)
	want := []LineEntry{
		{Start: 0, End: 4, Line: 10},
		{Start: 4, End: 8, Line: 11},
		{Start: 8, End: 12, Line: 13},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, e := range got {
		if e != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, e, want[i])
		}
	}
}

func TestCoLinesNegativeDelta(t *testing.T) {
	c := &Code{
		Firstlineno: 10,
		Linetable: []byte{
			0x02, 0x05, // +5 -> 15
			0x02, 0xfb, // -5 -> 10
		},
	}
	got := CoLines(c)
	if len(got) != 2 || got[0].Line != 15 || got[1].Line != 10 {
		t.Errorf("delta math: %+v", got)
	}
}

func TestCoLinesGapEntry(t *testing.T) {
	c := &Code{
		Firstlineno: 5,
		Linetable: []byte{
			0xff, 0x00, // gap span: line stays -1
			0x02, 0x01,
		},
	}
	got := CoLines(c)
	if len(got) != 2 || got[0].Line != -1 || got[1].Line != 6 {
		t.Errorf("gap handling: %+v", got)
	}
}

func TestCoPositionsRoundTrip(t *testing.T) {
	c := &Code{
		Firstlineno: 1,
		Linetable: []byte{
			0x02, 0x00,
			0x02, 0x02,
		},
	}
	got := CoPositions(c)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Line != 1 || got[1].Line != 3 {
		t.Errorf("position lines: %+v", got)
	}
	if got[0].Column != -1 {
		t.Errorf("column placeholder: %d", got[0].Column)
	}
}
