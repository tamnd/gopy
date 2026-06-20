// Legacy sys.settrace / sys.setprofile bridge. CPython runs the
// classic PyTrace_* callback model on top of PEP 669 monitoring by
// registering a fan-out of small vectorcall handlers under tool IDs
// 6 (SYS_PROFILE) and 7 (SYS_TRACE), then dispatching back to
// PyThreadState.c_tracefunc / c_profilefunc when one of those fires.
// gopy mirrors the same shape: per-thread function pointers live on
// threadVM, the per-event handlers are objects.BuiltinFunctions
// captured from this package, and SetProfile / SetTrace stamp the
// global event mask through monitor.InterpState so the instrument
// pass picks them up.
//
// CPython: Python/legacy_tracing.c

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// LegacyTraceFunc is the Go shape of CPython's Py_tracefunc. The
// runtime hands the trace object the user installed, the current
// frame, the legacy event ID (one of the PyTrace_* constants), and
// the per-event argument the bridge synthesized. Returning a non-nil
// error aborts the trace path.
//
// CPython: Include/cpython/pystate.h:Py_tracefunc
type LegacyTraceFunc func(obj objects.Object, f *frame.Frame, kind int, arg objects.Object) error

// PyTrace_* are the legacy event kinds the bridge feeds to a
// LegacyTraceFunc. The integer values match the public
// Include/cpython/pystate.h constants so a port of pdb / cProfile
// behaves like the real thing.
//
// CPython: Include/cpython/pystate.h:Py_tracefunc constants
const (
	PyTraceCall       = 0
	PyTraceException  = 1
	PyTraceLine       = 2
	PyTraceReturn     = 3
	PyTraceCCall      = 4
	PyTraceCException = 5
	PyTraceCReturn    = 6
	PyTraceOpcode     = 7
)

// CTraceFunc returns the thread's legacy tracefunc, or nil if
// sys.settrace has not been called on this thread.
func CTraceFunc(ts *state.Thread) (LegacyTraceFunc, objects.Object) {
	v := vmFor(ts)
	return v.cTraceFunc, v.cTraceObj
}

// CProfileFunc returns the thread's legacy profilefunc.
func CProfileFunc(ts *state.Thread) (LegacyTraceFunc, objects.Object) {
	v := vmFor(ts)
	return v.cProfileFunc, v.cProfileObj
}

// callProfileFunc dispatches a PEP 669 profile-side event back to
// the thread's legacy c_profilefunc, mirroring CPython's
// call_profile_func.
//
// CPython: Python/legacy_tracing.c:38 call_profile_func
func callProfileFunc(event int, arg objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return objects.None(), nil
	}
	if ts.Tracing > 0 {
		return objects.None(), nil
	}
	fn, obj := CProfileFunc(ts)
	if fn == nil {
		return objects.None(), nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, fmt.Errorf("SystemError: missing frame when calling profile function")
	}
	ts.EnterTracing()
	err := fn(obj, f, event, arg)
	ts.LeaveTracing()
	if err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// callTraceFunc is the trace-side counterpart of callProfileFunc.
//
// CPython: Python/legacy_tracing.c:184 call_trace_func
func callTraceFunc(event int, arg objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return objects.None(), nil
	}
	// Re-entrance guard: skip if a trace callback is already running on
	// this thread, matching CPython's tstate->tracing check in
	// call_instrumentation_vector (Python/instrumentation.c:1146).
	//
	// CPython: Python/ceval.c:2607 PyThreadState_EnterTracing
	if ts.Tracing > 0 {
		return objects.None(), nil
	}
	fn, obj := CTraceFunc(ts)
	if fn == nil {
		return objects.None(), nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, fmt.Errorf("SystemError: missing frame when calling trace function")
	}
	if f.TraceOpcodes {
		if err := setOpcodeTrace(f, true); err != nil {
			return nil, err
		}
	}
	ts.EnterTracing()
	ts.WhatEvent = legacyTraceToEvent(event)
	err := fn(obj, f, event, arg)
	ts.WhatEvent = -1
	ts.LeaveTracing()
	if err != nil {
		return nil, err
	}
	f.Lineno = 0
	return objects.None(), nil
}

