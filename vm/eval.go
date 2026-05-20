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
	"fmt"

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

	// pendingErr is set by translator-emitted helper wrappers (the ones
	// modeled on CPython helpers that return NULL + set the thread
	// exception state). The matching ERROR_IF in the body checks for
	// the NULL/zero sentinel and bubbles this error up.
	//
	// CPython: tstate->current_exception, set by helpers that signal
	// failure via NULL return.
	pendingErr error

	// lastOpcode mirrors CPython's `int lastopcode` local in
	// _PyEval_EvalFrameDefault. Used only when GOPY_STATS is on; the
	// pair_count[last][cur] update reads this every dispatch.
	//
	// CPython: Python/ceval.c:1156 lastopcode
	lastOpcode compile.Opcode
}

// Eval runs f to completion under ts and returns the value the frame
// produced (RETURN_VALUE) or the error that escaped.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault
func Eval(ts *state.Thread, f *frame.Frame) (objects.Object, error) {
	prev, g := setActiveThread(ts)
	defer restoreActiveThread(prev, g)
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
	} else {
		// CPython: Python/frame.c _PyFrame_Initialize sets f_locals
		// from the caller; for module-level execution PyEval_EvalCode
		// passes locals == globals. Mirror that here so LOCALS()-using
		// opcodes (SETUP_ANNOTATIONS, STORE_NAME, ...) see the module
		// dict instead of NULL. Function-call frames are built in
		// vm/eval_call.go and keep Locals nil to drive fast-locals.
		f.Locals = globals
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
		return globals
	}
	v, err := d.GetItem(objects.NewStr("__builtins__"))
	if err != nil || v == nil {
		// CPython makes up a minimal builtins dict here; gopy
		// callers (notably tests) commonly pass the builtins dict
		// itself as globals, so fall back to globals to preserve
		// that pattern. LOAD_NAME / LOAD_GLOBAL / LOAD_BUILD_CLASS
		// all read builtins for fallback, and treating globals as
		// the implicit builtins matches the v0.7 single-dict setup.
		return globals
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
			name := "<unknown>"
			if e.f != nil && e.f.Code != nil {
				name = e.f.Code.Name
			}
			return nil, fmt.Errorf("vm: instruction pointer past end of code in %q (ip=%d, len=%d)", name, e.f.InstrPtr, len(e.f.Code.Code))
		}
		next, retVal, retErr, retDone, err := e.dispatch(op, oparg)
		if retDone {
			return retVal, retErr
		}
		if err != nil {
			if e.handleException(err) {
				continue
			}
			return nil, err
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
			// Point InstrPtr at the actual instruction so advance() and
			// jumpBy() compute correct offsets past any EXTENDED_ARG prefix.
			//
			// CPython: Python/ceval_macros.h NEXTOPARG — next_instr is
			// always left pointing at the real opcode, not the prefix.
			e.f.InstrPtr = pc
			return raw, oparg<<8 | arg, true
		}
		// CPython: each EXTENDED_ARG shifts the accumulated value left by 8
		// and ORs in the new byte. The old formula (oparg | arg) << 8 was
		// wrong: it shifted one position too many.
		//
		// CPython: Python/ceval.c TARGET(EXTENDED_ARG) oparg <<= 8; oparg |= arg
		oparg = (oparg << 8) | arg
		pc += 2
	}
}

// advance returns the next pc, one instruction word past the current
// instr ptr plus any inline-cache codeunits the current opcode owns
// (_PyOpcode_Caches[op]). The eval loop relies on this so adaptive
// opcodes don't run their cache cells as real instructions.
//
// CPython: Python/ceval_macros.h NEXTOPARG / DISPATCH (the implicit
// `next_instr += INLINE_CACHE_ENTRIES_<OP>` step inside each arm).
func (e *evalState) advance() int {
	ip := e.f.InstrPtr
	if ip < 0 || ip >= len(e.f.Code.Code) {
		return ip + 2
	}
	op := compile.Opcode(e.f.Code.Code[ip])
	return ip + 2 + 2*compile.CacheCount(op)
}

// jumpBy returns the instruction offset delta instructions away.
// Forward jumps pass a positive delta; backward jumps pass negative.
// The delta is in codeunits relative to the *next* instruction (the
// one right after this op's cache cells), matching CPython's JUMPBY.
//
// CPython: Python/ceval_macros.h JUMPBY
func (e *evalState) jumpBy(delta int) int { return e.advance() + 2*(delta-1) }

// push pushes r onto the value stack.
func (e *evalState) push(r stackref.Ref) { e.f.PushStack(r) }

// pop pops the top of the value stack.
func (e *evalState) pop() stackref.Ref { return e.f.PopStack() }

// peek returns the value at depth from the top (0 = top).
func (e *evalState) peek(depth int) stackref.Ref { return e.f.PeekStack(depth) }

// setPeek writes r at depth from the top. Mirrors CPython's POKE,
// used by generated dispatch arms to commit a value back to a
// passthrough slot before STACK_SHRINK runs.
func (e *evalState) setPeek(depth int, r stackref.Ref) { e.f.SetPeekStack(depth, r) }

// drop pops n stack entries without binding them to locals; mirrors
// CPython's STACK_SHRINK(n).
func (e *evalState) drop(n int) { e.f.DropStack(n) }

// pushObject is a shortcut for push(stackref.FromObject(o)).
func (e *evalState) pushObject(o objects.Object) {
	e.push(stackref.FromObject(o))
}

// popObject is a shortcut for pop().AsObjectSteal().
func (e *evalState) popObject() objects.Object {
	return e.pop().AsObjectSteal()
}

// constAt returns co_consts[i] lifted into an Object. Mirrors CPython's
// GETITEM(FRAME_CO_CONSTS, i): the bytecode compiler guarantees i is in
// range and the const wraps cleanly, so a failure here is a compiler bug
// rather than a runtime error.
//
// CPython: Python/ceval_macros.h GETITEM
func (e *evalState) constAt(i int) objects.Object {
	obj, err := wrapConst(e.f.Code.Consts[i])
	if err != nil {
		panic(fmt.Sprintf("vm: bad const at %d: %v", i, err))
	}
	return obj
}

// nameAt returns co_names[i] as a Python str. CPython stores names as
// already-interned PyUnicodes; in gopy the storage is a Go string, so
// we wrap on access.
//
// CPython: Python/ceval_macros.h GETITEM
func (e *evalState) nameAt(i int) objects.Object {
	return objects.NewStr(e.f.Code.Names[i])
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
