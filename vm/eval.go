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

	// code caches f.Code.Code so fetch / advance skip two pointer
	// chases (e.f -> *Code -> .Code slice header) on every dispatch.
	// Stable for the lifetime of this evalState because each Eval
	// call creates a fresh evalState bound to one frame, and Frame.Init
	// is the only path that swaps Code.
	//
	// CPython: Python/ceval.c next_instr local in _PyEval_EvalFrameDefault
	code []byte
}

// Eval runs f to completion under ts and returns the value the frame
// produced (RETURN_VALUE) or the error that escaped.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault
func Eval(ts *state.Thread, f *frame.Frame) (objects.Object, error) {
	prev, g := setActiveThread(ts)
	defer restoreActiveThread(prev, g)
	v := vmFor(ts)
	e := &evalState{ts: ts, f: f, breaker: v.breaker, gilTimer: &v.gilTimer, gil: v.gil, code: f.Code.Code}
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

// run is the dispatch loop driver. The eval-breaker poll lives only
// on the RESUME and JUMP_BACKWARD arms, matching CPython's policy
// (Python/bytecodes.c CHECK_EVAL_BREAKER fires at backward branches
// and at frame resume, not every instruction). Pushing the poll out
// of the per-iteration path saved ~5% on the dispatch micro-bench
// without changing observable behavior, since pending signals and
// GIL drops still reach the next loop iteration that hits a backward
// branch or a yield/resume boundary.
//
// CPython: Python/ceval.c _PyEval_EvalFrameDefault main loop
func (e *evalState) run() (objects.Object, error) {
	for {
		op, oparg, ok := e.fetch()
		if !ok {
			name := "<unknown>"
			if e.f != nil && e.f.Code != nil {
				name = e.f.Code.Name
			}
			return nil, fmt.Errorf("vm: instruction pointer past end of code in %q (ip=%d, len=%d)", name, e.f.InstrPtr, len(e.f.Code.Code))
		}
		// Hot-opcode fast switch inlined into the loop body to skip
		// the dispatch() method-call frame. recordOpcode runs from
		// dispatch() on the slow path; the fast arms record inline so
		// the stats counter stays in sync. The four arms here are the
		// same ones that ship in vm/dispatch.go (see D5); duplicating
		// them at the loop level lets Go's compiler inline the body
		// straight into the loop and skip the dispatch() call entirely
		// on every LOAD_CONST / LOAD_FAST / STORE_FAST / POP_TOP.
		//
		// CPython: Python/ceval.c TARGET labels reached directly from
		// the computed-goto table, no function-call layer.
		switch op {
		case compile.LOAD_CONST:
			e.recordOpcode(op)
			f := e.f
			co := f.Code
			var obj objects.Object
			if uint(oparg) < uint(len(co.ConstObjs)) {
				obj = co.ConstObjs[oparg]
			}
			if obj == nil {
				obj = e.constAtSlow(co, int(oparg))
			}
			f.LocalsPlus[f.StackBase+f.StackTop] = stackref.FromObject(obj)
			f.StackTop++
			f.PrevInstr = f.InstrPtr
			f.InstrPtr = f.InstrPtr + 2
			continue
		case compile.LOAD_FAST:
			e.recordOpcode(op)
			f := e.f
			r := f.LocalsPlus[oparg].Dup()
			f.LocalsPlus[f.StackBase+f.StackTop] = r
			f.StackTop++
			f.PrevInstr = f.InstrPtr
			f.InstrPtr = f.InstrPtr + 2
			continue
		case compile.STORE_FAST:
			e.recordOpcode(op)
			f := e.f
			f.StackTop--
			i := f.StackBase + f.StackTop
			value := f.LocalsPlus[i]
			f.LocalsPlus[i] = stackref.Null
			old := f.LocalsPlus[oparg]
			f.LocalsPlus[oparg] = value
			old.Close()
			f.PrevInstr = f.InstrPtr
			f.InstrPtr = f.InstrPtr + 2
			continue
		case compile.POP_TOP:
			e.recordOpcode(op)
			f := e.f
			f.StackTop--
			i := f.StackBase + f.StackTop
			r := f.LocalsPlus[i]
			f.LocalsPlus[i] = stackref.Null
			r.Close()
			f.PrevInstr = f.InstrPtr
			f.InstrPtr = f.InstrPtr + 2
			continue
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
// The common path (no EXTENDED_ARG prefix) is straight-line so the
// inliner can fold this into run(). The rare prefix path branches
// off to fetchExtended.
//
// CPython: Python/ceval_macros.h NEXTOPARG
func (e *evalState) fetch() (op compile.Opcode, oparg uint32, ok bool) {
	code := e.code
	pc := e.f.InstrPtr
	if pc+1 >= len(code) {
		return 0, 0, false
	}
	raw := compile.Opcode(code[pc])
	arg := uint32(code[pc+1])
	if raw == compile.EXTENDED_ARG {
		return e.fetchExtended(pc, arg)
	}
	return raw, arg, true
}

// fetchExtended handles the rare EXTENDED_ARG prefix run. Each
// EXTENDED_ARG shifts the accumulator left 8 and ORs in the next
// arg byte; the trailing real opcode picks up the accumulated arg.
// f.InstrPtr is left pointing at the real opcode, matching CPython's
// next_instr semantics.
//
// CPython: Python/ceval.c TARGET(EXTENDED_ARG)
func (e *evalState) fetchExtended(pc int, oparg uint32) (op compile.Opcode, arg uint32, ok bool) {
	code := e.code
	pc += 2
	for {
		if pc+1 >= len(code) {
			return 0, 0, false
		}
		raw := compile.Opcode(code[pc])
		b := uint32(code[pc+1])
		oparg = (oparg << 8) | b
		if raw != compile.EXTENDED_ARG {
			e.f.InstrPtr = pc
			return raw, oparg, true
		}
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
	code := e.code
	if ip < 0 || ip >= len(code) {
		return ip + 2
	}
	op := compile.Opcode(code[ip])
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
// CPython stores co_consts as a tuple of PyObject* directly, so GETITEM
// is one pointer load. gopy mirrors that shape via Code.ConstObjs, a
// parallel slice populated by SyncConstObjs at construction time. The
// lazy-fill fallback covers test fixtures that build Code by struct
// literal without calling SyncConstObjs, so the next dispatch on the
// same index returns the cached object.
//
// CPython: Python/ceval_macros.h GETITEM
func (e *evalState) constAt(i int) objects.Object {
	co := e.f.Code
	if uint(i) < uint(len(co.ConstObjs)) {
		if obj := co.ConstObjs[i]; obj != nil {
			return obj
		}
	}
	return e.constAtSlow(co, i)
}

// constAtSlow handles the lazy-fill path for test fixtures that build
// Code by struct literal without calling SyncConstObjs. Wraps the raw
// const and caches it in ConstObjs so the next dispatch hits the fast
// path in constAt.
func (e *evalState) constAtSlow(co *objects.Code, i int) objects.Object {
	obj, err := wrapConst(co.Consts[i])
	if err != nil {
		panic(fmt.Sprintf("vm: bad const at %d: %v", i, err))
	}
	if len(co.ConstObjs) < len(co.Consts) {
		filled := make([]objects.Object, len(co.Consts))
		copy(filled, co.ConstObjs)
		co.ConstObjs = filled
	}
	co.ConstObjs[i] = obj
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
