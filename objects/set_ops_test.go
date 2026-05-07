package objects

import "testing"

func setOf(items ...int64) *Set {
	s := NewSet()
	for _, v := range items {
		_ = s.Add(NewInt(v))
	}
	return s
}

func sortedKeys(s *Set) []int64 {
	out := []int64{}
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		v, _ := e.key.(*Int).Int64()
		out = append(out, v)
	}
	// insertion order isn't stable, sort manually for deterministic compare
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func equalSlice(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSetUnion(t *testing.T) {
	a := setOf(1, 2, 3)
	b := setOf(3, 4, 5)
	out, err := a.Union(b)
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if got := sortedKeys(out); !equalSlice(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("Union = %v", got)
	}
}

func TestSetIntersection(t *testing.T) {
	a := setOf(1, 2, 3, 4)
	b := setOf(3, 4, 5)
	c := setOf(2, 3, 4)
	out, _ := a.Intersection(b, c)
	if got := sortedKeys(out); !equalSlice(got, []int64{3, 4}) {
		t.Fatalf("Intersection = %v", got)
	}
}

func TestSetDifference(t *testing.T) {
	a := setOf(1, 2, 3, 4, 5)
	b := setOf(2, 4)
	c := setOf(5)
	out, _ := a.Difference(b, c)
	if got := sortedKeys(out); !equalSlice(got, []int64{1, 3}) {
		t.Fatalf("Difference = %v", got)
	}
}

func TestSetSymmetricDifference(t *testing.T) {
	a := setOf(1, 2, 3)
	b := setOf(2, 3, 4)
	out, _ := a.SymmetricDifference(b)
	if got := sortedKeys(out); !equalSlice(got, []int64{1, 4}) {
		t.Fatalf("SymmetricDifference = %v", got)
	}
}

func TestSetSubsetSuperset(t *testing.T) {
	a := setOf(1, 2)
	b := setOf(1, 2, 3)
	if ok, _ := a.IsSubset(b); !ok {
		t.Fatal("a should be subset of b")
	}
	if ok, _ := b.IsSuperset(a); !ok {
		t.Fatal("b should be superset of a")
	}
	if ok, _ := b.IsSubset(a); ok {
		t.Fatal("b is not subset of a")
	}
}

func TestSetIsDisjoint(t *testing.T) {
	a := setOf(1, 2)
	b := setOf(3, 4)
	c := setOf(2, 5)
	if ok, _ := a.IsDisjoint(b); !ok {
		t.Fatal("disjoint sets should report true")
	}
	if ok, _ := a.IsDisjoint(c); ok {
		t.Fatal("overlapping sets should report false")
	}
}

func TestSetUpdateRejectedOnFrozen(t *testing.T) {
	fs, _ := NewFrozenset([]Object{NewInt(1)})
	if err := fs.Update(setOf(2)); err == nil {
		t.Fatal("Update on frozenset must error")
	}
}

func TestFrozensetUnionReturnsFrozenset(t *testing.T) {
	a, _ := NewFrozenset([]Object{NewInt(1)})
	b, _ := NewFrozenset([]Object{NewInt(2)})
	out, err := a.Union(b)
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if out.Type() != FrozensetType {
		t.Fatalf("type = %s, want frozenset", out.Type().Name)
	}
}
