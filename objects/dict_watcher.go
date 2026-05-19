// Dict-watcher public API. Ports CPython's PyDict_AddWatcher /
// ClearWatcher / Watch / Unwatch surface plus the _PyDict_SendEvent
// dispatch helper so any C-level consumer (and gopy's Tier-2
// optimizer) can subscribe to dict mutations without poking dict
// internals.
//
// Design notes worth keeping for future readers:
//
//  1. CPython stashes the watcher callback table on
//     PyInterpreterState->dict_state.watchers[DICT_MAX_WATCHERS].
//     gopy is still single-interpreter at v0.12 so the table is a
//     package-level array. v0.13's sub-interpreter work needs to
//     promote this onto state.Interpreter; the call sites in
//     dict_mutate.go would then take an interp pointer instead of
//     reading the global.
//
//  2. Subscription state lives on the Dict itself (`watcherTag`,
//     8-bit mask, one bit per watcher slot) exactly like
//     CPython's _ma_watcher_tag. That puts the "is anyone watching
//     this dict?" check on a single load and bitwise-and rather
//     than a per-watcher map lookup. The previous gopy design used
//     a per-watcher map keyed on *Dict pointer; replacing it with
//     the bitmask matches CPython's data layout and removes the
//     allocation per first-subscribe.
//
//  3. Watcher slots 0 and 1 are reserved for CPython. The Tier-2
//     optimizer uses 0 for BUILTINS and 1 for GLOBALS, mirroring
//     optimizer_analysis.c. PyDict_AddWatcher returns slot 2 or
//     higher; the reserved slots have to be installed with the
//     internal helper dictSetWatcherCallback.
//
//  4. Notification fires from every mutation site (insert/replace
//     ADDED|MODIFIED, delete DELETED, clear CLEARED, copy CLONED).
//     The CPython DEALLOCATED event has no faithful Go equivalent
//     because Dict is GC'd; we document the gap rather than fake
//     it with a finalizer (finalizers run after object death and
//     could resurrect references through the callback).
//
//  5. The callback signature mirrors PyDict_WatchCallback:
//     (event, dict, key, new_value) -> int. CLEARED and CLONED
//     pass nil for key; CLONED passes the source dict as key (per
//     CPython at dictobject.c:3915); DELETED passes nil for value;
//     ADDED/MODIFIED pass both key and value.
//
// CPython: Objects/dictobject.c:7710 PyDict_Watch /
// Objects/dictobject.c:7741 PyDict_AddWatcher /
// Objects/dictobject.c:7780 _PyDict_SendEvent

package objects

import (
	"fmt"
	"sync/atomic"
)

// DictWatchEvent mirrors PyDict_WatchEvent. The numeric values match
// the CPython enum order (Include/cpython/dictobject.h:88) so the
// callback signature stays portable between gopy and any future C
// extension.
//
// CPython: Include/cpython/dictobject.h:88 PyDict_WatchEvent
type DictWatchEvent int

const (
	DictEventAdded DictWatchEvent = iota
	DictEventModified
	DictEventDeleted
	DictEventCloned
	DictEventCleared
	DictEventDeallocated
)

// DictWatchCallback is the signature CPython expects from a registered
// dict watcher. The bool return signals "abort on error" the way
// CPython treats a negative return; the parent path logs an unraisable
// and continues with the remaining watchers.
//
// CPython: Include/cpython/dictobject.h:97 PyDict_WatchCallback
type DictWatchCallback func(event DictWatchEvent, dict *Dict, key, newValue Object) int

// DictMaxWatchers is the table size. CPython hard-codes this to 8 in
// pycore_dict_state.h; we mirror the constant.
//
// CPython: Include/internal/pycore_dict_state.h:11 DICT_MAX_WATCHERS
const DictMaxWatchers = 8

// dictWatcherMask is the low-bit mask of watcherTag carved out for
// subscription bits. CPython's DICT_WATCHER_MASK has the same shape.
//
// CPython: Include/internal/pycore_dict.h:268 DICT_WATCHER_MASK
const dictWatcherMask = uint64(1<<DictMaxWatchers) - 1

// dictReservedWatchers is the count of watcher IDs reserved for
// CPython core (the Tier-2 optimizer's BUILTINS=0 / GLOBALS=1 pair).
// PyDict_AddWatcher refuses to allocate inside this range so that
// installing a user watcher never collides with the optimizer.
//
// CPython: Objects/dictobject.c:7745 ("Start at 2, as 0 and 1 are
// reserved for CPython")
const dictReservedWatchers = 2

// dictWatchers is the canonical watcher callback table. Single-
// interpreter at v0.12; promote to state.Interpreter once sub-
// interpreters land.
//
// CPython: Include/internal/pycore_dict_state.h:16
// dict_state.watchers[DICT_MAX_WATCHERS]
var dictWatchers [DictMaxWatchers]DictWatchCallback

// validateDictWatcherID is the gopy port of validate_watcher_id. It
// returns an error rather than calling PyErr_Format because gopy's
// error model is value-typed.
//
// CPython: Objects/dictobject.c:7697 validate_watcher_id
func validateDictWatcherID(id int) error {
	if id < 0 || id >= DictMaxWatchers {
		return fmt.Errorf("ValueError: Invalid dict watcher ID %d", id)
	}
	if dictWatchers[id] == nil {
		return fmt.Errorf("ValueError: No dict watcher set for ID %d", id)
	}
	return nil
}

