---
format: md
id: 1695_gopy_instrumentation
title: "1695. gopy sys.monitoring instrumentation"
sidebar_label: "1695. instrumentation"
sidebar_position: 1695
slug: /specs/1695-instrumentation
description: "v0.11 port of Python/instrumentation.c. PEP 669 sys.monitoring tool slots, the 19-event taxonomy, INSTRUMENTED_* opcode shadowing, _Py_Instrument chain, and DISABLE handling."
---


## Goal

Port CPython's PEP 669 instrumentation runtime into `monitor/` so
that `sys.monitoring.set_events(tool_id, events)` causes the
matching events to fire on the gopy VM. This is the first half of
the v0.11 monitor stack; spec 1696 (`legacy_tracing`) bolts
`sys.settrace` / `sys.setprofile` on top.

The v0.6 VM stubbed `_Py_call_instrumentation*` to no-op. v0.11
makes the chain real, opcode shadowing included.

## Why this lives in its own package

CPython merges three concerns inside `Python/instrumentation.c`:

1. The PEP 669 surface (`sys.monitoring.use_tool_id` and friends).
2. Per-code-object instrumentation bookkeeping
   (`_PyCoMonitoringData`, line tables, per-instruction tools).
3. Opcode-shadowing: rewriting in-stream opcodes to their
   `INSTRUMENTED_<X>` variants when a tool subscribes to the
   matching event, then unshadowing on unsubscribe.

The monitoring version sits in the upper bits of
`tstate.eval_breaker` so the dispatch loop can detect a stale
instrumentation snapshot and re-shadow. We keep all of this in one
package (`monitor/`) because the lock discipline crosses every
boundary: install, dispatch, fire all hold or release the same
`monitoring_lock` on `PyInterpreterState`.

## Sources of truth

| CPython file                                | Lines | Target                              |
|---------------------------------------------|------:|-------------------------------------|
| `Python/instrumentation.c`                  |  3174 | `monitor/instrumentation.go` and splits |
| `Include/cpython/monitoring.h`              |    50 | `monitor/events.go` event ids       |
| `Include/internal/pycore_instruments.h`     |   127 | `monitor/types.go`                  |
| `Modules/_monitoring.c`                     |  ~700 | `monitor/module.go` (built-in)      |

## Package layout

```
monitor/
  events.go         Event id constants, event-set bitmask helpers
  types.go          LocalMonitors, GlobalMonitors, CoMonitoringData,
                    LineInstrumentationData
  state.go          Per-interpreter monitoring state (tools, callbacks,
                    versions, lock)
  install.go        _Py_Instrument and the shadow / unshadow walk
                    (instrumentation.c:1923 _Py_Instrument)
  dispatch.go       call_instrumentation_vector / call_one_instrument
                    fan-out to registered callbacks
  line.go           Per-code line instrumentation: _PyCoLineInstrumentationData,
                    _Py_Instrumentation_GetLine, INSTRUMENTED_LINE arm
  tool.go           Tool slot lifecycle: use_tool_id / free_tool_id,
                    set_events, set_local_events
  fire.go           _PyMonitoring_Fire<Event> entry points (one per event)
  module.go         The "_monitoring" built-in module surface
                    (sys.monitoring is exposed via a sysmodule shim)
```

## Tool slots and event taxonomy

PEP 669 defines 8 tool slots and 19 event ids. The constants port
directly from `Include/cpython/monitoring.h`:

