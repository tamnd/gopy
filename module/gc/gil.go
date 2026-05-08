// Collector-vs-mutator interlock counterpart to CPython's
// Python/gc_gil.c. Upstream the file holds a single function,
// _PyGC_ClearAllFreeLists, which the collector calls at the end of a
// generation pass to release per-type freelists back to pymalloc.
//
// gopy has no per-type freelists. PyTuple/PyList/PyDict/PyFrame all
// allocate through the Go runtime, which manages its own free pool.
// The function therefore reduces to a no-op; the call site stays so
// the collector shape mirrors CPython, and the body documents why
// nothing happens here.
//
// CPython: Python/gc_gil.c _PyGC_ClearAllFreeLists

package gc

// clearAllFreeLists is the gopy stand-in for _PyGC_ClearAllFreeLists.
// It runs at the tail of collectMain on the highest generation pass,
// where CPython would drop cached objects to give memory back to the
// OS. With Go's allocator there is nothing to release; the function
// is intentionally empty.
//
// CPython: Python/gc_gil.c:11 _PyGC_ClearAllFreeLists
func clearAllFreeLists() {}
