// Adaptive specialization driver. Bridges the eval loop with the
// per-family specializers in specialize/. Two responsibilities:
//
//  1. When the dispatch loop reads a specialized opcode (LOAD_ATTR_SLOT,
//     CALL_PY_EXACT_ARGS, ...), rewrite it back to the adaptive parent
//     so the generic body runs. gopy does not yet generate fast-path
//     arms for the specialized variants; deopt-on-entry keeps the VM
//     correct while still letting the specializer record its decision.
//
//  2. For each adaptive opcode (LOAD_ATTR, BINARY_OP, ...), tick the
//     16-bit BackoffCounter that lives in the first cache cell. When
//     the counter triggers, peek the argument(s) the specializer
//     consumes off the value stack and call the matching family
//     specializer.
//
// Both paths only run when the executing Code object has been
// Quickened (specialize.Quicken stamped fresh counters into every
// cache cell). Hand-written test bytecode that omits cache padding
// stays on the slow path.
//
// CPython: Python/ceval.c TARGET(<adaptive>) bodies + DEOPT_IF macros.

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// instrIdx returns the codeunit index of the instruction at f.InstrPtr.
func (e *evalState) instrIdx() int { return e.f.InstrPtr / 2 }

// cacheAdvance returns the next-pc one full instruction (opcode plus
// inline cache cells) past the current instr. For non-Quickened code
// the cache count is treated as zero so the existing test bytecodes
// (which do not include cache padding) still advance one codeunit.
//
// CPython: Python/ceval_macros.h DISPATCH advances by 1 + caches.
func (e *evalState) cacheAdvance(op compile.Opcode) int {
	if !e.f.Code.Quickened {
		return e.f.InstrPtr + 2
	}
	return e.f.InstrPtr + 2*(1+specialize.CacheCount(op))
}

// maybeDeopt checks whether op is a specialized variant. If so it
// rewrites the bytecode at f.InstrPtr back to the adaptive parent
// and returns the parent opcode plus true so the caller knows to
// dispatch the generic body. Returns op unchanged otherwise.
//
// CPython: Python/ceval.c DEOPT_IF / specialize_if_unstable.
func (e *evalState) maybeDeopt(op compile.Opcode) (compile.Opcode, bool) {
	if !e.f.Code.Quickened {
		return op, false
	}
	parent := specialize.Deopt(op)
	if parent == op {
		return op, false
	}
	specialize.Unspecialize(e.f.Code.Code, e.instrIdx())
	return parent, true
}

