// Package hamt is the gopy port of cpython/Python/hamt.c. It is the
// immutable persistent mapping that backs Context (see contextvar/).
//
// The tree branches on 5 bits of a 32-bit hash per level so the
// maximum non-collision depth is 7. A level-7 collision node lifts the
// total maximum depth to 8 (MaxTreeDepth).
//
// CPython: Python/hamt.c
package hamt

import (
	"math/bits"

	"github.com/tamnd/gopy/objects"
)

// MaxTreeDepth caps an iterator's stack. CPython exposes the same
// constant as _Py_HAMT_MAX_TREE_DEPTH.
//
// CPython: Include/internal/pycore_hamt.h:21 _Py_HAMT_MAX_TREE_DEPTH
const MaxTreeDepth = 8

// arrayNodeSize is the fan-out of an arrayNode. 32 = 2**5.
//
// CPython: Python/hamt.c:313 HAMT_ARRAY_NODE_SIZE
const arrayNodeSize = 32

// bitmapPromoteThreshold is the bitmapNode population at which we
// switch to an arrayNode. CPython promotes when n >= 16, i.e. when the
// 17th entry would be inserted.
//
// CPython: Python/hamt.c:768 if (n >= 16)
const bitmapPromoteThreshold = 16

// withoutResult mirrors hamt_without_t. The four constants name the
// four cases the caller of without() must handle. The order does not
// matter; we follow CPython's enum order so switch statements line up.
//
// CPython: Python/hamt.c:302 hamt_without_t
type withoutResult int

const (
	wError withoutResult = iota
	wNotFound
	wEmpty
	wNewNode
)

// node is the internal interface for the three node shapes. The
// dispatch matches hamt_node_assoc / without / find in CPython.
type node interface {
	assoc(shift uint32, hash int32, key, val objects.Object) (newNode node, addedLeaf bool, err error)
	without(shift uint32, hash int32, key objects.Object) (res withoutResult, newNode node, err error)
	find(shift uint32, hash int32, key objects.Object) (val objects.Object, found bool, err error)
}

// bitmapNode is the default node. b_bitmap mirrors the CPython
// bitmap; b_array packs (key, val) pairs in the order their bits
// appear in the bitmap. A nil key indicates the slot points to a
// child node (stored in the value position).
//
// CPython: Python/hamt.c:316 PyHamtNode_Bitmap
type bitmapNode struct {
	bitmap uint32
	array  []objects.Object
}

// arrayNode is the dense 32-slot fan-out node. count tracks how many
// children are non-nil; it is updated by every clone. Iteration walks
// children in index order which matches CPython's emission order.
//
// CPython: Python/hamt.c:316 PyHamtNode_Array
type arrayNode struct {
	count    int
	children [arrayNodeSize]node
}

// collisionNode lives at level 7 (shift == 30 so the next 5-bit
// window would go off the top of the 32-bit hash). All keys in the
// node share the same 32-bit hash value.
//
// CPython: Python/hamt.c:325 PyHamtNode_Collision
type collisionNode struct {
	hash  int32
	array []objects.Object
}

// emptyBitmap is the singleton empty bitmap node. CPython caches the
// same instance; we cache to keep New() allocation-free for empty
// HAMTs.
//
// CPython: Python/hamt.c:498 _Py_SINGLETON(hamt_bitmap_node_empty)
var emptyBitmap = &bitmapNode{}

// hamtHash reduces a Python hash to 32 bits via XOR-fold. CPython
// pins this exact reducer so test fixtures can target specific tree
// shapes; do not change the formula.
//
// CPython: Python/hamt.c:392 hamt_hash
func hamtHash(o objects.Object) (int32, error) {
	h, err := objects.Hash(o)
	if err != nil {
		return 0, err
	}
	xored := int32(uint32(h&0xffffffff) ^ uint32(h>>32))
	if xored == -1 {
		return -2, nil
	}
	return xored, nil
}

// hamtMask returns the 5-bit slice of hash starting at shift.
//
// CPython: Python/hamt.c:428 hamt_mask
func hamtMask(hash int32, shift uint32) uint32 {
	return (uint32(hash) >> shift) & 0x1f
}