// legacyTraceToEvent maps a legacy PyTrace_* event to the PEP 669
// monitoring event the f_lineno setter switches on. The setter only
// permits a line jump from a 'line' event; the other events resolve to
// the monitoring IDs whose setter arms raise the matching diagnostic
// (PY_START for 'call', PY_RETURN for 'return', and so on).
//
// CPython: Python/legacy_tracing.c the per-event sys_trace_* shims set
// tstate->what_event before invoking the Python callback.
func legacyTraceToEvent(event int) int {
	switch event {
	case PyTraceLine:
		return int(monitor.EventLine)
	case PyTraceCall:
		return int(monitor.EventPyStart)
	case PyTraceReturn:
		return int(monitor.EventPyReturn)
	case PyTraceException:
		return int(monitor.EventRaise)
	case PyTraceOpcode:
		return int(monitor.EventInstruction)
	default:
		return int(monitor.EventPyStart)
	}
}

// setOpcodeTrace toggles per-instruction tracing on f.Code so the
// LINE-mode callback can opt into PyTrace_OPCODE for that frame.
// Mirrors set_opcode_trace_world_stopped from CPython, minus the
// stop-the-world dance gopy does not need yet.
//
// CPython: Python/legacy_tracing.c:136 set_opcode_trace_world_stopped
func setOpcodeTrace(f *frame.Frame, enable bool) error {
	ts := currentThread()
	if ts == nil {
		return nil
	}
	interp := ts.Interp().Monitors
	co := f.Code
	events, err := interp.GetLocalEvents(co, monitor.ToolSysTrace)
	if err != nil {
		return err
	}
	mask := monitor.EventSet(1) << monitor.EventInstruction
	if enable {
		if events&mask != 0 {
			return nil
		}
		events |= mask
	} else {
		if events&mask == 0 {
			return nil
		}
		events &^= mask
	}
	return interp.SetLocalEvents(co, monitor.ToolSysTrace, events)
}

// sysProfileStart handles PY_START / PY_RESUME on the profile tool.
//
// CPython: Python/legacy_tracing.c:60 sys_profile_start
func sysProfileStart(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callProfileFunc(PyTraceCall, objects.None())
}

// sysProfileThrow handles PY_THROW on the profile tool.
//
// CPython: Python/legacy_tracing.c:71 sys_profile_throw
func sysProfileThrow(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callProfileFunc(PyTraceCall, objects.None())
}

// sysProfileReturn handles PY_RETURN / PY_YIELD on the profile tool.
//
// CPython: Python/legacy_tracing.c:82 sys_profile_return
func sysProfileReturn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	retval := objects.None()
	if len(args) >= 3 {
		retval = args[2]
	}
	return callProfileFunc(PyTraceReturn, retval)
}

// sysProfileUnwind handles PY_UNWIND on the profile tool.
//
// CPython: Python/legacy_tracing.c:93 sys_profile_unwind
func sysProfileUnwind(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callProfileFunc(PyTraceReturn, nil)
}

// sysProfileCallOrReturn handles CALL / C_RETURN / C_RAISE on the
// profile tool. CPython narrows the callable type before forwarding
// so a method-descriptor call shows up as a builtin method to the
// user's tracefunc; gopy just hands the raw callable through until
// the typed PyMethodDescr port lands.
//
// CPython: Python/legacy_tracing.c:104 sys_profile_call_or_return
func sysProfileCallOrReturn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return objects.None(), nil
	}
	callable := args[2]
	if monitor.IsMissing(callable) {
		return objects.None(), nil
	}
	return callProfileFunc(PyTraceCall, callable)
}

