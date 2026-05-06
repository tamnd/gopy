package hamt

import (
	"fmt"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// hashedKey is a test-only Object with a controllable Hash so we can
// drive the tree into specific shapes (collisions, promotions, etc.).
type hashedKey struct {
	objects.Header
	tag  string
	hash int64
}

var hashedKeyType = func() *objects.Type {
	t := objects.NewType("hashedKey", []*objects.Type{objects.ObjectType()})
	t.Hash = func(o objects.Object) (int64, error) { return o.(*hashedKey).hash, nil }
	t.RichCmp = func(a, b objects.Object, op objects.CompareOp) (objects.Object, error) {
		bh, ok := b.(*hashedKey)
		if !ok {
			return objects.NotImplemented(), nil
		}
		ah := a.(*hashedKey)
		switch op {
		case objects.CompareEQ:
			return objects.NewBool(ah.tag == bh.tag), nil
		case objects.CompareNE:
			return objects.NewBool(ah.tag != bh.tag), nil
		}
		return objects.NotImplemented(), nil
	}
	return t
}()

func newKey(tag string, hash int64) *hashedKey {
	k := &hashedKey{tag: tag, hash: hash}
	k.Init(hashedKeyType)
	return k
}

func TestHamtRoundTrip(t *testing.T) {
	h := New()
	k1 := objects.NewStr("a")
	k2 := objects.NewStr("b")
	v1 := objects.NewInt(1)
	v2 := objects.NewInt(2)

	h1, err := h.Assoc(k1, v1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := h1.Assoc(k2, v2)
	if err != nil {
		t.Fatal(err)
	}
	if h2.Len() != 2 {
		t.Errorf("Len = %d, want 2", h2.Len())
	}
	got, ok, err := h2.Find(k1)
	if err != nil || !ok || got != v1 {
		t.Errorf("Find(k1) = (%v, %v, %v), want (v1, true, nil)", got, ok, err)
	}
	got, ok, err = h2.Find(k2)
	if err != nil || !ok || got != v2 {
		t.Errorf("Find(k2) = (%v, %v, %v), want (v2, true, nil)", got, ok, err)
	}
	// Original h1 unchanged.
	if h1.Len() != 1 {
		t.Errorf("h1.Len = %d, want 1", h1.Len())
	}
	if _, ok, _ := h1.Find(k2); ok {
		t.Error("h1 sees k2")
	}
}

func TestHamtAssocReplace(t *testing.T) {
	h, err := New().Assoc(objects.NewStr("k"), objects.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := h.Assoc(objects.NewStr("k"), objects.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if h2.Len() != 1 {
		t.Errorf("Len after replace = %d, want 1", h2.Len())
	}
	got, ok, err := h2.Find(objects.NewStr("k"))
	if err != nil || !ok {
		t.Fatalf("Find missing: %v %v", ok, err)
	}
	gi := got.(*objects.Int)
	if v, _ := gi.Int64(); v != 2 {
		t.Errorf("value = %d, want 2", v)
	}
}

func TestHamtWithoutMissing(t *testing.T) {
	h := New()
	got, removed, err := h.Without(objects.NewStr("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("removed=true for empty hamt")
	}
	if got != h {
		t.Error("Without on missing key should return receiver")
	}
}

func TestHamtWithoutFound(t *testing.T) {
	h, _ := New().Assoc(objects.NewStr("a"), objects.NewInt(1))
	h, _ = h.Assoc(objects.NewStr("b"), objects.NewInt(2))
	out, removed, err := h.Without(objects.NewStr("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("removed=false for present key")
	}
	if out.Len() != 1 {
		t.Errorf("Len after remove = %d, want 1", out.Len())
	}
	if _, ok, _ := out.Find(objects.NewStr("a")); ok {
		t.Error("removed key still findable")
	}
	if _, ok, _ := out.Find(objects.NewStr("b")); !ok {
		t.Error("untouched key gone")
	}
}

// TestHamtPromotion checks that inserting 17 keys whose level-0 hash
// fragments are all distinct promotes the root from bitmapNode to
// arrayNode.
func TestHamtPromotion(t *testing.T) {
	h := New()
	for i := 0; i < 17; i++ {
		// Vary the bottom 5 bits (level 0) so each key wants its own
		// slot at level 0.
		k := newKey(fmt.Sprintf("k%d", i), int64(i))
		var err error
		h, err = h.Assoc(k, objects.NewInt(int64(i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	if h.Len() != 17 {
		t.Errorf("Len = %d, want 17", h.Len())
	}
	if _, ok := h.root.(*arrayNode); !ok {
		t.Errorf("root type = %T, want *arrayNode", h.root)
	}
}

// TestHamtCollision puts two keys with identical 32-bit hashes into
// the tree and checks the level-7 leaf is a *collisionNode.
func TestHamtCollision(t *testing.T) {
	h := New()
	k1 := newKey("k1", 0xdeadbeef)
	k2 := newKey("k2", 0xdeadbeef)
	h, err := h.Assoc(k1, objects.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	h, err = h.Assoc(k2, objects.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	if h.Len() != 2 {
		t.Errorf("Len = %d, want 2", h.Len())
	}
	// Walk down to the leaf following the hash.
	hash, _ := hamtHash(k1)
	n := h.root
	depth := 0
	for {
		if c, ok := n.(*collisionNode); ok {
			if c.hash != hash {
				t.Errorf("collision hash = %x, want %x", c.hash, hash)
			}
			if len(c.array) != 4 {
				t.Errorf("collision len = %d, want 4", len(c.array))
			}
			break
		}
		if depth >= MaxTreeDepth {
			t.Fatal("no collision node found within MaxTreeDepth")
		}
		switch sub := n.(type) {
		case *bitmapNode:
			bit := hamtBitpos(hash, uint32(depth*5))
			idx := hamtBitindex(sub.bitmap, bit)
			n = sub.array[2*idx+1].(node)
		case *arrayNode:
			idx := hamtMask(hash, uint32(depth*5))
			n = sub.children[idx]
		default:
			t.Fatalf("unexpected node type %T", n)
		}
		depth++
	}
}

// TestHamtIterCovers checks the iterator yields every (k, v) pair
// once, in some deterministic order.
func TestHamtIterCovers(t *testing.T) {
	h := New()
	want := map[string]int64{}
	for i := 0; i < 50; i++ {
		k := newKey(fmt.Sprintf("k%d", i), int64(i)*7919)
		var err error
		h, err = h.Assoc(k, objects.NewInt(int64(i)))
		if err != nil {
			t.Fatal(err)
		}
		want[k.tag] = int64(i)
	}
	got := map[string]int64{}
	it := h.Iter()
	for {
		k, v, ok, err := it.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		hk := k.(*hashedKey)
		hv := v.(*objects.Int)
		val, _ := hv.Int64()
		if _, dup := got[hk.tag]; dup {
			t.Errorf("duplicate key %q", hk.tag)
		}
		got[hk.tag] = val
	}
	if len(got) != len(want) {
		t.Errorf("len got=%d want=%d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %d, want %d", k, got[k], v)
		}
	}
}

// TestHamtIterDeterministic checks two iterators over the same Hamt
// emit keys in the same order.
func TestHamtIterDeterministic(t *testing.T) {
	h := New()
	for i := 0; i < 32; i++ {
		k := newKey(fmt.Sprintf("k%d", i), int64(i)*1234567)
		h, _ = h.Assoc(k, objects.NewInt(int64(i)))
	}
	collect := func() []string {
		out := []string{}
		it := h.Iter()
		for {
			k, _, ok, _ := it.Next()
			if !ok {
				return out
			}
			out = append(out, k.(*hashedKey).tag)
		}
	}
	a := collect()
	b := collect()
	if len(a) != len(b) {
		t.Fatalf("len mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("at %d: %q vs %q", i, a[i], b[i])
			break
		}
	}
}

// TestHamtEqInsertionOrderIndependent: two trees built by inserting
// the same pairs in different orders compare equal.
func TestHamtEqInsertionOrderIndependent(t *testing.T) {
	keys := []*hashedKey{
		newKey("a", 1),
		newKey("b", 32),
		newKey("c", 64),
		newKey("d", 1024),
	}
	a, b := New(), New()
	for i, k := range keys {
		var err error
		a, err = a.Assoc(k, objects.NewInt(int64(i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := len(keys) - 1; i >= 0; i-- {
		var err error
		b, err = b.Assoc(keys[i], objects.NewInt(int64(i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	eq, err := a.Eq(b)
	if err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Error("a != b but pairs are identical")
	}
}

// TestHamtEqValueMismatch: same keys, different value for one key.
func TestHamtEqValueMismatch(t *testing.T) {
	a, _ := New().Assoc(objects.NewStr("k"), objects.NewInt(1))
	b, _ := New().Assoc(objects.NewStr("k"), objects.NewInt(2))
	eq, err := a.Eq(b)
	if err != nil {
		t.Fatal(err)
	}
	if eq {
		t.Error("a Eq b should be false")
	}
}

// TestHamtStructuralSharing pins the no-op-Assoc identity guarantee.
// Setting a key to a value it already maps to returns the receiver,
// which is a stronger form of structural sharing than just sharing
// untouched subtrees.
func TestHamtStructuralSharing(t *testing.T) {
	v := objects.NewInt(1)
	h, _ := New().Assoc(objects.NewStr("a"), v)
	h2, _ := h.Assoc(objects.NewStr("a"), v)
	if h2 != h {
		t.Error("re-Assoc with identical k/v should return receiver")
	}
}

// TestHamtPropertyOracle is the property-style sweep from the spec:
// random Assoc/Without sequences must agree with a Go map oracle.
func TestHamtPropertyOracle(t *testing.T) {
	h := New()
	oracle := map[string]int64{}
	const N = 200
	keys := make([]*hashedKey, N)
	for i := range keys {
		keys[i] = newKey(fmt.Sprintf("k%d", i), int64(i)*2654435761)
	}
	// Insert every key.
	for i, k := range keys {
		v := int64(i)
		var err error
		h, err = h.Assoc(k, objects.NewInt(v))
		if err != nil {
			t.Fatal(err)
		}
		oracle[k.tag] = v
	}
	// Remove every other key.
	for i := 0; i < N; i += 2 {
		k := keys[i]
		var err error
		var removed bool
		h, removed, err = h.Without(k)
		if err != nil {
			t.Fatal(err)
		}
		if !removed {
			t.Errorf("remove %q: not removed", k.tag)
		}
		delete(oracle, k.tag)
	}
	if h.Len() != len(oracle) {
		t.Fatalf("len mismatch hamt=%d oracle=%d", h.Len(), len(oracle))
	}
	for tag, want := range oracle {
		k := newKey(tag, 0) // hash overridden below
		// Find the original key with the right hash.
		for _, kk := range keys {
			if kk.tag == tag {
				k = kk
				break
			}
		}
		got, ok, err := h.Find(k)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("missing %q", tag)
			continue
		}
		gv, _ := got.(*objects.Int).Int64()
		if gv != want {
			t.Errorf("%q: got %d, want %d", tag, gv, want)
		}
	}
	// Removed keys must be gone.
	for i := 0; i < N; i += 2 {
		if _, ok, _ := h.Find(keys[i]); ok {
			t.Errorf("removed key %q reappeared", keys[i].tag)
		}
	}
}
