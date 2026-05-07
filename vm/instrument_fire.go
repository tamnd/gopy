// Dispatch fan-out for the INSTRUMENTED_<X> opcodes. The shadow walk
// rewrites bytecode in place; the dispatch loop sees the
// INSTRUMENTED_<X> form and asks this helper to fire the registered
// callbacks before peeling the prefix and running the underlying body.
//
// CPython: Python/instrumentation.c TARGET(INSTRUMENTED_*)

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
)

// fireInstrumented dispatches the registered callbacks for the
// INSTRUMENTED_<X> opcode at the current instruction pointer. Returns
// nil if op is not instrumented or no tool is subscribed at this
// offset; returns an error if any callback raised.
//
// CPython: Python/instrumentation.c:2581 capi_call_instrumentation
func (e *evalState) fireInstrumented(op compile.Opcode, oparg uint32) error {
	if !monitor.IsInstrumented(op) {
		return nil
	}
	co := e.f.Code
	data := monitor.CoMonitoring(co)
	if data == nil || data.Tools == nil {
		return nil
	}
	instr := e.f.InstrPtr / 2
	if instr >= len(data.Tools) {
		return nil
	}
	tools := data.Tools[instr]
	if tools == 0 {
		return nil
	}
	interp := e.ts.Interp().Monitors
	if interp == nil {
		return nil
	}
	base := monitor.DeInstrument(op)
	ev := monitor.EventForOpcode(base)
	if int(ev) >= monitor.Events {
		return nil
	}
	state := monitor.MonState{Active: tools}
	offset := int32(instr)
	if err := fireForEvent(interp, &state, ev, co, offset, base, oparg, e); err != nil {
		return err
	}
	// fireForEvent may have cleared bits via Disable; sync them back
	// to the per-codeunit table so subsequent dispatches do not call
	// the cleared tools again.
	data.Tools[instr] = state.Active
	return nil
}

// fireForEvent dispatches to the matching FireXxx entry point in the
// monitor package. Currently covers the events whose argument
// signature the eval loop can already supply; the rest land with
// later ports of the underlying opcode bodies.
func fireForEvent(
	interp *monitor.InterpState,
	state *monitor.MonState,
	ev monitor.Event,
	co *objects.Code,
	offset int32,
	base compile.Opcode,
	oparg uint32,
	e *evalState,
) error {
	switch ev {
	case monitor.EventPyReturn:
		retval := objects.Object(objects.None())
		if e.f.StackTop > 0 {
			retval = e.peek(0).AsObject()
		}
		return monitor.FirePyReturn(interp, state, co, offset, retval)
	case monitor.EventPyYield:
		retval := objects.Object(objects.None())
		if e.f.StackTop > 0 {
			retval = e.peek(0).AsObject()
		}
		return monitor.FirePyYield(interp, state, co, offset, retval)
	case monitor.EventCall:
		callable := objects.Object(objects.None())
		arg0 := objects.Object(objects.None())
		if base == compile.CALL || base == compile.CALL_KW {
			n := int(oparg)
			if e.f.StackTop >= n+2 {
				callable = e.peek(n + 1).AsObject()
				if n > 0 {
					arg0 = e.peek(n).AsObject()
				}
			}
		}
		return monitor.FireCall(interp, state, co, offset, callable, arg0)
	case monitor.EventJump:
		return monitor.FireJump(interp, state, co, offset, objects.NewInt(int64(oparg)))
	case monitor.EventBranchLeft:
		return monitor.FireBranchLeft(interp, state, co, offset, objects.NewInt(int64(oparg)))
	case monitor.EventBranchRight:
		return monitor.FireBranchRight(interp, state, co, offset, objects.NewInt(int64(oparg)))
	case monitor.EventStopIteration:
		return monitor.FireStopIteration(interp, state, co, offset, objects.None())
	}
	return nil
}
