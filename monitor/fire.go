// Fire-event entry points. The eval loop calls these after the
// instrument pass has filled in MonState.Active with the per-event
// tool bitmask. Each entry point fans out across the active tools,
// handing the registered callback the per-event argument signature
// PEP 669 specifies.
//
// CPython: Python/instrumentation.c:2666 _PyMonitoring_FirePyStartEvent

package monitor

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// MonState is the per-event tool bitmask the fire-event entry points
// read on every call. The instrument pass refreshes it via EnterScope
// when the interpreter's global monitoring version drifts.
//
// CPython: Include/internal/pycore_instruments.h _PyMonitoringState
type MonState struct {
	Active uint8
}

// EnterScope refreshes the MonState slots from interp.Monitors when
// the cached version is stale. The eval loop holds the cached version
// per stack frame so a fresh instrument pass propagates without
// touching every active state on every event.
//
// CPython: Python/instrumentation.c:2643 PyMonitoring_EnterScope
func EnterScope(interp *InterpState, states []MonState, version *uint32, eventTypes []Event) {
	if interp.GlobalVersion() == *version {
		return
	}
	for i, ev := range eventTypes {
		if int(ev) < UngroupedEvents {
			states[i].Active = interp.Monitors.Tools[ev]
		} else {
			states[i].Active = 0
		}
	}
	*version = interp.GlobalVersion()
}

// ExitScope is currently a no-op. CPython keeps the symbol for
// symmetry; gopy mirrors the shape so callers do not have to special
// case it.
//
// CPython: Python/instrumentation.c:2660 PyMonitoring_ExitScope
func ExitScope() {}

// callOneInstrument invokes the (tool, event) callback with args.
// Return value: 1 if the tool returned Disable, 0 if it succeeded or
// the slot is empty, and a non-nil error on a runtime failure.
//
// CPython: Python/instrumentation.c:972 call_one_instrument
func callOneInstrument(interp *InterpState, tool Tool, ev Event, args []objects.Object) (int, error) {
	cb := interp.Callback(tool, ev)
	if cb == nil {
		return 0, nil
	}
	res, err := objects.Vectorcall(cb, args, uint(len(args)), nil)
	if err != nil {
		return -1, err
	}
	if res == Disable {
		return 1, nil
	}
	return 0, nil
}

// fireEvent walks the active tool bitmask from highest bit to lowest
// and dispatches to each tool's callback. Disable on an instrumented
// event clears the tool from the per-MonState bitmask; on an
// ungrouped event it clears the slot and reports an error so the
// calling tool sees the misuse.
//
// CPython: Python/instrumentation.c:2581 capi_call_instrumentation
func fireEvent(interp *InterpState, state *MonState, ev Event, args []objects.Object) error {
	tools := state.Active
	for tools != 0 {
		tool := mostSignificantBit(tools)
		tools ^= 1 << tool
		res, err := callOneInstrument(interp, Tool(tool), ev, args)
		if err != nil {
			return err
		}
		if res != 1 {
			continue
		}
		if !IsInstrumentedEvent(ev) {
			interp.Callables[tool][ev] = nil
			return fmt.Errorf("cannot disable %s events; callback removed", eventName(ev))
		}
		state.Active &^= 1 << tool
	}
	return nil
}

// mostSignificantBit returns the index of the highest set bit in
// tools. Mirrors the table-driven helper CPython uses to walk the
// per-event tool bitmask in reverse priority order.
//
// CPython: Python/instrumentation.c:1003 most_significant_bit
func mostSignificantBit(tools uint8) int {
	if tools == 0 {
		return -1
	}
	idx := 0
	if tools >= 16 {
		idx += 4
		tools >>= 4
	}
	for tools > 1 {
		idx++
		tools >>= 1
	}
	return idx
}

// eventName returns a human-readable identifier for ev, used in error
// messages. CPython keeps a parallel array; gopy rebuilds it each
// time because errors are rare.
//
// CPython: Python/instrumentation.c:46 event_names
func eventName(ev Event) string {
	switch ev {
	case EventPyStart:
		return "PY_START"
	case EventPyResume:
		return "PY_RESUME"
	case EventPyReturn:
		return "PY_RETURN"
	case EventPyYield:
		return "PY_YIELD"
	case EventCall:
		return "CALL"
	case EventLine:
		return "LINE"
	case EventInstruction:
		return "INSTRUCTION"
	case EventJump:
		return "JUMP"
	case EventBranchLeft:
		return "BRANCH_LEFT"
	case EventBranchRight:
		return "BRANCH_RIGHT"
	case EventStopIteration:
		return "STOP_ITERATION"
	case EventRaise:
		return "RAISE"
	case EventExceptionHandled:
		return "EXCEPTION_HANDLED"
	case EventPyUnwind:
		return "PY_UNWIND"
	case EventPyThrow:
		return "PY_THROW"
	case EventReraise:
		return "RERAISE"
	case EventCReturn:
		return "C_RETURN"
	case EventCRaise:
		return "C_RAISE"
	case EventBranch:
		return "BRANCH"
	}
	return "UNKNOWN"
}

// offsetObject builds the PyLong CPython hands to the callback as the
// instruction offset.
func offsetObject(offset int32) objects.Object {
	return objects.NewInt(int64(offset))
}

// FirePyStart fires PY_START.
//
// CPython: Python/instrumentation.c:2668 _PyMonitoring_FirePyStartEvent
func FirePyStart(interp *InterpState, state *MonState, codelike objects.Object, offset int32) error {
	args := []objects.Object{codelike, offsetObject(offset)}
	return fireEvent(interp, state, EventPyStart, args)
}

