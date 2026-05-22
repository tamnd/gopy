// Package cjkcodecs is the gopy port of CPython's Modules/cjkcodecs/.
// The runtime is the synchronous encode/decode path from
// multibytecodec.c. The mapping tables live in mappings_*_gen.go,
// emitted by tools/cjkcodecs_go from the CPython auto-generated
// headers (so the byte layout matches CPython byte-for-byte).
//
// CPython: Modules/cjkcodecs/multibytecodec.c
// CPython: Modules/cjkcodecs/multibytecodec.h
// CPython: Modules/cjkcodecs/cjkcodecs.h
package cjkcodecs

// MBERR_* sentinels returned by codec encode/decode callbacks.
// Mirrors Modules/cjkcodecs/multibytecodec.h:122.
//
//nolint:revive // ALL_CAPS names mirror Modules/cjkcodecs/multibytecodec.h.
const (
	MBERR_TOOSMALL  = -1
	MBERR_TOOFEW    = -2
	MBERR_INTERNAL  = -3
	MBERR_EXCEPTION = -4

	MBENC_FLUSH = 0x0001
	MBENC_RESET = 0x0002
)

// Sentinel codepoints/values used by the mapping tables.
// CPython: Modules/cjkcodecs/cjkcodecs.h:18
const (
	UNIINV = 0xFFFE
	NOCHAR = 0xFFFF
	MULTIC = 0xFFFE
	DBCINV = 0xFFFD
)

// dbcsIndex is the per-leading-byte decode dispatch row.
// Map points into the codec's flat ucs2_t decode array; Bottom and
// Top frame the inclusive trailing-byte window for which Map holds a
// codepoint.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:32 struct dbcs_index
type dbcsIndex struct {
	Map         []uint16
	Bottom, Top uint8
}

// wideDBCSIndex is the same shape but with Py_UCS4 entries, used by
// the JISX0213 pair decode map.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:38 struct widedbcs_index
type wideDBCSIndex struct {
	Map         []uint32
	Bottom, Top uint8
}

// unimIndex is the per-row encode dispatch (uni>>8 selects the row,
// uni&0xff indexes into Map after subtracting Bottom).
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:44 struct unim_index
type unimIndex struct {
	Map         []uint16
	Bottom, Top uint8
}

// pairEncodeMap holds one (uniseq, code) pair for the JISX0213 wide
// encode tables.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:61 struct pair_encodemap
type pairEncodeMap struct {
	UniSeq uint32
	Code   uint16
}

// tryMapDec looks up trailing byte c2 in the dispatch row m. Returns
// the mapped codepoint and true when the slot is populated and not
// UNIINV.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:221 _TRYMAP_DEC
func tryMapDec(m *dbcsIndex, c2 uint8) (uint16, bool) {
	if m.Map == nil {
		return 0, false
	}
	if c2 < m.Bottom || c2 > m.Top {
		return 0, false
	}
	v := m.Map[int(c2)-int(m.Bottom)]
	if v == UNIINV {
		return 0, false
	}
	return v, true
}

// tryMapWideDec is the Py_UCS4 sibling of tryMapDec.
func tryMapWideDec(m *wideDBCSIndex, c2 uint8) (uint32, bool) {
	if m.Map == nil {
		return 0, false
	}
	if c2 < m.Bottom || c2 > m.Top {
		return 0, false
	}
	v := m.Map[int(c2)-int(m.Bottom)]
	if v == UNIINV {
		return 0, false
	}
	return v, true
}

// tryMapEnc looks up low byte val in the encode dispatch row m.
// Returns the mapped DBCHAR and true when the slot is populated and
// not NOCHAR.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:211 _TRYMAP_ENC
func tryMapEnc(m *unimIndex, val uint8) (uint16, bool) {
	if m.Map == nil {
		return 0, false
	}
	if val < m.Bottom || val > m.Top {
		return 0, false
	}
	v := m.Map[int(val)-int(m.Bottom)]
	if v == NOCHAR {
		return 0, false
	}
	return v, true
}

// findPairEnc binary-searches the pair encode table for (body,
// modifier). Returns DBCINV when no entry matches.
//
// CPython: Modules/cjkcodecs/cjkcodecs.h:407 find_pairencmap
func findPairEnc(body, modifier uint16, table []pairEncodeMap) uint16 {
	value := uint32(body)<<16 | uint32(modifier)
	lo := 0
	hi := len(table)
	if hi == 0 {
		return DBCINV
	}
	pos := hi >> 1
	for lo != hi {
		switch {
		case value < table[pos].UniSeq:
			if hi != pos {
				hi = pos
				pos = (lo + hi) >> 1
				continue
			}
		case value > table[pos].UniSeq:
			if lo != pos {
				lo = pos
				pos = (lo + hi) >> 1
				continue
			}
		}
		break
	}
	if value == table[pos].UniSeq {
		return table[pos].Code
	}
	return DBCINV
}
