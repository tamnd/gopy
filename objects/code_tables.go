// PEP 626 location-table decoder. The encoded form is the post-3.11
// 4-bit-code stream emitted by Python/assemble.c
// write_location_info_entry: each entry's first byte sets bit 7 plus a
// 4-bit format code, the rest of the entry follows in 6-bit varints
// (bit 6 = continuation). Five formats: short (codes 0..9), one-line
// (10..12), no-column (13), long (14), none (15). One codeunit = 2
// bytes.
//
// CPython: Objects/codeobject.c:1199 advance_with_locations
// CPython: Objects/codeobject.c:1252 PyCode_Addr2Location

package objects

const (
	locShort0   = 0
	locOneLine0 = 10
	locNoCols   = 13
	locLong     = 14
	locNone     = 15

	locContinuationBit byte = 0x40
	locEntryStartBit   byte = 0x80
)

// LineEntry is one decoded line-table record. Start and End are
// bytecode byte offsets (half-open span); Line is the source line
// (1-based) or -1 when the entry has no associated source line.
//
// CPython: Objects/codeobject.c:1156 PyCodeAddressRange (ar_start /
// ar_end / ar_line fields, projected without the column data).
type LineEntry struct {
	Start int
	End   int
	Line  int
}

// PositionEntry adds the column span; matches the PEP 657 four-field
// position used for fine-grained traceback rendering.
//
// CPython: Objects/codeobject.c:1252 PyCode_Addr2Location output.
type PositionEntry struct {
	Start     int
	End       int
	Line      int
	EndLine   int
	Column    int
	EndColumn int
}

// readLocVarint decodes one unsigned varint starting at b[pos]. Bits
// 0..5 of each byte carry the value chunks little-endian; bit 6
// (locContinuationBit) signals more bytes follow.
//
// CPython: Objects/codeobject.c:1133 read_varint
func readLocVarint(b []byte, pos int) (val, n int, ok bool) {
	if pos >= len(b) {
		return 0, 0, false
	}
	first := b[pos]
	val = int(first & 0x3f)
	n = 1
	shift := uint(0)
	for first&locContinuationBit != 0 {
		if pos+n >= len(b) {
			return 0, 0, false
		}
		x := b[pos+n]
		shift += 6
		val |= int(x&0x3f) << shift
		first = x
		n++
	}
	return val, n, true
}

// readLocSignedVarint decodes a zigzag-encoded signed varint.
//
// CPython: Objects/codeobject.c:1147 read_signed_varint
func readLocSignedVarint(b []byte, pos int) (val, n int, ok bool) {
	u, n, ok := readLocVarint(b, pos)
	if !ok {
		return 0, 0, false
	}
	if u&1 != 0 {
		return -(u >> 1), n, true
	}
	return u >> 1, n, true
}

// CoPositions decodes the PEP 626 line table into one PositionEntry
// per coalesced run, in source order. Start/End are bytecode bytes
// (1 codeunit = 2 bytes); Column / EndColumn are -1 when the form
// omits column information; Line / EndLine are -1 for the "none"
// form.
//
// CPython: Objects/codeobject.c:1199 advance_with_locations (called
// in a loop).
func CoPositions(c *Code) []PositionEntry {
	if c == nil || len(c.Linetable) == 0 {
		return nil
	}
	tab := c.Linetable
	line := c.Firstlineno
	if line <= 0 {
		line = 1
	}

	var out []PositionEntry
	pos := 0
	end := 0
	for pos < len(tab) {
		first := tab[pos]
		if first&locEntryStartBit == 0 {
			pos++
			continue
		}
		code := int((first >> 3) & 15)
		length := int(first&7) + 1
		spanBytes := length * 2
		start := end
		end = start + spanBytes
		pos++

		entry := PositionEntry{Start: start, End: end, Column: -1, EndColumn: -1}
		switch {
		case code == locNone:
			entry.Line = -1
			entry.EndLine = -1
		case code == locLong:
			delta, n, ok := readLocSignedVarint(tab, pos)
			if !ok {
				return out
			}
			pos += n
			line += delta
			entry.Line = line
			endDelta, n, ok := readLocVarint(tab, pos)
			if !ok {
				return out
			}
			pos += n
			entry.EndLine = line + endDelta
			col, n, ok := readLocVarint(tab, pos)
			if !ok {
				return out
			}
			pos += n
			entry.Column = col - 1
			endCol, n, ok := readLocVarint(tab, pos)
			if !ok {
				return out
			}
			pos += n
			entry.EndColumn = endCol - 1
		case code == locNoCols:
			delta, n, ok := readLocSignedVarint(tab, pos)
			if !ok {
				return out
			}
			pos += n
			line += delta
			entry.Line = line
			entry.EndLine = line
		case code >= locOneLine0 && code <= locOneLine0+2:
			lineDelta := code - locOneLine0
			line += lineDelta
			entry.Line = line
			entry.EndLine = line
			if pos+1 >= len(tab) {
				return out
			}
			entry.Column = int(tab[pos])
			entry.EndColumn = int(tab[pos+1])
			pos += 2
		default:
			// Short form (codes 0..9): column = (code << 3) | (b >> 4),
			// end column = column + (b & 15).
			if pos >= len(tab) {
				return out
			}
			second := int(tab[pos])
			pos++
			entry.Line = line
			entry.EndLine = line
			entry.Column = (code << 3) | (second >> 4)
			entry.EndColumn = entry.Column + (second & 15)
		}
		out = append(out, entry)
	}
	return out
}

// CoLines projects CoPositions down to (start, end, line). The
// caller pays the column-decoding cost only when they need column
// data via CoPositions.
//
// CPython: Objects/codeobject.c:1017 PyCode_Addr2Line (called per
// instruction range).
func CoLines(c *Code) []LineEntry {
	pos := CoPositions(c)
	if pos == nil {
		return nil
	}
	out := make([]LineEntry, len(pos))
	for i, p := range pos {
		out[i] = LineEntry{Start: p.Start, End: p.End, Line: p.Line}
	}
	return out
}

// CoAddr2Location returns the PositionEntry covering bytecode byte
// offset addr, or false when no entry covers it. Mirrors the
// single-address form of PyCode_Addr2Location, sparing callers the
// linear scan when they have a precomputed entries slice.
//
// CPython: Objects/codeobject.c:1252 PyCode_Addr2Location
func CoAddr2Location(c *Code, addr int) (PositionEntry, bool) {
	for _, p := range CoPositions(c) {
		if addr >= p.Start && addr < p.End {
			return p, true
		}
	}
	return PositionEntry{}, false
}
