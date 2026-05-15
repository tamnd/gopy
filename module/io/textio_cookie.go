// The TextIOWrapper tell/seek cookie. CPython packs five integer
// fields into a single Python int, little-endian, so a mid-stream
// tell returns an opaque number that a later seek can round-trip back
// into the exact decode state. Without the cookie, tell raises any time
// decoded characters are buffered ahead of the caller.
//
// CPython: Modules/_io/textio.c:2387 cookie_type and the surrounding
// build_cookie / parse_cookie helpers.

package io

import (
	"fmt"
	"math/big"
)

// cookie is the in-memory shape. The packed form is laid out as:
//
//	bytes [0:8]   start_pos     (uint64 little-endian)
//	bytes [8:12]  dec_flags     (uint32 little-endian)
//	bytes [12:16] bytes_to_feed (uint32 little-endian)
//	bytes [16:20] chars_to_skip (uint32 little-endian)
//	byte  [20]    need_eof      (0 or 1)
//
// CPython: Modules/_io/textio.c:2387 cookie_type
type cookie struct {
	StartPos    int64
	DecFlags    int32
	BytesToFeed int32
	CharsToSkip int32
	NeedEOF     bool
}

const cookieSize = 21

// packCookie writes the cookie out as a big-endian *big.Int built from
// little-endian bytes, matching CPython's `_PyLong_FromByteArray(..., 1,
// 1)` call (little-endian, signed=0). The resulting integer is positive
// because the MSB (byte 20 = need_eof) is always 0 or 1.
//
// CPython: Modules/_io/textio.c:2722 textiowrapper_build_cookie
func packCookie(c cookie) *big.Int {
	buf := make([]byte, cookieSize)
	u := uint64(c.StartPos)
	for i := 0; i < 8; i++ {
		buf[i] = byte(u >> (8 * i))
	}
	df := uint32(c.DecFlags)
	for i := 0; i < 4; i++ {
		buf[8+i] = byte(df >> (8 * i))
	}
	btf := uint32(c.BytesToFeed)
	for i := 0; i < 4; i++ {
		buf[12+i] = byte(btf >> (8 * i))
	}
	cts := uint32(c.CharsToSkip)
	for i := 0; i < 4; i++ {
		buf[16+i] = byte(cts >> (8 * i))
	}
	if c.NeedEOF {
		buf[20] = 1
	}
	// big.Int expects big-endian, so reverse.
	be := make([]byte, cookieSize)
	for i, b := range buf {
		be[cookieSize-1-i] = b
	}
	return new(big.Int).SetBytes(be)
}

// parseCookie reverses packCookie. Negative cookies are rejected to
// match CPython's `_PyLong_AsByteArray(..., 1, 0)` (little-endian,
// signed=0) which raises OverflowError on negative.
//
// CPython: Modules/_io/textio.c:2629 textiowrapper_parse_cookie
func parseCookie(v *big.Int) (cookie, error) {
	if v.Sign() < 0 {
		return cookie{}, fmt.Errorf("ValueError: negative seek position %s", v.String())
	}
	be := v.Bytes()
	if len(be) > cookieSize {
		return cookie{}, fmt.Errorf("ValueError: cookie is too large")
	}
	buf := make([]byte, cookieSize)
	for i, b := range be {
		buf[len(be)-1-i] = b
	}
	var c cookie
	var u uint64
	for i := 0; i < 8; i++ {
		u |= uint64(buf[i]) << (8 * i)
	}
	c.StartPos = int64(u)
	var df uint32
	for i := 0; i < 4; i++ {
		df |= uint32(buf[8+i]) << (8 * i)
	}
	c.DecFlags = int32(df)
	var btf uint32
	for i := 0; i < 4; i++ {
		btf |= uint32(buf[12+i]) << (8 * i)
	}
	c.BytesToFeed = int32(btf)
	var cts uint32
	for i := 0; i < 4; i++ {
		cts |= uint32(buf[16+i]) << (8 * i)
	}
	c.CharsToSkip = int32(cts)
	c.NeedEOF = buf[20] != 0
	return c, nil
}
