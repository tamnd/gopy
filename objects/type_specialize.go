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
}

// PyTypeModified is the CPython entrypoint name; kept as an alias
// so the call sites in code that ports new CPython files line up
// 1-for-1 with PyType_Modified.
//
// CPython: Objects/typeobject.c:1200 PyType_Modified
func (t *Type) PyTypeModified() {
	t.InvalidateVersionTag()
}