```go
// CPython: Include/internal/pycore_instruments.h:70
const ToolIDs = 8

// Reserved IDs (PEP 669, plus the two we steal for legacy tracing).
// CPython: Include/internal/pycore_instruments.h:20
const (
    ToolDebugger    = 0
    ToolCoverage    = 1
    ToolProfiler    = 2
    ToolOptimizer   = 5
    ToolSysProfile  = 6 // legacy sys.setprofile bridge
    ToolSysTrace    = 7 // legacy sys.settrace bridge
)

// CPython: Include/cpython/monitoring.h:8
const (
    EventPyStart           = 0
    EventPyResume          = 1
    EventPyReturn          = 2
    EventPyYield           = 3
    EventCall              = 4
    EventLine              = 5
    EventInstruction       = 6
    EventJump              = 7
    EventBranchLeft        = 8
    EventBranchRight       = 9
    EventStopIteration     = 10
    // ungrouped boundary at 11 (LOCAL_EVENTS = 11)
    EventRaise             = 11
    EventExceptionHandled  = 12
    EventPyUnwind          = 13
    EventPyThrow           = 14
    EventReraise           = 15
    // ungrouped boundary at 16 (UNGROUPED_EVENTS = 16)
    EventCReturn           = 16
    EventCRaise            = 17
    EventBranch            = 18 // grouped: BRANCH_LEFT | BRANCH_RIGHT
    NumEvents              = 19
)

const (
    LocalEvents     = 11 // events 0..10 may be set per-code-object
    UngroupedEvents = 16 // events 0..15 are concrete, 16..18 are derived
)
```

Events 0..10 are *local*: a tool can opt in per-code-object via
`set_local_events(code, tool_id, events)`. Events 11..15 are
*global only*. Events 16..18 are *grouped* and resolve to one of
the 0..15 events at fire time.

## The two monitor tables

```go
// CPython: Include/internal/pycore_instruments.h:79
type LocalMonitors struct {
    Tools [LocalEvents]uint8 // bit i set = tool i subscribed
}

// CPython: Include/internal/pycore_instruments.h:83
type GlobalMonitors struct {
    Tools [UngroupedEvents]uint8
}
```

Each `Tools[event]` byte is a bitmap of subscribed tool ids. Eight
tools fit in one byte exactly. The interpreter holds one
`GlobalMonitors`; every `PyCodeObject` holds two `LocalMonitors`
(one for the user-set local events, one for the active subset
after global-event union).

## Per-code monitoring data

```go
// CPython: Include/internal/pycore_instruments.h:100
type CoMonitoringData struct {
    LocalMonitors  LocalMonitors
    ActiveMonitors LocalMonitors
    Tools          []uint8 // per-codeunit tool bitmap (len = code size)
    ToolVersions   [ToolIDs]uintptr
    Lines          *LineInstrumentationData
    LineTools      []uint8
    PerInstrOpcodes []uint8 // shadow buffer: original opcodes
    PerInstrTools   []uint8 // per-codeunit instruction-event bitmap
}
```

`PerInstrOpcodes` is the unshadow target: when an opcode is rewritten
to an `INSTRUMENTED_*` variant in the live bytecode, the original
opcode goes here so an unsubscribe can restore it.

`Lines` is the packed line table. Each codeunit gets
`bytes_per_entry` bytes (1 or 2 depending on line-number range) so
`_Py_Instrumentation_GetLine` is a single memory read.

## The shadowing rewrite

The core trick of PEP 669 is opcode shadowing: when a tool
subscribes to `EVENT_CALL`, the dispatcher rewrites the entry
point of every Python function from `RESUME` to
`INSTRUMENTED_RESUME`. The instrumented arm fires the event, then
falls through to the original RESUME logic.

`monitor/install.go:Instrument` (CPython:
`instrumentation.c:1923 _Py_Instrument`) is the rewrite pass. It
runs:

1. Take `interp.monitoring_lock`.
2. Compute the active union: `ActiveMonitors = LocalMonitors | (GlobalMonitors filtered to local events)`.
3. Walk every codeunit. For each opcode, look up its event in the
   `EVENT_FOR_OPCODE` table. If any tool is subscribed, rewrite to
   `INSTRUMENTED_<X>`; if no tool is subscribed and the opcode is
   already `INSTRUMENTED_<X>`, restore from `PerInstrOpcodes`.
4. Bump `code.co_monitoring_version` to a fresh value so any thread
   running this code sees the change at next dispatch.
5. Release the lock.

The rewrite is idempotent. A second call with the same active set
is a no-op except for the version bump.

The `EVENT_FOR_OPCODE`, `DE_INSTRUMENT`, and `INSTRUMENTED_OPCODES`
tables are generated alongside `vm/opcodes_gen.go`. We extend
`tools/bytecodes_gen/` to emit them in `monitor/tables_gen.go`.

