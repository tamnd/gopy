// sys.monitoring exposes the PEP 669 instrumentation hooks. CPython
// builds it in Python/instrumentation.c as a real subsystem wired into
// the bytecode interpreter; gopy hasn't ported the instrumentation
// runtime yet. This file installs the public surface as a stub so
// pure-Python callers (bdb, coverage, profilers) can import without
// trapping, even though set_events / register_callback are no-ops.
//
// The shape mirrors `sys.monitoring` exactly so attribute access,
// bitmask arithmetic on `events.*`, and tool-id constants behave the
// same way they would under upstream CPython. Anyone reaching past
// the stubs (calling set_events with a real callback expecting
// dispatch) needs the full runtime port, tracked in a follow-up
// spec.
//
// CPython: Python/instrumentation.c:3001 _PyMonitoring_PrintEvents
// CPython: Include/cpython/monitoring.h:8 PY_MONITORING_EVENT_*
// CPython: Doc/library/sys.monitoring.rst PEP 669 API

package sys

import (
	"github.com/tamnd/gopy/objects"
)

// monitoringEvents maps the upstream PY_MONITORING_EVENT_* enum to
// the bitmask values sys.monitoring.events publishes. CPython stores
// the bit positions in a C enum and computes the user-visible mask
// as `1 << event_id`. Mirroring the table here keeps the constant
// values byte-identical so user code doing `E.PY_START | E.LINE`
// gets the same integer CPython would.
//
// CPython: Include/cpython/monitoring.h:8 PY_MONITORING_EVENT_PY_START
var monitoringEvents = []struct {
	name string
	bit  int64
}{
	{"NO_EVENTS", 0},
	{"PY_START", 1 << 0},
	{"PY_RESUME", 1 << 1},
	{"PY_RETURN", 1 << 2},
	{"PY_YIELD", 1 << 3},
	{"CALL", 1 << 4},
	{"LINE", 1 << 5},
	{"INSTRUCTION", 1 << 6},
	{"JUMP", 1 << 7},
	{"BRANCH_LEFT", 1 << 8},
	{"BRANCH_RIGHT", 1 << 9},
	{"STOP_ITERATION", 1 << 10},
	{"RAISE", 1 << 11},
	{"EXCEPTION_HANDLED", 1 << 12},
	{"PY_UNWIND", 1 << 13},
	{"PY_THROW", 1 << 14},
	{"RERAISE", 1 << 15},
	{"C_RETURN", 1 << 16},
	{"C_RAISE", 1 << 17},
	{"BRANCH", 1 << 18},
}

// monitoringToolIDs maps the public DEBUGGER_ID / COVERAGE_ID etc.
// to the same small integers CPython hard-codes.
//
// CPython: Python/instrumentation.c PEP 669 reserved tool IDs
var monitoringToolIDs = []struct {
	name string
	id   int64
}{
	{"DEBUGGER_ID", 0},
	{"COVERAGE_ID", 1},
	{"PROFILER_ID", 2},
	{"OPTIMIZER_ID", 5},
}

// makeMonitoring builds the sys.monitoring namespace. Returns it as
// a SimpleNamespace so attribute access matches CPython.
//
// CPython: Python/instrumentation.c monitoring_methods + monitoring_events
func makeMonitoring() objects.Object {
	events := objects.NewNamespace()
	ed := events.Dict()
	for _, e := range monitoringEvents {
		_ = ed.SetItem(objects.NewStr(e.name), objects.NewInt(e.bit))
	}

	mon := objects.NewNamespace()
	md := mon.Dict()
	_ = md.SetItem(objects.NewStr("events"), events)
	for _, t := range monitoringToolIDs {
		_ = md.SetItem(objects.NewStr(t.name), objects.NewInt(t.id))
	}
	// DISABLE is the sentinel sys.monitoring callbacks return to opt
	// out of the per-event-location dispatch slot. CPython publishes
	// it as a dedicated singleton; gopy uses a distinct object so
	// identity comparisons (`return sys.monitoring.DISABLE`) survive
	// round-trips through user code.
	//
	// CPython: Python/instrumentation.c PY_MONITORING_DISABLE
	_ = md.SetItem(objects.NewStr("DISABLE"), objects.NewNamespace())
	_ = md.SetItem(objects.NewStr("MISSING"), objects.NewNamespace())

	for _, name := range []string{
		"use_tool_id", "free_tool_id", "get_tool", "clear_tool_id",
		"register_callback", "set_events", "get_events",
		"set_local_events", "get_local_events", "restart_events",
	} {
		_ = md.SetItem(objects.NewStr(name),
			objects.NewBuiltinFunction(name, monitoringNoop))
	}
	return mon
}

// monitoringNoop returns None for every sys.monitoring entry point.
// Callbacks registered through register_callback never fire under the
// stub, which is the only correct behaviour until the bytecode
// interpreter learns to emit instrumentation events.
//
// CPython: Python/instrumentation.c sys_monitoring_set_events
func monitoringNoop(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	_ = args
	_ = kwargs
	return objects.None(), nil
}
