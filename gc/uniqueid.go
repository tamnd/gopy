// Package gc port of Python/uniqueid.c. Allocates unique ids for
// per-thread reference counting and recycles them through a freelist
// when the owning object goes away.
//
// This file ports only the pool / freelist side: AssignUniqueID,
// ReleaseUniqueID, FinalizeUniqueIdPool, and the internal resize
// helper. The refcount-merge side (resize_local_refcounts,
// _PyObject_ThreadIncrefSlow, _PyObject_MergePerThreadRefcounts,
// _PyObject_FinalizePerThreadRefcounts, _PyObject_DisablePerThreadRefcounting,
// clear_unique_id) is deferred: it touches a per-thread refcount
// array and PyType / PyDict / PyCode internal fields that gopy has
// not modeled yet.
//
// CPython: Python/uniqueid.c (Py_GIL_DISABLED #ifdef block)

package gc

import (
	"github.com/tamnd/gopy/pysync"
)

// InvalidUniqueID is the sentinel returned when AssignUniqueID fails
// or when an object has not been assigned an id.
//
// CPython: Include/internal/pycore_uniqueid.h:28 _Py_INVALID_UNIQUE_ID
const InvalidUniqueID int64 = 0

// poolMinSize is the smallest table size resize_interp_type_id_pool
// will ever pick. Mirrors POOL_MIN_SIZE.
//
// CPython: Python/uniqueid.c:18 POOL_MIN_SIZE
const poolMinSize = 8

// uniqueIDEntry is one slot in the unique-id table. CPython uses a
// union of next-pointer (when free) and obj (when allocated); gopy
// keeps both fields and switches on freelistHead membership. Free
// entries chain through next as 1-based indices into table; 0 means
// "end of freelist" so we can keep next as a plain int without a
// separate sentinel.
//
// CPython: Include/internal/pycore_interp_structs.h:752-758 _Py_unique_id_entry
type uniqueIDEntry struct {
	next int
	obj  any
}

// UniqueIDPool is the per-interpreter pool of unique ids. The table
// is grown lazily; freelistHead chains the free slots through their
// next field as 1-based indices (0 == end of list).
//
// CPython: Include/internal/pycore_interp_structs.h:760-771 _Py_unique_id_pool
type UniqueIDPool struct {
	mu           pysync.Mutex
	table        []uniqueIDEntry
	freelistHead int
	size         int
}

// resize doubles the pool table (or jumps to poolMinSize on first
// grow) and threads the new tail entries onto the freelist. Caller
// must hold p.mu.
//
// CPython: Python/uniqueid.c:23-51 resize_interp_type_id_pool
func (p *UniqueIDPool) resize() {
	newSize := p.size * 2
	if newSize < poolMinSize {
		newSize = poolMinSize
	}
	newTable := make([]uniqueIDEntry, newSize)
	if p.table != nil {
		copy(newTable, p.table[:p.size])
	}
	start := p.size
	for i := start; i < newSize-1; i++ {
		newTable[i].next = i + 2 // 1-based: index i links to i+1, stored as i+2
	}
	newTable[newSize-1].next = 0
	p.table = newTable
	p.freelistHead = start + 1 // 1-based id of first new entry
	p.size = newSize
}

// AssignUniqueID pulls the head off the freelist, stores obj in the
// resulting slot, and returns the 1-based id. Returns InvalidUniqueID
// if the resize fails.
//
// CPython: Python/uniqueid.c:79-102 _PyObject_AssignUniqueID
func (p *UniqueIDPool) AssignUniqueID(obj any) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.freelistHead == 0 {
		p.resize()
	}
	id := p.freelistHead
	entry := &p.table[id-1]
	p.freelistHead = entry.next
	entry.next = 0
	entry.obj = obj
	return int64(id)
}

// ReleaseUniqueID returns id to the freelist. The caller is
// responsible for ensuring nothing still holds id.
//
// CPython: Python/uniqueid.c:104-117 _PyObject_ReleaseUniqueID
func (p *UniqueIDPool) ReleaseUniqueID(id int64) {
	if id <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if int(id) > p.size {
		return
	}
	entry := &p.table[id-1]
	entry.obj = nil
	entry.next = p.freelistHead
	p.freelistHead = int(id)
}

// Lookup returns the object currently associated with id, or nil
// when the slot is free or out of range. No CPython analog, but
// useful for the refcount-merge port that lands later.
func (p *UniqueIDPool) Lookup(id int64) any {
	if id <= 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if int(id) > p.size {
		return nil
	}
	return p.table[id-1].obj
}

// Size returns the current table size. Used by the per-thread
// refcount layer to right-size its values array.
//
// CPython: read of _Py_atomic_load_ssize(&pool->size)
func (p *UniqueIDPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}

// Finalize releases the pool's storage. Allocated entries have their
// obj cleared so the interpreter teardown does not leave dangling
// references in the pool table.
//
// CPython: Python/uniqueid.c:203-230 _PyObject_FinalizeUniqueIdPool
func (p *UniqueIDPool) Finalize() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Walk the freelist first so we can tell allocated vs free entries
	// after this loop: free entries have obj == nil already, and
	// allocated entries are everything else.
	for p.freelistHead != 0 {
		next := p.table[p.freelistHead-1].next
		p.table[p.freelistHead-1].obj = nil
		p.freelistHead = next
	}
	for i := 0; i < p.size; i++ {
		p.table[i].obj = nil
	}
	p.table = nil
	p.freelistHead = 0
	p.size = 0
}
