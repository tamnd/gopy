// PEP 626 / PEP 657 varint helpers for the location and exception
// tables. CPython encodes both with the same 6-bit varint: low six
// bits hold a payload chunk, bit 6 is a continuation marker, bit 7 is
// a "first byte of entry" marker for the exception table; the line
// table reuses the same low-level helper without the entry marker.
//
// CPython: Python/assemble.c:L196 write_location_info_varint and
// Python/assemble.c:L106 assemble_emit_exception_table_entry

package compile

// writeVarint appends an unsigned 6-bit varint to buf and returns the
// updated buffer. Each output byte holds 6 payload bits; bit 6 set
// means more bytes follow.
//
// CPython: Python/assemble.c:L211 write_varint
func writeVarint(buf []byte, v uint32) []byte {
	for v >= 0x40 {
		buf = append(buf, byte(v&0x3f)|0x40)
		v >>= 6
	}
	buf = append(buf, byte(v&0x3f))
	return buf
}

// writeSignedVarint zig-zag encodes a signed value: bit 0 is the sign,
//
// bits 1+ are the magnitude. Mirrors CPython's
// write_signed_varint.
//
// CPython: Python/assemble.c:L222 write_signed_varint
func writeSignedVarint(buf []byte, v int32) []byte {
	var u uint32
	if v < 0 {
		u = uint32(-v)<<1 | 1
	} else {
		u = uint32(v) << 1
	}
	return writeVarint(buf, u)
}
