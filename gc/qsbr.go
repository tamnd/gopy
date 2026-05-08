// Package gc port of Python/qsbr.c. Quiescent state-based reclamation
// for the free-threaded build: writers stamp a per-interpreter write
// sequence whenever they retire a pointer, and readers periodically
// publish the sequence they have observed. Once every reader has
// passed the goal sequence, the writer can safely free the retired
// memory.
//
// gopy ports the bookkeeping verbatim. The grow-the-array path in
// Reserve calls StopTheWorld / StartTheWorld in upstream so no other
// thread observes a moving array; gopy has no stop-the-world hook
// yet, so the calls are placeholders documented inline. The mutex on
// QSBRShared still serializes the resize itself.
//
// CPython: Python/qsbr.c (Py_GIL_DISABLED #ifdef block)

package gc

import (
	"sync/atomic"

	"github.com/tamnd/gopy/pysync"
)

// QSBR sequence-number constants. The write sequence stays odd
// (incremented by 2) so the offline sentinel 0 is unambiguous.
//
// CPython: Include/internal/pycore_qsbr.h:27-29 QSBR_OFFLINE / QSBR_INITIAL /
// QSBR_INCR
const (
	QSBROffline uint64 = 0
	QSBRInitial uint64 = 1
	QSBRIncr    uint64 = 2
)

// minQSBRArraySize is the smallest QSBR array size grow_thread_array
// will pick on first allocation.
//
// CPython: Python/qsbr.c:42 MIN_ARRAY_SIZE
const minQSBRArraySize = 8

// QSBRLT reports a < b under wrap-around-safe comparison.
//
// CPython: Include/internal/pycore_qsbr.h:34 QSBR_LT
func QSBRLT(a, b uint64) bool {
	return int64(a-b) < 0
}

// QSBRLEQ reports a <= b under wrap-around-safe comparison.
//
// CPython: Include/internal/pycore_qsbr.h:35 QSBR_LEQ
func QSBRLEQ(a, b uint64) bool {
	return int64(a-b) <= 0
}

// QSBRThreadState is the per-thread QSBR slot. seq is the last
// sequence the thread observed (or QSBROffline when detached).
//
// CPython: Include/internal/pycore_qsbr.h:41-70 _qsbr_thread_state
type QSBRThreadState struct {
	Seq atomic.Uint64

	Shared *QSBRShared

	TState any

	DeferredCount      int
	DeferredMemory     uint64
	DeferredPageMemory uint64
	ShouldProcess      bool

	Allocated    bool
	freelistNext *QSBRThreadState
}

// QSBRPad pads QSBRThreadState out to a 64-byte cache line so two
// threads do not false-share their sequence stores.
//
// CPython: Include/internal/pycore_qsbr.h:73-76 _qsbr_pad
type QSBRPad struct {
	QSBR QSBRThreadState
	_    [64]byte // cache-line padding
}

// QSBRShared is the per-interpreter QSBR state. wrSeq advances on
// every Advance; rdSeq is the minimum observed sequence across all
// attached threads, computed lazily by qsbrPollScan.
//
// CPython: Include/internal/pycore_qsbr.h:79-94 _qsbr_shared
type QSBRShared struct {
	WrSeq atomic.Uint64
	RdSeq atomic.Uint64

	array []QSBRPad
	size  int

	mu       pysync.Mutex
	freelist *QSBRThreadState
}

// Init seeds wrSeq and rdSeq to QSBRInitial. The runtime-init macro
// in CPython does this statically; gopy needs an explicit call so a
// freshly-constructed QSBRShared does not look like every reader is
// permanently offline (Seq == 0 == QSBROffline).
//
// CPython: Include/internal/pycore_runtime_init.h:143-144 .wr_seq /
// .rd_seq = QSBR_INITIAL
func (s *QSBRShared) Init() {
	s.WrSeq.Store(QSBRInitial)
	s.RdSeq.Store(QSBRInitial)
}

// SharedCurrent returns the latest write sequence.
//
// CPython: Include/internal/pycore_qsbr.h:96-100 _Py_qsbr_shared_current
func (s *QSBRShared) SharedCurrent() uint64 {
	return s.WrSeq.Load()
}

