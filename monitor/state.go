// Per-code and per-interpreter state used by the instrumentation
// runtime. CPython stores monitoring data on three levels:
//
//   - Global per-interpreter: which tool registered which callback,
//     plus the global event mask the interpreter tries to instrument.
//   - Per-code "local monitors": the tools currently observing a
//     specific code object's events. Read by _Py_Instrument when the
//     interpreter walks the bytecode shadow.
//   - Per-codeunit tool tables: which tools want a notification when
//     the matching codeunit fires. Allocated lazily because most code
//     objects never get instrumented.
//
// gopy mirrors these structures one-for-one so the fire-event entry
// points (and later the _Py_Instrument shadow walk) read the same
// shape CPython does.
//
// CPython: Include/internal/pycore_instruments.h:79-118

package monitor

// LocalMonitors carries the tool bitmask per local-event slot for a
// single code object. tools[ev] is a uint8 with one bit per tool
// (ToolDebugger ... ToolSysTrace).
//
// CPython: Include/internal/pycore_instruments.h:79 _Py_LocalMonitors
type LocalMonitors struct {
	Tools [LocalEvents]uint8
}

// GlobalMonitors carries the tool bitmask per ungrouped event for
// the whole interpreter. The grouped BRANCH event is split into
// BRANCH_LEFT / BRANCH_RIGHT before reaching this table, so the
// shape matches CPython's count of 16 "real" events.
//
// CPython: Include/internal/pycore_instruments.h:83 _Py_GlobalMonitors
type GlobalMonitors struct {
	Tools [UngroupedEvents]uint8
}

// IsActive reports whether any tool is registered for ev.
func (g *GlobalMonitors) IsActive(ev Event) bool {
	if int(ev) >= len(g.Tools) {
		return false
	}
	return g.Tools[ev] != 0
}

// IsActive reports whether any tool is registered for ev on this code.
func (l *LocalMonitors) IsActive(ev Event) bool {
	if int(ev) >= len(l.Tools) {
		return false
	}
	return l.Tools[ev] != 0
}

// LineInstrumentationData is the per-codeunit auxiliary table used
// by line events. CPython stores variable-width entries inline; gopy
// uses a flat byte slice indexed by codeunit because the deltas are
// small and we don't need the union-y layout.
//
// CPython: Include/internal/pycore_instruments.h:91 _PyCoLineInstrumentationData
type LineInstrumentationData struct {
	BytesPerEntry uint8
	Data          []byte
}

// CoMonitoringData holds the per-code instrumentation state. Keep it
// nil on code objects that have never been instrumented; allocate it
// lazily in _Py_Instrument.
//
// CPython: Include/internal/pycore_instruments.h:100 _PyCoMonitoringData
type CoMonitoringData struct {
	// LocalMonitors records the tools currently observing each
	// local event for this code.
	LocalMonitors LocalMonitors
	// ActiveMonitors is the union of LocalMonitors and the
	// per-interpreter GlobalMonitors at the time of the last
	// instrument pass. Read on the hot path to skip the global
	// table dereference.
	ActiveMonitors LocalMonitors
	// Tools is the per-codeunit bitmask used by the fire-event
	// entry points to find which tools want to be notified at the
	// current instr offset. Length matches the bytecode in
	// codeunits when allocated; nil otherwise.
	Tools []uint8
	// ToolVersions tracks the per-tool monitoring version that was
	// active when the instrument pass ran. The shadow walk
	// re-instruments the code when these drift.
	ToolVersions [MaxToolID]uintptr
	// Lines / LineTools back the line-event arms.
	Lines     *LineInstrumentationData
	LineTools []uint8
	// PerInstructionOpcodes / PerInstructionTools back the
	// per-instruction event arm. These hold the underlying
	// (de-instrumented) opcodes the dispatcher should restore to
	// when the per-instruction tools are removed.
	PerInstructionOpcodes []uint8
	PerInstructionTools   []uint8
}