// sysTraceStart handles PY_START / PY_RESUME on the trace tool.
//
// CPython: Python/legacy_tracing.c:239 sys_trace_start
func sysTraceStart(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callTraceFunc(PyTraceCall, objects.None())
}

// sysTraceThrow handles PY_THROW on the trace tool.
//
// CPython: Python/legacy_tracing.c:250 sys_trace_throw
func sysTraceThrow(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callTraceFunc(PyTraceCall, objects.None())
}

// sysTraceUnwind handles PY_UNWIND on the trace tool.
//
// CPython: Python/legacy_tracing.c:261 sys_trace_unwind
func sysTraceUnwind(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return callTraceFunc(PyTraceReturn, nil)
}

// sysTraceReturn handles PY_RETURN on the trace tool.
//
// CPython: Python/legacy_tracing.c:272 sys_trace_return
func sysTraceReturn(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	retval := objects.None()
	if len(args) >= 3 {
		retval = args[2]
	}
	return callTraceFunc(PyTraceReturn, retval)
}

// sysTraceYield handles PY_YIELD on the trace tool.
//
// CPython: Python/legacy_tracing.c:287 sys_trace_yield
func sysTraceYield(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	retval := objects.None()
	if len(args) >= 3 {
		retval = args[2]
	}
	return callTraceFunc(PyTraceReturn, retval)
}

// sysTraceExceptionFunc handles RAISE / STOP_ITERATION on the trace
// tool. CPython packs (type, exc, tb) into a 3-tuple before calling;
// gopy mirrors the shape so a ported pdb sees the same arg.
//
// CPython: Python/legacy_tracing.c:213 sys_trace_exception_func
func sysTraceExceptionFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	exc := objects.None()
	if len(args) >= 3 {
		exc = args[2]
	}
	exType := objects.None()
	if exc != objects.None() {
		exType = exc.Type()
	}
	tuple := objects.NewTuple([]objects.Object{exType, exc, objects.None()})
	return callTraceFunc(PyTraceException, tuple)
}

// sysTraceInstructionFunc handles INSTRUCTION on the trace tool. It
// flips PyTrace_OPCODE on, but only when the per-frame opcode flag
// is set; otherwise it disables the per-instruction event again so
// the bytecode does not pay the dispatch cost.
//
// CPython: Python/legacy_tracing.c:298 sys_trace_instruction_func
func sysTraceInstructionFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return objects.None(), nil
	}
	if ts.Tracing > 0 {
		return objects.None(), nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, fmt.Errorf("SystemError: missing frame when calling trace function")
	}
	fn, obj := CTraceFunc(ts)
	if fn == nil || !f.TraceOpcodes {
		if err := setOpcodeTrace(f, false); err != nil {
			return nil, err
		}
		return objects.None(), nil
	}
	ts.EnterTracing()
	ts.WhatEvent = int(monitor.EventInstruction)
	err := fn(obj, f, PyTraceOpcode, objects.None())
	ts.WhatEvent = -1
	ts.LeaveTracing()
	if err != nil {
		return nil, err
	}
	f.Lineno = 0
	return objects.None(), nil
}

// traceLine fires PyTrace_LINE on the trace tool unless the frame
// opted out. Mirrors CPython's trace_line helper.
//
// CPython: Python/legacy_tracing.c:329 trace_line
func traceLine(ts *state.Thread, f *frame.Frame, line int) (objects.Object, error) {
	if !f.TraceLines {
		return objects.None(), nil
	}
	if line < 0 {
		return objects.None(), nil
	}
	if ts.Tracing > 0 {
		return objects.None(), nil
	}
	fn, obj := CTraceFunc(ts)
	if fn == nil {
		return objects.None(), nil
	}
	f.Lineno = line
	ts.EnterTracing()
	ts.WhatEvent = int(monitor.EventLine)
	err := fn(obj, f, PyTraceLine, objects.None())
	ts.WhatEvent = -1
	ts.LeaveTracing()
	if err != nil {
		return nil, err
	}
	f.Lineno = 0
	return objects.None(), nil
}

