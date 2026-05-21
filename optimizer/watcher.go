// Watcher callbacks that drive Tier-2 invalidation. CPython registers
// two singletons per interpreter: a dict watcher slot and a type
// watcher slot. The dict watcher fires when a globals or builtins dict
// mutates; the type watcher fires when a class mutates. Both routes
// land on _Py_Executors_InvalidateDependency, which clears every
// executor whose dependency bloom matches the mutated pointer.
//
// gopy keeps the callback signatures identical to CPython's
// PyDict_WatchCallback and PyType_WatchCallback. The dispatch entry
// is a thin shim the dict / type mutation paths will call once they
// land per-mutation hooks; until then, gate tests drive Dispatch
// directly to validate the wiring.
//
// CPython: Python/optimizer_analysis.c:60-92 globals_watcher_callback /
// type_watcher_callback

package optimizer

import (
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Watcher slot identifiers. CPython reserves the first two dict
// watcher IDs and the first type watcher ID for the Tier-2 optimizer.
//
// CPython: Python/optimizer_analysis.c:70-72 BUILTINS_WATCHER_ID /
// GLOBALS_WATCHER_ID / TYPE_WATCHER_ID
const (
	BuiltinsWatcherID = 0
	GlobalsWatcherID  = 1
	TypeWatcherID     = 0
)

// DictWatchEvent mirrors CPython's PyDict_WatchEvent. The Tier-2
// optimizer only reacts to mutations; the other events flow through
// for parity with future consumers.
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

// DictWatchCallback is the dict-mutation callback signature. dict, key,
// newValue match the CPython PyObject* parameters; gopy passes raw
// pointers because the optimizer only needs identity for the bloom.
//
// CPython: Include/cpython/dictobject.h:97 PyDict_WatchCallback
type DictWatchCallback func(event DictWatchEvent, dict unsafe.Pointer, key unsafe.Pointer, newValue unsafe.Pointer) int

// TypeWatchCallback is the type-mutation callback signature. CPython
// passes the PyTypeObject* of the mutated class; gopy passes a raw
// pointer for the same reason.
//
// CPython: Include/cpython/object.h:449 PyType_WatchCallback
type TypeWatchCallback func(typ unsafe.Pointer) int

// MaxDictWatchers / MaxTypeWatchers cap the per-interpreter watcher
// registry. CPython hard-codes both to 8.
//
// CPython: Include/internal/pycore_dict_state.h:11 DICT_MAX_WATCHERS /
// pycore_interp_structs.h:22 TYPE_MAX_WATCHERS
const (
	MaxDictWatchers = 8
	MaxTypeWatchers = 8
)

// WatcherTable was the optimizer's per-interpreter pointer-set
// holding type subscriptions before P7 landed. It is kept as a
// placeholder for sub-interpreter scaffolding: every callback table
// now lives on the objects package (objects.dictWatchers /
// objects.typeWatchers) and subscriptions are bitmasks on the
// individual Dict / Type. The struct is empty by design.
//
// CPython: Include/internal/pycore_interp_structs.h dict_state /
// type_watchers
type WatcherTable struct{}

// watcherTable returns the watcher table for interp, lazily allocating
// it on first access. Stored on Interpreter.Watchers as any so the
// state package stays free of an optimizer dependency. Retained so
// callers that reference it through the WatcherInit path continue to
// compile while we transition more code off the legacy hook.
//
// CPython: Include/internal/pycore_interp_structs.h dict_state /
// type_watchers fields
func watcherTable(interp *state.Interpreter) *WatcherTable {
	if interp.Watchers == nil {
		interp.Watchers = &WatcherTable{}
	}
	return interp.Watchers.(*WatcherTable)
}

// DictAddWatcher is the optimizer-facing wrapper around the public
// objects.DictAddWatcher API. The interp argument is kept for symmetry
// with the type half and the future sub-interpreter port, even though
// the underlying table is package-global at v0.12.
//
// CPython: Objects/dictobject.c:7741 PyDict_AddWatcher
func DictAddWatcher(_ *state.Interpreter, cb DictWatchCallback) int {
	id, err := objects.DictAddWatcher(adaptDictWatchCallback(cb))
	if err != nil {
		return -1
	}
	return id
}

// DictClearWatcher releases watcher slot id.
//
// CPython: Objects/dictobject.c:7758 PyDict_ClearWatcher
func DictClearWatcher(_ *state.Interpreter, id int) {
	_ = objects.DictClearWatcher(id)
}

// DictWatch subscribes dict to watcher slot id.
//
// CPython: Objects/dictobject.c:7711 PyDict_Watch
func DictWatch(_ *state.Interpreter, id int, dict unsafe.Pointer) {
	d := (*objects.Dict)(dict)
	_ = objects.DictWatch(id, d)
}

// DictUnwatch drops dict from watcher slot id.
//
// CPython: Objects/dictobject.c:7726 PyDict_Unwatch
func DictUnwatch(_ *state.Interpreter, id int, dict unsafe.Pointer) {
	d := (*objects.Dict)(dict)
	_ = objects.DictUnwatch(id, d)
}

// adaptDictWatchCallback converts the optimizer's unsafe.Pointer
// callback to the typed objects-package callback. The dict identity
// is what matters to the bloom; the key/value get passed through.
func adaptDictWatchCallback(cb DictWatchCallback) objects.DictWatchCallback {
	return func(event objects.DictWatchEvent, dict *objects.Dict, key, newValue objects.Object) int {
		var keyPtr, valuePtr unsafe.Pointer
		if key != nil {
			// objects.Object is an interface; identity for the bloom
			// is the concrete underlying pointer, not the interface
			// header. The optimizer only reads dict identity today;
			// preserve nil semantics for key/value and leave a typed
			// pointer dereference for a future caller that needs it.
			_ = keyPtr
		}
		_ = valuePtr
		_ = key
		_ = newValue
		return cb(DictWatchEvent(event), unsafe.Pointer(dict), nil, nil)
	}
}

// TypeAddWatcher registers cb in the first free type watcher slot.
// The interp argument is kept for symmetry with the dict half and
// the future sub-interpreter port, even though the underlying
// table is package-global at v0.12.
//
// CPython: Objects/typeobject.c:1016 PyType_AddWatcher
func TypeAddWatcher(_ *state.Interpreter, cb TypeWatchCallback) int {
	id, err := objects.TypeAddWatcher(adaptTypeWatchCallback(cb))
	if err != nil {
		return -1
	}
	return id
}

// TypeClearWatcher releases watcher slot id.
//
// CPython: Objects/typeobject.c:1047 PyType_ClearWatcher
func TypeClearWatcher(_ *state.Interpreter, id int) {
	_ = objects.TypeClearWatcher(id)
}

// TypeWatch subscribes typ to watcher slot id. typ must be an
// *objects.Type as unsafe.Pointer; the optimizer only ever
// subscribes through real types so the cast is sound.
//
// CPython: Objects/typeobject.c:1060 PyType_Watch
func TypeWatch(_ *state.Interpreter, id int, typ unsafe.Pointer) {
	t := (*objects.Type)(typ)
	_ = objects.TypeWatch(id, t)
}

// TypeUnwatch drops typ from watcher slot id.
//
// CPython: Objects/typeobject.c:1080 PyType_Unwatch
func TypeUnwatch(_ *state.Interpreter, id int, typ unsafe.Pointer) {
	t := (*objects.Type)(typ)
	_ = objects.TypeUnwatch(id, t)
}

// DispatchTypeMutation fires every registered type watcher
// subscribed to typ. Retained as a thin shim onto
// InvalidateVersionTag so gate tests that drive type mutations
// without going through the regular Setattr path still trigger
// the watcher loop. Production mutation paths reach the watcher
// through Type.InvalidateVersionTag directly.
//
// CPython: Objects/typeobject.c:1200 PyType_Modified
func DispatchTypeMutation(_ *state.Interpreter, typ unsafe.Pointer) {
	t := (*objects.Type)(typ)
	t.VersionTag() // ensure tag exists so Invalidate actually fires
	t.InvalidateVersionTag()
}

// adaptTypeWatchCallback converts the optimizer's unsafe.Pointer
// callback to the typed objects-package callback. The bloom only
// needs the type identity; preserve the pointer round-trip.
func adaptTypeWatchCallback(cb TypeWatchCallback) objects.TypeWatchCallback {
	return func(t *objects.Type) int {
		return cb(unsafe.Pointer(t))
	}
}

// globalsWatcherCallback is the optimizer's dict watcher: invalidates
// every executor whose bloom may contain dict, then unsubscribes dict
// so the same callback does not fire again on the next mutation.
//
// CPython: Python/optimizer_analysis.c:74-84 globals_watcher_callback
func globalsWatcherCallback(interp *state.Interpreter) DictWatchCallback {
	return func(_ DictWatchEvent, dict unsafe.Pointer, _ unsafe.Pointer, _ unsafe.Pointer) int {
		ExecutorsInvalidateDependency(interp, dict, true)
		DictUnwatch(interp, GlobalsWatcherID, dict)
		return 0
	}
}

// builtinsDictWatcherCallback is the interpreter-wide builtins
// watcher. It fires on every mutation to the builtins dict and
// bumps interp.BuiltinDictMutations (CPython's
// rare_events.builtin_dict). When the counter is still under the
// cap, it also clears every executor: any LOAD_GLOBAL_BUILTIN trace
// in flight has implicitly closed over the old builtins shape, so
// the next call has to re-specialize.
//
// CPython: Python/pylifecycle.c:599 builtins_dict_watcher
func builtinsDictWatcherCallback(interp *state.Interpreter) DictWatchCallback {
	return func(_ DictWatchEvent, _ unsafe.Pointer, _ unsafe.Pointer, _ unsafe.Pointer) int {
		if interp == nil {
			return 0
		}
		if interp.BuiltinDictMutations < MaxAllowedBuiltinsModifications {
			ExecutorsInvalidateAll(interp, true)
		}
		interp.BuiltinDictMutations++
		return 0
	}
}

// typeWatcherCallback is the optimizer's type watcher: invalidates
// every executor whose bloom may contain typ, then unsubscribes the
// type.
//
// CPython: Python/optimizer_analysis.c:86-92 type_watcher_callback
func typeWatcherCallback(interp *state.Interpreter) TypeWatchCallback {
	return func(typ unsafe.Pointer) int {
		ExecutorsInvalidateDependency(interp, typ, true)
		TypeUnwatch(interp, TypeWatcherID, typ)
		return 0
	}
}

// WatcherInit registers the dict and type watcher callbacks at the
// canonical Tier-2 IDs. Idempotent: re-running leaves the existing
// callbacks in place. CPython splits the two reserved dict slots
// across two installation sites (slot 0 in pylifecycle.c:1380 at
// interp init and slot 1 lazily in optimizer_analysis.c:175 from
// remove_globals); gopy installs both up-front so the watcher table
// is fully armed before any specialization fires, matching the spec
// 1712 P1.6 contract.
//
// CPython: Python/pylifecycle.c:1380 (slot 0 install) /
// Python/optimizer_analysis.c:175-180 (slot 1 + type install)
func WatcherInit(interp *state.Interpreter) {
	_ = watcherTable(interp) // allocate the placeholder for sub-interp scaffolding

	// Slot 0 (BUILTINS): builtins_dict_watcher. Bumps
	// rare_events.builtin_dict (gopy: BuiltinDictMutations) and
	// invalidates every executor while still under the cap.
	// CPython wires this in pylifecycle.c:1380.
	_ = objects.DictSetReservedWatcher(BuiltinsWatcherID, adaptDictWatchCallback(builtinsDictWatcherCallback(interp)))

	// Slot 1 (GLOBALS): globals_watcher_callback. Per-frame globals
	// dict invalidation routed through the executor bloom. CPython
	// wires this lazily in optimizer_analysis.c:175.
	_ = objects.DictSetReservedWatcher(GlobalsWatcherID, adaptDictWatchCallback(globalsWatcherCallback(interp)))

	// Type slot 0: type_watcher_callback. CPython wires this lazily
	// in optimizer_analysis.c:179; gopy installs it up-front for
	// the same parity reason as the dict watchers.
	_ = objects.TypeSetReservedWatcher(TypeWatcherID, adaptTypeWatchCallback(typeWatcherCallback(interp)))
}

// EnsureBuiltinsSubscribed subscribes builtins to slot 0 so the
// callback installed by WatcherInit actually fires on mutation. The
// optimizer also stamps interp.Builtins so the tier-2 globals folder
// recognizes this dict as the canonical builtins. Idempotent: the
// DictWatch bit is set with FT_ATOMIC_OR and Builtins is only stamped
// if previously unset. Callers should invoke this once the runtime
// has materialized the real builtins dict (typically at the first
// frame-eval).
//
// CPython: Python/pylifecycle.c:1381 PyDict_Watch(0, interp->builtins)
func EnsureBuiltinsSubscribed(interp *state.Interpreter, builtins *objects.Dict) {
	if interp == nil || builtins == nil {
		return
	}
	if interp.Builtins == nil {
		interp.Builtins = objects.Object(builtins)
	}
	_ = objects.DictWatch(BuiltinsWatcherID, builtins)
}