// QuiescentState publishes the latest write sequence as the thread's
// observed read sequence. Called at points where the thread holds no
// shared pointers needing protection.
//
// CPython: Include/internal/pycore_qsbr.h:104-109 _Py_qsbr_quiescent_state
func (s *QSBRShared) QuiescentState(qsbr *QSBRThreadState) {
	qsbr.Seq.Store(s.SharedCurrent())
}

// GoalReached reports whether every reader has published a sequence
// at or after goal. Cheaper than Poll because it skips the per-thread
// scan.
//
// CPython: Include/internal/pycore_qsbr.h:113-118 _Py_qbsr_goal_reached
func GoalReached(qsbr *QSBRThreadState, goal uint64) bool {
	return QSBRLEQ(goal, qsbr.Shared.RdSeq.Load())
}

// Advance bumps the write sequence by QSBRIncr and returns the new
// value. Callers use the return value as a goal for Poll.
//
// CPython: Python/qsbr.c:113-120 _Py_qsbr_advance
func (s *QSBRShared) Advance() uint64 {
	return s.WrSeq.Add(QSBRIncr)
}

// SharedNext returns the next sequence value (current + increment)
// without bumping the writer.
//
// CPython: Python/qsbr.c:122-126 _Py_qsbr_shared_next
func (s *QSBRShared) SharedNext() uint64 {
	return s.SharedCurrent() + QSBRIncr
}

// qsbrPollScan walks the per-thread sequences, computes the minimum,
// and publishes it as the shared read sequence (winning the CAS race
// is best-effort).
//
// CPython: Python/qsbr.c:128-157 qsbr_poll_scan
func (s *QSBRShared) qsbrPollScan() uint64 {
	minSeq := s.WrSeq.Load()
	for i := 0; i < s.size; i++ {
		seq := s.array[i].QSBR.Seq.Load()
		if seq != QSBROffline && QSBRLT(seq, minSeq) {
			minSeq = seq
		}
	}
	rdSeq := s.RdSeq.Load()
	if QSBRLT(rdSeq, minSeq) {
		s.RdSeq.CompareAndSwap(rdSeq, minSeq)
		rdSeq = minSeq
	}
	return rdSeq
}

// Poll reports whether goal has been reached. Triggers a scan when
// the cached rdSeq is too old.
//
// CPython: Python/qsbr.c:159-171 _Py_qsbr_poll
func Poll(qsbr *QSBRThreadState, goal uint64) bool {
	if GoalReached(qsbr, goal) {
		return true
	}
	rdSeq := qsbr.Shared.qsbrPollScan()
	return QSBRLEQ(goal, rdSeq)
}

// Attach marks qsbr as online by stamping the current write sequence
// into qsbr.Seq. Caller must not have an outstanding attach.
//
// CPython: Python/qsbr.c:173-180 _Py_qsbr_attach
func Attach(qsbr *QSBRThreadState) {
	qsbr.Seq.Store(qsbr.Shared.SharedCurrent())
}

// Detach marks qsbr as offline so the next scan ignores it. Caller
// must not have outstanding pointer accesses to shared data.
//
// CPython: Python/qsbr.c:182-188 _Py_qsbr_detach
func Detach(qsbr *QSBRThreadState) {
	qsbr.Seq.Store(QSBROffline)
}

// qsbrAllocate pops the head off the freelist. Caller must hold s.mu.
// Returns nil when the freelist is empty.
//
// CPython: Python/qsbr.c:45-57 qsbr_allocate
func (s *QSBRShared) qsbrAllocate() *QSBRThreadState {
	qsbr := s.freelist
	if qsbr == nil {
		return nil
	}
	s.freelist = qsbr.freelistNext
	qsbr.freelistNext = nil
	qsbr.Shared = s
	qsbr.Allocated = true
	return qsbr
}

// initializeNewArray walks the array after a grow and re-threads any
// unallocated entries onto the freelist.
//
// CPython: Python/qsbr.c:60-76 initialize_new_array
func (s *QSBRShared) initializeNewArray() {
	for i := 0; i < s.size; i++ {
		qsbr := &s.array[i].QSBR
		if !qsbr.Allocated {
			qsbr.freelistNext = s.freelist
			s.freelist = qsbr
		}
	}
}