// sysTraceLineFunc handles LINE on the trace tool. The first arg is
// the code object the event fired against; the second is the line
// number CPython packs as a PyLong.
//
// CPython: Python/legacy_tracing.c:351 sys_trace_line_func
func sysTraceLineFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return objects.None(), nil
	}
	fn, _ := CTraceFunc(ts)
	if fn == nil {
		return objects.None(), nil
	}
	if len(args) < 2 {
		return objects.None(), nil
	}
	li, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("LINE handler expected int line, got %T", args[1])
	}
	v, _ := li.Int64()
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, fmt.Errorf("SystemError: missing frame when calling trace function")
	}
	return traceLine(ts, f, int(v))
}

// sysTraceJumpFunc handles JUMP on the trace tool. Forward jumps
// suppress firing because the matching INSTRUMENTED_LINE will pick
// it up; backward jumps that land on a fresh line are also handled
// by INSTRUMENTED_LINE; only a backward jump landing on the same
// line emits a synthetic line event so a python-level tracer sees
// the loop iteration boundary.
//
// CPython: Python/legacy_tracing.c:378 sys_trace_jump_func
func sysTraceJumpFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return objects.None(), nil
	}
	if fn, _ := CTraceFunc(ts); fn == nil {
		return objects.None(), nil
	}
	if len(args) < 3 {
		return objects.None(), nil
	}
	codeObj, ok := args[0].(*objects.Code)
	if !ok {
		return nil, fmt.Errorf("JUMP handler expected code object, got %T", args[0])
	}
	fromI, ok1 := args[1].(*objects.Int)
	toI, ok2 := args[2].(*objects.Int)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("JUMP handler expected int offsets")
	}
	fromV, _ := fromI.Int64()
	toV, _ := toI.Int64()
	from := int(fromV) / 2
	to := int(toV) / 2
	if to > from {
		return monitor.Disable, nil
	}
	toLine := monitor.LineForOffset(codeObj, to)
	fromLine := monitor.LineForOffset(codeObj, from)
	if toLine != fromLine {
		return monitor.Disable, nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, fmt.Errorf("SystemError: missing frame when calling trace function")
	}
	if !f.TraceLines {
		return objects.None(), nil
	}
	return traceLine(ts, f, toLine)
}

// setCallbacks registers handler under tool against event1 (and
// event2 when non-negative). Mirrors set_callbacks in CPython, minus
// the legacy_event field on _PyLegacyEventHandler since gopy stores
// the legacy event ID in each closure directly.
//
// CPython: Python/legacy_tracing.c:430 set_callbacks
func setCallbacks(interp *monitor.InterpState, tool monitor.Tool, name string,
	handler func([]objects.Object, map[string]objects.Object) (objects.Object, error),
	event1, event2 monitor.Event,
) error {
	cb := objects.NewBuiltinFunction(name, handler)
	if _, err := interp.RegisterCallback(tool, event1, cb); err != nil {
		return err
	}
	if event2 < monitor.Events {
		if _, err := interp.RegisterCallback(tool, event2, cb); err != nil {
			return err
		}
	}
	return nil
}

// sentinelInvalidEvent is the "no second event" marker setCallbacks
// checks against. monitor.Events is one past the last valid event,
// so any value >= it is unused.
const sentinelInvalidEvent = monitor.Event(monitor.Events)

