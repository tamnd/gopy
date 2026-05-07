// Per-opcode tables read by the shadow walk. CPython keeps three:
// EVENT_FOR_OPCODE maps a base opcode to the event it raises,
// INSTRUMENTED_OPCODES maps a base opcode to its INSTRUMENTED_<X>
// variant, and DE_INSTRUMENT maps INSTRUMENTED_<X> back to <X>.
//
// CPython: Python/instrumentation.c:74 EVENT_FOR_OPCODE
// CPython: Python/instrumentation.c:113 DE_INSTRUMENT
// CPython: Python/instrumentation.c:135 INSTRUMENTED_OPCODES

package monitor

import "github.com/tamnd/gopy/compile"

// eventForOpcode is keyed by base opcode and returns the event ID
// the opcode fires, or -1 for "no event".
//
// CPython: Python/instrumentation.c:74 EVENT_FOR_OPCODE
var eventForOpcode = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	t[compile.RETURN_VALUE] = int8(EventPyReturn)
	t[compile.INSTRUMENTED_RETURN_VALUE] = int8(EventPyReturn)
	t[compile.CALL] = int8(EventCall)
	t[compile.INSTRUMENTED_CALL] = int8(EventCall)
	t[compile.CALL_KW] = int8(EventCall)
	t[compile.INSTRUMENTED_CALL_KW] = int8(EventCall)
	t[compile.CALL_FUNCTION_EX] = int8(EventCall)
	t[compile.INSTRUMENTED_CALL_FUNCTION_EX] = int8(EventCall)
	t[compile.LOAD_SUPER_ATTR] = int8(EventCall)
	t[compile.INSTRUMENTED_LOAD_SUPER_ATTR] = int8(EventCall)
	t[compile.YIELD_VALUE] = int8(EventPyYield)
	t[compile.INSTRUMENTED_YIELD_VALUE] = int8(EventPyYield)
	t[compile.JUMP_FORWARD] = int8(EventJump)
	t[compile.JUMP_BACKWARD] = int8(EventJump)
	t[compile.INSTRUMENTED_JUMP_FORWARD] = int8(EventJump)
	t[compile.INSTRUMENTED_JUMP_BACKWARD] = int8(EventJump)
	t[compile.POP_JUMP_IF_FALSE] = int8(EventBranchRight)
	t[compile.POP_JUMP_IF_TRUE] = int8(EventBranchRight)
	t[compile.POP_JUMP_IF_NONE] = int8(EventBranchRight)
	t[compile.POP_JUMP_IF_NOT_NONE] = int8(EventBranchRight)
	t[compile.INSTRUMENTED_POP_JUMP_IF_FALSE] = int8(EventBranchRight)
	t[compile.INSTRUMENTED_POP_JUMP_IF_TRUE] = int8(EventBranchRight)
	t[compile.INSTRUMENTED_POP_JUMP_IF_NONE] = int8(EventBranchRight)
	t[compile.INSTRUMENTED_POP_JUMP_IF_NOT_NONE] = int8(EventBranchRight)
	t[compile.FOR_ITER] = int8(EventBranchLeft)
	t[compile.INSTRUMENTED_FOR_ITER] = int8(EventBranchLeft)
	t[compile.POP_ITER] = int8(EventBranchRight)
	t[compile.INSTRUMENTED_POP_ITER] = int8(EventBranchRight)
	t[compile.END_FOR] = int8(EventStopIteration)
	t[compile.INSTRUMENTED_END_FOR] = int8(EventStopIteration)
	t[compile.END_SEND] = int8(EventStopIteration)
	t[compile.INSTRUMENTED_END_SEND] = int8(EventStopIteration)
	t[compile.NOT_TAKEN] = int8(EventBranchLeft)
	t[compile.INSTRUMENTED_NOT_TAKEN] = int8(EventBranchLeft)
	t[compile.END_ASYNC_FOR] = int8(EventBranchRight)
	return t
}()

// EventForOpcode returns the event ID op fires, or -1 for none.
//
// CPython: Python/instrumentation.c:74 EVENT_FOR_OPCODE
func EventForOpcode(op compile.Opcode) Event {
	if int(op) >= len(eventForOpcode) {
		return Event(0xff)
	}
	ev := eventForOpcode[op]
	if ev < 0 {
		return Event(0xff)
	}
	return Event(ev)
}

// OpcodeHasEvent reports whether op fires a monitoring event. Mirrors
// the predicate the instrument pass uses to skip non-event opcodes.
//
// CPython: Python/instrumentation.c:180 opcode_has_event
func OpcodeHasEvent(op compile.Opcode) bool {
	if op == compile.INSTRUMENTED_LINE {
		return false
	}
	if int(op) >= len(instrumentedOpcodes) {
		return false
	}
	return instrumentedOpcodes[op] != 0
}

