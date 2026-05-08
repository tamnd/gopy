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
// Fires TypeModifiedHook so registered type watchers (notably the
// Tier-2 optimizer's invalidation pass) can drop any cached state
// keyed on this type. Mirrors the type_modified_unlocked walk over
// interp->type_watchers in CPython.
//
// CPython: Objects/typeobject.c:1130 type_modified_unlocked /
// Objects/typeobject.c:1200 PyType_Modified
func (t *Type) InvalidateVersionTag() {
	t.versionTag = 0
	if hook := TypeModifiedHook; hook != nil {
		hook(t)
	}
}

// TypeModifiedHook is fired by InvalidateVersionTag whenever a type
// mutates. The Tier-2 optimizer installs a closure here at
// WatcherInit time so it can run DispatchTypeMutation without
// objects depending on optimizer.
//
// Single-interpreter assumption holds in v0.12; sub-interpreters
// (v0.13) need a per-interpreter registry instead.
//
// CPython: Objects/typeobject.c:1170-1188 (notify type watchers
// loop inside type_modified_unlocked)
var TypeModifiedHook func(t *Type)