// setupProfileCallbacks installs the legacy sys.setprofile fan-out.
// CPython runs this once per interpreter via _PyOnceFlag; gopy uses
// sync.Once on InterpState.
//
// CPython: Python/legacy_tracing.c:462 setup_profile_callbacks
func setupProfileCallbacks(interp *monitor.InterpState) error {
	tool := monitor.ToolSysProfile
	type entry struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
		ev1  monitor.Event
		ev2  monitor.Event
	}
	for _, e := range []entry{
		{"sys_profile_start", sysProfileStart, monitor.EventPyStart, monitor.EventPyResume},
		{"sys_profile_throw", sysProfileThrow, monitor.EventPyThrow, sentinelInvalidEvent},
		{"sys_profile_return", sysProfileReturn, monitor.EventPyReturn, monitor.EventPyYield},
		{"sys_profile_unwind", sysProfileUnwind, monitor.EventPyUnwind, sentinelInvalidEvent},
		{"sys_profile_call", sysProfileCallOrReturn, monitor.EventCall, sentinelInvalidEvent},
		{"sys_profile_creturn", sysProfileCallOrReturn, monitor.EventCReturn, sentinelInvalidEvent},
		{"sys_profile_craise", sysProfileCallOrReturn, monitor.EventCRaise, sentinelInvalidEvent},
	} {
		if err := setCallbacks(interp, tool, e.name, e.fn, e.ev1, e.ev2); err != nil {
			return err
		}
	}
	return nil
}

// setupTraceCallbacks installs the legacy sys.settrace fan-out.
//
// CPython: Python/legacy_tracing.c:600 setup_trace_callbacks
func setupTraceCallbacks(interp *monitor.InterpState) error {
	tool := monitor.ToolSysTrace
	type entry struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
		ev1  monitor.Event
		ev2  monitor.Event
	}
	for _, e := range []entry{
		{"sys_trace_start", sysTraceStart, monitor.EventPyStart, monitor.EventPyResume},
		{"sys_trace_throw", sysTraceThrow, monitor.EventPyThrow, sentinelInvalidEvent},
		{"sys_trace_return", sysTraceReturn, monitor.EventPyReturn, sentinelInvalidEvent},
		{"sys_trace_yield", sysTraceYield, monitor.EventPyYield, sentinelInvalidEvent},
		{"sys_trace_exception", sysTraceExceptionFunc, monitor.EventRaise, monitor.EventStopIteration},
		{"sys_trace_line", sysTraceLineFunc, monitor.EventLine, sentinelInvalidEvent},
		{"sys_trace_unwind", sysTraceUnwind, monitor.EventPyUnwind, sentinelInvalidEvent},
		{"sys_trace_jump", sysTraceJumpFunc, monitor.EventJump, sentinelInvalidEvent},
		{"sys_trace_instruction", sysTraceInstructionFunc, monitor.EventInstruction, sentinelInvalidEvent},
	} {
		if err := setCallbacks(interp, tool, e.name, e.fn, e.ev1, e.ev2); err != nil {
			return err
		}
	}
	return nil
}

// swapProfileFuncArg installs func / arg on ts and returns the old
// profile object for the caller to drop. Bumps the interp profiling
// counter so the instrumentation runtime can short-circuit when no
// thread is profiling.
//
// CPython: Python/legacy_tracing.c:506 swap_profile_func_arg
func swapProfileFuncArg(ts *state.Thread, fn LegacyTraceFunc, arg objects.Object) objects.Object {
	v := vmFor(ts)
	delta := 0
	if fn != nil {
		delta++
	}
	if v.cProfileFunc != nil {
		delta--
	}
	v.cProfileFunc = fn
	old := v.cProfileObj
	v.cProfileObj = arg
	ts.Interp().Monitors.SysProfilingThreads += delta
	return old
}

// swapTraceFuncArg is the trace-side counterpart of
// swapProfileFuncArg.
//
// CPython: Python/legacy_tracing.c:654 swap_trace_func_arg
func swapTraceFuncArg(ts *state.Thread, fn LegacyTraceFunc, arg objects.Object) objects.Object {
	v := vmFor(ts)
	delta := 0
	if fn != nil {
		delta++
	}
	if v.cTraceFunc != nil {
		delta--
	}
	v.cTraceFunc = fn
	old := v.cTraceObj
	v.cTraceObj = arg
	ts.Interp().Monitors.SysTracingThreads += delta
	return old
}

