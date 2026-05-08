// Package vm is the Tier-1 bytecode interpreter. Eval drives one
// frame to completion: read an instruction, dispatch to the matching
// arm, walk the exception table on error, poll the breaker at each
// RESUME and backward jump.
//
// The per-opcode arms live in opcodes_gen.go (1621). This file owns
// the loop driver, evalState, and the entry points.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault

package vm

import (
	"errors"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/gil"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// ErrNotImplemented is returned by dispatch arms that have not yet
// been generated. The eval loop surfaces it as a runtime error so
// tests can pin the surface without crashing.
var ErrNotImplemented = errors.New("vm: opcode not implemented in v0.6")

// evalState is the per-call state the dispatch arms read and write.
// One per Eval invocation; not safe for concurrent use across
// goroutines.
//
// CPython: Python/ceval.c locals at the top of _PyEval_EvalFrameDefault
type evalState struct {
	ts       *state.Thread
	f        *frame.Frame
	breaker  *gil.Breaker    // shadow of ts's breaker, hot-path read
	gilTimer *gilSwitchTimer // shadow of ts's switch-interval timer
	gil      *gil.GIL        // attached interpreter GIL; nil in v0.9

	// genYield/genSend are non-nil when this evalState is running the body
	// of a generator object. YIELD_VALUE sends on genYield and blocks on
	// genSend; RETURN_GENERATOR wires them in when spawning the goroutine.
	//
	// CPython: (no direct equivalent — CPython uses tstate->current_frame
	// and the generator object's gi_frame_state)
	genYield chan<- objects.GenMsg
	genSend  <-chan objects.GenMsg
}

// Eval runs f to completion under ts and returns the value the frame
// produced (RETURN_VALUE) or the error that escaped.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault
func Eval(ts *state.Thread, f *frame.Frame) (objects.Object, error) {
	prev := setActiveThread(ts)
	defer restoreActiveThread(prev)
	v := vmFor(ts)
	e := &evalState{ts: ts, f: f, breaker: v.breaker, gilTimer: &v.gilTimer, gil: v.gil}
	return e.run()
}

// EvalCode is the convenience wrapper that builds a frame from a
// code object plus globals/locals and calls Eval.
//
// CPython: Python/ceval.c PyEval_EvalCode
func EvalCode(ts *state.Thread, co *objects.Code, globals, locals objects.Object) (objects.Object, error) {
	stack := frameStackFor(ts)
	f := stack.Push(co, globals, builtinsFromGlobals(globals), nil, nil)
	if locals != nil {
		f.Locals = locals
	}
	defer stack.Pop()
	return Eval(ts, f)
}

// builtinsFromGlobals extracts globals['__builtins__'] for use as the
// frame builtins. Mirrors _PyEval_BuildFrame, which reads the same
// slot off the function's globals before calling _PyFrame_Initialize.
// __builtins__ is conventionally a module on __main__ and a dict on
// imported modules; LOAD_GLOBAL needs the dict either way, so unwrap
// the module if it shows up.
//
// CPython: Python/ceval.c:1849 _PyEval_BuildFrame frame->f_builtins setup
func builtinsFromGlobals(globals objects.Object) objects.Object {
	d, ok := globals.(*objects.Dict)
	if !ok {
		return nil
	}
	v, err := d.GetItem(objects.NewStr("__builtins__"))
	if err != nil || v == nil {
		return nil
	}
	if m, ok := v.(*objects.Module); ok {
		return m.Dict()
	}
	return v
}

// run is the dispatch loop driver.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault main loop
func (e *evalState) run() (objects.Object, error) {
	for {
		if e.gilTimer != nil {
			e.gilTimer.poll(e.gil, e.breaker)
		}
		if e.breaker != nil && e.breaker.Load() != 0 {
			if err := e.handleEvalBreaker(); err != nil {
				return e.unwind(err)
			}
		}
		op, oparg, ok := e.fetch()
		if !ok {
			return nil, errors.New("vm: instruction pointer past end of code")
		}
		next, retVal, retErr, retDone, err := e.dispatch(op, oparg)
		if retDone {
			return retVal, retErr
		}
		if err != nil {
			v, handled := e.handleException(err)
			if handled {
				continue
			}
			return v, err
		}
		e.f.PrevInstr = e.f.InstrPtr
		e.f.InstrPtr = next
	}
}

// fetch decodes the instruction at f.InstrPtr. The bytecode is a
// flat byte slice with two bytes per instruction: opcode then oparg.
// EXTENDED_ARG accumulates into the next instruction's oparg.
//
// CPython: Python/ceval_macros.h NEXTOPARG
func (e *evalState) fetch() (op compile.Opcode, oparg uint32, ok bool) {
	co := e.f.Code
	pc := e.f.InstrPtr
	for {
		if pc+1 >= len(co.Code) {
			return 0, 0, false
		}
		raw := compile.Opcode(co.Code[pc])
		arg := uint32(co.Code[pc+1])
		if raw != compile.EXTENDED_ARG {
			return raw, oparg<<8 | arg, true
		}
		oparg = (oparg | arg) << 8
		pc += 2
	}
}

// advance returns the instruction offset n instructions ahead of
// f.InstrPtr. The eval loop uses this to pin the next-pc value the
// generated arms return.
// advance returns the next pc, one instruction word past the current
// instr ptr. Cache-word advances will need a parameterised variant
// once instructions with inline caches dispatch through this path.
func (e *evalState) advance() int { return e.f.InstrPtr + 2 }

// jumpBy returns the instruction offset delta instructions away.
// Forward jumps pass a positive delta; backward jumps pass negative.
//
// CPython: Python/ceval_macros.h JUMPBY
func (e *evalState) jumpBy(delta int) int { return e.f.InstrPtr + 2*delta }

// push pushes r onto the value stack.
func (e *evalState) push(r stackref.Ref) { e.f.PushStack(r) }

// pop pops the top of the value stack.
func (e *evalState) pop() stackref.Ref { return e.f.PopStack() }

// peek returns the value at depth from the top (0 = top).
func (e *evalState) peek(depth int) stackref.Ref { return e.f.PeekStack(depth) }

// pushObject is a shortcut for push(stackref.FromObject(o)).
func (e *evalState) pushObject(o objects.Object) {
	e.push(stackref.FromObject(o))
}

// popObject is a shortcut for pop().AsObjectSteal().
func (e *evalState) popObject() objects.Object {
	return e.pop().AsObjectSteal()
}

// localAt returns fast-local i. Out-of-range indexes panic since the
// compiler is supposed to validate them.
//
// CPython: Python/ceval_macros.h GETLOCAL
func (e *evalState) localAt(i int) stackref.Ref { return e.f.LocalAt(i) }

// setLocal stores r in fast-local slot i.
//
// CPython: Python/ceval_macros.h SETLOCAL
func (e *evalState) setLocal(i int, r stackref.Ref) { e.f.SetLocal(i, r) }
