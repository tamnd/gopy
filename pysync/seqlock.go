package pysync

import (
	"runtime"
	"sync/atomic"
)

// SeqLock mirrors _PySeqLock from cpython/Python/lock.c. Writers
// take an exclusive lock that toggles the low bit on entry and exit;
// readers retry if the sequence number changed mid-read or is odd.
type SeqLock struct {
	sequence atomic.Uint32
}

func seqIsUpdating(s uint32) bool { return s&1 != 0 }

// LockWrite acquires write access by moving the sequence to an odd
// value. Concurrent writers spin until the lock is released.
func (s *SeqLock) LockWrite() {
	for {
		prev := s.sequence.Load()
		if seqIsUpdating(prev) {
			runtime.Gosched()
			continue
		}
		if s.sequence.CompareAndSwap(prev, prev+1) {
			return
		}
		runtime.Gosched()
	}
}

// AbandonWrite rolls back a LockWrite without publishing changes.
func (s *SeqLock) AbandonWrite() {
	s.sequence.Store(s.sequence.Load() - 1)
}

// UnlockWrite publishes changes and releases the lock by moving the
// sequence to an even value.
func (s *SeqLock) UnlockWrite() {
	s.sequence.Store(s.sequence.Load() + 1)
}

// BeginRead returns a sequence token to pass to EndRead. It spins
// until a non-updating sequence is observed.
func (s *SeqLock) BeginRead() uint32 {
	for {
		seq := s.sequence.Load()
		if !seqIsUpdating(seq) {
			return seq
		}
		runtime.Gosched()
	}
}

// EndRead returns true if the sequence number is unchanged since
// the matching BeginRead. Callers retry the read on a false result.
func (s *SeqLock) EndRead(previous uint32) bool {
	if s.sequence.Load() == previous {
		return true
	}
	runtime.Gosched()
	return false
}

// AfterFork resets the sequence if it was mid-update at fork time.
// Returns true if a reset happened.
func (s *SeqLock) AfterFork() bool {
	if seqIsUpdating(s.sequence.Load()) {
		s.sequence.Store(0)
		return true
	}
	return false
}
