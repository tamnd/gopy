// Hooks the adaptive specializer reads off Type.

package objects

// VersionTag returns the type's tp_version_tag, allocating a fresh
// 32-bit version on first call. Returns 0 when the global counter
// has wrapped (the specializer treats this as "give up").
//
// CPython: Python/typeobject.c:L312 _PyType_AssignVersionTag
func (t *Type) VersionTag() uint32 {
	if t.versionTag != 0 {
		return t.versionTag
	}
	v := allocTypeVersionTag()
	if v == 0 {
		return 0
	}
	t.versionTag = v
	return v
}

// InvalidateVersionTag is gopy's port of CPython's
// type_modified_unlocked / PyType_Modified pair. It walks every
// subclass recursively (matching the invariant that a subclass's
// cached state must be cleared before the parent's), fires the
// registered watcher loop for this type, then zeroes the version
// tag so the next read allocates a fresh one.
//
// The recursion order matters: each subclass runs the full
// function (clear-its-subclasses, fire-its-watchers, then zero its
// tag) before this type's watcher fires and tag is zeroed. That
// way a subclass watcher sees the still-watched, still-valid
// state, exactly the ordering inside type_modified_unlocked.
//
// CPython: Objects/typeobject.c:1130 type_modified_unlocked /
// Objects/typeobject.c:1200 PyType_Modified
func (t *Type) InvalidateVersionTag() {
	if t.versionTag == 0 {
		return
	}
	for _, sub := range t.subclasses {
		if sub != nil {
			sub.InvalidateVersionTag()
		}
	}
	notifyTypeWatchers(t)
	t.versionTag = 0
	t.specCacheInit = nil
	t.specCacheInitVersion = 0
}

// CacheInitForSpecialization stashes init as the cached __init__ for
// CALL_ALLOC_AND_ENTER_INIT and stamps the type's current version into
// specCacheInitVersion. Returns the type version the caller should
// write into the CALL inline cache, plus a bool reporting whether the
// cache was populated (false when VersionTag returns 0, signalling the
// global counter has wrapped and specialization must give up).
//
// CPython: Objects/typeobject.c:6219 _PyType_CacheInitForSpecialization
func (t *Type) CacheInitForSpecialization(init *Function) (uint32, bool) {
	v := t.VersionTag()
	if v == 0 {
		return 0, false
	}
	t.specCacheInit = init
	t.specCacheInitVersion = v
	return v, true
}

// SpecCacheInit returns the cached __init__ function (or nil when
// nothing has been cached yet). Reads are paired with cache cell 2-3
// validation in the fast arm.
func (t *Type) SpecCacheInit() *Function { return t.specCacheInit }

// SpecCacheInitVersion returns the type version stamped at the time
// SpecCacheInit was populated. The fast arm rejects when this
// disagrees with the cache cell's stored version.
func (t *Type) SpecCacheInitVersion() uint32 { return t.specCacheInitVersion }

// PyTypeModified is the CPython entrypoint name; kept as an alias
// so the call sites in code that ports new CPython files line up
// 1-for-1 with PyType_Modified.
//
// CPython: Objects/typeobject.c:1200 PyType_Modified
func (t *Type) PyTypeModified() {
	t.InvalidateVersionTag()
}
