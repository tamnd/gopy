// Type-watcher public API. Ports CPython's PyType_AddWatcher /
// ClearWatcher / Watch / Unwatch surface plus the type_modified
// dispatch helper so any C-level consumer (and gopy's Tier-2
// optimizer) can subscribe to type-modification events without
// poking type internals.
//
// Design notes worth keeping for future readers:
//
//  1. CPython stashes the watcher callback table on
//     PyInterpreterState->type_watchers[TYPE_MAX_WATCHERS].
//     gopy is still single-interpreter at v0.12 so the table is a
//     package-level array. v0.13's sub-interpreter work needs to
//     promote this onto state.Interpreter; InvalidateVersionTag is
//     the only call site that reads the table today.
//
//  2. Subscription state lives on the Type itself (`tpWatched`,
//     8-bit mask, one bit per watcher slot) exactly like CPython's
//     tp_watched (Include/cpython/object.h:234). That keeps the
//     "is anyone watching this type?" check on a single load and
//     bitwise-and rather than a per-watcher map lookup. The
//     previous gopy design routed every type-modify through a
//     single TypeModifiedHook global; replacing it with the
//     bitmask matches CPython's data layout and lets multiple
//     watchers coexist (the optimizer keeps slot 0; users start
//     at slot 1).
//
//  3. Watcher slot 0 is reserved for CPython. The Tier-2 optimizer
//     uses it for the global type-mutation invalidation watcher,
//     mirroring optimizer_analysis.c. PyType_AddWatcher returns
//     slot 1 or higher; the reserved slot has to be installed with
//     the internal helper TypeSetReservedWatcher.
//
//  4. The callback signature mirrors PyType_WatchCallback: it
//     receives the mutated type and returns int (<0 signals an
//     unraisable; gopy currently swallows the return until the
//     unraisable hook lands).
//
// CPython: Objects/typeobject.c:1016 PyType_AddWatcher /
// Objects/typeobject.c:1047 PyType_ClearWatcher /
// Objects/typeobject.c:1060 PyType_Watch /
// Objects/typeobject.c:1080 PyType_Unwatch /
// Objects/typeobject.c:1170-1188 (notify loop inside
// type_modified_unlocked)

package objects

import "fmt"

// TypeWatchCallback is the signature CPython expects from a
// registered type watcher. The int return signals "abort on
// error" the way CPython treats a negative return; the parent
// path logs an unraisable and continues with the remaining
// watchers.
//
// CPython: Include/cpython/object.h:449 PyType_WatchCallback
type TypeWatchCallback func(t *Type) int

// TypeMaxWatchers is the table size. CPython hard-codes this to 8
// in pycore_interp_structs.h.
//
// CPython: Include/internal/pycore_interp_structs.h:22 TYPE_MAX_WATCHERS
const TypeMaxWatchers = 8

// typeWatcherMask is the low-bit mask carved out for subscription
// bits in tpWatched. CPython's tp_watched is already a uint8 and
// uses all 8 bits, but we keep an explicit mask so the bookkeeping
// stays symmetric with dictWatcherMask.
const typeWatcherMask = uint8(1<<TypeMaxWatchers - 1)

// typeReservedWatchers is the count of watcher IDs reserved for
// CPython core (the Tier-2 optimizer's slot). PyType_AddWatcher
// refuses to allocate inside this range so that installing a user
// watcher never collides with the optimizer.
//
// CPython: Objects/typeobject.c:1022 ("start at 1, 0 is reserved
// for cpython optimizer")
const typeReservedWatchers = 1

// typeWatchers is the canonical watcher callback table. Single-
// interpreter at v0.12; promote to state.Interpreter once sub-
// interpreters land.
//
// CPython: Include/internal/pycore_interp_structs.h:939
// type_watchers[TYPE_MAX_WATCHERS]
var typeWatchers [TypeMaxWatchers]TypeWatchCallback

