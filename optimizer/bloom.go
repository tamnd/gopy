// Bloom filter over the type / dict / code pointers a trace's guards
// rely on. Trace projection adds every dependency to the executor's
// filter; when a watcher fires the runtime hashes the mutated pointer
// once and walks the executor list asking each filter whether the
// pointer might be in scope. False positives waste an invalidation;
// false negatives skip a needed one.
//
// CPython: Python/optimizer.c:1357-1414

package optimizer

import "unsafe"

// bloomK is the number of hash functions the filter applies. With m =
// 256 and K = 6, k/m ratios are around 2.3% per element which is fine
// for the small dependency sets a single trace tracks.
//
// CPython: Python/optimizer.c:1363 #define K 6
const bloomK = 6

// bloomSeed seeds the hash. Matches CPython byte for byte so gate
// fixtures recorded against CPython traces map to the same bits.
//
// CPython: Python/optimizer.c:1365 #define SEED
const bloomSeed = 20221211

// bloomMultiplier matches PyHASH_MULTIPLIER.
//
// CPython: Include/cpython/pyhash.h:6
const bloomMultiplier uint64 = 1000003

// addressToHash mixes the pointer's bytes with the multiplier-seed
// scheme CPython uses for its bloom hashes.
//
// CPython: Python/optimizer.c:1368-1379 address_to_hash
func addressToHash(ptr unsafe.Pointer) uint64 {
	if ptr == nil {
		return 0
	}
	hash := uint64(bloomSeed)
	addr := uintptr(ptr)
	for i := 0; i < int(unsafe.Sizeof(addr)); i++ {
		hash ^= uint64(addr & 0xFF)
		hash *= bloomMultiplier
		addr >>= 8
	}
	return hash
}

// Init zeroes every bit. Matches the manual loop CPython runs for
// shape-parity with the gate fixtures.
//
// CPython: Python/optimizer.c:1381-1387 _Py_BloomFilter_Init
func (b *BloomFilter) Init() {
	for i := 0; i < BloomFilterWords; i++ {
		b.Bits[i] = 0
	}
}

// Add hashes ptr and stamps bloomK bits across the filter. Each hash
// pulls 8 bits off the running 64-bit hash, picks the word from the
// top 3 bits and the bit-index from the bottom 5.
//
// CPython: Python/optimizer.c:1394-1404 _Py_BloomFilter_Add
func (b *BloomFilter) Add(ptr unsafe.Pointer) {
	hash := addressToHash(ptr)
	for i := 0; i < bloomK; i++ {
		bits := uint8(hash & 0xFF)
		b.Bits[bits>>5] |= 1 << (bits & 31)
		hash >>= 8
	}
}

// MayContain reports whether every bit set in hashes is also set in
// b. False positives are intentional; the filter exists to keep the
// invalidation walk O(1) per executor in the common case.
//
// CPython: Python/optimizer.c:1406-1415 bloom_filter_may_contain
func (b *BloomFilter) MayContain(hashes *BloomFilter) bool {
	for i := 0; i < BloomFilterWords; i++ {
		if (b.Bits[i] & hashes.Bits[i]) != hashes.Bits[i] {
			return false
		}
	}
	return true
}
