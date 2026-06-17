// Package hamt is the gopy port of cpython/Python/hamt.c. It is the
// immutable persistent mapping that backs Context (see contextvar/).
//
// The tree branches on 5 bits of a 32-bit hash per level so the
// maximum non-collision depth is 7. A level-7 collision node lifts the
// total maximum depth to 8 (MaxTreeDepth).
//
// Nodes are refcounted objects.Object values: each node owns one
// reference on every key, value, and child it stores. assoc/without
// return an owned reference (the caller must Decref it); the unchanged
// path returns Incref(self). This mirrors CPython's tp_dealloc /
// Py_NewRef / Py_SETREF discipline 1:1 so that a value stored only in a
// ContextVar (via the HAMT) carries an honest refcount and does not get
// torn down while it is still reachable.
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
// dispatch matches hamt_node_assoc / without / find in CPython. Every
// node embeds objects.Header so a node value is a full objects.Object:
// it can be stored in a bitmap's value slot, incref'd, and decref'd
// uniformly with the keys and values it sits beside.
type node interface {
	objects.Object
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
	objects.Header
	bitmap uint32
	array  []objects.Object
}

// arrayNode is the dense 32-slot fan-out node. count tracks how many
// children are non-nil; it is updated by every clone. Iteration walks
// children in index order which matches CPython's emission order.
//
// CPython: Python/hamt.c:316 PyHamtNode_Array
type arrayNode struct {
	objects.Header
	count    int
	children [arrayNodeSize]node
}

// collisionNode lives at level 7 (shift == 30 so the next 5-bit
// window would go off the top of the 32-bit hash). All keys in the
// node share the same 32-bit hash value.
//
// CPython: Python/hamt.c:325 PyHamtNode_Collision
type collisionNode struct {
	objects.Header
	hash  int32
	array []objects.Object
}

// Node type objects. They exist so a node satisfies objects.Object and
// so Decref can reach the per-shape Dealloc that releases the stored
// references. HAMT is private to the runtime, so the bare type name is
// all we register.
//
// CPython: Python/hamt.c:2843 _PyHamt_BitmapNode_Type / _PyHamt_ArrayNode_Type / _PyHamt_CollisionNode_Type
var (
	bitmapNodeType    = objects.NewType("hamt_bitmap_node", []*objects.Type{objects.ObjectType()})
	arrayNodeType     = objects.NewType("hamt_array_node", []*objects.Type{objects.ObjectType()})
	collisionNodeType = objects.NewType("hamt_collision_node", []*objects.Type{objects.ObjectType()})
)

// emptyBitmap is the singleton empty bitmap node. CPython caches the
// same instance and statically allocates it (immortal); we stamp it
// immortal so the Incref/Decref it sees as the root of every empty
// Hamt and as the working node in assoc are all no-ops and it never
// deallocs.
//
// CPython: Python/hamt.c:498 _Py_SINGLETON(hamt_bitmap_node_empty)
var emptyBitmap = &bitmapNode{}

func init() {
	bitmapNodeType.Dealloc = bitmapNodeDealloc
	bitmapNodeType.TpTraverse = bitmapNodeTraverse
	arrayNodeType.Dealloc = arrayNodeDealloc
	arrayNodeType.TpTraverse = arrayNodeTraverse
	collisionNodeType.Dealloc = collisionNodeDealloc
	collisionNodeType.TpTraverse = collisionNodeTraverse

	emptyBitmap.Init(bitmapNodeType)
	emptyBitmap.MakeImmortal()
}

// xIncref / xDecref mirror Py_XINCREF / Py_XDECREF: a nil operand is a
// no-op. A nil bitmap key slot is the common case (it marks a child
// node in the value slot), so the guard earns its keep.
func xIncref(o objects.Object) {
	if o != nil {
		objects.Incref(o)
	}
}

func xDecref(o objects.Object) {
	if o != nil {
		objects.Decref(o)
	}
}

// setref stores v into arr[i] and drops the reference the slot held
// before. Mirrors Py_SETREF / Py_XSETREF: the store happens before the
// decref so a self-referential value cannot be freed mid-swap. v is
// already an owned reference (or nil); the slot adopts it.
func setref(arr []objects.Object, i int, v objects.Object) {
	old := arr[i]
	arr[i] = v
	xDecref(old)
}

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
// reuses the immortal empty singleton (CPython returns Py_NewRef of
// the statically allocated singleton; here the incref is a no-op).
//
// CPython: Python/hamt.c:489 hamt_node_bitmap_new
func newBitmap(size int) *bitmapNode {
	if size == 0 {
		return emptyBitmap
	}
	b := &bitmapNode{array: make([]objects.Object, size)}
	b.Init(bitmapNodeType)
	return b
}

