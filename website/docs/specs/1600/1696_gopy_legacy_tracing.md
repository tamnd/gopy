---
format: md
id: 1696_gopy_legacy_tracing
title: "1696. gopy legacy tracing bridge"
sidebar_label: "1696. legacy_tracing"
sidebar_position: 1696
slug: /specs/1696-legacy_tracing
description: "v0.11 port of Python/legacy_tracing.c. Bridges sys.settrace and sys.setprofile onto the PEP 669 sys.monitoring runtime via reserved tool ids 6 and 7."
---


## Goal

Port `Python/legacy_tracing.c` to `monitor/legacy.go` so that
`sys.settrace(f)` and `sys.setprofile(f)` continue to work after
sys.monitoring (spec 1695) lands. Every callback the legacy API
forwards to the user is reachable from PEP 669 events. We do not
keep two parallel tracing systems; the legacy API becomes a thin
adapter layer over PEP 669.

## What's actually happening

`sys.settrace(f)` installs `f` on `tstate.c_tracefunc` and
`tstate.c_traceobj`. CPython 3.14 then registers a
`_PyLegacyEventHandler` against tool id 7 (`PY_MONITORING_SYS_TRACE_ID`)
for every event a Py_tracefunc cares about: `PyTrace_CALL`,
`PyTrace_LINE`, `PyTrace_RETURN`, `PyTrace_EXCEPTION`,
`PyTrace_OPCODE`, plus the `BRANCH` / `JUMP` PEP 669 events that
get folded back into `PyTrace_LINE` for legacy semantics.

When PEP 669 fires one of those events, the handler reads
`tstate.c_tracefunc` and forwards. The legacy API never has its
own dispatch loop; sys.monitoring does the work.

`sys.setprofile(f)` is the same thing against tool id 6
(`PY_MONITORING_SYS_PROFILE_ID`) for `PyTrace_CALL`,
`PyTrace_RETURN`, `PyTrace_C_CALL`, `PyTrace_C_RETURN`,
`PyTrace_C_EXCEPTION`.

## Sources of truth

| CPython file              | Lines | Target              |
|---------------------------|------:|---------------------|
| `Python/legacy_tracing.c` |   781 | `monitor/legacy.go` |
| `Include/pyframe.h`       |    -- | (already on `objects.Frame`) |

Frame fields involved (already present on `objects.Frame` from
v0.6):

| Field            | Used by                                  |
|------------------|------------------------------------------|
| `f_trace`        | the user trace object for this frame     |
| `f_trace_lines`  | bool, off implies skip line events       |
| `f_trace_opcodes`| bool, on implies enable INSTRUCTION events |
| `f_lineno`       | last reported line, used to suppress dupes |

Threadstate fields (on `state.Tstate`):

| Field            | Role                          |
|------------------|-------------------------------|
| `c_tracefunc`    | legacy trace callback         |
| `c_traceobj`     | legacy trace state object     |
| `c_profilefunc`  | legacy profile callback       |
| `c_profileobj`   | legacy profile state object   |

## Adapter table

`legacy_tracing.c` defines one vectorcall adapter per PEP 669
event. Each takes the PEP 669 callback args, projects them back
into the (frame, kind, arg) shape the legacy callback expects, and
invokes the user's `c_tracefunc` / `c_profilefunc`.

| Adapter                       | Source line | PEP 669 event           | Legacy kind        |
|-------------------------------|------------:|-------------------------|--------------------|
| `sys_profile_start`           |          60 | EVENT_PY_START          | PyTrace_CALL       |
| `sys_profile_throw`           |          71 | EVENT_PY_THROW          | PyTrace_CALL       |
| `sys_profile_return`          |          82 | EVENT_PY_RETURN         | PyTrace_RETURN     |
| `sys_profile_yield`           |          ~ | EVENT_PY_YIELD          | PyTrace_RETURN     |
| `sys_profile_unwind`          |          93 | EVENT_PY_UNWIND         | PyTrace_RETURN(NULL) |
| `sys_profile_c_call`          |         ~ | EVENT_C_RAISE/EVENT_CALL | PyTrace_C_CALL    |
| `sys_profile_c_return`        |         ~ | EVENT_C_RETURN          | PyTrace_C_RETURN   |
| `sys_profile_c_exception`     |         ~ | EVENT_C_RAISE           | PyTrace_C_EXCEPTION |
| `sys_trace_start`             |         ~ | EVENT_PY_START          | PyTrace_CALL       |
| `sys_trace_throw`             |         ~ | EVENT_PY_THROW          | PyTrace_CALL       |
| `sys_trace_return`            |         ~ | EVENT_PY_RETURN         | PyTrace_RETURN     |
| `sys_trace_yield`             |         ~ | EVENT_PY_YIELD          | PyTrace_RETURN     |
| `sys_trace_unwind`            |         ~ | EVENT_PY_UNWIND         | PyTrace_RETURN(NULL) |
| `sys_trace_exception_func`    |         ~ | EVENT_RAISE             | PyTrace_EXCEPTION  |
| `sys_trace_line_func`         |         ~ | EVENT_LINE              | PyTrace_LINE       |
| `sys_trace_jump_func`         |         ~ | EVENT_JUMP              | PyTrace_LINE (folded) |
| `sys_trace_instruction_func`  |         ~ | EVENT_INSTRUCTION       | PyTrace_OPCODE     |
| `sys_trace_c_call_or_return`  |         ~ | EVENT_C_CALL/_C_RETURN  | (no-op for trace, profile only) |

