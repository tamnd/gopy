// Package hashtable is the gopy port of cpython/Python/hashtable.c. It
// implements _Py_hashtable_t, the generic open-chained hash table the
// runtime uses for tracemalloc, the type method cache, the .pyc rdep
// tracker, and similar internal indexes. It is *not* the Python dict.
//
// The layout matches CPython byte-for-byte: a power-of-two bucket
// array of singly-linked entry chains, with rehash-on-load thresholds
// at HASHTABLE_HIGH (50%) and HASHTABLE_LOW (10%).
//
// CPython: Python/hashtable.c
package hashtable

import (
	"math/bits"
	"unsafe"
)

// initialSize is the smallest bucket count the table ever has.
//
// CPython: Python/hashtable.c:51 HASHTABLE_MIN_SIZE
const initialSize = 16

// highLoad and lowLoad are the rehash thresholds. Set crosses high to
// grow, Steal crosses low to shrink. rehashFactor sizes the new
// bucket array so the load lands near the midpoint.
//
// CPython: Python/hashtable.c:52 HASHTABLE_HIGH / HASHTABLE_LOW / HASHTABLE_REHASH_FACTOR
const (
	highLoad     = 0.50
	lowLoad      = 0.10
	rehashFactor = 2.0 / (lowLoad + highLoad)
)

// HashFunc returns the 64-bit hash of key. Mirrors
// _Py_hashtable_hash_func.
//
// CPython: Include/internal/pycore_hashtable.h:45 _Py_hashtable_hash_func
type HashFunc func(key any) uint64

// CompareFunc returns true when a and b are the same key. Mirrors
// _Py_hashtable_compare_func (which returns int).
//
// CPython: Include/internal/pycore_hashtable.h:46 _Py_hashtable_compare_func
type CompareFunc func(a, b any) bool

// DestroyFunc runs at entry teardown. Mirrors
// _Py_hashtable_destroy_func.
//
// CPython: Include/internal/pycore_hashtable.h:47 _Py_hashtable_destroy_func
type DestroyFunc func(value any)

// Entry is one (key, value) record in the table. next links to the
// rest of the bucket chain. CPython embeds an _Py_slist_item_t at
// offset 0; we use a plain pointer because Go's GC tracks it.
//
// CPython: Include/internal/pycore_hashtable.h:28 _Py_hashtable_entry_t
type Entry struct {
	Key     any
	Value   any
	KeyHash uint64
	next    *Entry
}

// Table is the runtime hash table. nentries and nbuckets are kept in
// step with the buckets slice. The function pointers cannot be nil
// after construction; New / NewFull install defaults if the caller
// passes nil for the optional ones.
//
// CPython: Include/internal/pycore_hashtable.h:60 _Py_hashtable_t
type Table struct {
	nentries int
	nbuckets int
	buckets  []*Entry

	hash    HashFunc
	cmp     CompareFunc
	keyDtor DestroyFunc
	valDtor DestroyFunc
}

// New builds a table with default destructors and the default
// allocator. Mirrors _Py_hashtable_new.
//
// CPython: Python/hashtable.c:370 _Py_hashtable_new
func New(hash HashFunc, cmp CompareFunc) *Table {
	return NewFull(hash, cmp, nil, nil)
}

// NewFull builds a table with optional key / value destructors. Pass
// nil for either dtor to skip teardown work for that side. Mirrors
// _Py_hashtable_new_full; the allocator argument is dropped because
// Go's runtime owns memory.
//
// CPython: Python/hashtable.c:323 _Py_hashtable_new_full
func NewFull(hash HashFunc, cmp CompareFunc, keyDtor, valDtor DestroyFunc) *Table {
	t := &Table{
		nbuckets: initialSize,
		buckets:  make([]*Entry, initialSize),
		hash:     hash,
		cmp:      cmp,
		keyDtor:  keyDtor,
		valDtor:  valDtor,
	}
	return t
}

// HashPointer mirrors _Py_hashtable_hash_ptr: cast the address to a
// uintptr and rotate so low alignment bits do not crowd the bucket.
//
// CPython: Python/hashtable.c:93 _Py_hashtable_hash_ptr
func HashPointer(p unsafe.Pointer) uint64 {
	y := uint64(uintptr(p))
	return bits.RotateLeft64(y, -4)
}

// CompareDirect mirrors _Py_hashtable_compare_direct: pointer
// equality. Used with HashPointer for tables keyed on raw addresses.
//
// CPython: Python/hashtable.c:100 _Py_hashtable_compare_direct
func CompareDirect(a, b any) bool { return a == b }

// Len returns the number of entries.
//
// CPython: Python/hashtable.c:133 _Py_hashtable_len
func (t *Table) Len() int { return t.nentries }

// Size returns the rough memory footprint of the table in bytes,
// matching _Py_hashtable_size's accounting (table header plus bucket
// array plus entries).
//
// CPython: Python/hashtable.c:121 _Py_hashtable_size
func (t *Table) Size() int {
	var hdr Table
	var bp *Entry
	var ent Entry
	return int(unsafe.Sizeof(hdr)) +
		t.nbuckets*int(unsafe.Sizeof(bp)) +
		t.nentries*int(unsafe.Sizeof(ent))
}

