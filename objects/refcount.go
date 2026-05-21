package objects

// Incref bumps the refcount. gopy runs under a global mutator lock
// (Python's GIL), so the increment is a plain ++ matching CPython's
// GIL-build expansion of Py_INCREF. Immortal objects (refcount >=
// ImmortalRefcnt) skip the bump entirely so their refcount cannot
// overflow into the mortal range.
//
// CPython: Include/object.h:L605 Py_INCREF,
// Include/internal/pycore_object.h _Py_INCREF_IMMORTAL_STAT_INC
func Incref(o Object) {
	h := o.Hdr()
	if h.refcnt >= ImmortalRefcnt {
		return
	}
	h.refcnt++
}

// Decref drops the refcount. Immortal objects short-circuit before
// touching the counter so dealloc never fires for singletons, the
// small-int cache, or interned strings. Once a mortal counter hits
// zero, the type's Dealloc slot runs; until v0.10 lands the cycle
// collector, Dealloc is the only finalize hook.
//
// CPython: Include/object.h:L631 Py_DECREF,
// Include/internal/pycore_object.h _Py_DECREF_IMMORTAL_STAT_INC
func Decref(o Object) {
	h := o.Hdr()
	if h.refcnt >= ImmortalRefcnt {
		return
	}
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