// FirePyResume fires PY_RESUME.
//
// CPython: Python/instrumentation.c:2677 _PyMonitoring_FirePyResumeEvent
func FirePyResume(interp *InterpState, state *MonState, codelike objects.Object, offset int32) error {
	args := []objects.Object{codelike, offsetObject(offset)}
	return fireEvent(interp, state, EventPyResume, args)
}

// FirePyReturn fires PY_RETURN.
//
// CPython: Python/instrumentation.c:2688 _PyMonitoring_FirePyReturnEvent
func FirePyReturn(interp *InterpState, state *MonState, codelike objects.Object, offset int32, retval objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), retval}
	return fireEvent(interp, state, EventPyReturn, args)
}

// FirePyYield fires PY_YIELD.
//
// CPython: Python/instrumentation.c:2698 _PyMonitoring_FirePyYieldEvent
func FirePyYield(interp *InterpState, state *MonState, codelike objects.Object, offset int32, retval objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), retval}
	return fireEvent(interp, state, EventPyYield, args)
}

// FireCall fires CALL.
//
// CPython: Python/instrumentation.c:2708 _PyMonitoring_FireCallEvent
func FireCall(interp *InterpState, state *MonState, codelike objects.Object, offset int32, callable, arg0 objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), callable, arg0}
	return fireEvent(interp, state, EventCall, args)
}

// FireLine fires LINE. Unlike the other entry points the offset is
// replaced by the line number on the way out.
//
// CPython: Python/instrumentation.c:2718 _PyMonitoring_FireLineEvent
func FireLine(interp *InterpState, state *MonState, codelike objects.Object, offset int32, lineno int) error {
	args := []objects.Object{codelike, objects.NewInt(int64(lineno))}
	return fireEvent(interp, state, EventLine, args)
}

// FireJump fires JUMP.
//
// CPython: Python/instrumentation.c:2734 _PyMonitoring_FireJumpEvent
func FireJump(interp *InterpState, state *MonState, codelike objects.Object, offset int32, target objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), target}
	return fireEvent(interp, state, EventJump, args)
}

// FireBranchRight fires BRANCH_RIGHT.
//
// CPython: Python/instrumentation.c:2754 _PyMonitoring_FireBranchRightEvent
func FireBranchRight(interp *InterpState, state *MonState, codelike objects.Object, offset int32, target objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), target}
	return fireEvent(interp, state, EventBranchRight, args)
}

// FireBranchLeft fires BRANCH_LEFT.
//
// CPython: Python/instrumentation.c:2764 _PyMonitoring_FireBranchLeftEvent
func FireBranchLeft(interp *InterpState, state *MonState, codelike objects.Object, offset int32, target objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), target}
	return fireEvent(interp, state, EventBranchLeft, args)
}

// FireCReturn fires C_RETURN.
//
// CPython: Python/instrumentation.c:2774 _PyMonitoring_FireCReturnEvent
func FireCReturn(interp *InterpState, state *MonState, codelike objects.Object, offset int32, retval objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), retval}
	return fireEvent(interp, state, EventCReturn, args)
}

// FirePyThrow fires PY_THROW with the currently-raised exception.
//
// CPython: Python/instrumentation.c:2809 _PyMonitoring_FirePyThrowEvent
func FirePyThrow(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventPyThrow, args)
}

// FireRaise fires RAISE.
//
// CPython: Python/instrumentation.c:2823 _PyMonitoring_FireRaiseEvent
func FireRaise(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventRaise, args)
}

// FireCRaise fires C_RAISE.
//
// CPython: Python/instrumentation.c:2837 _PyMonitoring_FireCRaiseEvent
func FireCRaise(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventCRaise, args)
}

// FireReraise fires RERAISE.
//
// CPython: Python/instrumentation.c:2851 _PyMonitoring_FireReraiseEvent
func FireReraise(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventReraise, args)
}

// FireExceptionHandled fires EXCEPTION_HANDLED.
//
// CPython: Python/instrumentation.c:2865 _PyMonitoring_FireExceptionHandledEvent
func FireExceptionHandled(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventExceptionHandled, args)
}

// FirePyUnwind fires PY_UNWIND.
//
// CPython: Python/instrumentation.c:2879 _PyMonitoring_FirePyUnwindEvent
func FirePyUnwind(interp *InterpState, state *MonState, codelike objects.Object, offset int32, exc objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), exc}
	return fireEvent(interp, state, EventPyUnwind, args)
}

// FireStopIteration fires STOP_ITERATION. The caller passes the
// StopIteration value (the iterator's return value); CPython sets it
// as the current exception before firing and restores afterwards.
//
// CPython: Python/instrumentation.c:2893 _PyMonitoring_FireStopIterationEvent
func FireStopIteration(interp *InterpState, state *MonState, codelike objects.Object, offset int32, value objects.Object) error {
	args := []objects.Object{codelike, offsetObject(offset), value}
	return fireEvent(interp, state, EventStopIteration, args)
}

// FireInstruction fires INSTRUCTION.
//
// CPython: Python/instrumentation.c instruments PER_INSTRUCTION via
// the same fire path; the standalone entry exists in gopy because the
// dispatch fan-out lives on the Go side.
func FireInstruction(interp *InterpState, state *MonState, codelike objects.Object, offset int32) error {
	args := []objects.Object{codelike, offsetObject(offset)}
	return fireEvent(interp, state, EventInstruction, args)
}