// instrumentedOpcodes maps base or already-instrumented opcodes to
// their INSTRUMENTED_<X> form. A zero entry means "no instrumented
// counterpart".
//
// CPython: Python/instrumentation.c:135 INSTRUMENTED_OPCODES
var instrumentedOpcodes = func() [256]compile.Opcode {
	var t [256]compile.Opcode
	pairs := [...][2]compile.Opcode{
		{compile.RETURN_VALUE, compile.INSTRUMENTED_RETURN_VALUE},
		{compile.CALL, compile.INSTRUMENTED_CALL},
		{compile.CALL_KW, compile.INSTRUMENTED_CALL_KW},
		{compile.CALL_FUNCTION_EX, compile.INSTRUMENTED_CALL_FUNCTION_EX},
		{compile.YIELD_VALUE, compile.INSTRUMENTED_YIELD_VALUE},
		{compile.RESUME, compile.INSTRUMENTED_RESUME},
		{compile.JUMP_FORWARD, compile.INSTRUMENTED_JUMP_FORWARD},
		{compile.JUMP_BACKWARD, compile.INSTRUMENTED_JUMP_BACKWARD},
		{compile.POP_JUMP_IF_FALSE, compile.INSTRUMENTED_POP_JUMP_IF_FALSE},
		{compile.POP_JUMP_IF_TRUE, compile.INSTRUMENTED_POP_JUMP_IF_TRUE},
		{compile.POP_JUMP_IF_NONE, compile.INSTRUMENTED_POP_JUMP_IF_NONE},
		{compile.POP_JUMP_IF_NOT_NONE, compile.INSTRUMENTED_POP_JUMP_IF_NOT_NONE},
		{compile.END_FOR, compile.INSTRUMENTED_END_FOR},
		{compile.END_SEND, compile.INSTRUMENTED_END_SEND},
		{compile.FOR_ITER, compile.INSTRUMENTED_FOR_ITER},
		{compile.POP_ITER, compile.INSTRUMENTED_POP_ITER},
		{compile.LOAD_SUPER_ATTR, compile.INSTRUMENTED_LOAD_SUPER_ATTR},
		{compile.NOT_TAKEN, compile.INSTRUMENTED_NOT_TAKEN},
		{compile.END_ASYNC_FOR, compile.INSTRUMENTED_END_ASYNC_FOR},
	}
	for _, p := range pairs {
		t[p[0]] = p[1]
		t[p[1]] = p[1]
	}
	t[compile.INSTRUMENTED_LINE] = compile.INSTRUMENTED_LINE
	t[compile.INSTRUMENTED_INSTRUCTION] = compile.INSTRUMENTED_INSTRUCTION
	return t
}()

// InstrumentedFor returns the INSTRUMENTED_<X> variant of op, or zero
// if none exists.
//
// CPython: Python/instrumentation.c:135 INSTRUMENTED_OPCODES
func InstrumentedFor(op compile.Opcode) compile.Opcode {
	if int(op) >= len(instrumentedOpcodes) {
		return 0
	}
	return instrumentedOpcodes[op]
}

// deinstrument maps INSTRUMENTED_<X> back to <X>. Zero entries mean
// "not an instrumented opcode"; the caller leaves the opcode alone.
//
// CPython: Python/instrumentation.c:113 DE_INSTRUMENT
var deinstrument = func() [256]compile.Opcode {
	var t [256]compile.Opcode
	t[compile.INSTRUMENTED_RESUME] = compile.RESUME
	t[compile.INSTRUMENTED_RETURN_VALUE] = compile.RETURN_VALUE
	t[compile.INSTRUMENTED_CALL] = compile.CALL
	t[compile.INSTRUMENTED_CALL_KW] = compile.CALL_KW
	t[compile.INSTRUMENTED_CALL_FUNCTION_EX] = compile.CALL_FUNCTION_EX
	t[compile.INSTRUMENTED_YIELD_VALUE] = compile.YIELD_VALUE
	t[compile.INSTRUMENTED_JUMP_FORWARD] = compile.JUMP_FORWARD
	t[compile.INSTRUMENTED_JUMP_BACKWARD] = compile.JUMP_BACKWARD
	t[compile.INSTRUMENTED_POP_JUMP_IF_FALSE] = compile.POP_JUMP_IF_FALSE
	t[compile.INSTRUMENTED_POP_JUMP_IF_TRUE] = compile.POP_JUMP_IF_TRUE
	t[compile.INSTRUMENTED_POP_JUMP_IF_NONE] = compile.POP_JUMP_IF_NONE
	t[compile.INSTRUMENTED_POP_JUMP_IF_NOT_NONE] = compile.POP_JUMP_IF_NOT_NONE
	t[compile.INSTRUMENTED_FOR_ITER] = compile.FOR_ITER
	t[compile.INSTRUMENTED_POP_ITER] = compile.POP_ITER
	t[compile.INSTRUMENTED_END_FOR] = compile.END_FOR
	t[compile.INSTRUMENTED_END_SEND] = compile.END_SEND
	t[compile.INSTRUMENTED_LOAD_SUPER_ATTR] = compile.LOAD_SUPER_ATTR
	t[compile.INSTRUMENTED_NOT_TAKEN] = compile.NOT_TAKEN
	t[compile.INSTRUMENTED_END_ASYNC_FOR] = compile.END_ASYNC_FOR
	return t
}()

// DeInstrument maps INSTRUMENTED_<X> back to <X>, or returns op
// unchanged if it has no instrumented form.
//
// CPython: Python/instrumentation.c:113 DE_INSTRUMENT
func DeInstrument(op compile.Opcode) compile.Opcode {
	if int(op) >= len(deinstrument) {
		return op
	}
	if base := deinstrument[op]; base != 0 {
		return base
	}
	return op
}

// IsInstrumented reports whether op is one of the INSTRUMENTED_<X>
// variants. Used by the shadow walk to decide whether to write a
// fresh INSTRUMENTED_<X> opcode or leave the slot alone.
//
// CPython: Python/instrumentation.c:184 is_instrumented
func IsInstrumented(op compile.Opcode) bool {
	if int(op) >= len(deinstrument) {
		return false
	}
	return deinstrument[op] != 0 || op == compile.INSTRUMENTED_LINE || op == compile.INSTRUMENTED_INSTRUCTION
}
