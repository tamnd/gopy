// Exception-event dispatch for PEP 669 monitoring. Ports the static
// helpers from Python/ceval.c that the INSTRUMENTED_* arms call when
// they want to fire RAISE / RERAISE / STOP_ITERATION to subscribed
// tools.

package vm

import (
	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
)

// noToolsForGlobalEvent reports whether any tool is subscribed to ev
// at the interpreter level.
//
// CPython: Python/ceval.c:2511 no_tools_for_global_event
func (e *evalState) noToolsForGlobalEvent(ev monitor.Event) bool {
	interp := e.ts.Interp().Monitors
	if interp == nil {
		return true
	}
	if int(ev) >= len(interp.Monitors.Tools) {
		return true
	}
	return interp.Monitors.Tools[ev] == 0
}

// noToolsForLocalEvent reports whether any tool is subscribed to ev on
// the running frame's code (falling back to the global table when the
// code has no instrumentation data).
//
// CPython: Python/ceval.c:2517 no_tools_for_local_event
func (e *evalState) noToolsForLocalEvent(ev monitor.Event) bool {
	if int(ev) >= monitor.LocalEvents {
		return e.noToolsForGlobalEvent(ev)
	}
	data := monitor.CoMonitoring(e.f.Code)
	if data != nil {
		return data.ActiveMonitors.Tools[ev] == 0
	}
	return e.noToolsForGlobalEvent(ev)
}

// doMonitorExc fires ev for the running frame, handing the callback
// the currently-raised exception. On success the exception slot is
// restored. On callback failure the original exception is dropped and
// the callback's exception remains current. ev must be one of RAISE,
// RERAISE, or STOP_ITERATION.
//
// CPython: Python/ceval.c:2490 do_monitor_exc
func (e *evalState) doMonitorExc(ev monitor.Event) error {
	if int(ev) >= monitor.UngroupedEvents {
		return nil
	}
	if uint32(e.f.Code.Flags)&compile.CoNoMonitoringEvents != 0 {
		return nil
	}
	exc := pyerrors.Occurred(e.ts)
	if exc == nil {
		return nil
	}
	// Match CPython's _Py_call_instrumentation_arg: the exception slot
	// must be clear while the tool callback runs so the callback can
	// raise its own.
	pyerrors.Clear(e.ts)
	interp := e.ts.Interp().Monitors
	state := monitor.MonState{}
	if interp != nil && int(ev) < len(interp.Monitors.Tools) {
		state.Active = interp.Monitors.Tools[ev]
	}
	data := monitor.CoMonitoring(e.f.Code)
	if data != nil && int(ev) < monitor.LocalEvents {
		state.Active = data.ActiveMonitors.Tools[ev]
	}
	offset := int32(e.f.InstrPtr / 2)
	excObj := excObject(exc)
	var err error
	switch ev {
	case monitor.EventRaise:
		err = monitor.FireRaise(interp, &state, e.f.Code, offset, excObj)
	case monitor.EventReraise:
		err = monitor.FireReraise(interp, &state, e.f.Code, offset, excObj)
	case monitor.EventStopIteration:
		err = monitor.FireStopIteration(interp, &state, e.f.Code, offset, excObj)
	}
	if err == nil {
		pyerrors.Raise(e.ts, exc)
	}
	return err
}

// monitorRaise fires RAISE for the current exception. Mirrors the
// public _PyEval_MonitorRaise entry point.
//
// CPython: Python/ceval.c:2531 _PyEval_MonitorRaise
func (e *evalState) monitorRaise() error {
	if e.noToolsForGlobalEvent(monitor.EventRaise) {
		return nil
	}
	return e.doMonitorExc(monitor.EventRaise)
}

// monitorReraise fires RERAISE for the current exception.
//
// CPython: Python/ceval.c:2540 monitor_reraise
func (e *evalState) monitorReraise() error {
	if e.noToolsForGlobalEvent(monitor.EventReraise) {
		return nil
	}
	return e.doMonitorExc(monitor.EventReraise)
}

// monitorStopIteration fires STOP_ITERATION with v as the iterator
// value. Builds the synthetic StopIteration(v) the tool sees as the
// raised exception, then clears it on the way out. Returns nil if no
// tool is subscribed at the frame's code or if the firing succeeded;
// returns the tool's error otherwise.
//
// CPython: Python/ceval.c:2550 monitor_stop_iteration
//
// Returns 0 on success and -1 if a callback raised; the raised
// exception is stashed on pendingErr so the surrounding ERROR_NO_POP /
// ERROR_IF dispatches into the per-instruction error path.
func (e *evalState) monitorStopIteration(v objects.Object) int32 {
	if e.noToolsForLocalEvent(monitor.EventStopIteration) {
		return 0
	}
	args := objects.NewTuple([]objects.Object{v})
	exc := pyerrors.New(pyerrors.PyExc_StopIteration, args)
	pyerrors.Raise(e.ts, exc)
	if err := e.doMonitorExc(monitor.EventStopIteration); err != nil {
		e.pendingErr = err
		return -1
	}
	pyerrors.Clear(e.ts)
	return 0
}

// excObject narrows a *pyerrors.Exception to objects.Object so it can
// ride the fireEvent arg slot. CPython hands the callback the raw
// PyBaseExceptionObject; the gopy mirror is *pyerrors.Exception.
func excObject(exc *pyerrors.Exception) objects.Object {
	if exc == nil {
		return objects.None()
	}
	return exc
}
