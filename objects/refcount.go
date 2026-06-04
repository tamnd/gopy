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
// zero, the type's Dealloc slot runs.
//
// When Dealloc is not set but Finalize is (the common shape for user
// classes that define __del__ but inherit object's tp_dealloc), the
// Finalize slot runs in its place. CPython's subtype_dealloc routes
// through _PyObject_CallFinalizerFromDealloc to call tp_finalize before
// memory is reclaimed; gopy mirrors that here so __del__ fires
// synchronously on the last drop.
//
// CPython: Include/object.h:L631 Py_DECREF,
// Include/internal/pycore_object.h _Py_DECREF_IMMORTAL_STAT_INC
// CPython: Objects/typeobject.c:1450 subtype_dealloc
//
//	(PyObject_CallFinalizerFromDealloc call before deallocation)
func Decref(o Object) {
	h := o.Hdr()
	if h.refcnt >= ImmortalRefcnt {
		return
	}
	h.refcnt--
	if h.refcnt != 0 {
		return
	}
	t := o.Type()
	if t == nil {
		return
	}
	if t.Finalize != nil && !h.finalized {
		h.finalized = true
		h.refcnt = 1
		t.Finalize(o)
		h.refcnt--
		if h.refcnt > 0 {
			return
		}
	}
	// Drop the object from the cycle collector's tracked map only when
	// the type has a Finalize slot. This guards generators (and other
	// PEP 442 finalizable types) so that gc.collect() does not re-run
	// genFinalize on a corpse and inadvertently close-send into adjacent
	// live generators. For non-finalizable containers (list, dict,
	// tuple, set) we leave the object on the tracked list at refcnt 0;
	// the next cycle pass walks tp_traverse, finds zero external refs,
	// reclaims through reclaimUnreachable, and fires any registered
	// weakref callbacks via handleWeakrefs. Untracking here would steal
	// that work and the weakref dies silently.
	//
	// CPython: Include/internal/pycore_object.h:248 _PyObject_GC_UNTRACK
	// CPython: Objects/typeobject.c subtype_dealloc (tp_dealloc untrack)
	if t.Finalize != nil {
		if hook := GCUntrackHook; hook != nil {
			hook(o)
		}
	}
	// Clear weakrefs before tp_dealloc runs. CPython has each tp_dealloc
	// call PyObject_ClearWeakRefs (via subtype_dealloc for user classes,
	// directly for built-ins with tp_weaklistoffset). Doing it here covers
	// every Object whose Header carries a non-empty weakref list,
	// regardless of whether the type defines its own Dealloc slot.
	//
	// CPython: Objects/typeobject.c:1450 subtype_dealloc
	// (PyObject_ClearWeakRefs call before slot teardown)
	if h.weakrefs != nil {
		ClearWeakRefs(o)
	}
	if t.Dealloc != nil {
		t.Dealloc(o)
	}
}

// Is reports identity equality, the Python `is` operator. Mirrors
// Py_Is.
//
// CPython: Include/object.h:L257 Py_Is
func Is(a, b Object) bool {
	return a == b
}