## Dispatch hook in vm/eval

`vm/dispatch.go` already exposes a `tryInstrumented(...)` no-op
hook for every adaptive opcode. v0.11 swaps each hook for a real
table lookup:

```go
// pseudo-Go inside the dispatch loop, hot path
if frame.Code.MonitoringVersion != frame.Tstate.MonitoringVersion {
    monitor.ResyncFrame(frame) // pick up shadow changes
}
op := codeUnits[frame.IP]
if monitor.IsInstrumented(op) {
    err := monitor.Fire(frame, op)
    if err != nil { return err }
    op = monitor.OriginalOpcode(frame, frame.IP) // unshadowed view
}
// then specialize / generic arm as before
```

The version snapshot lives in the upper bits of `tstate.eval_breaker`
just as in CPython, so the version compare is a single machine word
load on the hot path.

## DISABLE sentinel

A monitoring callback can return `sys.monitoring.DISABLE` to mean
"do not fire this event again at this code location for this tool".
`_PyMonitoring_DISABLE` is a sentinel object exposed at
`monitor.SentinelDisable` and recognised by every fire path.

`call_one_instrument` (instrumentation.c:`call_one_instrument`)
checks for the sentinel after each callback invocation. On hit, it
clears the matching tool bit out of `code.co_monitoring.tools[ip]`
(or `line_tools[ip]` for line events) and bumps the local
monitoring version, which causes the next dispatch to re-shadow
this single codeunit (potentially restoring the original opcode if
that was the last subscriber).

## Fire-event entry points

`monitor/fire.go` ports the 19 `_PyMonitoring_Fire<Event>` entry
points (`instrumentation.c:2667-2900`). Each follows the same
template:

```go
// CPython: Python/instrumentation.c:2667 _PyMonitoring_FirePyStartEvent
func FirePyStart(state *MonitoringState, codeLike objects.Object, offset int32) error
```

The signature varies per event (some carry a value, some carry an
exception, BRANCH_LEFT/RIGHT carry a destination). The internal
fan-out goes through `call_instrumentation_vector` ->
`call_one_instrument` -> the user-registered callback.

Each fire entry checks the per-tool bit on
`code.co_monitoring.active_monitors.tools[event]`, snaps the
active tool list, releases the monitoring lock, and invokes each
callback with the lock dropped. This matches CPython's
"locks held only across bookkeeping, never across user callbacks"
contract.

## Tool slot lifecycle

```go
// CPython: Python/instrumentation.c (sys.monitoring.use_tool_id)
func UseToolID(toolID int, name string) error

// CPython: Python/instrumentation.c (sys.monitoring.free_tool_id)
func FreeToolID(toolID int) error

// CPython: Python/instrumentation.c:2003 _PyMonitoring_SetEvents
func SetEvents(toolID int, events EventSet) error

// CPython: Python/instrumentation.c:2032 _PyMonitoring_SetLocalEvents
func SetLocalEvents(code *objects.Code, toolID int, events EventSet) error
```

Tool ids 6 and 7 are reserved for the legacy bridge (spec 1696).
Calling `UseToolID(6)` or `(7)` from Python user code raises
`ValueError`, matching CPython.

`SetEvents` walks every code object reachable from the
interpreter (via `_PyEval_GetBuiltins` + module globals). gopy
keeps a per-interpreter weak code-object set in `state.Interp` so
this walk is O(reachable code) rather than a heap scan.

## Out of scope for v0.11

* Per-thread instrumentation (`sys.monitoring.set_thread_local_events`):
  not in 3.14, lands when CPython lands it.
* `INSTRUMENTED_INSTRUCTION` profiling firehose at full granularity:
  ports as a stub that always fires for now. Tuning is post-v0.11.

## Gate

`sys.monitoring.set_events(0, sys.monitoring.events.LINE)` then
running a small fixture fires `EVENT_LINE` once per source line
in the executed code, matching CPython byte-for-byte on the same
fixture. Combined with spec 1696 the legacy `sys.settrace(f)`
gate falls out for free.
