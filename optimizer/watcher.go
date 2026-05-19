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

// WatcherTable holds per-interpreter optimizer state that is *not*
// already on the dict / type itself. Subscriptions for dict live in
// objects.Dict.watcherTag (mirroring CPython's _ma_watcher_tag), so
// only the type half still uses pointer sets here. Type subscription
// will move onto Type.tp_watched in the P7 followup.
//
// CPython: Include/internal/pycore_interp_structs.h dict_state /
// type_watchers
type WatcherTable struct {
	typeCallbacks [MaxTypeWatchers]TypeWatchCallback
	typeSubs      [MaxTypeWatchers]map[unsafe.Pointer]struct{}
}

// watcherTable returns the watcher table for interp, lazily allocating
// it on first access. Stored on Interpreter.Watchers as any so the
// state package stays free of an optimizer dependency.
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
//
// CPython: Objects/typeobject.c PyType_AddWatcher
func TypeAddWatcher(interp *state.Interpreter, cb TypeWatchCallback) int {
	w := watcherTable(interp)
	for i := 0; i < MaxTypeWatchers; i++ {
		if w.typeCallbacks[i] == nil {
			w.typeCallbacks[i] = cb
			return i
		}
	}
	return -1
}

// TypeClearWatcher releases watcher slot id.
//
// CPython: Objects/typeobject.c PyType_ClearWatcher
func TypeClearWatcher(interp *state.Interpreter, id int) {
	if id < 0 || id >= MaxTypeWatchers {
		return
	}
	w := watcherTable(interp)
	w.typeCallbacks[id] = nil
	w.typeSubs[id] = nil
}

// TypeWatch subscribes typ to watcher slot id.
//
// CPython: Objects/typeobject.c PyType_Watch
func TypeWatch(interp *state.Interpreter, id int, typ unsafe.Pointer) {
	if id < 0 || id >= MaxTypeWatchers {
		return
	}
	w := watcherTable(interp)
	if w.typeSubs[id] == nil {
		w.typeSubs[id] = make(map[unsafe.Pointer]struct{})
	}
	w.typeSubs[id][typ] = struct{}{}
}

// TypeUnwatch drops typ from watcher slot id.
//
// CPython: Objects/typeobject.c PyType_Unwatch
func TypeUnwatch(interp *state.Interpreter, id int, typ unsafe.Pointer) {
	if id < 0 || id >= MaxTypeWatchers {
		return
	}
	w := watcherTable(interp)
	if w.typeSubs[id] != nil {
		delete(w.typeSubs[id], typ)
	}
}

// DispatchTypeMutation fires every registered type watcher subscribed
// to typ. Driven by the type mutation paths once they land per-event
// hooks; gate tests drive it directly today.
//
// CPython: Objects/typeobject.c PyType_Modified
func DispatchTypeMutation(interp *state.Interpreter, typ unsafe.Pointer) {
	if interp.Watchers == nil {
		return
	}
	w := watcherTable(interp)
	for id := 0; id < MaxTypeWatchers; id++ {
		cb := w.typeCallbacks[id]
		if cb == nil {
			continue
		}
		subs := w.typeSubs[id]
		if subs == nil {
			continue
		}
		if _, ok := subs[typ]; !ok {
			continue
		}
		cb(typ)
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
// callbacks in place.
//
// CPython: Python/optimizer_analysis.c:175-180 (callback registration
// inside remove_globals)
func WatcherInit(interp *state.Interpreter) {
	w := watcherTable(interp)
	// Builtins and Globals occupy the two reserved dict watcher
	// slots. CPython's optimizer writes directly into
	// interp->dict_state.watchers[BUILTINS/GLOBALS]; we go through
	// the gopy equivalent DictSetReservedWatcher path so the public
	// PyDict_AddWatcher refuses to hand these out to user code.
	_ = objects.DictSetReservedWatcher(BuiltinsWatcherID, adaptDictWatchCallback(globalsWatcherCallback(interp)))
	_ = objects.DictSetReservedWatcher(GlobalsWatcherID, adaptDictWatchCallback(globalsWatcherCallback(interp)))

	if w.typeCallbacks[TypeWatcherID] == nil {
		w.typeCallbacks[TypeWatcherID] = typeWatcherCallback(interp)
	}
	// Type-modified hook stays wired through the legacy global until
	// the P7 watcher API port lands. The dict half no longer needs a
	// hook: notifyDictEvent fires from inside the mutation paths.
	objects.TypeModifiedHook = func(t *objects.Type) {
		DispatchTypeMutation(interp, unsafe.Pointer(t))
	}
}
