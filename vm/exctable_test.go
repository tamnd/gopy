package vm

import (
	"testing"
)

// encVarint emits one PEP 657 varint with the given msb (0x80 on the
// very first byte of an entry). Mirrors compile.writeExcItem so tests
// can build tables by hand without dragging in the compile package.
func encVarint(buf []byte, value int, msb byte) []byte {
	v := uint32(value)
	if v >= 1<<24 {
		buf = append(buf, byte(v>>24)|0x40|msb)
		msb = 0
	}
	if v >= 1<<18 {
		buf = append(buf, byte((v>>18)&0x3f)|0x40|msb)
		msb = 0
	}
	if v >= 1<<12 {
		buf = append(buf, byte((v>>12)&0x3f)|0x40|msb)
		msb = 0
	}
	if v >= 1<<6 {
		buf = append(buf, byte((v>>6)&0x3f)|0x40|msb)
		msb = 0
	}
	return append(buf, byte(v&0x3f)|msb)
}

func encEntry(buf []byte, start, size, target, depthLasti int) []byte {
	buf = encVarint(buf, start, 0x80)
	buf = encVarint(buf, size, 0)
	buf = encVarint(buf, target, 0)
	buf = encVarint(buf, depthLasti, 0)
	return buf
}

func TestReadExcVarintSingleByte(t *testing.T) {
	v, n, ok := readExcVarint([]byte{0x80 | 5}, 0)
	if !ok || v != 5 || n != 1 {
		t.Errorf("got (%d,%d,%v), want (5,1,true)", v, n, ok)
	}
}

func TestReadExcVarintTwoByte(t *testing.T) {
	// 64 -> 0x41, 0x00 (continuation set on first byte, low 6 bits of value follow)
	v, n, ok := readExcVarint([]byte{0x41, 0x00}, 0)
	if !ok || v != 64 || n != 2 {
		t.Errorf("got (%d,%d,%v), want (64,2,true)", v, n, ok)
	}
}

func TestReadExcVarintTruncated(t *testing.T) {
	// continuation bit set but no follow-up byte
	if _, _, ok := readExcVarint([]byte{0x40}, 0); ok {
		t.Error("expected ok=false for truncated varint")
	}
}

func TestFindExcHandlerHit(t *testing.T) {
	// Encoded fields are code-unit offsets: start=4, size=6 (covers
	// code units [4,10)), target=20, depth=2. findExcHandler scales
	// these to bytes, so the live range is [8,20) and target=40.
	tab := encEntry(nil, 4, 6, 20, 2<<1)
	for _, pc := range []int{8, 9, 19} {
		entry, ok := findExcHandler(tab, pc)
		if !ok {
			t.Errorf("pc=%d: expected hit", pc)
			continue
		}
		if entry.start != 8 || entry.end != 20 || entry.target != 40 || entry.depth != 2 || entry.preserveLasti {
			t.Errorf("pc=%d: got %+v", pc, entry)
		}
	}
}

func TestFindExcHandlerMiss(t *testing.T) {
	// Code units [4,10) scale to bytes [8,20); everything outside misses.
	tab := encEntry(nil, 4, 6, 20, 0)
	for _, pc := range []int{0, 7, 20, 100} {
		if _, ok := findExcHandler(tab, pc); ok {
			t.Errorf("pc=%d: expected miss", pc)
		}
	}
}

func TestFindExcHandlerSecondEntry(t *testing.T) {
	tab := encEntry(nil, 0, 4, 100, 0)
	tab = encEntry(tab, 8, 4, 200, (3<<1)|1)
	// Second entry covers code units [8,12) -> bytes [16,24).
	entry, ok := findExcHandler(tab, 18)
	if !ok {
		t.Fatal("expected hit on second entry")
	}
	if entry.target != 400 || entry.depth != 3 || !entry.preserveLasti {
		t.Errorf("got %+v", entry)
	}
}

func TestFindExcHandlerEmpty(t *testing.T) {
	if _, ok := findExcHandler(nil, 0); ok {
		t.Error("expected miss on empty table")
	}
}

func TestFindExcHandlerLargeOffsets(t *testing.T) {
	// Force multi-byte varints. Encoded code units scale to bytes (2x).
	tab := encEntry(nil, 1<<14, 1<<10, 1<<16, 0)
	entry, ok := findExcHandler(tab, 2<<14+5)
	if !ok {
		t.Fatal("expected hit")
	}
	if entry.start != 2<<14 || entry.end != 2<<14+2<<10 || entry.target != 2<<16 {
		t.Errorf("got %+v", entry)
	}
}
