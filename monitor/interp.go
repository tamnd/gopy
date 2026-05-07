// Per-interpreter monitoring state. CPython stamps a cluster of
// fields directly onto PyInterpreterState (the global event mask, the
// per-tool callback table, the tool names, the version counters);
// gopy gathers them into one struct so the state package only carries
// a single pointer.
//
// CPython: Include/internal/pycore_interp_structs.h:948-955

package monitor

import (
	"fmt"
	"sync/atomic"

	"github.com/tamnd/gopy/objects"
)

// InterpState is the per-interpreter monitoring slab. Allocate one on
// interpreter construction and reach it from the eval loop, the
// instrument pass, and the sys.monitoring builtin.
//
// CPython: Include/internal/pycore_interp_structs.h:948 monitors
type InterpState struct {
	// Monitors is the per-interpreter event mask, indexed by event.
	// Each entry is a uint8 with one bit per tool. The instrument
	// pass unions this with each code object's local monitors to
	// compute ActiveMonitors.
	//
	// CPython: Include/internal/pycore_interp_structs.h:948
	Monitors GlobalMonitors

	// Callables[tool][event] is the user callback registered for
	// that slot, or nil if no tool has registered there.
	//
	// CPython: Include/internal/pycore_interp_structs.h:953
	Callables [MaxToolID][Events]objects.Object

	// ToolNames[tool] is the human-readable identifier the user
	// passed to sys.monitoring.use_tool_id. nil for free slots.
	//
	// CPython: Include/internal/pycore_interp_structs.h:954
	ToolNames [MaxToolID]objects.Object

	// ToolVersions[tool] is bumped every time the tool's set of
	// registered events changes. The shadow walk caches the version
	// it last saw on each code object so it only re-instruments
	// when something changed.
	//
	// CPython: Include/internal/pycore_interp_structs.h:955
	ToolVersions [MaxToolID]uintptr

	// SysProfilingThreads counts the threads with a c_profilefunc
	// installed; SysTracingThreads counts c_tracefunc threads. The
	// instrumentation runtime uses them to short-circuit the legacy
	// sys.setprofile / sys.settrace shims when no thread has them
	// enabled.
	//
	// CPython: Include/internal/pycore_interp_structs.h:951-952
	SysProfilingThreads int
	SysTracingThreads   int

	// version is the global instrumentation version. Bumped on
	// every SetEvents / RegisterCallback so the shadow walk knows
	// to re-instrument code objects that ran under an older
	// snapshot. CPython packs this into the eval breaker; gopy
	// keeps it standalone because the eval breaker is not yet
	// ported.
	//
	// CPython: Include/internal/pycore_interp_structs.h
	//          ceval.instrumentation_version
	version atomic.Uint32
}

// NewInterpState allocates a zero-valued InterpState. All slots start
// empty: no tool has claimed an ID, no callbacks are registered, and
// the global event mask is zero.
func NewInterpState() *InterpState {
	return &InterpState{}
}

// GlobalVersion returns the current instrumentation version.
//
// CPython: Python/instrumentation.c:1014 global_version
func (s *InterpState) GlobalVersion() uint32 {
	return s.version.Load()
}

// bumpVersion increments the instrumentation version and returns the
// new value. The eval-loop bit-packing CPython does for the eval
// breaker is not yet relevant in gopy; the counter just monotonically
// increases.
//
// CPython: Python/instrumentation.c:2019 new_version
func (s *InterpState) bumpVersion() uint32 {
	return s.version.Add(1)
}

// validTool reports whether tool is in the 0..MaxToolID-1 range.
//
// CPython: Python/instrumentation.c:2126 check_valid_tool
func validTool(tool Tool) bool { return tool < MaxToolID }

// validEvent reports whether ev is one of the named events.
func validEvent(ev Event) bool { return uint8(ev) < Events }

// CheckToolID reports whether tool is a valid index. Mirrors the
// CPython argument check used by the sys.monitoring entry points
// before they touch InterpState.
//
// CPython: Python/instrumentation.c:2126 check_valid_tool
func CheckToolID(tool Tool) error {
	if !validTool(tool) {
		return fmt.Errorf("invalid tool %d (must be in [0, %d))", tool, MaxToolID)
	}
	return nil
}

// UseToolID claims tool for name. Errors if the slot is already in
// use. Returns nil and stores the name on success.
//
// CPython: Python/instrumentation.c:2147 monitoring_use_tool_id_impl
func (s *InterpState) UseToolID(tool Tool, name objects.Object) error {
	if err := CheckToolID(tool); err != nil {
		return err
	}
	if s.ToolNames[tool] != nil {
		return fmt.Errorf("tool %d is already in use", tool)
	}
	s.ToolNames[tool] = name
	return nil
}

// FreeToolID releases a tool slot, clearing every callback and event
// registration that pinned to it. Mirrors CPython's free_tool_id,
// which calls _PyMonitoring_ClearToolId before nulling the name.
//
// CPython: Python/instrumentation.c:2202 monitoring_free_tool_id_impl
func (s *InterpState) FreeToolID(tool Tool) error {
	if err := CheckToolID(tool); err != nil {
		return err
	}
	if s.ToolNames[tool] != nil {
		s.clearToolID(tool)
	}
	s.ToolNames[tool] = nil
	return nil
}