// hamtBitpos turns the 5-bit mask into the single-bit position.
//
// CPython: Python/hamt.c:434 hamt_bitpos
func hamtBitpos(hash int32, shift uint32) uint32 {
	return uint32(1) << hamtMask(hash, shift)
}

// hamtBitindex counts how many of the bits below `bit` are set in
// `bitmap`. That count is the entry's index in the packed bitmap
// array.
//
// CPython: Python/hamt.c:440 hamt_bitindex
func hamtBitindex(bitmap, bit uint32) uint32 {
	return uint32(bits.OnesCount32(bitmap & (bit - 1)))
}

///////////////////////////////// Bitmap node /////////////////////////

// newBitmap returns a bitmap node with `size` empty slots. size==0
// reuses the empty singleton.
//
// CPython: Python/hamt.c:489 hamt_node_bitmap_new
func newBitmap(size int) *bitmapNode {
	if size == 0 {
		return emptyBitmap
	}
	return &bitmapNode{array: make([]objects.Object, size)}
}

// count returns the number of (k, v) pairs in the bitmap node. CPython
// stores the same value as Py_SIZE(node)/2.
//
// CPython: Python/hamt.c:527 hamt_node_bitmap_count
func (b *bitmapNode) count() int {
	return len(b.array) / 2
}

// clone copies the bitmap and the slot array.
//
// CPython: Python/hamt.c:533 hamt_node_bitmap_clone
func (b *bitmapNode) clone() *bitmapNode {
	c := newBitmap(len(b.array))
	copy(c.array, b.array)
	c.bitmap = b.bitmap
	return c
}

// cloneWithout returns a copy of b with the slot for `bit` removed.
// The caller has already verified bit is set and the count is > 1.
//
// CPython: Python/hamt.c:554 hamt_node_bitmap_clone_without
func (b *bitmapNode) cloneWithout(bit uint32) *bitmapNode {
	c := newBitmap(len(b.array) - 2)
	idx := hamtBitindex(b.bitmap, bit)
	keyIdx := 2 * idx
	valIdx := keyIdx + 1
	for i := uint32(0); i < keyIdx; i++ {
		c.array[i] = b.array[i]
	}
	for i := valIdx + 1; i < uint32(len(b.array)); i++ {
		c.array[i-2] = b.array[i]
	}
	c.bitmap = b.bitmap & ^bit
	return c
}

// newBitmapOrCollision returns a node holding two key/value pairs
// that collided in the parent bitmap node. It promotes to a collision
// node only when the full 32-bit hashes are identical.
//
// CPython: Python/hamt.c:584 hamt_node_new_bitmap_or_collision
func newBitmapOrCollision(shift uint32, key1 objects.Object, val1 objects.Object, key2Hash int32, key2, val2 objects.Object) (node, error) {
	key1Hash, err := hamtHash(key1)
	if err != nil {
		return nil, err
	}
	if key1Hash == key2Hash {
		return &collisionNode{
			hash:  key1Hash,
			array: []objects.Object{key1, val1, key2, val2},
		}, nil
	}
	n := newBitmap(0)
	n2, _, err := n.assoc(shift, key1Hash, key1, val1)
	if err != nil {
		return nil, err
	}
	n3, _, err := n2.assoc(shift, key2Hash, key2, val2)
	if err != nil {
		return nil, err
	}
	return n3, nil
}