// count returns the number of (k, v) pairs in the bitmap node. CPython
// stores the same value as Py_SIZE(node)/2.
//
// CPython: Python/hamt.c:527 hamt_node_bitmap_count
func (b *bitmapNode) count() int {
	return len(b.array) / 2
}

// clone copies the bitmap and the slot array, taking a reference on
// every copied entry.
//
// CPython: Python/hamt.c:533 hamt_node_bitmap_clone
func (b *bitmapNode) clone() *bitmapNode {
	c := newBitmap(len(b.array))
	for i := range b.array {
		c.array[i] = b.array[i]
		xIncref(c.array[i])
	}
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
		xIncref(c.array[i])
	}
	for i := valIdx + 1; i < uint32(len(b.array)); i++ {
		c.array[i-2] = b.array[i]
		xIncref(b.array[i])
	}
	c.bitmap = b.bitmap & ^bit
	return c
}

// newBitmapOrCollision returns a node holding two key/value pairs
// that collided in the parent bitmap node. It promotes to a collision
// node only when the full 32-bit hashes are identical. The returned
// node is an owned reference.
//
// CPython: Python/hamt.c:584 hamt_node_new_bitmap_or_collision
func newBitmapOrCollision(shift uint32, key1 objects.Object, val1 objects.Object, key2Hash int32, key2, val2 objects.Object) (node, error) {
	key1Hash, err := hamtHash(key1)
	if err != nil {
		return nil, err
	}
	if key1Hash == key2Hash {
		n := newCollision(key1Hash, 4)
		n.array[0] = key1
		objects.Incref(key1)
		n.array[1] = val1
		objects.Incref(val1)
		n.array[2] = key2
		objects.Incref(key2)
		n.array[3] = val2
		objects.Incref(val2)
		return n, nil
	}
	n := newBitmap(0)
	n2, _, err := n.assoc(shift, key1Hash, key1, val1)
	objects.Decref(n)
	if err != nil {
		return nil, err
	}
	n3, _, err := n2.assoc(shift, key2Hash, key2, val2)
	objects.Decref(n2)
	if err != nil {
		return nil, err
	}
	return n3, nil
}

// assoc on a bitmap node. The four code paths (sub-node descent,
// equal-key replace, collision promotion, and the new-key insert /
// promote-to-array branch) line up with CPython 1:1. The returned
// node is an owned reference.
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
		newArr := newArray(n + 1)

		empty := newBitmap(0)
		child, _, err := empty.assoc(shift+5, hash, key, val)
		if err != nil {
			objects.Decref(empty)
			objects.Decref(newArr)
			return nil, false, err
		}
		newArr.children[jdx] = child // borrow: adopt the owned ref

		// Re-distribute existing entries.
		j := 0
		for i := 0; i < arrayNodeSize; i++ {
			if (b.bitmap>>uint(i))&1 == 0 {
				continue
			}
			if b.array[j] == nil {
				cn := b.array[j+1].(node)
				objects.Incref(cn)
				newArr.children[i] = cn
			} else {
				rehash, err := hamtHash(b.array[j])
				if err != nil {
					objects.Decref(empty)
					objects.Decref(newArr)
					return nil, false, err
				}
				child, _, err := empty.assoc(shift+5, rehash, b.array[j], b.array[j+1])
				if err != nil {
					objects.Decref(empty)
					objects.Decref(newArr)
					return nil, false, err
				}
				newArr.children[i] = child // borrow
			}
			j += 2
		}
		objects.Decref(empty)
		return newArr, true, nil
	}

	// Stay a bitmap; widen by two slots.
	keyIdx := 2 * idx
	valIdx := keyIdx + 1
	out := newBitmap(2 * (n + 1))
	for i := uint32(0); i < keyIdx; i++ {
		out.array[i] = b.array[i]
		xIncref(b.array[i])
	}
	out.array[keyIdx] = key
	objects.Incref(key)
	out.array[valIdx] = val
	objects.Incref(val)
	for i := keyIdx; i < uint32(len(b.array)); i++ {
		out.array[i+2] = b.array[i]
		xIncref(b.array[i])
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
			objects.Decref(subNode)
			objects.Incref(b)
			return b, addedLeaf, nil
		}
		ret := b.clone()
		setref(ret.array, int(valIdx), subNode) // adopt owned subNode
		return ret, addedLeaf, nil
	}

	eq, err := objects.RichCmpBool(key, keyOrNull, objects.CompareEQ)
	if err != nil {
		return nil, false, err
	}
	if eq {
		if val == valOrNode {
			objects.Incref(b)
			return b, false, nil
		}
		ret := b.clone()
		objects.Incref(val)
		setref(ret.array, int(valIdx), val)
		return ret, false, nil
	}

	subNode, err := newBitmapOrCollision(shift+5, keyOrNull, valOrNode, hash, key, val)
	if err != nil {
		return nil, false, err
	}
	ret := b.clone()
	setref(ret.array, int(keyIdx), nil)     // drop the old key
	setref(ret.array, int(valIdx), subNode) // adopt owned subNode
	return ret, true, nil
}