// DictAddWatcher registers cb in the first free user-allocatable slot
// (starting at index 2 since 0 and 1 belong to CPython's optimizer).
// Returns the slot ID, or -1 plus a RuntimeError if every user slot
// is occupied.
//
// CPython: Objects/dictobject.c:7741 PyDict_AddWatcher
func DictAddWatcher(cb DictWatchCallback) (int, error) {
	for i := dictReservedWatchers; i < DictMaxWatchers; i++ {
		if dictWatchers[i] == nil {
			dictWatchers[i] = cb
			return i, nil
		}
	}
	return -1, fmt.Errorf("RuntimeError: no more dict watcher IDs available")
}

// DictClearWatcher releases watcher slot id.
//
// CPython: Objects/dictobject.c:7758 PyDict_ClearWatcher
func DictClearWatcher(id int) error {
	if err := validateDictWatcherID(id); err != nil {
		return err
	}
	dictWatchers[id] = nil
	return nil
}

// DictSetReservedWatcher installs cb in one of the reserved slots
// (id < dictReservedWatchers). PyDict_AddWatcher refuses to touch
// these, so the Tier-2 optimizer (which owns BUILTINS=0 and
// GLOBALS=1) needs an internal path. CPython's optimizer wires its
// callbacks by writing directly into interp->dict_state.watchers[i]
// inside remove_globals; this is the equivalent gopy entrypoint.
//
// CPython: Python/optimizer_analysis.c:175 (callback registration
// inside remove_globals)
func DictSetReservedWatcher(id int, cb DictWatchCallback) error {
	if id < 0 || id >= dictReservedWatchers {
		return fmt.Errorf("ValueError: reserved dict watcher ID %d out of range", id)
	}
	dictWatchers[id] = cb
	return nil
}

// DictWatch subscribes d to watcher slot id. Idempotent; CPython sets
// the bit with FT_ATOMIC_OR_UINT64 because the free-threaded build can
// race here. gopy uses sync/atomic to stay GC-safe under concurrent
// mutation.
//
// CPython: Objects/dictobject.c:7711 PyDict_Watch
func DictWatch(id int, d *Dict) error {
	if err := validateDictWatcherID(id); err != nil {
		return err
	}
	atomic.OrUint64(&d.watcherTag, uint64(1)<<id)
	return nil
}

// DictUnwatch drops d from watcher slot id. Idempotent.
//
// CPython: Objects/dictobject.c:7726 PyDict_Unwatch
func DictUnwatch(id int, d *Dict) error {
	if err := validateDictWatcherID(id); err != nil {
		return err
	}
	atomic.AndUint64(&d.watcherTag, ^(uint64(1) << id))
	return nil
}

// dictEventName mirrors CPython's dict_event_name for the unraisable
// message path. Kept lower-case so it is package-private; the public
// name is the enum.
//
// CPython: Objects/dictobject.c:7769 dict_event_name
func dictEventName(event DictWatchEvent) string {
	switch event {
	case DictEventAdded:
		return "PyDict_EVENT_ADDED"
	case DictEventModified:
		return "PyDict_EVENT_MODIFIED"
	case DictEventDeleted:
		return "PyDict_EVENT_DELETED"
	case DictEventCloned:
		return "PyDict_EVENT_CLONED"
	case DictEventCleared:
		return "PyDict_EVENT_CLEARED"
	case DictEventDeallocated:
		return "PyDict_EVENT_DEALLOCATED"
	}
	return "PyDict_EVENT_<unknown>"
}

// sendDictEvent dispatches an event to every watcher whose bit is set
// in watcherBits. Mirrors _PyDict_SendEvent: callback errors do not
// stop the loop, they only log an unraisable. gopy currently swallows
// the error rather than wiring it into PyErr_FormatUnraisable since
// the unraisable hook is not yet ported; a follow-up will route this
// through sys.unraisablehook once that surface lands (#608 traceback
// work added the prereqs).
//
// CPython: Objects/dictobject.c:7780 _PyDict_SendEvent
func sendDictEvent(watcherBits uint64, event DictWatchEvent, d *Dict, key, value Object) {
	for i := range DictMaxWatchers {
		if watcherBits&1 != 0 {
			cb := dictWatchers[i]
			if cb != nil && cb(event, d, key, value) < 0 {
				_ = dictEventName(event) // reserved for unraisable hook
			}
		}
		watcherBits >>= 1
	}
}

// notifyDictEvent is the inline _PyDict_NotifyEvent helper: bump the
// mutation counter the Tier-2 globals folder reads, then fan out to
// any subscribed watcher. Called from every mutation site
// (insert/delete/clear/clone) in dict.go and dict_mutate.go. Kept
// package-private; mutation paths invoke this, public callers go
// through Watch/Unwatch.
//
// CPython's _PyDict_NotifyEvent only dispatches; the version bump
// lives in update_keys_version (DICT_VERSION_INCREMENT). gopy folds
// the two into one call so we don't have two parallel hooks on every
// hot mutation; the effect on the counter is the same.
//
// CPython: Include/internal/pycore_dict.h:281 _PyDict_NotifyEvent /
// Include/internal/pycore_dict.h:267 DICT_VERSION_INCREMENT
func notifyDictEvent(event DictWatchEvent, d *Dict, key, value Object) {
	d.mutationCount++
	bits := atomic.LoadUint64(&d.watcherTag) & dictWatcherMask
	if bits != 0 {
		sendDictEvent(bits, event, d, key, value)
	}
}
