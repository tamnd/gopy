package pythread

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestInitStubs(t *testing.T) {
	Init()
	Init() // idempotent
	if got := GetStacksize(); got != 0 {
		t.Errorf("GetStacksize() = %d, want 0", got)
	}
	if got := SetStacksize(1 << 20); got != -2 {
		t.Errorf("SetStacksize() = %d, want -2", got)
	}
}

func TestStartAndJoin(t *testing.T) {
	var ran atomic.Bool
	h := Start(func() { ran.Store(true) })
	if r := h.Join(); r != nil {
		t.Fatalf("Join returned panic: %v", r)
	}
	if !ran.Load() {
		t.Fatal("fn did not run")
	}
}

func TestJoinIdempotent(t *testing.T) {
	h := Start(func() {})
	first := h.Join()
	second := h.Join()
	if first != second {
		t.Fatalf("Join returned different values: %v vs %v", first, second)
	}
}

func TestIdentUnique(t *testing.T) {
	const N = 200
	idents := make([]Ident, N)
	var wg sync.WaitGroup
	for i := range N {
		wg.Go(func() {
			h := Start(func() {})
			idents[i] = h.Ident()
			h.Join()
		})
	}
	wg.Wait()
	seen := make(map[Ident]bool, N)
	for _, id := range idents {
		if id == 0 {
			t.Fatal("Ident is zero")
		}
		if seen[id] {
			t.Fatalf("duplicate ident %d", id)
		}
		seen[id] = true
	}
}

func TestPanicSurfacedViaJoin(t *testing.T) {
	h := Start(func() { panic("boom") })
	got := h.Join()
	if got != "boom" {
		t.Fatalf("Join = %v, want \"boom\"", got)
	}
}

func TestStartNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil fn")
		}
	}()
	Start(nil)
}

func TestTimeoutMaxNanoFits(t *testing.T) {
	if TimeoutMax <= 0 {
		t.Fatalf("TimeoutMax = %d, want positive", TimeoutMax)
	}
	// The POSIX invariant: microseconds * 1000 fits in int64.
	if TimeoutMax > (1<<63-1)/1000 {
		t.Fatalf("TimeoutMax * 1000 overflows int64")
	}
}
