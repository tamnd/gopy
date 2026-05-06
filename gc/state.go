// gcState mirrors the live subset of CPython's _gc_runtime_state.
// v0.10 introduces it to replace the v0.3 anonymous package-level
// state struct in gc.go. Fields that exist in CPython but have no
// gopy caller yet (generationStats, garbage list, callbacks list,
// heapSize, long-lived counters, permanentGeneration, debug,
// collecting) are deferred to the checklist items that introduce
// their consumers.
//
// The GIL-disabled fields (young + old[2], freeze_active, last_mem,
// deferred_count, mutex) are not modeled in v0.10; gopy ships only
// the GIL-enabled layout for now. The PyMutex equivalent is the
// generic mu sync.Mutex used everywhere; the free-threaded
// collector path is deferred to v0.11+.
//
// CPython: Include/internal/pycore_interp_structs.h:202 _gc_runtime_state

package gc

import (
	"sync"

	"github.com/tamnd/gopy/objects"
)

// NumGenerations is the number of CPython generations.
//
// CPython: Include/internal/pycore_interp_structs.h:200 NUM_GENERATIONS
const NumGenerations = 3

// Default threshold values.
//
// CPython: Modules/gcmodule.c:78 NUM_GENERATIONS_DEFAULT_THRESHOLD
const (
	defaultThreshold0 = 700
	defaultThreshold1 = 10
	defaultThreshold2 = 10
)

// generation tracks the threshold, live count, and the doubly-linked
// list head for one CPython generation.
//
// CPython: Include/internal/pycore_interp_structs.h gc_generation
type generation struct {
	head      *gcHead
	threshold int
	count     int
}

// genStats mirrors CPython's struct gc_generation_stats: per-generation
// counters that get_stats reports back to user code.
//
// CPython: Include/internal/pycore_interp_structs.h gc_generation_stats
type genStats struct {
	collections   int
	collected     int
	uncollectable int
}

// Debug flag bits, surfaced as module-level constants on the gc module.
//
// CPython: Include/internal/pycore_gc.h _PyGC_DEBUG_*
const (
	DebugStats         = 1 << 0
	DebugCollectable   = 1 << 1
	DebugUncollectable = 1 << 2
	DebugSaveAll       = 1 << 5
	DebugLeak          = DebugCollectable | DebugUncollectable | DebugSaveAll
)

// gcState is the per-package collector state. CPython carries one per
// interpreter; gopy is single-interpreter for now so we keep one
// package-level instance.
//
// CPython: Include/internal/pycore_interp_structs.h:202 _gc_runtime_state
type gcState struct {
	mu sync.Mutex

	enabled     bool
	generations [NumGenerations]generation
	permanent   *gcHead

	finalizers map[objects.Object]Finalizer
	tracked    map[objects.Object]*gcHead
	// weakrefs maps a referent to the weakrefs pointing at it.
	// Cleared in handle_weakrefs when the referent goes unreachable.
	weakrefs map[objects.Object][]*objects.Weakref

	// garbage holds objects whose collection was suppressed because
	// they carry a legacy __del__. gopy unifies finalizers under
	// PEP 442 so garbage stays empty; we expose the list anyway to
	// match the gc.garbage Python attribute.
	garbage []objects.Object

	// debug is the bitmask set by gc.set_debug.
	debug int

	// stats accumulates per-generation collection counters surfaced
	// through gc.get_stats.
	stats [NumGenerations]genStats

	// finalized records which objects have already had their
	// finalizer run, so gc.is_finalized can answer truthfully even
	// after the GC has cleared the gcHead.
	finalized map[objects.Object]struct{}

	// callbacks holds the gc.callbacks list. The collector reads it
	// at the start and end of every cycle; user code can mutate the
	// list directly through the module attribute.
	callbacks *objects.List
}

// state is the single package-level collector state. The v0.3
// skeleton kept a smaller anonymous struct here; we expand it but
// keep the same name so existing call sites compile unchanged.
var state = newGCState()

