// Cache validation versions used by the adaptive specializer.
//
// CPython tracks two monotonic 32-bit counters that the specializer
// stamps into inline caches and the dispatch loop checks on every
// hit: tp_version_tag (per Type, bumps on type or mro mutation) and
// dk_version (per dict-keys, bumps on key insert or delete).
// gopy hangs them off the matching object structs and allocates from
// a process-wide atomic counter on first read; mutations that would
// invalidate a cached version reset the field to zero so the next
// read re-allocates and previously cached entries no longer match.
//
// CPython: Include/internal/pycore_typeobject.h tp_version_tag
// CPython: Include/internal/pycore_dict.h dk_version
// CPython: Objects/dictobject.c:7150 _PyDict_GetKeysVersionForCurrentState

package objects

import "sync/atomic"

// nextDictKeysVersion feeds Dict.GetKeysVersion. It starts at 1 so
// 0 stays a "not yet allocated / invalidated" sentinel.
//
// CPython: Include/internal/pycore_interp_structs.h dict_state.next_keys_version
var nextDictKeysVersion atomic.Uint32

// nextTypeVersionTag feeds Type.VersionTag. Same convention: 0 is
// reserved as the "not yet allocated / invalidated" sentinel.
//
// CPython: Python/typeobject.c:L325 next_version_tag
var nextTypeVersionTag atomic.Uint32

// allocDictKeysVersion returns a fresh dk_version, or 0 if the
// global counter has wrapped (in which case the specializer treats
// the dict as unverifiable and refuses to specialize).
//
// CPython: Objects/dictobject.c:7128 next_dict_keys_version
func allocDictKeysVersion() uint32 {
	v := nextDictKeysVersion.Add(1)
	if v == 0 {
		// The counter wrapped through 0; future calls keep returning 0
		// so the specializer permanently bails out. Matches CPython.
		return 0
	}
	return v
}

// allocTypeVersionTag returns a fresh tp_version_tag, or 0 on wrap.
//
// CPython: Python/typeobject.c:L320 assign_version_tag
func allocTypeVersionTag() uint32 {
	v := nextTypeVersionTag.Add(1)
	if v == 0 {
		return 0
	}
	return v
}
