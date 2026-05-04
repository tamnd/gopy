package objects

// Incref bumps the refcount. Concurrent Increfs are safe.
//
// CPython: Include/object.h:L605 Py_INCREF
func Incref(o Object) {
	o.Hdr().refcnt.Add(1)
}

// Decref drops the refcount. If it reaches zero and the type defines
// a Dealloc slot, Dealloc is invoked. Until v0.10 lands the cycle
// collector, Dealloc is the only finalize hook.
//
// CPython: Include/object.h:L631 Py_DECREF
func Decref(o Object) {
	if o.Hdr().refcnt.Add(-1) != 0 {
		return
	}
	if dealloc := o.Type().Dealloc; dealloc != nil {
		dealloc(o)
	}
}

// Is reports identity equality, the Python `is` operator. Mirrors
// Py_Is.
//
// CPython: Include/object.h:L257 Py_Is
func Is(a, b Object) bool {
	return a == b
}