// assoc on a bitmap node. The four code paths (sub-node descent,
// equal-key replace, collision promotion, and the new-key insert /
// promote-to-array branch) line up with CPython 1:1.
//
// CPython: Python/hamt.c:642 hamt_node_bitmap_assoc
func (b *bitmapNode) assoc(shift uint32, hash int32, key, val objects.Object) (node, bool, error) {
	bit := hamtBitpos(hash, shift)
	idx := hamtBitindex(b.bitmap, bit)

	if b.bitmap&bit != 0 {
		return b.assocFilled(shift, hash, idx, key, val)
	}

	// Bit not set: a fresh slot in this node.
	n := bits.OnesCount32(b.bitmap)
	if n >= bitmapPromoteThreshold {
		jdx := hamtMask(hash, shift)
		newArr := &arrayNode{count: n + 1}

		empty := newBitmap(0)
		child, _, err := empty.assoc(shift+5, hash, key, val)
		if err != nil {
			return nil, false, err
		}
		newArr.children[jdx] = child

		// Re-distribute existing entries.
		j := 0
		for i := 0; i < arrayNodeSize; i++ {
			if (b.bitmap>>uint(i))&1 == 0 {
				continue
			}
			if b.array[j] == nil {
				newArr.children[i] = b.array[j+1].(node)
			} else {
				rehash, err := hamtHash(b.array[j])
				if err != nil {
					return nil, false, err
				}
				child, _, err := empty.assoc(shift+5, rehash, b.array[j], b.array[j+1])
				if err != nil {
					return nil, false, err
				}
				newArr.children[i] = child
			}
			j += 2
		}
		return newArr, true, nil
	}

	// Stay a bitmap; widen by two slots.
	keyIdx := 2 * idx
	valIdx := keyIdx + 1
	out := newBitmap(2 * (n + 1))
	for i := uint32(0); i < keyIdx; i++ {
		out.array[i] = b.array[i]
	}
	out.array[keyIdx] = key
	out.array[valIdx] = val
	for i := keyIdx; i < uint32(len(b.array)); i++ {
		out.array[i+2] = b.array[i]
	}
	out.bitmap = b.bitmap | bit
	return out, true, nil
}

// assocFilled handles the bit-set branch of assoc: descend into a
// sub-node, replace an equal key, or promote two colliding keys into
// a new sub-node.
//
// CPython: Python/hamt.c:686 hamt_node_bitmap_assoc bit-set branch
func (b *bitmapNode) assocFilled(shift uint32, hash int32, idx uint32, key, val objects.Object) (node, bool, error) {
	keyIdx := 2 * idx
	valIdx := keyIdx + 1
	keyOrNull := b.array[keyIdx]
	valOrNode := b.array[valIdx]

	if keyOrNull == nil {
		oldSub := valOrNode.(node)
		subNode, addedLeaf, err := oldSub.assoc(shift+5, hash, key, val)
		if err != nil {
			return nil, false, err
		}
		if subNode == oldSub {
			return b, addedLeaf, nil
		}
		ret := b.clone()
		ret.array[valIdx] = subNode.(objects.Object)
		return ret, addedLeaf, nil
	}

	eq, err := objects.RichCmpBool(key, keyOrNull, objects.CompareEQ)
	if err != nil {
		return nil, false, err
	}
	if eq {
		if val == valOrNode {
			return b, false, nil
		}
		ret := b.clone()
		ret.array[valIdx] = val
		return ret, false, nil
	}

	subNode, err := newBitmapOrCollision(shift+5, keyOrNull, valOrNode, hash, key, val)
	if err != nil {
		return nil, false, err
	}
	ret := b.clone()
	ret.array[keyIdx] = nil
	ret.array[valIdx] = subNode.(objects.Object)
	return ret, true, nil
}

// without on a bitmap node.
//
// CPython: Python/hamt.c:902 hamt_node_bitmap_without
func (b *bitmapNode) without(shift uint32, hash int32, key objects.Object) (withoutResult, node, error) {
	bit := hamtBitpos(hash, shift)
	if b.bitmap&bit == 0 {
		return wNotFound, nil, nil
	}
	idx := hamtBitindex(b.bitmap, bit)
	keyIdx := 2 * idx
	valIdx := keyIdx + 1
	keyOrNull := b.array[keyIdx]
	valOrNode := b.array[valIdx]

	if keyOrNull == nil {
		res, sub, err := valOrNode.(node).without(shift+5, hash, key)
		switch res {
		case wEmpty:
			// CPython claims this is unreachable: bitmap children of
			// bitmap nodes never collapse to empty.
			panic("hamt: bitmap-of-bitmap collapsed to empty")
		case wNewNode:
			if sb, ok := sub.(*bitmapNode); ok {
				if sb.count() == 1 && sb.array[0] != nil {
					// Inline a single-entry bitmap into the parent.
					clone := b.clone()
					clone.array[keyIdx] = sb.array[0]
					clone.array[valIdx] = sb.array[1]
					return wNewNode, clone, nil
				}
			}
			clone := b.clone()
			clone.array[valIdx] = sub.(objects.Object)
			return wNewNode, clone, nil
		case wError, wNotFound:
			return res, nil, err
		default:
			panic("hamt: unknown without result")
		}
	}

	eq, err := objects.RichCmpBool(keyOrNull, key, objects.CompareEQ)
	if err != nil {
		return wError, nil, err
	}
	if !eq {
		return wNotFound, nil, nil
	}
	if b.count() == 1 {
		return wEmpty, nil, nil
	}
	return wNewNode, b.cloneWithout(bit), nil
}

