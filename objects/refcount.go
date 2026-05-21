package objects

// Incref bumps the refcount. gopy runs under a global mutator lock
// (Python's GIL), so the increment is a plain ++ matching CPython's
// GIL-build expansion of Py_INCREF.
//
// CPython: Include/object.h:L605 Py_INCREF
func Incref(o Object) {
	o.Hdr().refcnt++
}

// Decref drops the refcount. If it reaches zero and the type defines
// a Dealloc slot, Dealloc is invoked. Until v0.10 lands the cycle
// collector, Dealloc is the only finalize hook.
//
// CPython: Include/object.h:L631 Py_DECREF
func Decref(o Object) {
	h := o.Hdr()
	h.refcnt--
	if h.refcnt != 0 {
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
