package pysync

// CriticalSection mirrors PyCriticalSection (PEP 703).
//
// The full design serializes access to a Python object across threads
// when the GIL is disabled. In a GIL build (the default and the only
// build gopy supports in v0.1) the GIL already serializes, so Begin
// and End reduce to bookkeeping: push and pop a per-thread linked
// stack. v0.14 will turn the bookkeeping into real mutex acquisitions
// when the pygil_disabled tag is enabled.
//
// The per-thread stack head lives in CSThread for now because gopy
// has no goroutine-local thread state. v0.7 (state package) will
// fold this into the real thread state.
//
// CPython: Include/cpython/critical_section.h:L108 PyCriticalSection
type CriticalSection struct {
	prev   *CriticalSection
	mutex  *Mutex
	mutex2 *Mutex
}

// CSThread holds the critical-section stack head for a single thread
// of execution. Pass the same value to every Begin and End call from
// the goroutine that owns it.
//
// CPython: Python/critical_section.c:L95 _PyCriticalSection_SuspendAll (adapted from)
type CSThread struct {
	top *CriticalSection
}

// Begin starts a critical section guarded by m. In v0.1 (GIL build)
// this only pushes onto the per-thread stack. v0.14 will acquire m.
//
// CPython: Python/critical_section.c:L21 _PyCriticalSection_BeginSlow
func (c *CriticalSection) Begin(t *CSThread, m *Mutex) {
	c.prev = t.top
	c.mutex = m
	c.mutex2 = nil
	t.top = c
}

// BeginMutex2 starts a critical section guarding two mutexes. The
// acquisition order is fixed by m1 then m2 to prevent deadlocks
// between callers that touch the same pair, mirroring
// _PyCriticalSection2 in C.
//
// CPython: Python/critical_section.c:L66 _PyCriticalSection2_BeginSlow
func (c *CriticalSection) BeginMutex2(t *CSThread, m1, m2 *Mutex) {
	c.prev = t.top
	c.mutex = m1
	c.mutex2 = m2
	t.top = c
}

// End pops the section. It panics if c is not on top of t's stack,
// which would indicate a Begin/End mismatch.
//
// CPython: Python/critical_section.c:L173 PyCriticalSection_End
func (c *CriticalSection) End(t *CSThread) {
	if t.top != c {
		panic("pysync: critical section End mismatched")
	}
	t.top = c.prev
	c.prev = nil
	c.mutex = nil
	c.mutex2 = nil
}

// Top returns the innermost critical section, or nil if none is
// active.
//
// CPython: Python/critical_section.c:L95 _PyCriticalSection_SuspendAll (adapted from)
func (t *CSThread) Top() *CriticalSection { return t.top }