Every adapter is a Go closure of type `vectorcallfunc`, registered
via `monitor.RegisterCallback(toolID, eventID, adapter)` (PEP 669
surface from spec 1695).

## Install path

```go
// CPython: Python/legacy_tracing.c:697 _PyEval_SetTrace
func SetTrace(tstate *state.Tstate, fn TraceFunc, arg objects.Object) error

// CPython: Python/legacy_tracing.c:532 _PyEval_SetProfile
func SetProfile(tstate *state.Tstate, fn TraceFunc, arg objects.Object) error
```

Each follows the same script:

1. Acquire `interp.monitoring_lock` (or stop-the-world under
   `Py_GIL_DISABLED`; we use the mutex on the GIL build only).
2. `swap_trace_func_arg(tstate, fn, arg)`: write the new pair into
   `c_tracefunc` / `c_traceobj`, return the old object so the
   caller can decref. (legacy_tracing.c:654)
3. `set_monitoring_trace_events(interp)`: union the events that
   any thread's `c_tracefunc` cares about, and call
   `monitor.SetEvents(ToolSysTrace, eventSet)` to subscribe.
   (legacy_tracing.c:666)
4. Release the lock.

The same shape applies to profile, against `ToolSysProfile`.

`SetTraceAllThreads` / `SetProfileAllThreads` walk every
`tstate` on the interpreter inside the lock. CPython's
`_PyEval_SetTraceAllThreads` (legacy_tracing.c:733) is the
reference. v0.7 already exposes the per-interp tstate list through
`state.Interp.Threads()`.

## Frame f_trace integration

`sys.settrace` installs a *global* trace; the per-frame `f_trace`
slot is the value returned by the global callback for that frame
(the "local trace function"). `sys_trace_line_func` (and friends)
reads `frame.f_trace`, calls it with `(frame, "line", None)`, then
either keeps `f_trace` or replaces it with the callback's return
value.

`f_trace_lines` and `f_trace_opcodes` gate line and instruction
events at the per-frame level. The PEP 669 EVENT_LINE arm checks
both flags before dispatching.

These three fields land on `objects.Frame` in this v0.11 work; the
v0.6 frame port left them out because no caller existed yet.

## Frame jump support

`PyFrame_FastToLocalsWithError` and `f_lineno` setter feed into
`sys.settrace`'s "set f_lineno from a trace callback to jump"
trick. v0.11 ports this via the EVENT_JUMP adapter; the user's
`c_tracefunc` writing to `frame.f_lineno` registers a pending jump
that the next dispatch consumes.

CPython implements this in `frame.c:frame_setlineno`. Already
ported in `objects/frame.go` for v0.7 (the bytecode-level setter).
v0.11 wires the trace adapter to it.

## Opcode-level tracing

`sys.settrace` callbacks can opt into per-instruction events by
setting `frame.f_trace_opcodes = True`. PEP 669 mapping: subscribe
tool 7 to `EVENT_INSTRUCTION`. The adapter pulls the original
opcode out of the frame's per-codeunit shadow table (set up by
spec 1695) and forwards as `PyTrace_OPCODE`.

`legacy_tracing.c:159 _PyEval_SetOpcodeTrace` is the entry point;
it lives in `monitor/legacy.go` rather than on the frame because
toggling it has to round-trip through the monitoring rewrite pass
to shadow / unshadow opcodes.

## DISABLE behaviour

If a legacy callback returns `None`, the frame's `f_trace` is
cleared (CPython behaviour). The adapter does not emit
`monitor.SentinelDisable`, because legacy trace expects to be
re-fired on the next event for the *next* frame. Returning
`SentinelDisable` would permanently disable that codeunit, which
breaks coverage tools and pdb.

This is the only place where we deliberately break PEP 669
"disable forever" semantics. Comment in code reads:

```go
// CPython: Python/legacy_tracing.c call_trace_func: legacy trace
// drops f_trace on a None return but does not propagate DISABLE
// upward. PEP 669 callers should subscribe directly.
```

## Locking order

1. `interp.monitoring_lock` (from spec 1695).
2. `tstate.c_trace_lock` (cheap mutex for the legacy field swap).

Always taken in that order. Never the reverse. `set_monitoring_trace_events`
holds the monitoring lock; `swap_trace_func_arg` holds the trace
lock. `_PyEval_SetTrace` takes monitoring first, swaps trace, then
re-runs `set_monitoring_trace_events` so the union is consistent
with the new trace state.

## Tests

* **`monitor/legacy_settrace_test.go`**. Install a Go-side trace
  function, run a fixture, assert event sequence
  (`call`, `line`, `line`, `return`).
* **`monitor/legacy_setprofile_test.go`**. Same shape, profile
  events.
* **`monitor/legacy_jump_test.go`**. Trace function sets
  `frame.f_lineno`, assert the dispatch jumps.
* **`monitor/legacy_parity_test.go`**. CPython-side: run the same
  fixture under `python3` with `sys.settrace`, capture event log,
  compare byte-for-byte against gopy. Mirrors
  `parser/parity_test.go` from v0.10.2.

## Out of scope for v0.11

* `bdb` / `pdb` integration. They use `sys.settrace` but extra
  hooks (sigint, breakpoints) are post-v0.11 polish.
* C-level profiling (`PyTrace_C_CALL` for native extensions)
  matters less for gopy because we do not load C extensions
  (1602). The adapters port for completeness; the events fire when
  a user calls a Go-implemented builtin.

## Gate

`sys.settrace(f); f(...)` calls back on every line of `f`. Combined
with spec 1695, this is the v0.11 "monitoring works" gate.