// find on a bitmap node.
//
// CPython: Python/hamt.c:1040 hamt_node_bitmap_find
func (b *bitmapNode) find(shift uint32, hash int32, key objects.Object) (objects.Object, bool, error) {
	bit := hamtBitpos(hash, shift)
	if b.bitmap&bit == 0 {
		return nil, false, nil
	}
	idx := hamtBitindex(b.bitmap, bit)
	keyIdx := idx * 2
	valIdx := keyIdx + 1
	keyOrNull := b.array[keyIdx]
	valOrNode := b.array[valIdx]
	if keyOrNull == nil {
		return valOrNode.(node).find(shift+5, hash, key)
	}
	eq, err := objects.RichCmpBool(key, keyOrNull, objects.CompareEQ)
	if err != nil {
		return nil, false, err
	}
	if eq {
		return valOrNode, true, nil
	}
	return nil, false, nil
}

///////////////////////////////// Collision node //////////////////////

// findIndex linearly scans for `key`. Returns the index of the key or
// -1 if absent.
//
// CPython: Python/hamt.c:1241 hamt_node_collision_find_index
func (c *collisionNode) findIndex(key objects.Object) (int, error) {
	for i := 0; i < len(c.array); i += 2 {
		eq, err := objects.RichCmpBool(key, c.array[i], objects.CompareEQ)
		if err != nil {
			return -1, err
		}
		if eq {
			return i, nil
		}
	}
	return -1, nil
}

// assoc on a collision node.
//
// CPython: Python/hamt.c:1268 hamt_node_collision_assoc
func (c *collisionNode) assoc(shift uint32, hash int32, key, val objects.Object) (node, bool, error) {
	if hash == c.hash {
		idx, err := c.findIndex(key)
		if err != nil {
			return nil, false, err
		}
		if idx < 0 {
			// Append the new pair.
			out := &collisionNode{hash: c.hash, array: make([]objects.Object, len(c.array)+2)}
			copy(out.array, c.array)
			out.array[len(c.array)] = key
			out.array[len(c.array)+1] = val
			return out, true, nil
		}
		// Replace value.
		if c.array[idx+1] == val {
			return c, false, nil
		}
		out := &collisionNode{hash: c.hash, array: make([]objects.Object, len(c.array))}
		copy(out.array, c.array)
		out.array[idx+1] = val
		return out, false, nil
	}
	// Different 32-bit hash: lift into a bitmap node containing the
	// existing collision and the new entry.
	wrap := newBitmap(2)
	wrap.bitmap = hamtBitpos(c.hash, shift)
	wrap.array[1] = c
	return wrap.assoc(shift, hash, key, val)
}

// without on a collision node.
//
// CPython: Python/hamt.c:1378 hamt_node_collision_without
func (c *collisionNode) without(shift uint32, hash int32, key objects.Object) (withoutResult, node, error) {
	if hash != c.hash {
		return wNotFound, nil, nil
	}
	idx, err := c.findIndex(key)
	if err != nil {
		return wError, nil, err
	}
	if idx < 0 {
		return wNotFound, nil, nil
	}
	newCount := len(c.array)/2 - 1
	if newCount == 0 {
		return wEmpty, nil, nil
	}
	if newCount == 1 {
		// Single entry left: collapse to a bitmap node.
		out := newBitmap(2)
		if idx == 0 {
			out.array[0] = c.array[2]
			out.array[1] = c.array[3]
		} else {
			out.array[0] = c.array[0]
			out.array[1] = c.array[1]
		}
		out.bitmap = hamtBitpos(hash, shift)
		return wNewNode, out, nil
	}
	out := &collisionNode{hash: c.hash, array: make([]objects.Object, len(c.array)-2)}
	for i := 0; i < idx; i++ {
		out.array[i] = c.array[i]
	}
	for i := idx + 2; i < len(c.array); i++ {
		out.array[i-2] = c.array[i]
	}
	return wNewNode, out, nil
}

