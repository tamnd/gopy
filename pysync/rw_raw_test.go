package pysync

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRWMutexBasic(t *testing.T) {
	var r RWMutex
	var counter int
	r.RLock()
	r.RLock()
	_ = counter
	r.RUnlock()
	r.RUnlock()
	r.Lock()
	counter++
	r.Unlock()
	if counter != 1 {
		t.Fatalf("counter = %d", counter)
	}
}

func TestRWMutexWriterBlocksReaders(t *testing.T) {
	var r RWMutex
	r.Lock()
	gotRead := make(chan struct{})
	go func() {
		r.RLock()
		close(gotRead)
		r.RUnlock()
	}()
	select {
	case <-gotRead:
		t.Fatal("reader acquired while writer held the lock")
	case <-time.After(15 * time.Millisecond):
	}
	r.Unlock()
	<-gotRead
}

func TestRWMutexWriterFairness(t *testing.T) {
	var r RWMutex
	r.RLock()
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(writerStarted)
		r.Lock()
		close(writerDone)
		r.Unlock()
	}()
	<-writerStarted
	time.Sleep(10 * time.Millisecond)

	// New reader must wait behind the parked writer.
	gotReader := make(chan struct{})
	go func() {
		r.RLock()
		close(gotReader)
		r.RUnlock()
	}()
	select {
	case <-gotReader:
		t.Fatal("reader slipped past parked writer")
	case <-time.After(15 * time.Millisecond):
	}
	r.RUnlock()
	<-writerDone
	<-gotReader
}

func TestRWMutexStress(t *testing.T) {
	var r RWMutex
	var counter int64
	const N = 50
	const Iter = 200
	var wg sync.WaitGroup
	for i := range N {
		isWriter := i%5 == 0
		wg.Go(func() {
			for range Iter {
				if isWriter {
					r.Lock()
					counter++
					r.Unlock()
				} else {
					r.RLock()
					_ = atomic.LoadInt64(&counter)
					r.RUnlock()
				}
			}
		})
	}
	wg.Wait()
}

func TestRawMutexBasic(t *testing.T) {
	var m RawMutex
	var n int
	m.Lock()
	n++
	m.Unlock()
	if n != 1 {
		t.Fatal("RawMutex did not allow critical section")
	}
}

func TestRawMutexUnlockUnlockedPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var m RawMutex
	m.Unlock()
}

func TestRawMutexStress(t *testing.T) {
	var m RawMutex
	var counter int
	const N = 50
	const Iter = 500
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