// IsToolInUse reports whether the slot has been claimed by
// use_tool_id. The instrument pass checks this before reading any
// other per-tool field.
//
// CPython: Python/instrumentation.c:2126 check_valid_tool (companion)
func (s *InterpState) IsToolInUse(tool Tool) bool {
	if !validTool(tool) {
		return false
	}
	return s.ToolNames[tool] != nil
}

// ToolName returns the registered tool name, or nil if the slot is
// free.
func (s *InterpState) ToolName(tool Tool) objects.Object {
	if !validTool(tool) {
		return nil
	}
	return s.ToolNames[tool]
}

// clearToolID drops every callback and event registration for tool.
// Used by FreeToolID and by sys.monitoring.clear_tool_id. The version
// counter bumps so the shadow walk re-instruments any code that ran
// under the old registration.
//
// CPython: Python/instrumentation.c:2081 _PyMonitoring_ClearToolId
func (s *InterpState) clearToolID(tool Tool) {
	for ev := 0; ev < Events; ev++ {
		s.Callables[tool][ev] = nil
	}
	bit := uint8(1) << tool
	for ev := 0; ev < UngroupedEvents; ev++ {
		s.Monitors.Tools[ev] &^= bit
	}
	s.ToolVersions[tool]++
	s.bumpVersion()
}

// ClearToolID is the public entry point. Mirrors CPython's
// monitoring.clear_tool_id, which is a no-op on unclaimed slots.
//
// CPython: Python/instrumentation.c:2174 monitoring_clear_tool_id_impl
func (s *InterpState) ClearToolID(tool Tool) error {
	if err := CheckToolID(tool); err != nil {
		return err
	}
	if s.ToolNames[tool] != nil {
		s.clearToolID(tool)
	}
	return nil
}

// GetEvents returns the event mask currently registered for tool.
//
// CPython: Python/instrumentation.c:281 get_events
func (s *InterpState) GetEvents(tool Tool) EventSet {
	if !validTool(tool) {
		return 0
	}
	var result EventSet
	for ev := 0; ev < UngroupedEvents; ev++ {
		if (s.Monitors.Tools[ev]>>tool)&1 != 0 {
			result |= 1 << ev
		}
	}
	return result
}

// RestartEvents bumps the global monitoring version twice: once into
// the new last_restart_version and again so live code objects that
// see this version know to re-instrument from scratch instead of
// diffing.
//
// CPython: Python/instrumentation.c:2436 monitoring_restart_events_impl
func (s *InterpState) RestartEvents() {
	s.bumpVersion()
	s.bumpVersion()
}

// SetEvents replaces tool's event mask. Bumps the global version when
// the mask actually changed so the shadow walk re-instruments
// dependent code. The caller (sys.monitoring.set_events) is
// responsible for the world-stop CPython does around this; gopy is
// single-interpreter and does not yet stop the world.
//
// CPython: Python/instrumentation.c:2004 _PyMonitoring_SetEvents
func (s *InterpState) SetEvents(tool Tool, events EventSet) error {
	if err := CheckToolID(tool); err != nil {
		return err
	}
	if events>>UngroupedEvents != 0 {
		return fmt.Errorf("event set %#x has bits outside the %d ungrouped events", events, UngroupedEvents)
	}
	if s.GetEvents(tool) == events {
		return nil
	}
	bit := uint8(1) << tool
	for ev := 0; ev < UngroupedEvents; ev++ {
		active := uint8((events >> ev) & 1)
		s.Monitors.Tools[ev] &^= bit
		s.Monitors.Tools[ev] |= active << tool
	}
	s.ToolVersions[tool]++
	s.bumpVersion()
	return nil
}

// RegisterCallback installs callable for the (tool, event) slot and
// returns the previous callable, or nil if none was registered. Pass
// nil for callable to clear the slot. The branch event splits into
// BranchLeft and BranchRight; CPython wraps it with a synthetic
// dispatcher before storing, gopy mirrors the same split but stores
// the user callable directly because the legacy branch handler type
// has not been ported yet.
//
// CPython: Python/instrumentation.c:3018 _PyMonitoring_RegisterCallback
func (s *InterpState) RegisterCallback(tool Tool, ev Event, callable objects.Object) (objects.Object, error) {
	if err := CheckToolID(tool); err != nil {
		return nil, err
	}
	if !validEvent(ev) {
		return nil, fmt.Errorf("invalid event %d", ev)
	}
	if ev == EventBranch {
		prev := s.Callables[tool][EventBranchLeft]
		s.Callables[tool][EventBranchLeft] = callable
		s.Callables[tool][EventBranchRight] = callable
		return prev, nil
	}
	prev := s.Callables[tool][ev]
	s.Callables[tool][ev] = callable
	return prev, nil
}

// Callback returns the callable registered for (tool, event), or nil
// if the slot is empty. The fire-event entry points read this on
// every event.
func (s *InterpState) Callback(tool Tool, ev Event) objects.Object {
	if !validTool(tool) || !validEvent(ev) {
		return nil
	}
	return s.Callables[tool][ev]
}
