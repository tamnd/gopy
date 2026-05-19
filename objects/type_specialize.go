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

// InvalidateVersionTag clears the cached version so the next read
// allocates a fresh one. Mutators that change observable type state
// (Setattr, Setattro on a class) call this so old inline caches no
// longer match.
//
// Notifies any subscribed type watcher before zeroing the version
// tag so the optimizer's invalidation pass sees the type while it
// is still in its "watched" state. Mirrors the notify-then-zero
// ordering inside type_modified_unlocked: the watcher loop runs
// first, then set_version_unlocked(type, 0) writes the new tag.
//
// CPython: Objects/typeobject.c:1130 type_modified_unlocked /
// Objects/typeobject.c:1200 PyType_Modified
func (t *Type) InvalidateVersionTag() {
	if t.versionTag == 0 {
		return
	}
	notifyTypeWatchers(t)
	t.versionTag = 0
}