// find on a collision node.
//
// CPython: Python/hamt.c:1466 hamt_node_collision_find
func (c *collisionNode) find(shift uint32, hash int32, key objects.Object) (objects.Object, bool, error) {
	idx, err := c.findIndex(key)
	if err != nil {
		return nil, false, err
	}
	if idx < 0 {
		return nil, false, nil
	}
	return c.array[idx+1], true, nil
}

///////////////////////////////// Array node //////////////////////////

// clone deep-copies the children slice. Cheap because every entry is
// itself a pointer.
//
// CPython: Python/hamt.c:1581 hamt_node_array_clone
func (a *arrayNode) clone() *arrayNode {
	out := &arrayNode{count: a.count}
	out.children = a.children
	return out
}

// assoc on an array node.
//
// CPython: Python/hamt.c:1604 hamt_node_array_assoc
func (a *arrayNode) assoc(shift uint32, hash int32, key, val objects.Object) (node, bool, error) {
	idx := hamtMask(hash, shift)
	child := a.children[idx]
	if child == nil {
		empty := newBitmap(0)
		newChild, _, err := empty.assoc(shift+5, hash, key, val)
		if err != nil {
			return nil, false, err
		}
		out := &arrayNode{count: a.count + 1}
		out.children = a.children
		out.children[idx] = newChild
		return out, true, nil
	}
	newChild, addedLeaf, err := child.assoc(shift+5, hash, key, val)
	if err != nil {
		return nil, false, err
	}
	if newChild == child {
		return a, addedLeaf, nil
	}
	out := a.clone()
	out.children[idx] = newChild
	return out, addedLeaf, nil
}

// without on an array node.
//
// CPython: Python/hamt.c:1687 hamt_node_array_without
func (a *arrayNode) without(shift uint32, hash int32, key objects.Object) (withoutResult, node, error) {
	idx := hamtMask(hash, shift)
	child := a.children[idx]
	if child == nil {
		return wNotFound, nil, nil
	}
	res, sub, err := child.without(shift+5, hash, key)
	switch res {
	case wNotFound, wError:
		return res, nil, err
	case wNewNode:
		clone := a.clone()
		clone.children[idx] = sub
		return wNewNode, clone, nil
	case wEmpty:
		newCount := a.count - 1
		if newCount == 0 {
			return wEmpty, nil, nil
		}
		if newCount >= bitmapPromoteThreshold {
			out := a.clone()
			out.count = newCount
			out.children[idx] = nil
			return wNewNode, out, nil
		}
		// Demote to a bitmap node.
		bitmapSize := newCount * 2
		out := newBitmap(bitmapSize)
		var bitmap uint32
		newI := 0
		for i := uint32(0); i < arrayNodeSize; i++ {
			if i == idx {
				continue
			}
			n := a.children[i]
			if n == nil {
				continue
			}
			bitmap |= 1 << i
			if bn, ok := n.(*bitmapNode); ok && bn.count() == 1 && bn.array[0] != nil {
				out.array[newI] = bn.array[0]
				out.array[newI+1] = bn.array[1]
			} else {
				out.array[newI] = nil
				out.array[newI+1] = n.(objects.Object)
			}
			newI += 2
		}
		out.bitmap = bitmap
		return wNewNode, out, nil
	default:
		panic("hamt: unknown array without result")
	}
}

// find on an array node.
//
// CPython: Python/hamt.c:1841 hamt_node_array_find
func (a *arrayNode) find(shift uint32, hash int32, key objects.Object) (objects.Object, bool, error) {
	idx := hamtMask(hash, shift)
	child := a.children[idx]
	if child == nil {
		return nil, false, nil
	}
	return child.find(shift+5, hash, key)
}

// arrayNode and collisionNode satisfy objects.Object so bitmap slots
// can carry them in the value position. The Hdr/Type slots are not
// needed by the runtime (HAMT is private to the runtime), but we
// implement them so the value can flow through any code that expects
// an Object.
func (a *arrayNode) Type() *objects.Type      { return nil }
func (a *arrayNode) Hdr() *objects.Header     { return nil }
func (b *bitmapNode) Type() *objects.Type     { return nil }
func (b *bitmapNode) Hdr() *objects.Header    { return nil }
func (c *collisionNode) Type() *objects.Type  { return nil }
func (c *collisionNode) Hdr() *objects.Header { return nil }
