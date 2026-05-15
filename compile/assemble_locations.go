// PEP 626 location table writer. The line table is a varint stream
// with a 4-bit "code" field selecting one of five record formats:
// short (codes 0..9), one-line (codes 10..12), no-column (code 13),
// long (code 14), none (code 15). Each entry header byte has bit 7
// set so a reader can locate entry boundaries.
//
// CPython: Python/assemble.c:196 location-table panel
// CPython: Include/cpython/code.h _PyCodeLocationInfoKind

package compile

import "github.com/tamnd/gopy/ast"

// Location-info record codes. Numeric values come from the
// PY_CODE_LOCATION_INFO_* enum in Include/cpython/code.h.
//
// CPython: Include/cpython/code.h _PyCodeLocationInfoKind
const (
	locShort0   = 0
	locOneLine0 = 10
	locNoCols   = 13
	locLong     = 14
	locNone     = 15
)

// noLineno mirrors NO_LOCATION.lineno in CPython. assemble.c uses -1
// to mean "no source line".
//
// CPython: Include/internal/pycore_compile.h NO_LOCATION
const noLineno = -1

// writeLocationEntryStart appends the leading byte of a location
// entry. Bit 7 is the "first byte of entry" marker, bits 6..3 hold
// the format code, bits 2..0 hold (length - 1) where length is the
// instruction span in code units.
//
// CPython: Include/internal/pycore_code.h:427 write_location_entry_start
func writeLocationEntryStart(buf []byte, code, length int) []byte {
	return append(buf, 0x80|byte(code<<3)|byte(length-1))
}

// writeLocationInfoShortForm emits a 2-byte short-form record. Used
// when the instruction stays on one line, columns fit in 7 bits, the
// column span fits in 4 bits, and the line delta is zero.
//
// CPython: Python/assemble.c:233 write_location_info_short_form
func writeLocationInfoShortForm(buf []byte, length, column, endColumn int) []byte {
	colLow := column & 7
	colGroup := column >> 3
	buf = writeLocationEntryStart(buf, locShort0+colGroup, length)
	return append(buf, byte((colLow<<4)|(endColumn-column)))
}

// writeLocationInfoOnelineForm emits a 3-byte oneline-form record.
// Used when the line delta is 0, 1, or 2, both columns fit in 7 bits,
// and the instruction stays within the same physical line.
//
// CPython: Python/assemble.c:246 write_location_info_oneline_form
func writeLocationInfoOnelineForm(buf []byte, length, lineDelta, column, endColumn int) []byte {
	buf = writeLocationEntryStart(buf, locOneLine0+lineDelta, length)
	buf = append(buf, byte(column))
	return append(buf, byte(endColumn))
}

// writeLocationInfoLongForm emits the worst-case long-form record:
// signed line delta varint, end-line delta varint, then column+1
// varints (the +1 is so unset (-1) becomes 0).
//
// CPython: Python/assemble.c:258 write_location_info_long_form
func writeLocationInfoLongForm(buf []byte, loc ast.Pos, lineCursor, length int) []byte {
	buf = writeLocationEntryStart(buf, locLong, length)
	buf = writeSignedVarint(buf, int32(loc.Lineno-lineCursor))
	buf = writeVarint(buf, uint32(loc.EndLineno-loc.Lineno))
	buf = writeVarint(buf, uint32(loc.ColOffset+1))
	buf = writeVarint(buf, uint32(loc.EndColOffset+1))
	return buf
}

// writeLocationInfoNone emits the no-location record. The cursor is
// not advanced.
//
// CPython: Python/assemble.c:270 write_location_info_none
func writeLocationInfoNone(buf []byte, length int) []byte {
	return writeLocationEntryStart(buf, locNone, length)
}

// writeLocationInfoNoColumn emits the no-column record. Only a signed
// line delta varint follows the entry start.
//
// CPython: Python/assemble.c:276 write_location_info_no_column
func writeLocationInfoNoColumn(buf []byte, length, lineDelta int) []byte {
	buf = writeLocationEntryStart(buf, locNoCols, length)
	return writeSignedVarint(buf, int32(lineDelta))
}