// without on a bitmap node. On wNewNode the returned node is an owned
// reference.
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
					objects.Incref(sb.array[0])
					setref(clone.array, int(keyIdx), sb.array[0])
					objects.Incref(sb.array[1])
					setref(clone.array, int(valIdx), sb.array[1])
					objects.Decref(sub)
					return wNewNode, clone, nil
				}
			}
			clone := b.clone()
			setref(clone.array, int(valIdx), sub) // adopt owned sub
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

// find on a bitmap node. The returned value is borrowed (CPython
// returns a borrowed reference from hamt_node_bitmap_find).
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

// bitmapNodeDealloc releases the reference the node holds on every
// slot. The empty singleton is immortal and never reaches here.
//
// CPython: Python/hamt.c:1102 hamt_node_bitmap_dealloc
func bitmapNodeDealloc(o objects.Object) {
	b := o.(*bitmapNode)
	if b == emptyBitmap {
		return
	}
	for i := len(b.array) - 1; i >= 0; i-- {
		xDecref(b.array[i])
		b.array[i] = nil
	}
}

// bitmapNodeTraverse visits every slot for the cyclic collector.
//
// CPython: Python/hamt.c:1085 hamt_node_bitmap_traverse
func bitmapNodeTraverse(o objects.Object, visit objects.Visitor) error {
	b := o.(*bitmapNode)
	for i := len(b.array) - 1; i >= 0; i-- {
		if b.array[i] == nil {
			continue
		}
		if err := visit(b.array[i]); err != nil {
			return err
		}
	}
	return nil
}

///////////////////////////////// Collision node //////////////////////

// newCollision allocates a collision node with `size` slots and the
// given shared hash.
//
// CPython: Python/hamt.c:1192 hamt_node_collision_new
func newCollision(hash int32, size int) *collisionNode {
	c := &collisionNode{hash: hash, array: make([]objects.Object, size)}
	c.Init(collisionNodeType)
	return c
}

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

// assoc on a collision node. The returned node is an owned reference.
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
			out := newCollision(c.hash, len(c.array)+2)
			for i := range c.array {
				out.array[i] = c.array[i]
				objects.Incref(c.array[i])
			}
			out.array[len(c.array)] = key
			objects.Incref(key)
			out.array[len(c.array)+1] = val
			objects.Incref(val)
			return out, true, nil
		}
		// Replace value.
		if c.array[idx+1] == val {
			objects.Incref(c)
			return c, false, nil
		}
		out := newCollision(c.hash, len(c.array))
		for i := range c.array {
			out.array[i] = c.array[i]
			objects.Incref(c.array[i])
		}
		objects.Incref(val)
		setref(out.array, idx+1, val)
		return out, false, nil
	}
	// Different 32-bit hash: lift into a bitmap node containing the
	// existing collision and the new entry.
	wrap := newBitmap(2)
	wrap.bitmap = hamtBitpos(c.hash, shift)
	wrap.array[1] = c
	objects.Incref(c)
	res, addedLeaf, err := wrap.assoc(shift, hash, key, val)
	objects.Decref(wrap)
	return res, addedLeaf, err
}

// without on a collision node. On wNewNode the returned node is an
// owned reference.
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
			objects.Incref(c.array[2])
			out.array[1] = c.array[3]
			objects.Incref(c.array[3])
		} else {
			out.array[0] = c.array[0]
			objects.Incref(c.array[0])
			out.array[1] = c.array[1]
			objects.Incref(c.array[1])
		}
		out.bitmap = hamtBitpos(hash, shift)
		return wNewNode, out, nil
	}
	out := newCollision(c.hash, len(c.array)-2)
	for i := 0; i < idx; i++ {
		out.array[i] = c.array[i]
		objects.Incref(c.array[i])
	}
	for i := idx + 2; i < len(c.array); i++ {
		out.array[i-2] = c.array[i]
		objects.Incref(c.array[i])
	}
	return wNewNode, out, nil
}

// find on a collision node. The returned value is borrowed.
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

