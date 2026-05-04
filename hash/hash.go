package hash

import (
	"encoding/binary"
	"math"
	"math/bits"
	"unsafe"
)

// HashBits mirrors PyHASH_BITS on 64-bit builds: the exponent of the
// Mersenne prime modulus used by numeric hashes.
//
// CPython: Include/cpython/pyhash.h:L13 PyHASH_BITS
const HashBits = 61

// HashModulus mirrors PyHASH_MODULUS = 2**HashBits - 1.
//
// CPython: Include/cpython/pyhash.h:L18 PyHASH_MODULUS
const HashModulus = (uint64(1) << HashBits) - 1

// HashInf mirrors PyHASH_INF, the hash of +inf.
//
// CPython: Include/cpython/pyhash.h:L19 PyHASH_INF
const HashInf int64 = 314159

// HashImag mirrors PyHASH_IMAG, the multiplier for the imaginary part
// of complex numbers.
//
// CPython: Include/cpython/pyhash.h:L20 PyHASH_IMAG
const HashImag int64 = hashMultiplier

// siphashKeys returns (k0, k1) read from Secret as little-endian
// uint64s. This mirrors `_le64toh(_Py_HashSecret.siphash.k0)`.
//
// CPython: Python/pyhash.c:L482 pysiphash
func siphashKeys() (k0, k1 uint64) {
	return binary.LittleEndian.Uint64(Secret[0:8]),
		binary.LittleEndian.Uint64(Secret[8:16])
}

// fnvKeys returns (prefix, suffix) read from Secret. CPython treats
// these as native-endian Py_hash_t; gopy's runtime targets are
// little-endian so the LE decode produces the same bytes.
//
// CPython: Python/pyhash.c:L265 fnv (prefix/suffix load)
func fnvKeys() (prefix, suffix uint64) {
	return binary.LittleEndian.Uint64(Secret[0:8]),
		binary.LittleEndian.Uint64(Secret[8:16])
}

// Buffer mirrors Py_HashBuffer. Returns the hash of src under the
// runtime hash secret, with the -1 sentinel remapped to -2.
//
// CPython: Python/pyhash.c:L148 Py_HashBuffer
func Buffer(src []byte) int64 {
	if len(src) == 0 {
		return 0
	}
	k0, k1 := siphashKeys()
	x := int64(siphash13(k0, k1, src))
	if x == -1 {
		return -2
	}
	return x
}

// BufferFNV mirrors the FNV path of Py_HashBuffer for builds that
// select Py_HASH_FNV. Default builds use Buffer; this stays callable
// for parity tests.
//
// CPython: Python/pyhash.c:L148 Py_HashBuffer (Py_HASH_FNV branch)
func BufferFNV(src []byte) int64 {
	if len(src) == 0 {
		return 0
	}
	prefix, suffix := fnvKeys()
	x := int64(fnv(prefix, suffix, src))
	if x == -1 {
		return -2
	}
	return x
}

// FuncDef mirrors PyHash_FuncDef. Describes the active hash algorithm.
//
// CPython: Include/cpython/pyhash.h:L34 PyHash_FuncDef
type FuncDef struct {
	Name     string
	HashBits int
	SeedBits int
}

// GetFuncDef mirrors PyHash_GetFuncDef. The active algorithm is
// SipHash-1-3, matching CPython 3.14's default build.
//
// CPython: Python/pyhash.c:L214 PyHash_GetFuncDef
func GetFuncDef() FuncDef {
	return FuncDef{Name: "siphash13", HashBits: 64, SeedBits: 128}
}

// Pointer mirrors Py_HashPointer. Rotates the address right by 4 to
// dilute alignment patterns, then remaps the -1 sentinel.
//
// CPython: Python/pyhash.c:L132 Py_HashPointer
func Pointer(p unsafe.Pointer) int64 {
	y := uint(uintptr(p))
	y = bits.RotateLeft(y, -4)
	h := int64(y)
	if h == -1 {
		return -2
	}
	return h
}

// Double mirrors _Py_HashDouble. Reduces v modulo HashModulus following
// the strategy documented in pyhash.c. NaNs return the pointer hash of
// inst (left to the caller); pass 0 for the NaN-fallback to receive 0
// and let the type's tp_hash supply identity.
//
// CPython: Python/pyhash.c:L86 _Py_HashDouble
func Double(v float64, nanFallback int64) int64 {
	if math.IsInf(v, 0) {
		if v > 0 {
			return HashInf
		}
		return -HashInf
	}
	if math.IsNaN(v) {
		return nanFallback
	}

	m, e := math.Frexp(v)
	sign := int64(1)
	if m < 0 {
		sign = -1
		m = -m
	}

	var x uint64
	for m != 0 {
		x = ((x << 28) & HashModulus) | x>>(HashBits-28)
		m *= 268435456.0 // 2**28
		e -= 28
		y := uint64(m)
		m -= float64(y)
		x += y
		if x >= HashModulus {
			x -= HashModulus
		}
	}

	switch {
	case e >= 0:
		e %= HashBits
	default:
		e = HashBits - 1 - ((-1 - e) % HashBits)
	}
	x = ((x << e) & HashModulus) | x>>(HashBits-e)

	h := int64(x) * sign
	if h == -1 {
		h = -2
	}
	return h
}
