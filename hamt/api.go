// Public Hamt API. Mirrors the surface declared in
// cpython/Include/internal/pycore_hamt.h.
//
// CPython: Include/internal/pycore_hamt.h

package hamt

import "github.com/tamnd/gopy/objects"

// Hamt is the immutable persistent mapping. The zero value is invalid;
// use New() to construct an empty mapping.
//
// CPython: Include/internal/pycore_hamt.h PyHamtObject
type Hamt struct {
	objects.Header
	root  node
	count int
}

// New returns a fresh empty Hamt. Each call returns a distinct
// header, but they all share the singleton empty bitmap as their root
// so the allocation is a single struct.
//
// CPython: Python/hamt.c:2390 hamt_alloc / Python/hamt.c:_PyHamt_New
func New() *Hamt {
	h := &Hamt{root: emptyBitmap}
	h.Init(HamtType)
	objects.Incref(emptyBitmap) // h owns +1 on its root (no-op: immortal)
	return h
}

// Assoc returns a new Hamt that has key bound to val. If key already
// maps to val (identity-equal value object) the receiver is returned
// unchanged.
//
// CPython: Python/hamt.c:2209 _PyHamt_Assoc
func (h *Hamt) Assoc(key, val objects.Object) (*Hamt, error) {
	keyHash, err := hamtHash(key)
	if err != nil {
		return nil, err
	}
	newRoot, addedLeaf, err := h.root.assoc(0, keyHash, key, val)
	if err != nil {
		return nil, err
	}
	if newRoot == h.root {
		objects.Decref(newRoot) // drop the Incref(self) from the unchanged path
		objects.Incref(h)       // _PyHamt_Assoc returns a new reference
		return h, nil
	}
	out := &Hamt{root: newRoot, count: h.count} // adopt owned newRoot
	if addedLeaf {
		out.count++
	}
	out.Init(HamtType)
	return out, nil
}

// Without returns a new Hamt with key removed and ok==true. If the
// key is absent, the receiver is returned unchanged with ok==false.
//
// CPython: Python/hamt.c:2246 _PyHamt_Without
func (h *Hamt) Without(key objects.Object) (*Hamt, bool, error) {
	keyHash, err := hamtHash(key)
	if err != nil {
		return nil, false, err
	}
	res, newRoot, err := h.root.without(0, keyHash, key)
	switch res {
	case wError:
		return nil, false, err
	case wNotFound:
		objects.Incref(h) // _PyHamt_Without returns a new reference
		return h, false, nil
	case wEmpty:
		return New(), true, nil
	case wNewNode:
		out := &Hamt{root: newRoot, count: h.count - 1} // adopt owned newRoot
		out.Init(HamtType)
		return out, true, nil
	default:
		panic("hamt: unknown without result")
	}
}

// Find returns the value bound to key. ok==false reports a missing
// key without an error; err is non-nil only on hashing or comparison
// failures.
//
// CPython: Python/hamt.c:2303 _PyHamt_Find
func (h *Hamt) Find(key objects.Object) (objects.Object, bool, error) {
	if h.count == 0 {
		return nil, false, nil
	}
	keyHash, err := hamtHash(key)
	if err != nil {
		return nil, false, err
	}
	return h.root.find(0, keyHash, key)
}

// Len returns the number of key/value pairs.
//
// CPython: Python/hamt.c:2384 _PyHamt_Len
func (h *Hamt) Len() int { return h.count }

// Eq reports whether two Hamts hold the same key/value pairs. It
// short-circuits on identity and on size mismatch, then walks the
// receiver in tree order and looks each key up in `other`.
//
// CPython: Python/hamt.c:2320 _PyHamt_Eq
func (h *Hamt) Eq(other *Hamt) (bool, error) {
	if h == other {
		return true, nil
	}
	if h.count != other.count {
		return false, nil
	}
	it := h.Iter()
	for {
		k, v, ok, err := it.Next()
		if err != nil {
			return false, err
		}
		if !ok {
			return true, nil
		}
		ov, found, err := other.Find(k)
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		eq, err := objects.RichCmpBool(v, ov, objects.CompareEQ)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
}
