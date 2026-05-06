// Gate panel for the hashtable port. Mirrors the cases the 1668 spec
// calls out: insert / get / steal round-trip, growth curve under
// 100-entry insertion, Foreach visits every entry, and Destroy fires
// the value destructor for each entry.

package hashtable

import (
	"errors"
	"hash/fnv"
	"testing"
)

func strHash(k any) uint64 {
	h := fnv.New64a()
	h.Write([]byte(k.(string)))
	return h.Sum64()
}

func strEq(a, b any) bool { return a.(string) == b.(string) }

func intHash(k any) uint64 { return uint64(k.(int)) }
func intEq(a, b any) bool  { return a.(int) == b.(int) }

func TestInsertGetSteal(t *testing.T) {
	tbl := New(strHash, strEq)
	tbl.Set("a", 1)
	tbl.Set("b", 2)
	if v, ok := tbl.Get("a"); !ok || v.(int) != 1 {
		t.Fatalf("Get a: %v %v", v, ok)
	}
	if v, ok := tbl.Get("b"); !ok || v.(int) != 2 {
		t.Fatalf("Get b: %v %v", v, ok)
	}
	if v, ok := tbl.Steal("a"); !ok || v.(int) != 1 {
		t.Fatalf("Steal a: %v %v", v, ok)
	}
	if _, ok := tbl.Get("a"); ok {
		t.Fatalf("Get after Steal should miss")
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len after Steal: got %d, want 1", tbl.Len())
	}
}

func TestSetReplaces(t *testing.T) {
	tbl := New(strHash, strEq)
	tbl.Set("k", 1)
	tbl.Set("k", 2)
	if v, ok := tbl.Get("k"); !ok || v.(int) != 2 {
		t.Fatalf("Get after replace: %v %v", v, ok)
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", tbl.Len())
	}
}

func TestStealMissingLeavesTableAlone(t *testing.T) {
	tbl := New(intHash, intEq)
	tbl.Set(1, "one")
	if _, ok := tbl.Steal(2); ok {
		t.Fatalf("Steal of absent key reported found")
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", tbl.Len())
	}
}

func TestGrowthCurve(t *testing.T) {
	tbl := New(intHash, intEq)
	for i := range 100 {
		tbl.Set(i, i*10)
	}
	if tbl.Len() != 100 {
		t.Fatalf("Len: got %d, want 100", tbl.Len())
	}
	for i := range 100 {
		v, ok := tbl.Get(i)
		if !ok || v.(int) != i*10 {
			t.Fatalf("Get %d: %v %v", i, v, ok)
		}
	}
	// Load factor must remain at or below highLoad after the 100th
	// insert. roundSize keeps nbuckets a power of two so we expect
	// 256 buckets here (next power of two above 100*rehashFactor).
	if got := tbl.nbuckets; got != 256 {
		t.Fatalf("nbuckets: got %d, want 256", got)
	}
	if load := float64(tbl.nentries) / float64(tbl.nbuckets); load > highLoad {
		t.Fatalf("load %.3f exceeds highLoad %.3f", load, highLoad)
	}
}

func TestForeachVisitsEveryEntry(t *testing.T) {
	tbl := New(intHash, intEq)
	want := map[int]int{}
	for i := range 50 {
		tbl.Set(i, i+1000)
		want[i] = i + 1000
	}

	got := map[int]int{}
	if err := tbl.Foreach(func(e *Entry) error {
		got[e.Key.(int)] = e.Value.(int)
		return nil
	}); err != nil {
		t.Fatalf("Foreach: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Foreach saw %d entries, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("Foreach[%d] = %d, want %d", k, got[k], v)
		}
	}
}

func TestForeachStopsOnError(t *testing.T) {
	tbl := New(intHash, intEq)
	for i := range 20 {
		tbl.Set(i, i)
	}
	stopErr := errSentinel("stop")
	visited := 0
	err := tbl.Foreach(func(*Entry) error {
		visited++
		if visited == 5 {
			return stopErr
		}
		return nil
	})
	if !errors.Is(err, stopErr) {
		t.Fatalf("Foreach error: got %v, want stop", err)
	}
	if visited != 5 {
		t.Fatalf("Foreach visited %d, want 5", visited)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestDestroyRunsValueDestructor(t *testing.T) {
	freed := []int{}
	dtor := func(v any) { freed = append(freed, v.(int)) }
	tbl := NewFull(intHash, intEq, nil, dtor)
	for i := range 10 {
		tbl.Set(i, i)
	}
	tbl.Destroy()
	if len(freed) != 10 {
		t.Fatalf("Destroy fired %d destructors, want 10", len(freed))
	}
}

func TestClearShrinksBuckets(t *testing.T) {
	tbl := New(intHash, intEq)
	for i := range 200 {
		tbl.Set(i, i)
	}
	tbl.Clear()
	if tbl.Len() != 0 {
		t.Fatalf("Len after Clear: %d", tbl.Len())
	}
	if tbl.nbuckets != initialSize {
		t.Fatalf("Clear should shrink to %d buckets, got %d", initialSize, tbl.nbuckets)
	}
}

func TestStealShrinksWhenLoadDropsLow(t *testing.T) {
	tbl := New(intHash, intEq)
	for i := range 200 {
		tbl.Set(i, i)
	}
	startBuckets := tbl.nbuckets
	for i := range 190 {
		tbl.Steal(i)
	}
	if tbl.nbuckets >= startBuckets {
		t.Fatalf("Steal should have shrunk bucket array (start=%d, end=%d)",
			startBuckets, tbl.nbuckets)
	}
	if tbl.Len() != 10 {
		t.Fatalf("Len: got %d, want 10", tbl.Len())
	}
}