// adaptiveTick handles the counter side of one dispatch tick on an
// adaptive opcode. Returns true when the bytecode was rewritten in
// place (the caller must re-fetch). When the counter has not yet
// triggered the function decrements it and returns false.
//
// CPython: Python/ceval.c <adaptive> bodies (counter check + call
// into _Py_Specialize_*).
func (e *evalState) adaptiveTick(op compile.Opcode, oparg uint32) bool {
	if !e.f.Code.Quickened {
		return false
	}
	if specialize.CacheCount(op) == 0 {
		return false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cnt := specialize.LoadCounter(code, idx)
	if specialize.IsUnreachable(cnt) {
		return false
	}
	if !specialize.BackoffCounterTriggers(cnt) {
		specialize.StoreCounter(code, idx, specialize.AdvanceBackoffCounter(cnt))
		return false
	}
	return e.specializeAt(op, oparg, idx)
}

// specializeAt dispatches to the per-family specializer matching op,
// peeking arguments off the value stack as the matching CPython
// specialize_* helper expects. Returns true when the bytecode was
// rewritten (so dispatch should re-fetch).
//
// CPython: Python/specialize.c _Py_Specialize_<family> entry points.
func (e *evalState) specializeAt(op compile.Opcode, oparg uint32, idx int) bool {
	code := e.f.Code.Code
	co := e.f.Code
	switch op {
	case compile.TO_BOOL:
		v := e.peek(0).AsObject()
		specialize.ToBool(v, code, idx)
		return true
	case compile.CONTAINS_OP:
		container := e.peek(0).AsObject()
		specialize.ContainsOp(container, code, idx)
		return true
	case compile.BINARY_OP:
		lhs := e.peek(1).AsObject()
		rhs := e.peek(0).AsObject()
		nextOp, nextArg := e.peekNextInstr(op)
		specialize.BinaryOp(lhs, rhs, code, idx, int32(oparg), nextOp, int32(nextArg), e.localsAsObjects())
		return true
	case compile.COMPARE_OP:
		lhs := e.peek(1).AsObject()
		rhs := e.peek(0).AsObject()
		specialize.CompareOp(lhs, rhs, code, idx, int32(oparg))
		return true
	case compile.STORE_SUBSCR:
		container := e.peek(1).AsObject()
		sub := e.peek(0).AsObject()
		specialize.StoreSubscr(container, sub, code, idx)
		return true
	case compile.FOR_ITER:
		iter := e.peek(0).AsObject()
		specialize.ForIter(iter, code, idx, int32(oparg))
		return true
	case compile.SEND:
		recv := e.peek(1).AsObject()
		specialize.Send(recv, code, idx)
		return true
	case compile.UNPACK_SEQUENCE:
		seq := e.peek(0).AsObject()
		specialize.UnpackSequence(seq, code, idx, int32(oparg))
		return true
	case compile.LOAD_GLOBAL:
		nameIdx := int(oparg >> 1)
		if nameIdx < 0 || nameIdx >= len(co.Names) {
			return false
		}
		name := co.NameObj(nameIdx)
		specialize.LoadGlobal(e.f.Globals, e.f.Builtins, code, idx, name)
		return true
	case compile.LOAD_ATTR:
		owner := e.peek(0).AsObject()
		nameIdx := int(oparg >> 1)
		if nameIdx < 0 || nameIdx >= len(co.Names) {
			return false
		}
		name := co.NameObj(nameIdx)
		_ = code
		specialize.LoadAttr(owner, name, co, idx)
		return true
	case compile.STORE_ATTR:
		owner := e.peek(0).AsObject()
		nameIdx := int(oparg)
		if nameIdx < 0 || nameIdx >= len(co.Names) {
			return false
		}
		name := co.NameObj(nameIdx)
		specialize.StoreAttr(owner, name, code, idx)
		return true
	case compile.LOAD_SUPER_ATTR:
		// LOAD_SUPER_ATTR sees three values on the stack: super, class, self.
		// CPython packs the load_method flag into bit 0 of oparg.
		globalSuper := e.peek(2).AsObject()
		cls := e.peek(1).AsObject()
		loadMethod := (oparg & 1) != 0
		specialize.LoadSuperAttr(globalSuper, cls, code, idx, loadMethod)
		return true
	case compile.CALL:
		// Stack: [callable, self_or_null, args[oparg]]. CPython bumps
		// nargs by 1 when self_or_null is non-NULL so specialize_py_call
		// sees the effective total_args (matches the LOAD_ATTR_METHOD
		// unbound-method shape where the descr is the callable and self
		// rides in the self_or_null slot).
		//
		// CPython: Python/bytecodes.c:3725 _SPECIALIZE_CALL
		nargs := int32(oparg)
		callable := e.peek(int(nargs) + 1).AsObject()
		if e.peek(int(nargs)).AsObject() != nil {
			nargs++
		}
		specialize.Call(callable, code, idx, int32(oparg), nargs)
		return true
	case compile.CALL_KW:
		// Same self_or_null bump as CALL; the kw tuple sits at PEEK(0)
		// so the callable is one slot deeper.
		//
		// CPython: Python/bytecodes.c:3725 _SPECIALIZE_CALL (CALL_KW)
		nargs := int32(oparg)
		callable := e.peek(int(nargs) + 2).AsObject()
		if e.peek(int(nargs) + 1).AsObject() != nil {
			nargs++
		}
		specialize.CallKw(callable, code, idx, nargs)
		return true
	}
	return false
}

// peekNextInstr returns the (opcode, oparg) pair immediately after
// the current instruction's cache cells. Used by the BINARY_OP
// specializer to look ahead at the STORE_FAST that consumes the
// result. Returns (NOP, 0) when the read would run past the end of
// the bytecode.
func (e *evalState) peekNextInstr(op compile.Opcode) (compile.Opcode, uint32) {
	pc := e.f.InstrPtr + 2*(1+specialize.CacheCount(op))
	code := e.f.Code.Code
	if pc+1 >= len(code) {
		return compile.NOP, 0
	}
	return compile.Opcode(code[pc]), uint32(code[pc+1])
}

// localsAsObjects materializes the fast-local slice as a []Object
// view for specializers that look at the next STORE_FAST target
// (BINARY_OP's INPLACE_ADD_UNICODE arm). The slice mirrors the
// LocalsPlus prefix that holds Argcount + nlocals fast locals.
func (e *evalState) localsAsObjects() []objects.Object {
	co := e.f.Code
	n := len(co.Varnames)
	if n == 0 {
		return nil
	}
	out := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		out[i] = e.f.LocalAt(i).AsObject()
	}
	return out
}
