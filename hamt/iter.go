// Hamt depth-first iterator. Mirrors the PyHamtIteratorState walk in
// cpython/Python/hamt.c. We use an explicit per-level stack rather
// than recursion so iteration is allocation-free after the initial
// Iter() call.
//
// CPython: Python/hamt.c:2059 Iterators: Machinery

package hamt

import "github.com/tamnd/gopy/objects"

// Iter walks a Hamt in tree order. It is safe to call Iter multiple
// times on the same Hamt; each call returns a fresh iterator.
type Iter struct {
	nodes [MaxTreeDepth]node
	pos   [MaxTreeDepth]int
	level int8
}

// Iter returns a new iterator positioned at the start of the tree.
//
// CPython: Python/hamt.c:2066 hamt_iterator_init
func (h *Hamt) Iter() *Iter {
	it := &Iter{}
	it.nodes[0] = h.root
	return it
}

// Next yields the next (key, val) pair. ok==false signals
// I_END. err is non-nil only on hashing/comparison failure during
// iteration (none today, but plumbed for parity with CPython).
//
// CPython: Python/hamt.c:2182 hamt_iterator_next
func (it *Iter) Next() (key, val objects.Object, ok bool, err error) {
	for {
		if it.level < 0 {
			return nil, nil, false, nil
		}
		current := it.nodes[it.level]
		switch n := current.(type) {
		case *bitmapNode:
			if k, v, advanced, ok := it.bitmapStep(n); advanced {
				if ok {
					return k, v, true, nil
				}
				continue
			}
		case *arrayNode:
			if advanced := it.arrayStep(n); advanced {
				continue
			}
		case *collisionNode:
			if k, v, advanced, ok := it.collisionStep(n); advanced {
				if ok {
					return k, v, true, nil
				}
				continue
			}
		}
		// step returned advanced==false: pop a level.
		it.level--
	}
}

// bitmapStep advances the iterator one slot in a bitmap node. The
// (advanced, ok) pair encodes three outcomes:
//
//	(true, true)  -- a (k, v) pair is ready, return it.
//	(true, false) -- we descended into a sub-node; recur.
//	(false, _)    -- the node is exhausted, pop a level.
//
// CPython: Python/hamt.c:2080 hamt_iterator_bitmap_next
func (it *Iter) bitmapStep(n *bitmapNode) (key, val objects.Object, advanced, ok bool) {
	level := it.level
	pos := it.pos[level]
	if pos+1 >= len(n.array) {
		return nil, nil, false, false
	}
	if n.array[pos] == nil {
		it.pos[level] = pos + 2
		next := level + 1
		it.level = next
		it.pos[next] = 0
		it.nodes[next] = n.array[pos+1].(node)
		return nil, nil, true, false
	}
	k := n.array[pos]
	v := n.array[pos+1]
	it.pos[level] = pos + 2
	return k, v, true, true
}

// arrayStep walks the next non-nil child slot. Returns true when it
// descended (so the outer loop recurs), false when the node is
// exhausted.
//
// CPython: Python/hamt.c:2141 hamt_iterator_array_next
func (it *Iter) arrayStep(n *arrayNode) bool {
	level := it.level
	pos := it.pos[level]
	if pos >= arrayNodeSize {
		return false
	}
	for i := pos; i < arrayNodeSize; i++ {
		if n.children[i] == nil {
			continue
		}
		it.pos[level] = i + 1
		next := level + 1
		it.pos[next] = 0
		it.nodes[next] = n.children[i]
		it.level = next
		return true
	}
	return false
}

// collisionStep yields the next pair from a collision node. Same
// signature as bitmapStep.
//
// CPython: Python/hamt.c:2117 hamt_iterator_collision_next
func (it *Iter) collisionStep(n *collisionNode) (key, val objects.Object, advanced, ok bool) {
	level := it.level
	pos := it.pos[level]
	if pos+1 >= len(n.array) {
		return nil, nil, false, false
	}
	k := n.array[pos]
	v := n.array[pos+1]
	it.pos[level] = pos + 2
	return k, v, true, true
}