// growThreadArray doubles the array (or jumps to minQSBRArraySize on
// first grow) and rebuilds the freelist. Caller holds s.mu and must
// have already paused the world so no thread is reading the old
// array.
//
// CPython: Python/qsbr.c:79-111 grow_thread_array
func (s *QSBRShared) growThreadArray() {
	newSize := s.size * 2
	if newSize < minQSBRArraySize {
		newSize = minQSBRArraySize
	}
	newArray := make([]QSBRPad, newSize)
	for i := 0; i < s.size; i++ {
		newArray[i].QSBR.Seq.Store(s.array[i].QSBR.Seq.Load())
		newArray[i].QSBR.Shared = s.array[i].QSBR.Shared
		newArray[i].QSBR.TState = s.array[i].QSBR.TState
		newArray[i].QSBR.DeferredCount = s.array[i].QSBR.DeferredCount
		newArray[i].QSBR.DeferredMemory = s.array[i].QSBR.DeferredMemory
		newArray[i].QSBR.DeferredPageMemory = s.array[i].QSBR.DeferredPageMemory
		newArray[i].QSBR.ShouldProcess = s.array[i].QSBR.ShouldProcess
		newArray[i].QSBR.Allocated = s.array[i].QSBR.Allocated
	}
	s.array = newArray
	s.size = newSize
	s.freelist = nil
	s.initializeNewArray()
}

// Reserve allocates a QSBR slot and returns its index. Returns -1 if
// the array could not be grown.
//
// gopy NOTE: upstream brackets growThreadArray with StopTheWorld /
// StartTheWorld; gopy has no stop-the-world hook yet, so the lock on
// s.mu is the only serialization. The freelist invariant still
// holds: the array slice is rebuilt before any reader sees the new
// indices.
//
// CPython: Python/qsbr.c:190-217 _Py_qsbr_reserve
func (s *QSBRShared) Reserve() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	qsbr := s.qsbrAllocate()
	if qsbr == nil {
		s.growThreadArray()
		qsbr = s.qsbrAllocate()
	}
	if qsbr == nil {
		return -1
	}
	for i := 0; i < s.size; i++ {
		if &s.array[i].QSBR == qsbr {
			return i
		}
	}
	return -1
}

// Register binds tstate to the QSBR slot at index. tstate is stored
// as any so package gc stays free of a state import.
//
// CPython: Python/qsbr.c:219-232 _Py_qsbr_register
func (s *QSBRShared) Register(tstate any, index int) *QSBRThreadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= s.size {
		return nil
	}
	qsbr := &s.array[index].QSBR
	qsbr.TState = tstate
	return qsbr
}

// Unregister releases qsbr back to the freelist. Caller must have
// already detached qsbr (Seq == QSBROffline).
//
// CPython: Python/qsbr.c:234-260 _Py_qsbr_unregister
func (s *QSBRShared) Unregister(qsbr *QSBRThreadState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	qsbr.TState = nil
	qsbr.Allocated = false
	qsbr.freelistNext = s.freelist
	s.freelist = qsbr
}

// Fini drops the array. Called from the interpreter teardown.
//
// CPython: Python/qsbr.c:262-271 _Py_qsbr_fini
func (s *QSBRShared) Fini() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.array = nil
	s.size = 0
	s.freelist = nil
}

// AfterFork rebuilds the freelist so only the surviving thread's
// slot stays allocated. Other allocated slots come from threads that
// did not fork, so they are returned to the freelist.
//
// CPython: Python/qsbr.c:273-290 _Py_qsbr_after_fork
func (s *QSBRShared) AfterFork(survivor *QSBRThreadState) {
	for i := 0; i < s.size; i++ {
		qsbr := &s.array[i].QSBR
		if qsbr != survivor && qsbr.Allocated {
			qsbr.TState = nil
			qsbr.Allocated = false
			qsbr.freelistNext = s.freelist
			s.freelist = qsbr
		}
	}
}
