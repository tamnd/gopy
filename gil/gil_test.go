package gil

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGILAcquireRelease(t *testing.T) {
	g := New()
	var ts struct{ id int }
	g.Acquire(&ts)
	if g.Holder() != &ts {
		t.Error("holder mismatch after acquire")
	}
	g.Release(&ts)
	if g.Holder() != nil {
		t.Error("holder should be nil after release")
	}
}

func TestGILWaitsForRelease(t *testing.T) {
	g := New()
	a := &struct{}{}
	b := &struct{}{}
	g.Acquire(a)

	got := make(chan struct{})
	go func() {
		g.Acquire(b)
		close(got)
		g.Release(b)
	}()

	select {
	case <-got:
		t.Fatal("second acquire should have blocked")
	case <-time.After(20 * time.Millisecond):
	}
	g.Release(a)
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("second acquire never woke")
	}
}

func TestGILRequestDrop(t *testing.T) {
	g := New()
	if g.DropRequested() {
		t.Error("fresh GIL should not have drop requested")
	}
	g.RequestDrop()
	if !g.DropRequested() {
		t.Error("RequestDrop should set the flag")
	}
	a := &struct{}{}
	g.Acquire(a)
	if g.DropRequested() {
		t.Error("Acquire should clear the drop-request flag")
	}
	g.Release(a)
}

func TestGILMismatchedReleasePanics(t *testing.T) {
	g := New()
	a := &struct{}{}
	b := &struct{}{}
	g.Acquire(a)
	defer g.Release(a)
	defer func() {
		if recover() == nil {
			t.Error("Release by non-holder should panic")
		}
	}()
	g.Release(b)
}

func TestGILSwitchInterval(t *testing.T) {
	g := New()
	g.SetSwitchInterval(7 * time.Millisecond)
	if g.SwitchInterval() != 7*time.Millisecond {
		t.Errorf("interval: %v", g.SwitchInterval())
	}
}

func TestBreakerSetClear(t *testing.T) {
	var b Breaker
	if b.Load() != 0 {
		t.Error("fresh breaker should be zero")
	}
	b.Set(BreakerGILDropRequest)
	b.Set(BreakerCallsPending)
	if !b.IsSet(BreakerGILDropRequest) || !b.IsSet(BreakerCallsPending) {
		t.Error("bits not set")
	}
	if b.IsSet(BreakerSignalsPending) {
		t.Error("unrelated bit set")
	}
	b.Clear(BreakerGILDropRequest)
	if b.IsSet(BreakerGILDropRequest) {
		t.Error("bit not cleared")
	}
	if !b.IsSet(BreakerCallsPending) {
		t.Error("Clear hit the wrong bit")
	}
}

func TestBreakerConcurrent(t *testing.T) {
	var b Breaker
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); b.Set(BreakerCallsPending) }()
		go func() { defer wg.Done(); b.Clear(BreakerCallsPending) }()
	}
	wg.Wait()
	// Final state isn't deterministic; we just want race-detector clean.
	_ = b.Load()
}

func TestBreakerBitValues(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"GILDropRequest", BreakerGILDropRequest, 1 << 0},
		{"SignalsPending", BreakerSignalsPending, 1 << 1},
		{"CallsPending", BreakerCallsPending, 1 << 2},
		{"AsyncException", BreakerAsyncException, 1 << 3},
		{"GCScheduled", BreakerGCScheduled, 1 << 4},
		{"PleaseStop", BreakerPleaseStop, 1 << 5},
		{"ExplicitMerge", BreakerExplicitMerge, 1 << 6},
		{"JITInvalidateCold", BreakerJITInvalidateCold, 1 << 7},
		{"EventsMask", BreakerEventsMask, (1 << 8) - 1},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", c.name, c.got, c.want)
		}
	}
}

func TestPendingFIFO(t *testing.T) {
	var p Pending
	var got []int
	for i := 0; i < 5; i++ {
		if err := p.Add(func() error { got = append(got, i); return nil }); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if p.Len() != 5 {
		t.Errorf("Len = %d, want 5", p.Len())
	}
	if err := p.Drain(); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if p.Len() != 0 {
		t.Errorf("Len after drain = %d, want 0", p.Len())
	}
	for i, v := range got {
		if v != i {
			t.Errorf("FIFO violated: got %v", got)
			break
		}
	}
}

func TestPendingOverflow(t *testing.T) {
	var p Pending
	for i := 0; i < PendingQueueSize; i++ {
		if err := p.Add(func() error { return nil }); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if err := p.Add(func() error { return nil }); !errors.Is(err, ErrPendingFull) {
		t.Errorf("overflow err = %v, want ErrPendingFull", err)
	}
	if p.Len() != PendingQueueSize {
		t.Errorf("overflow lost entries: Len = %d", p.Len())
	}
}

func TestPendingDrainStopsOnError(t *testing.T) {
	var p Pending
	boom := errors.New("boom")
	p.Add(func() error { return nil })
	p.Add(func() error { return boom })
	p.Add(func() error { return nil })
	if err := p.Drain(); !errors.Is(err, boom) {
		t.Fatalf("Drain err = %v, want boom", err)
	}
	if p.Len() != 1 {
		t.Errorf("post-error Len = %d, want 1", p.Len())
	}
}

func TestSignalBridgeDeliverUnhandled(t *testing.T) {
	var b Breaker
	var p Pending
	bridge := NewSignalBridge(&b, &p)
	bridge.deliver(fakeSignal{})
	if b.IsSet(BreakerSignalsPending) {
		t.Error("unhandled signal should not set bits")
	}
}

func TestSignalBridgeDeliverHandled(t *testing.T) {
	var b Breaker
	var p Pending
	bridge := NewSignalBridge(&b, &p)

	called := make(chan struct{}, 1)
	bridge.handlers[fakeSignal{}] = func() error { called <- struct{}{}; return nil }

	bridge.deliver(fakeSignal{})
	if !b.IsSet(BreakerSignalsPending) || !b.IsSet(BreakerCallsPending) {
		t.Error("delivered signal should set both bits")
	}
	if err := p.Drain(); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	select {
	case <-called:
	default:
		t.Error("handler did not run")
	}
}

type fakeSignal struct{}

func (fakeSignal) String() string { return "fake" }
func (fakeSignal) Signal()        {}