// profileEventMask returns the event-set legacy profiling subscribes
// to when at least one thread has setprofile installed. CPython only
// stamps the bits that fit inside the 16 ungrouped events; C_RETURN
// and C_RAISE handlers are registered but stay dormant in the mask
// because the global event slot is uint16-shaped.
//
// CPython: Python/legacy_tracing.c:518 set_monitoring_profile_events
func profileEventMask() monitor.EventSet {
	var s monitor.EventSet
	s |= 1 << monitor.EventPyStart
	s |= 1 << monitor.EventPyResume
	s |= 1 << monitor.EventPyReturn
	s |= 1 << monitor.EventPyYield
	s |= 1 << monitor.EventCall
	s |= 1 << monitor.EventPyUnwind
	s |= 1 << monitor.EventPyThrow
	return s
}

// traceEventMask returns the event-set legacy tracing subscribes to.
//
// CPython: Python/legacy_tracing.c:666 set_monitoring_trace_events
func traceEventMask() monitor.EventSet {
	var s monitor.EventSet
	s |= 1 << monitor.EventPyStart
	s |= 1 << monitor.EventPyResume
	s |= 1 << monitor.EventPyReturn
	s |= 1 << monitor.EventPyYield
	s |= 1 << monitor.EventRaise
	s |= 1 << monitor.EventLine
	s |= 1 << monitor.EventJump
	s |= 1 << monitor.EventPyUnwind
	s |= 1 << monitor.EventPyThrow
	s |= 1 << monitor.EventStopIteration
	return s
}

// setMonitoringProfileEvents stamps the global profile event mask
// based on whether any thread has profile installed.
//
// CPython: Python/legacy_tracing.c:518 set_monitoring_profile_events
func setMonitoringProfileEvents(interp *monitor.InterpState) error {
	var events monitor.EventSet
	if interp.SysProfilingThreads > 0 {
		events = profileEventMask()
	}
	return interp.SetEvents(monitor.ToolSysProfile, events)
}

// setMonitoringTraceEvents stamps the global trace event mask.
//
// CPython: Python/legacy_tracing.c:666 set_monitoring_trace_events
func setMonitoringTraceEvents(interp *monitor.InterpState) error {
	var events monitor.EventSet
	if interp.SysTracingThreads > 0 {
		events = traceEventMask()
	}
	return interp.SetEvents(monitor.ToolSysTrace, events)
}

// SetProfile is the Go form of CPython's _PyEval_SetProfile. The
// caller passes in a thread, the legacy profile function (or nil to
// clear), and the user-visible argument that gets handed back to
// every callback. Returns the previously-installed argument so the
// caller can drop its reference.
//
// CPython: Python/legacy_tracing.c:531 _PyEval_SetProfile
func SetProfile(ts *state.Thread, fn LegacyTraceFunc, arg objects.Object) (objects.Object, error) {
	interp := ts.Interp().Monitors
	var setupErr error
	interp.SysProfileOnce.Do(func() {
		setupErr = setupProfileCallbacks(interp)
	})
	if setupErr != nil {
		return nil, setupErr
	}
	old := swapProfileFuncArg(ts, fn, arg)
	if err := setMonitoringProfileEvents(interp); err != nil {
		return old, err
	}
	return old, nil
}

// SetTrace is the Go form of CPython's _PyEval_SetTrace.
//
// CPython: Python/legacy_tracing.c:696 _PyEval_SetTrace
func SetTrace(ts *state.Thread, fn LegacyTraceFunc, arg objects.Object) (objects.Object, error) {
	interp := ts.Interp().Monitors
	var setupErr error
	interp.SysTraceOnce.Do(func() {
		setupErr = setupTraceCallbacks(interp)
	})
	if setupErr != nil {
		return nil, setupErr
	}
	old := swapTraceFuncArg(ts, fn, arg)
	if err := setMonitoringTraceEvents(interp); err != nil {
		return old, err
	}
	// set_monitoring_trace_events stamps only the global event mask; in
	// CPython the matching re-instrumentation happens one level down in
	// _PyMonitoring_SetEvents, which runs instrument_all_executing_code_objects
	// under stop-the-world. gopy's SetEvents only bumps the global
	// version, and a frame picks new events up at its next RESUME, so a
	// frame already past its RESUME (the one that called settrace and
	// its live callers) would otherwise never see them. Walk the live
	// stack and instrument each frame's code so line / return events
	// fire on the current call chain.
	//
	// CPython: Python/instrumentation.c:1941 instrument_all_executing_code_objects
	if err := instrumentExecutingFrames(ts, interp); err != nil {
		return old, err
	}
	if interp.SysTracingThreads > 0 {
		if err := maybeSetOpcodeTrace(ts); err != nil {
			return old, err
		}
	}
	return old, nil
}

