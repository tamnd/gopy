package objects

// nomemory ports the allocation-fault injector CPython exposes through
// _testcapi.set_nomemory / _testcapi.remove_mem_hooks. CPython installs a
// PyMem hook (Modules/_testcapimodule.c FmHook) that counts every raw
// allocation request and returns NULL when the running count falls in the
// armed [start, stop) window (stop <= 0 means unbounded). The test suite
// uses it to drive deterministic MemoryError paths: a precise allocation
// index is made to fail, and the code under test must degrade gracefully.
//
// gopy has no single C allocator chokepoint to hook, so the fault is
// consumed at the specific allocation sites the vendored suite exercises
// while the injector is armed: list-literal build-up (test_list.test_no_memory)
// and the managed-dict detach at instance dealloc
// (test_class.test_detach_materialized_dict_no_memory). Each call to
// consumeAllocFault advances the counter exactly as the C hook does on
// every allocation request, so the start/stop window selects the same
// ordinal allocation it would in CPython.
//
// CPython: Modules/_testcapimodule.c:2050 set_nomemory / hook_malloc
var (
	nomemoryArmed bool
	nomemoryCount int64
	nomemoryStart int64
	nomemoryStop  int64
)

// SetNoMemory arms the injector. The next allocation request whose ordinal
// is >= start fails, and failures continue until the ordinal reaches stop
// (stop <= 0 leaves the window open-ended). The counter resets to zero so
// the ordinals are measured from the moment the injector is armed, matching
// the C hook which zeroes FmHook.count in set_nomemory.
//
// CPython: Modules/_testcapimodule.c:2050 set_nomemory
func SetNoMemory(start, stop int64) {
	nomemoryArmed = true
	nomemoryCount = 0
	nomemoryStart = start
	nomemoryStop = stop
}

// RemoveNoMemory disarms the injector, mirroring remove_mem_hooks which
// restores the original allocator.
//
// CPython: Modules/_testcapimodule.c:2075 remove_mem_hooks
func RemoveNoMemory() { nomemoryArmed = false }

// ConsumeAllocFault reports whether the allocation request at the current
// ordinal must fail, advancing the counter regardless of the outcome. The
// always-advance step matches hook_malloc, which increments FmHook.count on
// every request so that a one-shot window like set_nomemory(0, 1) fails
// exactly the first request and then lets the rest succeed.
//
// CPython: Modules/_testcapimodule.c hook_malloc
func ConsumeAllocFault() bool {
	if !nomemoryArmed {
		return false
	}
	n := nomemoryCount
	nomemoryCount++
	if n < nomemoryStart {
		return false
	}
	if nomemoryStop > 0 && n >= nomemoryStop {
		return false
	}
	return true
}
