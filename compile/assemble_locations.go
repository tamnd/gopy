// PEP 626 location table writer. The line table is a varint stream
// with a 4-bit "code" field selecting one of five record formats:
// short (codes 0..9), one-line (codes 10..12), no-column (code 13),
// long (code 14), none (code 15). Each entry header byte has bit 7
// set so a reader can locate entry boundaries.
//
// CPython: Python/assemble.c:L196 location-table panel

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
// CPython: Python/assemble.c NO_LOCATION
const noLineno = -1

// writeEntryStart appends the leading byte of a location entry. Bit 7
// is the "first byte of entry" marker, bits 6..3 hold the format code,
// bits 2..0 hold (length - 1) where length is the instruction span in
// code units.
//
// CPython: Include/internal/pycore_code.h:L427 write_location_entry_start
func writeEntryStart(buf []byte, code, length int) []byte {
	return append(buf, 0x80|byte(code<<3)|byte(length-1))
}

// writeLocShort emits a 2-byte short-form record. Used when the
// instruction stays on one line, columns fit in 7 bits, the column
// span fits in 4 bits, and the line delta is zero.
//
// CPython: Python/assemble.c:L233 write_location_info_short_form
func writeLocShort(buf []byte, length, column, endColumn int) []byte {
	colLow := column & 7
	colGroup := column >> 3
	buf = writeEntryStart(buf, locShort0+colGroup, length)
	return append(buf, byte((colLow<<4)|(endColumn-column)))
}

// writeLocOneline emits a 3-byte oneline-form record. Used when the
// line delta is 0, 1, or 2, both columns fit in 7 bits, and the
// instruction stays within the same physical line.
//
// CPython: Python/assemble.c:L246 write_location_info_oneline_form
func writeLocOneline(buf []byte, length, lineDelta, column, endColumn int) []byte {
	buf = writeEntryStart(buf, locOneLine0+lineDelta, length)
	buf = append(buf, byte(column))
	return append(buf, byte(endColumn))
}

// writeLocLong emits the worst-case long-form record: signed line
// delta varint, end-line delta varint, then column+1 varints (the +1
// is so unset (-1) becomes 0).
//
// CPython: Python/assemble.c:L258 write_location_info_long_form
func writeLocLong(buf []byte, loc ast.Pos, lineCursor, length int) []byte {
	buf = writeEntryStart(buf, locLong, length)
	buf = writeSignedVarint(buf, int32(loc.Lineno-lineCursor))
	buf = writeVarint(buf, uint32(loc.EndLineno-loc.Lineno))
	buf = writeVarint(buf, uint32(loc.ColOffset+1))
	buf = writeVarint(buf, uint32(loc.EndColOffset+1))
	return buf
}

// writeLocNone emits the no-location record. The cursor is not
// advanced.
//
// CPython: Python/assemble.c:L269 write_location_info_none
func writeLocNone(buf []byte, length int) []byte {
	return writeEntryStart(buf, locNone, length)
}

// writeLocNoColumn emits the no-column record. Only a signed line
// delta varint follows the entry start.
//
// CPython: Python/assemble.c:L275 write_location_info_no_column
func writeLocNoColumn(buf []byte, length, lineDelta int) []byte {
	buf = writeEntryStart(buf, locNoCols, length)
	return writeSignedVarint(buf, int32(lineDelta))
}

// writeLocEntry picks the smallest representation for one location
// span and appends it to buf. Returns the new buffer and the updated
// line cursor (CPython advances a_lineno after every form except
// none).
//
// CPython: Python/assemble.c:L285 write_location_info_entry
func writeLocEntry(buf []byte, loc ast.Pos, lineCursor, length int) ([]byte, int) {
	if loc.Lineno == noLineno {
		return writeLocNone(buf, length), lineCursor
	}
	lineDelta := loc.Lineno - lineCursor
	col := loc.ColOffset
	endCol := loc.EndColOffset
	if col < 0 || endCol < 0 {
		if loc.EndLineno == loc.Lineno || loc.EndLineno < 0 {
			return writeLocNoColumn(buf, length, lineDelta), loc.Lineno
		}
	} else if loc.EndLineno == loc.Lineno {
		if lineDelta == 0 && col < 80 && endCol-col < 16 && endCol >= col {
			return writeLocShort(buf, length, col, endCol), lineCursor
		}
		if lineDelta >= 0 && lineDelta < 3 && col < 128 && endCol < 128 {
			return writeLocOneline(buf, length, lineDelta, col, endCol), loc.Lineno
		}
	}
	return writeLocLong(buf, loc, lineCursor, length), loc.Lineno
}

// emitLocation appends one or more entries covering a span of
// codeunits. Spans longer than 8 codeunits split into 8-codeunit
// chunks because the entry-start byte only encodes (length-1) in 3
// bits.
//
// CPython: Python/assemble.c:L323 assemble_emit_location
func emitLocation(buf []byte, loc ast.Pos, lineCursor, length int) ([]byte, int) {
	if length == 0 {
		return buf, lineCursor
	}
	for length > 8 {
		buf, lineCursor = writeLocEntry(buf, loc, lineCursor, 8)
		length -= 8
	}
	return writeLocEntry(buf, loc, lineCursor, length)
}

// assembleLineTable walks the post-flowgraph instruction stream,
// coalesces adjacent same-location runs, and emits the location table
// in the post-PEP-626 4-bit-code format. Returns the encoded bytes.
// Callers pass the already-built code-stream offsets via the
// codeunits-per-instruction count from emitInstr.
//
// CPython: Python/assemble.c:L336 assemble_location_info
func assembleLineTable(seq *Sequence, firstLineno int) []byte {
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
		if !sameLoc(loc, ins.Loc) {
			buf, lineCursor = emitLocation(buf, loc, lineCursor, size)
			loc = ins.Loc
			size = 0
		}
		size += instrCodeunits(ins)
	}
	buf, _ = emitLocation(buf, loc, lineCursor, size)
	return buf
}

// sameLoc compares two ast.Pos values by every field. Mirrors
// same_location in assemble.c, which is a memcmp on the location
// struct.
//
// CPython: Python/assemble.c same_location
func sameLoc(a, b ast.Pos) bool {
	return a.Lineno == b.Lineno && a.EndLineno == b.EndLineno &&
		a.ColOffset == b.ColOffset && a.EndColOffset == b.EndColOffset
}

// instrCodeunits returns the number of 16-bit code units one
// instruction occupies in the final byte stream: one for the opcode +
// oparg, plus one EXTENDED_ARG prefix per non-zero high byte of the
// oparg. CACHE entries are not yet emitted (the v0.5 pipeline does
// not run the specializer).
//
// CPython: Python/assemble.c instr_size
func instrCodeunits(ins *Instr) int {
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
