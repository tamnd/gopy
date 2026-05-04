package hash

import (
	"encoding/binary"
	"math/bits"
)

// siphash13 is the 1-round-compress / 3-round-finalize variant of
// SipHash that CPython 3.14 uses by default. It is a verbatim port of
// the C implementation, with byte order fixed to little-endian (which
// is what _le64toh resolves to on every platform CPython supports for
// hash output).
//
// CPython: Python/pyhash.c:L370 siphash13
func siphash13(k0, k1 uint64, src []byte) uint64 {
	b := uint64(len(src)) << 56
	v0 := k0 ^ 0x736f6d6570736575
	v1 := k1 ^ 0x646f72616e646f6d
	v2 := k0 ^ 0x6c7967656e657261
	v3 := k1 ^ 0x7465646279746573

	for len(src) >= 8 {
		mi := binary.LittleEndian.Uint64(src)
		src = src[8:]
		v3 ^= mi
		v0, v1, v2, v3 = sipRound(v0, v1, v2, v3)
		v0 ^= mi
	}

	var t uint64
	switch len(src) {
	case 7:
		t |= uint64(src[6]) << 48
		fallthrough
	case 6:
		t |= uint64(src[5]) << 40
		fallthrough
	case 5:
		t |= uint64(src[4]) << 32
		fallthrough
	case 4:
		t |= uint64(binary.LittleEndian.Uint32(src[:4]))
	case 3:
		t |= uint64(src[2]) << 16
		fallthrough
	case 2:
		t |= uint64(src[1]) << 8
		fallthrough
	case 1:
		t |= uint64(src[0])
	}
	b |= t

	v3 ^= b
	v0, v1, v2, v3 = sipRound(v0, v1, v2, v3)
	v0 ^= b
	v2 ^= 0xff
	v0, v1, v2, v3 = sipRound(v0, v1, v2, v3)
	v0, v1, v2, v3 = sipRound(v0, v1, v2, v3)
	v0, v1, v2, v3 = sipRound(v0, v1, v2, v3)

	return (v0 ^ v1) ^ (v2 ^ v3)
}

// sipRound is one full SipHash round: two HALF_ROUNDs.
//
// CPython: Python/pyhash.c:L361 SINGLE_ROUND
func sipRound(v0, v1, v2, v3 uint64) (a, b, c, d uint64) {
	v0 += v1
	v2 += v3
	v1 = bits.RotateLeft64(v1, 13) ^ v0
	v3 = bits.RotateLeft64(v3, 16) ^ v2
	v0 = bits.RotateLeft64(v0, 32)

	v2 += v1
	v0 += v3
	v1 = bits.RotateLeft64(v1, 17) ^ v2
	v3 = bits.RotateLeft64(v3, 21) ^ v0
	v2 = bits.RotateLeft64(v2, 32)
	return v0, v1, v2, v3
}

// KeyedHash mirrors _Py_KeyedHash. SipHash with the second key set to 0.
//
// CPython: Python/pyhash.c:L471 _Py_KeyedHash
func KeyedHash(key uint64, src []byte) uint64 {
	return siphash13(key, 0, src)
}
