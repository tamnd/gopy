// Hand-ported per-uop method bodies. Every method on *Tier2State
// here is one row of the dispatch switch in uops_dispatch_gen.go;
// the un-ported rest live as StatusDeopt stubs in uops_stubs_gen.go.
//
// Adding a new port: drop the method here, regenerate
// uops_stubs_gen.go (the generator omits stubs for any name already
// defined on *Tier2State), and the dispatch picks the new body up
// without touching the switch.
//
// CPython: Python/executor_cases.c.h (per-uop case bodies)

package optimizer

// DEPRECATED (spec 1714): Spec 1714 phases 7-9: fully deleted; uop bodies move into vm/eval_uops_gen.go via the generator + body translator.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/stackref"
)

// uopNop is the empty body uop. Trace projection emits this as a
// padding marker; the dispatcher just advances.
//
// CPython: Python/executor_cases.c.h:11-13 _NOP
func (s *Tier2State) uopNop(_ *UOPInstruction) Tier2Status {
	return StatusContinue
}

// uopStartExecutor is the trace prologue. Asserts the executor is
// still valid; if invalidation raced with entry the trace deopts
// rather than running with a stale guard set.
//
// CPython: Python/executor_cases.c.h:7110-7117 _START_EXECUTOR
func (s *Tier2State) uopStartExecutor(_ *UOPInstruction) Tier2Status {
	if !s.Executor.VMData.Valid {
		return StatusDeopt
	}
	return StatusContinue
}

// uopMakeWarm marks the executor warm so the cold-pruning sweep
// leaves it alone. The trace_run_counter side effect that fires the
// cold-prune eval breaker is deferred; v0.12 only owns the warm bit.
//
// CPython: Python/executor_cases.c.h:7119-7125 _MAKE_WARM
func (s *Tier2State) uopMakeWarm(_ *UOPInstruction) Tier2Status {
	s.Executor.VMData.Warm = true
	return StatusContinue
}

// uopCheckValidity bails to the side-exit when the trace's executor
// has been invalidated (dict / type watcher fired between dispatches).
//
// CPython: Python/executor_cases.c.h:7013-7019 _CHECK_VALIDITY
func (s *Tier2State) uopCheckValidity(inst *UOPInstruction) Tier2Status {
	if !s.Executor.VMData.Valid {
		return s.jumpToJumpTarget(inst)
	}
	return StatusContinue
}

// uopSetIp advances the frame's instruction pointer to the Tier-1
// codeunit offset trace projection stamped in inst.Target. CPython
// stores a bytecode pointer in operand0; gopy uses the codeunit index
// already carried by the target field.
//
// CPython: Python/executor_cases.c.h:6930-6934 _SET_IP
//
//revive:disable-next-line:var-naming
func (s *Tier2State) uopSetIp(inst *UOPInstruction) Tier2Status {
	s.Frame.InstrPtr = int(inst.Target)
	return StatusContinue
}

// uopExitTrace ends the trace normally. The Tier-1 dispatcher resumes
// at the side-exit target the executor stashed at install time.
//
// CPython: Python/executor_cases.c.h:6961-7011 _EXIT_TRACE
func (s *Tier2State) uopExitTrace(_ *UOPInstruction) Tier2Status {
	return StatusExit
}

// uopJumpToTop is the loop-edge backward jump. Upstream encodes it as
// JUMP_TO_JUMP_TARGET() so the projector controls where the loop top
// sits in the trace.
//
// CPython: Python/executor_cases.c.h:6925-6928 _JUMP_TO_TOP
func (s *Tier2State) uopJumpToTop(inst *UOPInstruction) Tier2Status {
	return s.jumpToJumpTarget(inst)
}

// uopLoadFast pushes a duplicate of the local at oparg. Asserts the
// slot is bound; an unbound LOAD_FAST in a Tier-2 trace means the
// trace projection skipped a guard, so the trace deopts.
//
// CPython: Python/executor_cases.c.h:193-202 _LOAD_FAST
func (s *Tier2State) uopLoadFast(_ *UOPInstruction) Tier2Status {
	ref := s.Frame.LocalAt(int(s.Oparg))
	if ref.IsNull() {
		return StatusDeopt
	}
	s.Frame.PushStack(ref.Dup())
	return StatusContinue
}

// uopLoadFastBorrow is structurally identical to uopLoadFast in
// the GIL build (PyStackRef_Borrow == PyStackRef_DUP under our
// reference model). The two entry points stay separate so v0.14's
// deferred-refcount split lands without touching dispatch.
//
// CPython: Python/executor_cases.c.h:300-308 _LOAD_FAST_BORROW
func (s *Tier2State) uopLoadFastBorrow(_ *UOPInstruction) Tier2Status {
	ref := s.Frame.LocalAt(int(s.Oparg))
	if ref.IsNull() {
		return StatusDeopt
	}
	s.Frame.PushStack(ref.Dup())
	return StatusContinue
}

// uopStoreFast pops the top of stack and stores it at the local
// indexed by oparg, closing the previous binding.
//
// CPython: Python/executor_cases.c.h:563-578 _STORE_FAST
func (s *Tier2State) uopStoreFast(_ *UOPInstruction) Tier2Status {
	val := s.Frame.PopStack()
	old := s.Frame.LocalAt(int(s.Oparg))
	old.Close()
	s.Frame.SetLocal(int(s.Oparg), val)
	return StatusContinue
}

// uopPopTop discards the top stack value.
//
// CPython: Python/executor_cases.c.h:581-589 _POP_TOP
func (s *Tier2State) uopPopTop(_ *UOPInstruction) Tier2Status {
	v := s.Frame.PopStack()
	v.Close()
	return StatusContinue
}

// uopPushNull pushes the Null sentinel. Used by call-shape uops and
// by the early stages of LOAD_GLOBAL to reserve a callable slot.
//
// CPython: Python/executor_cases.c.h:592-598 _PUSH_NULL
func (s *Tier2State) uopPushNull(_ *UOPInstruction) Tier2Status {
	s.Frame.PushStack(stackref.Null)
	return StatusContinue
}

// uopCopy duplicates the value at depth (oparg-1) onto the top of the
// stack. oparg starts at 1 (depth 0 = top).
//
// CPython: Python/executor_cases.c.h:6785-6795 _COPY
func (s *Tier2State) uopCopy(_ *UOPInstruction) Tier2Status {
	bottom := s.Frame.PeekStack(int(s.Oparg) - 1)
	s.Frame.PushStack(bottom.Dup())
	return StatusContinue
}

// uopSwap swaps the top of stack with the value at depth (oparg-1).
// Direct slice access lets the swap stay in place; PeekStack returns
// by value.
//
// CPython: Python/executor_cases.c.h:6830-6842 _SWAP
func (s *Tier2State) uopSwap(_ *UOPInstruction) Tier2Status {
	f := s.Frame
	base := frame.NLocalsPlusOf(f.Code)
	topIdx := base + f.StackTop - 1
	bottomIdx := topIdx - (int(s.Oparg) - 1)
	f.LocalsPlus[topIdx], f.LocalsPlus[bottomIdx] = f.LocalsPlus[bottomIdx], f.LocalsPlus[topIdx]
	return StatusContinue
}
