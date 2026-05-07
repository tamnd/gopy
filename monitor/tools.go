// Tool IDs. PEP 669 reserves four IDs for well-known tools so
// debuggers, coverage tools, profilers, and the optimizer can
// coexist; CPython adds two internal IDs that back the legacy
// sys.settrace / sys.setprofile entry points.
//
// CPython: Include/internal/pycore_instruments.h:20-27

package monitor

// Tool is a 0..MaxToolID-1 index used to key per-tool callbacks.
type Tool uint8

// Reserved tool IDs from PEP 669.
//
// CPython: Include/internal/pycore_instruments.h:20-23
const (
	ToolDebugger  Tool = 0
	ToolCoverage  Tool = 1
	ToolProfiler  Tool = 2
	ToolOptimizer Tool = 5
)

// Internal tool IDs CPython uses to back sys.setprofile and
// sys.settrace. These do not appear in user-visible APIs but the
// instrumentation runtime treats them like any other tool slot.
//
// CPython: Include/internal/pycore_instruments.h:26-27
const (
	ToolSysProfile Tool = 6
	ToolSysTrace   Tool = 7
)

// MaxToolID is the upper bound on tool slot indices. CPython packs
// the active-tool bitmask into a single uint8 per event, so the
// table cannot grow past 8 without a wider type.
//
// CPython: Include/internal/pycore_instruments.h:70 PY_MONITORING_TOOL_IDS
const MaxToolID = 8