// getEntry returns the entry whose key matches, or nil if the key is
// not present. Mirrors _Py_hashtable_get_entry_generic. The
// _Py_hashtable_get_entry_ptr specialisation collapses into this
// path because Go interface dispatch already covers the fast case.
//
// CPython: Python/hashtable.c:140 _Py_hashtable_get_entry_generic
func (t *Table) getEntry(key any) *Entry {
	keyHash := t.hash(key)
	idx := keyHash & uint64(t.nbuckets-1)
	for e := t.buckets[idx]; e != nil; e = e.next {
		if e.KeyHash == keyHash && t.cmp(key, e.Key) {
			return e
		}
	}
	return nil
}

// Get returns the value for key. found==false means key is not in
// the table.
//
// CPython: Python/hashtable.c:255 _Py_hashtable_get
func (t *Table) Get(key any) (any, bool) {
	e := t.getEntry(key)
	if e == nil {
		return nil, false
	}
	return e.Value, true
}

// Set inserts or replaces key. CPython asserts the key is absent
// (the C version is "set or die"); we mirror by replacing in place
// when an entry already exists, which keeps the surface usable from
// Go without breaking the byte-for-byte shape of the rehash logic.
//
// CPython: Python/hashtable.c:217 _Py_hashtable_set
func (t *Table) Set(key, val any) {
	if existing := t.getEntry(key); existing != nil {
		existing.Value = val
		return
	}

	e := &Entry{
		Key:     key,
		Value:   val,
		KeyHash: t.hash(key),
	}
	t.nentries++
	if float64(t.nentries)/float64(t.nbuckets) > highLoad {
		t.rehash()
	}
	idx := e.KeyHash & uint64(t.nbuckets-1)
	e.next = t.buckets[idx]
	t.buckets[idx] = e
}

// Steal removes key and returns its value without invoking the
// destructor. found==false leaves the table unchanged.
//
// CPython: Python/hashtable.c:182 _Py_hashtable_steal
func (t *Table) Steal(key any) (any, bool) {
	keyHash := t.hash(key)
	idx := keyHash & uint64(t.nbuckets-1)
	var prev *Entry
	for e := t.buckets[idx]; e != nil; e = e.next {
		if e.KeyHash == keyHash && t.cmp(key, e.Key) {
			if prev == nil {
				t.buckets[idx] = e.next
			} else {
				prev.next = e.next
			}
			t.nentries--
			val := e.Value
			if float64(t.nentries)/float64(t.nbuckets) < lowLoad {
				t.rehash()
			}
			return val, true
		}
		prev = e
	}
	return nil, false
}

// Foreach calls fn on every entry. Returning a non-nil error from fn
// stops iteration and propagates the error. Bucket order matches
// CPython.
//
// CPython: Python/hashtable.c:268 _Py_hashtable_foreach
func (t *Table) Foreach(fn func(*Entry) error) error {
	for _, head := range t.buckets {
		for e := head; e != nil; e = e.next {
			if err := fn(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// Clear removes every entry, invoking the destructors, and shrinks
// the bucket array back toward the minimum.
//
// CPython: Python/hashtable.c:392 _Py_hashtable_clear
func (t *Table) Clear() {
	for i, head := range t.buckets {
		for e := head; e != nil; e = e.next {
			t.destroyEntry(e)
		}
		t.buckets[i] = nil
	}
	t.nentries = 0
	t.rehash()
}

// Destroy tears down every entry (running destructors) and drops the
// bucket array. The table value is unusable after Destroy returns.
//
// CPython: Python/hashtable.c:411 _Py_hashtable_destroy
func (t *Table) Destroy() {
	for _, head := range t.buckets {
		for e := head; e != nil; e = e.next {
			t.destroyEntry(e)
		}
	}
	t.buckets = nil
	t.nentries = 0
	t.nbuckets = 0
}

func (t *Table) destroyEntry(e *Entry) {
	if t.keyDtor != nil {
		t.keyDtor(e.Key)
	}
	if t.valDtor != nil {
		t.valDtor(e.Value)
	}
}

// roundSize rounds s up to a power of two, with a floor of
// initialSize. Mirrors round_size in hashtable.c.
//
// CPython: Python/hashtable.c:108 round_size
func roundSize(s int) int {
	if s < initialSize {
		return initialSize
	}
	i := 1
	for i < s {
		i <<= 1
	}
	return i
}

// rehash resizes the bucket array to keep the load near the
// midpoint of [lowLoad, highLoad]. Called from Set on overflow,
// Steal on underflow, and Clear after a wipe.
//
// CPython: Python/hashtable.c:287 hashtable_rehash
func (t *Table) rehash() {
	newSize := roundSize(int(float64(t.nentries) * rehashFactor))
	if newSize == t.nbuckets {
		return
	}
	newBuckets := make([]*Entry, newSize)
	for _, head := range t.buckets {
		for e := head; e != nil; {
			next := e.next
			idx := e.KeyHash & uint64(newSize-1)
			e.next = newBuckets[idx]
			newBuckets[idx] = e
			e = next
		}
	}
	t.buckets = newBuckets
	t.nbuckets = newSize
}
