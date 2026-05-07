// Package monitor implements PEP 669 (sys.monitoring) and the legacy
// sys.settrace / sys.setprofile glue. The package owns the tool /
// event tables, the per-code monitoring data, and the fire-event
// entry points the eval loop calls.
//
// CPython lays this out across three files: Include/cpython/monitoring.h
// has the public event IDs, Include/internal/pycore_instruments.h has
// the tool ID constants and the per-code data struct, and
// Python/instrumentation.c has the runtime. gopy keeps the same shape
// but flattens it into one Go package because Go does not have the
// header / .c separation.
//
// CPython: Include/cpython/monitoring.h
// CPython: Include/internal/pycore_instruments.h

package monitor

// Event is one of the PEP 669 monitoring event IDs.
//
// CPython: Include/cpython/monitoring.h:8 PY_MONITORING_EVENT_*
type Event uint8

// Local events fire from instrumented bytecode.
//
// CPython: Include/cpython/monitoring.h:8-18
const (
	EventPyStart       Event = 0
	EventPyResume      Event = 1
	EventPyReturn      Event = 2
	EventPyYield       Event = 3
	EventCall          Event = 4
	EventLine          Event = 5
	EventInstruction   Event = 6
	EventJump          Event = 7
	EventBranchLeft    Event = 8
	EventBranchRight   Event = 9
	EventStopIteration Event = 10
)

// Other events, mainly raised on exception flow.
//
// CPython: Include/cpython/monitoring.h:25-29
const (
	EventRaise            Event = 11
	EventExceptionHandled Event = 12
	EventPyUnwind         Event = 13
	EventPyThrow          Event = 14
	EventReraise          Event = 15
)

// Ancillary events.
//
// CPython: Include/cpython/monitoring.h:34-36
const (
	EventCReturn Event = 16
	EventCRaise  Event = 17
	EventBranch  Event = 18 // deprecated in 3.14, but still wired
)

// Event-count constants. CPython distinguishes three counts:
// LocalEvents covers the events that fire from instrumented bytecode
// (those need a per-codeunit tools table); UngroupedEvents counts
// every "real" event before the BRANCH_* split; Events is the total
// count including the derived BRANCH event.
//
// CPython: Include/internal/pycore_instruments.h:70-76
const (
	LocalEvents     = 11
	UngroupedEvents = 16
	Events          = 19
)

// IsInstrumentedEvent returns true for events that originate from
// instrumented bytecode (the local-events range). Mirrors the
// PY_MONITORING_IS_INSTRUMENTED_EVENT macro.
//
// CPython: Include/cpython/monitoring.h:20 PY_MONITORING_IS_INSTRUMENTED_EVENT
func IsInstrumentedEvent(ev Event) bool {
	return uint8(ev) < LocalEvents
}

// EventSet is a bitmask of event IDs. Tool registration takes one of
// these so a tool can opt into multiple events in one call.
//
// CPython: Include/internal/pycore_instruments.h:15 _PyMonitoringEventSet
type EventSet uint32

// Has reports whether ev is in the set.
func (s EventSet) Has(ev Event) bool { return s&(1<<ev) != 0 }

// With returns a copy of s with ev added.
func (s EventSet) With(ev Event) EventSet { return s | (1 << ev) }

// Without returns a copy of s with ev removed.
func (s EventSet) Without(ev Event) EventSet { return s &^ (1 << ev) }