// collisionNodeDealloc releases every stored reference.
//
// CPython: Python/hamt.c:1503 hamt_node_collision_dealloc
func collisionNodeDealloc(o objects.Object) {
	c := o.(*collisionNode)
	for i := len(c.array) - 1; i >= 0; i-- {
		xDecref(c.array[i])
		c.array[i] = nil
	}
}

// collisionNodeTraverse visits every stored value.
//
// CPython: Python/hamt.c:1489 hamt_node_collision_traverse
func collisionNodeTraverse(o objects.Object, visit objects.Visitor) error {
	c := o.(*collisionNode)
	for i := len(c.array) - 1; i >= 0; i-- {
		if c.array[i] == nil {
			continue
		}
		if err := visit(c.array[i]); err != nil {
			return err
		}
	}
	return nil
}

///////////////////////////////// Array node //////////////////////////

// newArray allocates an array node with the given non-nil child count.
//
// CPython: Python/hamt.c:1557 hamt_node_array_new
func newArray(count int) *arrayNode {
	a := &arrayNode{count: count}
	a.Init(arrayNodeType)
	return a
}

// clone copies the children, taking a reference on each non-nil child.
//
// CPython: Python/hamt.c:1581 hamt_node_array_clone
func (a *arrayNode) clone() *arrayNode {
	out := newArray(a.count)
	for i := 0; i < arrayNodeSize; i++ {
		out.children[i] = a.children[i]
		if a.children[i] != nil {
			objects.Incref(a.children[i])
		}
	}
	return out
}

// assoc on an array node. The returned node is an owned reference.
//
// CPython: Python/hamt.c:1604 hamt_node_array_assoc
func (a *arrayNode) assoc(shift uint32, hash int32, key, val objects.Object) (node, bool, error) {
	idx := hamtMask(hash, shift)
	child := a.children[idx]
	if child == nil {
		empty := newBitmap(0)
		newChild, addedLeaf, err := empty.assoc(shift+5, hash, key, val)
		objects.Decref(empty)
		if err != nil {
			return nil, false, err
		}
		out := newArray(a.count + 1)
		for i := 0; i < arrayNodeSize; i++ {
			out.children[i] = a.children[i]
			if a.children[i] != nil {
				objects.Incref(a.children[i])
			}
		}
		out.children[idx] = newChild // borrow: slot was nil, adopt owned ref
		return out, addedLeaf, nil
	}
	newChild, addedLeaf, err := child.assoc(shift+5, hash, key, val)
	if err != nil {
		return nil, false, err
	}
	if newChild == child {
		objects.Decref(newChild)
		objects.Incref(a)
		return a, addedLeaf, nil
	}
	out := a.clone()
	old := out.children[idx]
	out.children[idx] = newChild // adopt owned ref
	if old != nil {
		objects.Decref(old)
	}
	return out, addedLeaf, nil
}

// without on an array node. On wNewNode the returned node is an owned
// reference.
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
		old := clone.children[idx]
		clone.children[idx] = sub // adopt owned sub
		if old != nil {
			objects.Decref(old)
		}
		return wNewNode, clone, nil
	case wEmpty:
		newCount := a.count - 1
		if newCount == 0 {
			return wEmpty, nil, nil
		}
		if newCount >= bitmapPromoteThreshold {
			out := a.clone()
			out.count = newCount
			if out.children[idx] != nil {
				objects.Decref(out.children[idx])
				out.children[idx] = nil
			}
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
				objects.Incref(bn.array[0])
				out.array[newI+1] = bn.array[1]
				objects.Incref(bn.array[1])
			} else {
				out.array[newI] = nil
				out.array[newI+1] = n
				objects.Incref(n)
			}
			newI += 2
		}
		out.bitmap = bitmap
		return wNewNode, out, nil
	default:
		panic("hamt: unknown array without result")
	}
}

// find on an array node. The returned value is borrowed.
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

// arrayNodeDealloc releases the reference held on every non-nil child.
//
// CPython: Python/hamt.c:1872 hamt_node_array_dealloc
func arrayNodeDealloc(o objects.Object) {
	a := o.(*arrayNode)
	for i := 0; i < arrayNodeSize; i++ {
		if a.children[i] != nil {
			objects.Decref(a.children[i])
			a.children[i] = nil
		}
	}
}

// arrayNodeTraverse visits every non-nil child.
//
// CPython: Python/hamt.c:1857 hamt_node_array_traverse
func arrayNodeTraverse(o objects.Object, visit objects.Visitor) error {
	a := o.(*arrayNode)
	for i := 0; i < arrayNodeSize; i++ {
		if a.children[i] == nil {
			continue
		}
		if err := visit(a.children[i]); err != nil {
			return err
		}
	}
	return nil
}
