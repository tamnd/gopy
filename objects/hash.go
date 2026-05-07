// Hash adapters that route bytes-shaped values (str, bytes,
// memoryview, ...) through the SipHash-1-3 dispatcher in the hash
// package. Centralizing this here keeps the per-type slot
// implementations from poking at the hash secret directly and
// guarantees every hashable buffer goes through the same routine.
//
// CPython: Python/pyhash.c:148 Py_HashBuffer

package objects

import "github.com/tamnd/gopy/hash"

// HashBytes returns the hash of src using the active hash algorithm
// (SipHash-1-3 in CPython 3.14 default builds). The hash secret is
// supplied by the hash package; the -1 sentinel is remapped to -2
// inside hash.Buffer so callers don't have to care.
//
// CPython: Python/pyhash.c:148 Py_HashBuffer
func HashBytes(src []byte) int64 {
	return hash.Buffer(src)
}

// HashString is the string-shaped wrapper around HashBytes. Used by
// str so the slot implementation reads as
// "hash this string", not "convert to []byte and call the buffer
// hasher".
//
// CPython: Objects/unicodeobject.c:11532 unicode_hash
func HashString(s string) int64 {
	return hash.Buffer([]byte(s))
}