// instrumentExecutingFrames re-instruments the code object of every
// frame currently on the thread's stack, the gopy stand-in for the
// instrument_all_executing_code_objects pass _PyMonitoring_SetEvents
// runs under stop-the-world. Walking the f_back chain from the top
// frame covers the frame that called settrace and every live caller,
// so events fire on the current call chain instead of waiting for a
// RESUME those frames have already passed.
//
// CPython: Python/instrumentation.c:1941 instrument_all_executing_code_objects
func instrumentExecutingFrames(ts *state.Thread, interp *monitor.InterpState) error {
	for f := frameStackFor(ts).Top(); f != nil; f = f.Previous {
		if f.Code == nil {
			continue
		}
		if err := monitor.Instrument(f.Code, interp); err != nil {
			return err
		}
	}
	return nil
}

// maybeSetOpcodeTrace turns INSTRUMENTED_INSTRUCTION on for ts's
// current frame when that frame requested opcode tracing.
//
// CPython: Python/legacy_tracing.c:683 maybe_set_opcode_trace
func maybeSetOpcodeTrace(ts *state.Thread) error {
	f := frameStackFor(ts).Top()
	if f == nil || !f.TraceOpcodes {
		return nil
	}
	return setOpcodeTrace(f, true)
}

// setOpcodeTraceHook backs objects.SetOpcodeTraceHook. It is invoked
// when Python code assigns frame.f_trace_opcodes (bdb / pdb does this
// on the frame it is about to single-step). It toggles the local
// INSTRUCTION event on the frame's code and re-instruments the live
// call chain so the marker takes effect on the current instruction
// rather than only after the next RESUME the frame has already passed.
//
// CPython: Python/legacy_tracing.c:159 _PyEval_SetOpcodeTrace
func setOpcodeTraceHook(w *objects.Frame, enable bool) error {
	if w == nil {
		return nil
	}
	ip := w.Interp()
	f, ok := ip.(*frame.Frame)
	if !ok || f == nil {
		return nil
	}
	// Mirror the wrapper's f_trace_opcodes onto the iframe so the line
	// trampoline (callTraceFunc) re-arms opcode tracing each time it
	// dispatches an event for this frame.
	f.TraceOpcodes = enable
	// CPython only installs the instrumentation when enabling AND a
	// trace function is already set on the frame; otherwise the next
	// dispatched event re-arms it via callTraceFunc. Disabling always
	// tears the instrumentation down.
	//
	// CPython: Objects/frameobject.c:1148 frame_trace_opcodes_set_impl
	if enable && w.Trace() == objects.None() {
		return nil
	}
	if err := setOpcodeTrace(f, enable); err != nil {
		return err
	}
	ts := currentThread()
	if ts == nil {
		return nil
	}
	interp := ts.Interp().Monitors
	if interp == nil {
		return nil
	}
	// SetLocalEvents only bumped the version; the executing frame is
	// already past its RESUME, so walk the live stack and instrument
	// each code object now, matching CPython's stop-the-world
	// instrument_all_executing_code_objects.
	//
	// CPython: Python/instrumentation.c:1941 instrument_all_executing_code_objects
	return instrumentExecutingFrames(ts, interp)
}