// writeLocationInfoEntry picks the smallest representation for one
// location span and appends it to buf. Returns the new buffer and the
// updated line cursor (CPython advances a_lineno after every form
// except none and short).
//
// CPython: Python/assemble.c:286 write_location_info_entry
func writeLocationInfoEntry(buf []byte, loc ast.Pos, lineCursor, length int) (out []byte, newCursor int) {
	if loc.Lineno == noLineno {
		return writeLocationInfoNone(buf, length), lineCursor
	}
	lineDelta := loc.Lineno - lineCursor
	col := loc.ColOffset
	endCol := loc.EndColOffset
	if col < 0 || endCol < 0 {
		if loc.EndLineno == loc.Lineno || loc.EndLineno < 0 {
			return writeLocationInfoNoColumn(buf, length, lineDelta), loc.Lineno
		}
	} else if loc.EndLineno == loc.Lineno {
		if lineDelta == 0 && col < 80 && endCol-col < 16 && endCol >= col {
			return writeLocationInfoShortForm(buf, length, col, endCol), lineCursor
		}
		if lineDelta >= 0 && lineDelta < 3 && col < 128 && endCol < 128 {
			return writeLocationInfoOnelineForm(buf, length, lineDelta, col, endCol), loc.Lineno
		}
	}
	return writeLocationInfoLongForm(buf, loc, lineCursor, length), loc.Lineno
}

// assembleEmitLocation appends one or more entries covering a span of
// codeunits. Spans longer than 8 codeunits split into 8-codeunit
// chunks because the entry-start byte only encodes (length-1) in 3
// bits.
//
// CPython: Python/assemble.c:324 assemble_emit_location
func assembleEmitLocation(buf []byte, loc ast.Pos, lineCursor, length int) (out []byte, newCursor int) {
	if length == 0 {
		return buf, lineCursor
	}
	for length > 8 {
		buf, lineCursor = writeLocationInfoEntry(buf, loc, lineCursor, 8)
		length -= 8
	}
	return writeLocationInfoEntry(buf, loc, lineCursor, length)
}

// AssembleLineTable is the exported wrapper for assembleLocationInfo.
// External tests use it to build location-table bytes from a curated
// Sequence and round-trip them through the vm reader without going
// through the full compile pipeline.
func AssembleLineTable(seq *Sequence, firstLineno int) []byte {
	return assembleLocationInfo(seq, firstLineno)
}

// assembleLocationInfo walks the post-flowgraph instruction stream,
// coalesces adjacent same-location runs, and emits the location table
// in the post-PEP-626 4-bit-code format. Returns the encoded bytes.
//
// CPython runs a reverse pass first that rewrites NEXT_LOCATION
// markers to inherit from the next instruction (falling back to
// NO_LOCATION on terminator ops). gopy's codegen does not emit
// NEXT_LOCATION sentinels yet, so the reverse pass is a no-op here.
// POP_BLOCK is the only pseudo op the gopy flowgraph leaves behind
// in the assembled stream; skip it so its synthetic location does not
// disturb coalescing.
//
// CPython: Python/assemble.c:337 assemble_location_info
func assembleLocationInfo(seq *Sequence, firstLineno int) []byte {
	var buf []byte
	if seq == nil || len(seq.Instrs) == 0 {
		return buf
	}
	lineCursor := firstLineno
	if firstLineno <= 0 {
		lineCursor = 1
	}
	var loc ast.Pos
	loc.Lineno = noLineno
	loc.EndLineno = noLineno
	loc.ColOffset = -1
	loc.EndColOffset = -1
	size := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Op == POP_BLOCK {
			continue
		}
		if !sameLocation(loc, ins.Loc) {
			buf, lineCursor = assembleEmitLocation(buf, loc, lineCursor, size)
			loc = ins.Loc
			size = 0
		}
		size += instrSize(ins)
	}
	buf, _ = assembleEmitLocation(buf, loc, lineCursor, size)
	return buf
}

// sameLocation compares two ast.Pos values by every field. Mirrors
// same_location in assemble.c, which is a memcmp on the location
// struct.
//
// CPython: Python/assemble.c:30 same_location
func sameLocation(a, b ast.Pos) bool {
	return a.Lineno == b.Lineno && a.EndLineno == b.EndLineno &&
		a.ColOffset == b.ColOffset && a.EndColOffset == b.EndColOffset
}

// instrSize returns the number of 16-bit code units one instruction
// occupies in the final byte stream: one for the opcode + oparg, plus
// one EXTENDED_ARG prefix per non-zero high byte of the oparg. CACHE
// entries are not yet emitted (the v0.5 pipeline does not run the
// specializer).
//
// CPython: Python/assemble.c:39 instr_size
func instrSize(ins *Instr) int {
	if ins.Op == POP_BLOCK {
		return 0
	}
	n := 1
	arg := uint32(ins.Oparg)
	for arg > 0xff {
		n++
		arg >>= 8
	}
	return n
}
