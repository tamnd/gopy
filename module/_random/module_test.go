package _random

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// makeRandom builds a fresh Random instance via RandomType.TpNew.
func makeRandom(t *testing.T) *RandomObject {
	t.Helper()
	obj, err := randomNew(RandomType, nil, nil)
	if err != nil {
		t.Fatalf("randomNew: %v", err)
	}
	r, ok := obj.(*RandomObject)
	if !ok {
		t.Fatalf("randomNew returned %T, want *RandomObject", obj)
	}
	return r
}

// TestRandom_random verifies that random() returns a float in [0.0, 1.0).
func TestRandom_random(t *testing.T) {
	r := makeRandom(t)
	for i := 0; i < 100; i++ {
		res, err := randomRandom([]objects.Object{r}, nil)
		if err != nil {
			t.Fatalf("random(): %v", err)
		}
		f, ok := res.(*objects.Float)
		if !ok {
			t.Fatalf("random() returned %T, want *Float", res)
		}
		v := f.Float64()
		if v < 0.0 || v >= 1.0 {
			t.Errorf("random() = %v, want [0.0, 1.0)", v)
		}
	}
}

// TestRandom_seed_deterministic seeds twice with 42 and checks that the
// two resulting sequences are identical.
func TestRandom_seed_deterministic(t *testing.T) {
	seed42 := []objects.Object{objects.NewInt(42)}

	makeSeeded := func() *RandomObject {
		r := makeRandom(t)
		if _, err := randomSeed(append([]objects.Object{r}, seed42...), nil); err != nil {
			t.Fatalf("seed(42): %v", err)
		}
		return r
	}

	r1 := makeSeeded()
	r2 := makeSeeded()

	for i := 0; i < 20; i++ {
		res1, err := randomRandom([]objects.Object{r1}, nil)
		if err != nil {
			t.Fatalf("r1.random(): %v", err)
		}
		res2, err := randomRandom([]objects.Object{r2}, nil)
		if err != nil {
			t.Fatalf("r2.random(): %v", err)
		}
		f1 := res1.(*objects.Float).Float64()
		f2 := res2.(*objects.Float).Float64()
		if f1 != f2 {
			t.Errorf("step %d: r1.random()=%v != r2.random()=%v", i, f1, f2)
		}
	}
}

// TestRandom_getrandbits verifies that getrandbits(32) fits in 32 bits.
func TestRandom_getrandbits(t *testing.T) {
	r := makeRandom(t)
	for i := 0; i < 50; i++ {
		res, err := randomGetrandbits([]objects.Object{r, objects.NewInt(32)}, nil)
		if err != nil {
			t.Fatalf("getrandbits(32): %v", err)
		}
		n, ok := res.(*objects.Int)
		if !ok {
			t.Fatalf("getrandbits(32) returned %T, want *Int", res)
		}
		v, fits := n.Int64()
		if !fits {
			t.Fatalf("getrandbits(32) result does not fit in int64")
		}
		if v < 0 || v > 0xffffffff {
			t.Errorf("getrandbits(32) = %d, out of [0, 2^32)", v)
		}
	}
}

// TestRandom_getstate_setstate verifies that getstate returns a tuple of
// the right length and that setstate(getstate()) round-trips without error.
func TestRandom_getstate_setstate(t *testing.T) {
	r := makeRandom(t)

	// getstate.
	stateObj, err := randomGetstate([]objects.Object{r}, nil)
	if err != nil {
		t.Fatalf("getstate(): %v", err)
	}
	tup, ok := stateObj.(*objects.Tuple)
	if !ok {
		t.Fatalf("getstate() returned %T, want *Tuple", stateObj)
	}
	if tup.Len() != mtN+1 {
		t.Fatalf("getstate() tuple len = %d, want %d", tup.Len(), mtN+1)
	}

	// setstate(getstate()) must not error.
	_, err = randomSetstate([]objects.Object{r, tup}, nil)
	if err != nil {
		t.Fatalf("setstate(getstate()): %v", err)
	}

	// After a round-trip the next random() value should equal what we
	// would have gotten without the detour.
	r2 := makeRandom(t)
	if _, err := randomSetstate([]objects.Object{r2, tup}, nil); err != nil {
		t.Fatalf("setstate on r2: %v", err)
	}
	res1, _ := randomRandom([]objects.Object{r}, nil)
	res2, _ := randomRandom([]objects.Object{r2}, nil)
	v1 := res1.(*objects.Float).Float64()
	v2 := res2.(*objects.Float).Float64()
	if v1 != v2 {
		t.Errorf("after setstate r1.random()=%v != r2.random()=%v", v1, v2)
	}
}
