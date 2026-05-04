package pysync

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/gopy/pythread"
)

func TestMutexBasic(t *testing.T) {
	var m Mutex
	if m.IsLocked() {
		t.Fatal("zero value reported locked")
	}
	m.Lock()
	if !m.IsLocked() {
		t.Fatal("after Lock IsLocked == false")
	}
	m.Unlock()
	if m.IsLocked() {
		t.Fatal("after Unlock IsLocked == true")
	}
}

func TestMutexTryLock(t *testing.T) {
	var m Mutex
	if !m.TryLock() {
		t.Fatal("first TryLock failed")
	}
	if m.TryLock() {
		t.Fatal("second TryLock should fail")
	}
	m.Unlock()
	if !m.TryLock() {
		t.Fatal("TryLock after unlock failed")
	}
	m.Unlock()
}

func TestMutexUnlockUnlocked(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var m Mutex
	m.Unlock()
}

func TestMutexStress(t *testing.T) {
	var m Mutex
	var counter int
	const N = 100
	const Iter = 1000
	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			for range Iter {
				m.Lock()
				counter++
				m.Unlock()
			}
		})
	}
	wg.Wait()
	if counter != N*Iter {
		t.Fatalf("counter = %d, want %d", counter, N*Iter)
	}
}

func TestMutexLockTimedTimeout(t *testing.T) {
	var m Mutex
	m.Lock()
	defer m.Unlock()
	start := time.Now()
	r := m.LockTimed(20*time.Millisecond, 0)
	elapsed := time.Since(start)
	if r != LockFailure {
		t.Fatalf("LockTimed = %v, want LockFailure", r)
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
}

func TestEvent(t *testing.T) {
	var e Event
	if e.IsSet() {
		t.Fatal("zero event reports set")
	}
	const N = 50
	var wg sync.WaitGroup
	var woken atomic.Int32
	for range N {
		wg.Go(func() {
			e.Wait()
			woken.Add(1)
		})
	}
	time.Sleep(10 * time.Millisecond)
	if woken.Load() != 0 {
		t.Fatal("waiters returned before Notify")
	}
	e.Notify()
	wg.Wait()
	if woken.Load() != N {
		t.Fatalf("woken = %d, want %d", woken.Load(), N)
	}
	if !e.IsSet() {
		t.Fatal("after Notify IsSet == false")
	}
	// Double Notify is safe.
	e.Notify()
	e.Wait() // returns immediately
}

func TestEventWaitTimedTimeout(t *testing.T) {
	var e Event
	if e.WaitTimed(10*time.Millisecond, false) {
		t.Fatal("WaitTimed returned true on unset event")
	}
	e.Notify()
	if !e.WaitTimed(time.Second, false) {
		t.Fatal("WaitTimed returned false on set event")
	}
}

func TestOnceFlag(t *testing.T) {
	var o OnceFlag
	var calls atomic.Int32
	const N = 50
	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			err := o.Do(func() error {
				calls.Add(1)
				time.Sleep(5 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("Do returned %v", err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("fn ran %d times, want 1", calls.Load())
	}
	if !o.Done() {
		t.Fatal("Done() == false after success")
	}
}

func TestOnceFlagRetriesOnError(t *testing.T) {
	var o OnceFlag
	var calls atomic.Int32
	want := errors.New("fail")
	if got := o.Do(func() error { calls.Add(1); return want }); !errors.Is(got, want) {
		t.Fatalf("first Do = %v", got)
	}
	if o.Done() {
		t.Fatal("Done() == true after error")
	}
	if got := o.Do(func() error { calls.Add(1); return nil }); got != nil {
		t.Fatalf("second Do = %v", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestRecursiveMutex(t *testing.T) {
	var r RecursiveMutex
	id := pythread.Ident(1)
	if r.IsLockedBy(id) {
		t.Fatal("zero value reports locked")
	}
	r.Lock(id)
	r.Lock(id)
	r.Lock(id)
	if !r.IsLockedBy(id) {
		t.Fatal("IsLockedBy false after self-locks")
	}
	r.Unlock(id)
	r.Unlock(id)
	if !r.IsLockedBy(id) {
		t.Fatal("released too early")
	}
	r.Unlock(id)
	if r.IsLockedBy(id) {
		t.Fatal("still owned after final unlock")
	}
}

func TestSeqLock(t *testing.T) {
	var s SeqLock
	var v atomic.Int64
	done := make(chan struct{})
	go func() {
		for range 1000 {
			s.LockWrite()
			v.Add(1)
			s.UnlockWrite()
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			seq := s.BeginRead()
			_ = v.Load()
			s.EndRead(seq)
			runtime.Gosched()
		}
	}
}

func TestCriticalSection(t *testing.T) {
	var m1, m2 Mutex
	var th CSThread
	var c1, c2 CriticalSection
	c1.Begin(&th, &m1)
	if th.Top() != &c1 {
		t.Fatal("c1 not on top")
	}
	c2.BeginMutex2(&th, &m1, &m2)
	if th.Top() != &c2 {
		t.Fatal("c2 not on top")
	}
	c2.End(&th)
	c1.End(&th)
	if th.Top() != nil {
		t.Fatal("stack not empty")
	}
}

func TestCriticalSectionMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on mismatched End")
		}
	}()
	var m Mutex
	var th CSThread
	var c1, c2 CriticalSection
	c1.Begin(&th, &m)
	c2.End(&th)
}

func TestParkUnpark(t *testing.T) {
	var word atomic.Uint32
	word.Store(1)
	done := make(chan ParkStatus, 1)
	go func() {
		done <- Park(unsafePtr(&word), func() bool { return word.Load() == 1 },
			-1, "tag", false)
	}()
	time.Sleep(10 * time.Millisecond)
	var seen any
	Unpark(unsafePtr(&word), func(parkArg any, _ bool) { seen = parkArg })
	if seen != "tag" {
		t.Fatalf("Unpark fn saw %v, want \"tag\"", seen)
	}
	if r := <-done; r != ParkOK {
		t.Fatalf("Park = %v, want ParkOK", r)
	}
}

func TestParkAgain(t *testing.T) {
	var word atomic.Uint32
	r := Park(unsafePtr(&word), func() bool { return false }, -1, nil, false)
	if r != ParkAgain {
		t.Fatalf("Park = %v, want ParkAgain", r)
	}
}

func TestParkTimeout(t *testing.T) {
	var word atomic.Uint32
	r := Park(unsafePtr(&word), func() bool { return true },
		15*time.Millisecond, nil, false)
	if r != ParkTimeout {
		t.Fatalf("Park = %v, want ParkTimeout", r)
	}
}

func TestUnparkAll(t *testing.T) {
	var word atomic.Uint32
	const N = 20
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range N {
		wg.Go(func() {
			r := Park(unsafePtr(&word), func() bool { return true }, -1, nil, false)
			if r == ParkOK {
				ok.Add(1)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	UnparkAll(unsafePtr(&word))
	wg.Wait()
	if ok.Load() != N {
		t.Fatalf("woke %d, want %d", ok.Load(), N)
	}
}