// validateTypeWatcherID is the gopy port of validate_watcher_id.
// It returns an error rather than calling PyErr_Format because
// gopy's error model is value-typed.
//
// CPython: Objects/typeobject.c:1033 validate_watcher_id
func validateTypeWatcherID(id int) error {
	if id < 0 || id >= TypeMaxWatchers {
		return fmt.Errorf("ValueError: Invalid type watcher ID %d", id)
	}
	if typeWatchers[id] == nil {
		return fmt.Errorf("ValueError: No type watcher set for ID %d", id)
	}
	return nil
}

// TypeAddWatcher registers cb in the first free user-allocatable
// slot (starting at index 1 since 0 belongs to CPython's
// optimizer). Returns the slot ID, or -1 plus a RuntimeError if
// every user slot is occupied.
//
// CPython: Objects/typeobject.c:1016 PyType_AddWatcher
func TypeAddWatcher(cb TypeWatchCallback) (int, error) {
	for i := typeReservedWatchers; i < TypeMaxWatchers; i++ {
		if typeWatchers[i] == nil {
			typeWatchers[i] = cb
			return i, nil
		}
	}
	return -1, fmt.Errorf("RuntimeError: no more type watcher IDs available")
}

// TypeClearWatcher releases watcher slot id.
//
// CPython: Objects/typeobject.c:1047 PyType_ClearWatcher
func TypeClearWatcher(id int) error {
	if err := validateTypeWatcherID(id); err != nil {
		return err
	}
	typeWatchers[id] = nil
	return nil
}

// TypeSetReservedWatcher installs cb in one of the reserved slots
// (id < typeReservedWatchers). PyType_AddWatcher refuses to touch
// these, so the Tier-2 optimizer (which owns slot 0) needs an
// internal path. CPython's optimizer wires its callback by writing
// directly into interp->type_watchers[TYPE_WATCHER_ID] inside
// remove_globals; this is the equivalent gopy entrypoint.
//
// CPython: Python/optimizer_analysis.c:175 (callback registration
// inside remove_globals)
func TypeSetReservedWatcher(id int, cb TypeWatchCallback) error {
	if id < 0 || id >= typeReservedWatchers {
		return fmt.Errorf("ValueError: reserved type watcher ID %d out of range", id)
	}
	typeWatchers[id] = cb
	return nil
}

// TypeWatch subscribes t to watcher slot id. Idempotent; CPython
// guards this with the type lock because mutation can race with
// version-tag allocation. gopy's Type is mutated under the
// runtime lock in the same shape so the bare write is safe.
//
// CPython: Objects/typeobject.c:1060 PyType_Watch
func TypeWatch(id int, t *Type) error {
	if err := validateTypeWatcherID(id); err != nil {
		return err
	}
	// Ensure the version tag exists so the next mutation actually
	// runs the watcher loop (type_modified_unlocked short-circuits
	// when tp_version_tag is already 0).
	t.VersionTag()
	t.tpWatched |= uint8(1) << id
	return nil
}

// TypeUnwatch drops t from watcher slot id. Idempotent.
//
// CPython: Objects/typeobject.c:1080 PyType_Unwatch
func TypeUnwatch(id int, t *Type) error {
	if err := validateTypeWatcherID(id); err != nil {
		return err
	}
	t.tpWatched &^= uint8(1) << id
	return nil
}

// notifyTypeWatchers fires every registered watcher whose bit is
// set in t.tpWatched. Mirrors the notify loop inside
// type_modified_unlocked: callback errors do not stop the loop,
// they only log an unraisable. gopy currently swallows the error
// rather than wiring it into PyErr_FormatUnraisable since the
// unraisable hook is not yet ported; a follow-up will route this
// through sys.unraisablehook once that surface lands.
//
// CPython: Objects/typeobject.c:1170-1188 (notify loop inside
// type_modified_unlocked)
func notifyTypeWatchers(t *Type) {
	bits := t.tpWatched & typeWatcherMask
	for i := 0; bits != 0; i++ {
		if bits&1 != 0 {
			cb := typeWatchers[i]
			if cb != nil {
				_ = cb(t) // <0 reserved for unraisable hook
			}
		}
		bits >>= 1
	}
}
