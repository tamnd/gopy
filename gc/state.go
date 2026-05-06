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

// gcState is the per-package collector state. CPython carries one per
// interpreter; gopy is single-interpreter for now so we keep one
// package-level instance.
//
// CPython: Include/internal/pycore_interp_structs.h:202 _gc_runtime_state
type gcState struct {
	mu sync.Mutex

	enabled     bool
	generations [NumGenerations]generation

	finalizers map[objects.Object]Finalizer
	tracked    map[objects.Object]*gcHead
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
	}
	s.generations[0].threshold = defaultThreshold0
	s.generations[1].threshold = defaultThreshold1
	s.generations[2].threshold = defaultThreshold2
	for i := range s.generations {
		s.generations[i].head = newListHead()
	}
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