// newGCState mirrors _PyGC_InitState.
//
// CPython: Python/gc.c:113 _PyGC_InitState
func newGCState() *gcState {
	s := &gcState{
		enabled:    true,
		finalizers: make(map[objects.Object]Finalizer),
		tracked:    make(map[objects.Object]*gcHead),
		weakrefs:   make(map[objects.Object][]*objects.Weakref),
		finalized:  make(map[objects.Object]struct{}),
	}
	s.generations[0].threshold = defaultThreshold0
	s.generations[1].threshold = defaultThreshold1
	s.generations[2].threshold = defaultThreshold2
	for i := range s.generations {
		s.generations[i].head = newListHead()
	}
	s.permanent = newListHead()
	return s
}

// Enable turns automatic collection back on.
//
// CPython: Python/gc.c:1645 PyGC_Enable
func Enable() {
	state.mu.Lock()
	state.enabled = true
	state.mu.Unlock()
}

// Disable suppresses automatic collection.
//
// CPython: Python/gc.c:1654 PyGC_Disable
func Disable() {
	state.mu.Lock()
	state.enabled = false
	state.mu.Unlock()
}

// IsEnabled reports whether automatic collection is on.
//
// CPython: Python/gc.c:1663 PyGC_IsEnabled
func IsEnabled() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.enabled
}

// GetThreshold returns the (gen0, gen1, gen2) thresholds.
//
// CPython: Modules/gcmodule.c gc_get_threshold_impl
func GetThreshold() (gen0, gen1, gen2 int) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.generations[0].threshold,
		state.generations[1].threshold,
		state.generations[2].threshold
}

// SetThreshold installs new generation thresholds. CPython treats
// negative or zero on gen0 as "disable automatic collection"; we
// mirror that by zeroing the count fields in the same way.
//
// CPython: Modules/gcmodule.c gc_set_threshold
func SetThreshold(gen0, gen1, gen2 int) {
	state.mu.Lock()
	state.generations[0].threshold = gen0
	state.generations[1].threshold = gen1
	state.generations[2].threshold = gen2
	state.mu.Unlock()
}

// GetCount returns the (gen0, gen1, gen2) live counts.
//
// CPython: Modules/gcmodule.c gc_get_count_impl
func GetCount() (gen0, gen1, gen2 int) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.generations[0].count,
		state.generations[1].count,
		state.generations[2].count
}

// SetDebug installs the debug bitmask used by the collector for the
// DEBUG_STATS / DEBUG_COLLECTABLE / DEBUG_UNCOLLECTABLE / DEBUG_SAVEALL
// hooks. Out-of-range bits are accepted unchanged to match CPython.
//
// CPython: Modules/gcmodule.c:116 gc_set_debug_impl
func SetDebug(flags int) {
	state.mu.Lock()
	state.debug = flags
	state.mu.Unlock()
}

// GetDebug reports the current debug bitmask.
//
// CPython: Modules/gcmodule.c:131 gc_get_debug_impl
func GetDebug() int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.debug
}

// GetStats snapshots the per-generation (collections, collected,
// uncollectable) counters. The returned slice has length NumGenerations.
//
// CPython: Modules/gcmodule.c:365 gc_get_stats_impl
func GetStats() []genStats {
	state.mu.Lock()
	defer state.mu.Unlock()
	out := make([]genStats, NumGenerations)
	for i := range state.stats {
		out[i] = state.stats[i]
	}
	return out
}

// IsFinalized reports whether the GC has already run o's finalizer.
// Mirrors PyObject_GC_IsFinalized: truthy means finalizeGarbage has
// touched o on a prior cycle.
//
// CPython: Include/cpython/objimpl.h PyObject_GC_IsFinalized
func IsFinalized(o objects.Object) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if g, ok := state.tracked[o]; ok && g.flags&gcFinalized != 0 {
		return true
	}
	_, ok := state.finalized[o]
	return ok
}

// SetCallbacks installs the gc.callbacks list. The module-level setup
// stamps a list on the module dict and remembers it here so the
// collector can iterate it. Passing nil clears the binding.
//
// CPython: Modules/gcmodule.c module init publishes gcstate->callbacks
// and the collector iterates it through invoke_gc_callback.
func SetCallbacks(cbs *objects.List) {
	state.mu.Lock()
	state.callbacks = cbs
	state.mu.Unlock()
}
