// Tier-2 wiring. The runtime calls tryWarmupTier2 every time
// JUMP_BACKWARD fires (CPython gates this behind a per-instruction
// backoff counter; gopy attempts the optimize on each warm hit and
// lets the optimizer's bail short-circuit cheaply). When Optimize
// installs an executor the install site flips to ENTER_EXECUTOR;
// future hits land in enterExecutor instead.
//
// gopy stays interpreter-only: there is no uop dispatcher yet (#431
// lands the loop). enterExecutor therefore "deopts" each entry by
// dispatching the Tier-1 instruction the executor stashed at install
// time, returning the next-PC the dispatcher would have produced.
//
// CPython: Python/bytecodes.c:2884-2943 ENTER_EXECUTOR /
// JUMP_BACKWARD_JIT body
// CPython: Python/optimizer.c:113-163 _PyOptimizer_Optimize

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 7: tier-2 trace dispatcher is generated from Python/optimizer_bytecodes.c via tools/cases_generator. This file shrinks to trace-loop scaffolding.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/optimizer"
	"github.com/tamnd/gopy/state"
)

// tryWarmupTier2 invokes the optimizer once per JUMP_BACKWARD hit
// while a free executor slot remains at instrIdx. The bail-on-
// no-progress path inside Optimize keeps the call cheap; on success
// Optimize patches the bytecode at instrIdx to ENTER_EXECUTOR so
// subsequent hits route through enterExecutor instead.
//
// CPython: Python/optimizer.c:113-163 _PyOptimizer_Optimize
func (e *evalState) tryWarmupTier2(instrIdx int) {
	if e.ts == nil {
		return
	}
	interp := e.ts.Interp()
	if interp == nil {
		return
	}
	co := e.f.Code
	if compile.Opcode(co.Code[instrIdx*2]) == compile.ENTER_EXECUTOR {
		return
	}
	optimizer.Optimize(interp, e.f, co, instrIdx, 0, 0)
}

// enterExecutor is the dispatch arm for ENTER_EXECUTOR. With no uop
// loop yet, every entry deopts: read the executor from Code's side
// table, look up the Tier-1 op/arg the install path stashed, then
// fall back to the regular dispatcher with that synthetic
// instruction. Once #431 lands the uop interpreter, this arm runs
// the executor's trace instead and only deopts on a side exit.
//
// CPython: Python/bytecodes.c:2884-2899 ENTER_EXECUTOR (deopt path)
func (e *evalState) enterExecutor(oparg uint32) (int, objects.Object, error, bool, error) {
	co := e.f.Code
	exec := codeExecutorAt(co, int(oparg))
	if exec == nil {
		return 0, nil, nil, false, opcodeNotImplemented(compile.ENTER_EXECUTOR)
	}
	op := compile.Opcode(exec.VMData.Opcode)
	arg := uint32(exec.VMData.Oparg)
	next, retVal, retErr, retDone, _, err := e.trySimple(op, arg)
	return next, retVal, retErr, retDone, err
}

// codeExecutorAt resolves Code.Executors back to the executor at
// slot. Encapsulates the any-typed handoff so callers in vm/ stay
// independent of optimizer's concrete ExecutorArray type.
func codeExecutorAt(code *objects.Code, slot int) *optimizer.Executor {
	if code.Executors == nil {
		return nil
	}
	arr, ok := code.Executors.(*optimizer.ExecutorArray)
	if !ok {
		return nil
	}
	if slot < 0 || slot >= len(arr.Entries) {
		return nil
	}
	return arr.Entries[slot]
}

// _ keeps state imported even when no concrete reference survives
// dead-code elimination on builds that strip Tier-2.
var _ = state.Thread{}
