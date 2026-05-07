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
// CPython: Python/typeobject.c:L301 _PyType_Modified (the
// PyType_Modified entry point)
func (t *Type) InvalidateVersionTag() {
	t.versionTag = 0
}
