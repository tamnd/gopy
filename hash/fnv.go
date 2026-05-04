package hash

import "encoding/binary"

// hashMultiplier mirrors PyHASH_MULTIPLIER.
//
// CPython: Include/cpython/pyhash.h:L6 PyHASH_MULTIPLIER
const hashMultiplier = 1000003

// fnv mirrors the modified Fowler-Noll-Vo hash CPython falls back to
// when Py_HASH_ALGORITHM == Py_HASH_FNV. SipHash-1-3 is the default;
// this path is kept for parity and for builds that select FNV.
//
// CPython: Python/pyhash.c:L243 fnv
func fnv(prefix, suffix uint64, src []byte) uint64 {
	if len(src) == 0 {
		// _Py_HashBytes short-circuits empty inputs before reaching
		// the hash function, so fnv is never called with len==0 in
		// practice. We still guard for direct callers.
		return 0
	}

	const blockSize = 8
	remainder := len(src) % blockSize
	if remainder == 0 {
		remainder = blockSize
	}
	blocks := (len(src) - remainder) / blockSize

	x := prefix
	x ^= uint64(src[0]) << 7
	p := src
	for range blocks {
		block := binary.LittleEndian.Uint64(p)
		x = (hashMultiplier * x) ^ block
		p = p[blockSize:]
	}
	for i := range remainder {
		x = (hashMultiplier * x) ^ uint64(p[i])
	}
	x ^= uint64(len(src))
	x ^= suffix
	return x
}
