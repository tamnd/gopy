// Tier-2 trace analysis. The forward pass runs after projection and
// before installation: a bounded data-flow walk over the projected
// uops that prunes _SET_IP / _CHECK_VALIDITY where escapes can be
// proved out of reach, collapses the load-then-pop idiom into a NOP,
// and (when wired) folds globals/builtins references into immediate
// constants once the dict watchers are armed.
//
// gopy lands the orchestrator and the cleanup pass in this commit;
// the per-opcode abstract interpretation table and the watcher arm
// are gated behind #430 (DSL-generated cases) and #445 (watcher
// callbacks). The orchestrator runs all three phases unconditionally
// so wiring those follow-ups is a drop-in replacement for the
// stubbed bodies below.
//
// CPython: Python/optimizer_analysis.c:625-654
// _Py_uop_analyze_and_optimize
// CPython: Python/optimizer_analysis.c:432-526 optimize_uops
// CPython: Python/optimizer_analysis.c:529-620 remove_unneeded_uops
// CPython: Python/optimizer_analysis.c:145-304 remove_globals

package optimizer

import (
	"unsafe"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Analyze runs the three-pass forward analysis over a freshly
// projected trace. Returns >0 (the new length) on success, 0 on a
// benign bail (analyzer not ready, contradiction hit, etc), -1 on a
// hard failure. Mirrors CPython's three-step pipeline: globals
// folding, abstract interpretation, then the cleanup that drops the
// now-unneeded _SET_IP / _CHECK_VALIDITY rows and collapses the
// load-then-pop idiom.
//
// CPython: Python/optimizer_analysis.c:625-654
// _Py_uop_analyze_and_optimize
func Analyze(interp *state.Interpreter, frame objects.InterpreterFrame, co *objects.Code, buffer []UOPInstruction, length, currStacklen int, dependencies *BloomFilter) int {
	err := removeGlobals(interp, frame, buffer, length, dependencies)
	if err <= 0 {
		return err
	}
	length = optimizeUops(co, buffer, length, currStacklen, dependencies)
	if length <= 0 {
		return length
	}
	length = removeUnneededUops(buffer, length)
	return length
}

// removeGlobals folds _LOAD_GLOBAL_MODULE / _LOAD_GLOBAL_BUILTINS /
// _LOAD_ATTR_MODULE rows into inline-const loads when the dict
// version stamped on the uop still matches and the watching scheme
// can prove the dict has not mutated since. Returns 1 on a successful
// pass (with or without folds), 0 on a hard bail (incorrect keys
// version somewhere; the caller drops the trace), -1 on failure.
//
// frame==nil short-circuits to 1: the orchestrator passes nil from
// gate tests that don't have a real frame to thread, and from any
// call site that hasn't wired the running activation record yet. The
// fold path needs frame.f_globals / f_builtins / function for the
// version stamp.
//
// CPython: Python/optimizer_analysis.c:145-304 remove_globals
func removeGlobals(interp *state.Interpreter, frame objects.InterpreterFrame, buffer []UOPInstruction, length int, dependencies *BloomFilter) int {
	if frame == nil || interp == nil {
		return 1
	}
	builtins, _ := frame.FrameBuiltins().(*objects.Dict)
	globals, _ := frame.FrameGlobals().(*objects.Dict)
	if builtins == nil || globals == nil {
		return 1
	}
	if interp.Builtins != nil && interp.Builtins != objects.Object(builtins) {
		return 1
	}
	function, _ := frame.FrameFunc().(*objects.Function)
	if function == nil {
		return 1
	}
	functionVersion := function.GetVersionForCurrentState()

	// These values represent stacks of booleans (one bool per bit).
	// Pushing a frame shifts left, popping a frame shifts right.
	var (
		functionChecked           uint32
		builtinsWatched           uint32
		globalsWatched            uint32
		precheckedFunctionVersion uint32
	)

	// CPython arms the watcher callbacks lazily here; gopy installs
	// them once at WatcherInit so this body just registers
	// subscriptions.
	WatcherInit(interp)

	for pc := 0; pc < length; pc++ {
		inst := &buffer[pc]
		opcode := inst.Opcode()
		switch opcode {
		case UopGuardGlobalsVersion:
			if incorrectKeys(inst, globals) {
				return 0
			}
			if int(globals.Mutations()) >= MaxAllowedGlobalsModifications {
				continue
			}
			if globalsWatched&1 == 0 {
				DictWatch(interp, GlobalsWatcherID, unsafe.Pointer(globals))
				dependencies.Add(unsafe.Pointer(globals))
				globalsWatched |= 1
			}
			if functionChecked&1 != 0 {
				buffer[pc].SetOpcode(UopNop)
			} else {
				buffer[pc].SetOpcode(UopCheckFunction)
				buffer[pc].Operand0 = uint64(functionVersion)
				functionChecked |= 1
			}
		case UopLoadGlobalBuiltins:
			if incorrectKeys(inst, builtins) {
				return 0
			}
			if interp.BuiltinDictMutations >= MaxAllowedBuiltinsModifications {
				continue
			}
			if builtinsWatched&1 == 0 {
				DictWatch(interp, BuiltinsWatcherID, unsafe.Pointer(builtins))
				builtinsWatched |= 1
			}
			if functionChecked&globalsWatched&1 != 0 {
				convertGlobalToConst(inst, builtins, false)
			}
		case UopLoadGlobalModule:
			if incorrectKeys(inst, globals) {
				return 0
			}
			if int(globals.Mutations()) >= MaxAllowedGlobalsModifications {
				continue
			}
			if globalsWatched&1 == 0 {
				DictWatch(interp, GlobalsWatcherID, unsafe.Pointer(globals))
				dependencies.Add(unsafe.Pointer(globals))
				globalsWatched |= 1
			}
			if functionChecked&1 == 0 && pc > 0 && buffer[pc-1].Opcode() == UopNop {
				buffer[pc-1].SetOpcode(UopCheckFunction)
				buffer[pc-1].Operand0 = uint64(functionVersion)
				functionChecked |= 1
			}
			if functionChecked&1 != 0 {
				convertGlobalToConst(inst, globals, false)
			}
		case UopPushFrame:
			builtinsWatched <<= 1
			globalsWatched <<= 1
			functionChecked <<= 1
			operand := buffer[pc].Operand0
			if operand == 0 || operand&1 != 0 {
				// Either a code object (low bit tag) or NULL.
				return 1
			}
			fn := (*objects.Function)(unsafe.Pointer(uintptr(operand)))
			if fn == nil {
				return 1
			}
			functionVersion = fn.Version
			if precheckedFunctionVersion == functionVersion {
				functionChecked |= 1
			}
			precheckedFunctionVersion = 0
			globals, _ = fn.Globals.(*objects.Dict)
			builtins, _ = fn.Builtins.(*objects.Dict)
			if globals == nil || builtins == nil {
				return 1
			}
			if interp.Builtins != nil && interp.Builtins != objects.Object(builtins) {
				return 1
			}
		case UopReturnValue:
			builtinsWatched >>= 1
			globalsWatched >>= 1
			functionChecked >>= 1
			operand := buffer[pc].Operand0
			if operand == 0 || operand&1 != 0 {
				return 1
			}
			fn := (*objects.Function)(unsafe.Pointer(uintptr(operand)))
			if fn == nil {
				return 1
			}
			functionVersion = fn.Version
			globals, _ = fn.Globals.(*objects.Dict)
			builtins, _ = fn.Builtins.(*objects.Dict)
			if globals == nil || builtins == nil {
				return 1
			}
		case UopCheckFunctionExactArgs:
			precheckedFunctionVersion = uint32(buffer[pc].Operand0)
		default:
			if inst.IsTerminator() {
				return 1
			}
		}
	}
	return 0
}

// incorrectKeys reports whether obj's current keys version no longer
// matches the version stamped on inst at projection time. A mismatch
// (or a non-dict) means the inline cache has gone stale and the
// caller must drop the trace.
//
// CPython: Python/optimizer_analysis.c:129-140 incorrect_keys
func incorrectKeys(inst *UOPInstruction, obj objects.Object) bool {
	d, ok := obj.(*objects.Dict)
	if !ok {
		return true
	}
	return uint64(d.GetKeysVersion()) != inst.Operand0
}

// convertGlobalToConst rewrites a _LOAD_GLOBAL_MODULE /
// _LOAD_GLOBAL_BUILTINS / _LOAD_ATTR_MODULE row into the matching
// inline-const load. operand1 is the precomputed slot index; if the
// slot is dead or the keys version has slipped under us the rewrite
// bails. pop=true picks the _POP_TOP_LOAD_CONST_INLINE_BORROW variant
// (used for chained dict loads); pop=false picks the plain inline
// load. Returns the resolved value, or nil when the slot does not
// hold a live binding.
//
// CPython: Python/optimizer_analysis.c:94-127 convert_global_to_const
func convertGlobalToConst(inst *UOPInstruction, dict *objects.Dict, pop bool) {
	index := int(inst.Operand1)
	if uint64(dict.GetKeysVersion()) != inst.Operand0 {
		return
	}
	_, value, ok := dict.EntryAt(index)
	if !ok || value == nil {
		return
	}
	if pop {
		inst.SetOpcode(UopPopTopLoadConstInlineBorrow)
	} else {
		inst.SetOpcode(UopLoadConstInlineBorrow)
	}
	inst.Operand0 = uint64(uintptr(unsafe.Pointer(&value)))
}

// optimizeUops is the abstract-interpreter forward pass. CPython
// dispatches on every opcode through a generated case table
// (optimizer_cases.c.h) that mirrors the executor's per-uop
// semantics on the symbolic lattice. gopy's case table is gated on
// #430; until it lands the loop walks the trace and treats every
// opcode as opaque (Unknown stack effect). The init/fini bracket and
// the contradiction/out-of-space short-circuits are wired now so the
// per-opcode arm slots into the existing control flow.
//
// CPython: Python/optimizer_analysis.c:432-526 optimize_uops
func optimizeUops(co *objects.Code, trace []UOPInstruction, traceLen, currStacklen int, dependencies *BloomFilter) int {
	_ = dependencies
	var ctx JitOptContext
	AbstractContextInit(&ctx)
	frame := FrameNew(&ctx, co, currStacklen, nil, 0)
	if frame == nil {
		AbstractContextFini(&ctx)
		return 0
	}
	ctx.CurrFrameDepth++
	ctx.Frame = frame
	for i := 0; !ctx.Done && i < traceLen; i++ {
		// Per-opcode dispatch lands with #430. The default arm has
		// to keep the stack pointer in a consistent state, so we
		// bail to "unknown" semantics (no folding, no guard
		// elimination) on every row until the table arrives.
		this := &trace[i]
		if this.IsTerminator() {
			break
		}
	}
	if ctx.Contradiction {
		AbstractContextFini(&ctx)
		return 0
	}
	AbstractContextFini(&ctx)
	return traceLen
}

// removeUnneededUops is the post-analysis cleanup pass. It walks the
// trace once and drops _SET_IP / _CHECK_VALIDITY rows where escapes
// can be proved out of reach, then collapses the
// load-then-pop idiom into NOPs (or shortens the combined uops
// where possible). Returns the new length, with NOPs left in place
// for the trace-finalize pass to compact away.
//
// CPython: Python/optimizer_analysis.c:529-620 remove_unneeded_uops
func removeUnneededUops(buffer []UOPInstruction, bufferSize int) int {
	lastSetIP := -1
	mayHaveEscaped := true
	for pc := 0; pc < bufferSize; pc++ {
		opcode := buffer[pc].Opcode()
		switch opcode {
		case UopStartExecutor:
			mayHaveEscaped = false
		case UopSetIp:
			buffer[pc].SetOpcode(UopNop)
			lastSetIP = pc
		case UopCheckValidity:
			if mayHaveEscaped {
				mayHaveEscaped = false
			} else {
				buffer[pc].SetOpcode(UopNop)
			}
		case UopJumpToTop, UopExitTrace:
			return pc + 1
		case UopPopTop, UopPopTopLoadConstInline,
			UopPopTopLoadConstInlineBorrow,
			UopPopTwoLoadConstInlineBorrow:
			opcode = collapsePopTop(buffer, pc, opcode)
			fallthrough
		default:
			needsIP := opcode == UopPushFrame
			if uopFlags(opcode)&FlagEscapes != 0 {
				needsIP = true
				mayHaveEscaped = true
			}
			if needsIP && lastSetIP >= 0 {
				buffer[lastSetIP].SetOpcode(UopSetIp)
				lastSetIP = -1
			}
		}
	}
	return bufferSize
}

// collapsePopTop folds a load-then-pop pair into NOPs. CPython's
// loop loops back on _POP_TWO_LOAD_CONST_INLINE_BORROW after
// rewriting it to _POP_TOP_LOAD_CONST_INLINE_BORROW so a chained
// inline-load can be cleaned up too; gopy mirrors that with an
// explicit reentry. Returns the (possibly rewritten) opcode at pc so
// the surrounding switch's default arm sees the post-rewrite shape.
//
// CPython: Python/optimizer_analysis.c:556-597 (the
// optimize_pop_top_again label inside remove_unneeded_uops)
func collapsePopTop(buffer []UOPInstruction, pc int, opcode uint16) uint16 {
	for {
		last := pc - 1
		for last >= 0 && buffer[last].Opcode() == UopNop {
			last--
		}
		if last < 0 {
			return opcode
		}
		switch buffer[last].Opcode() {
		case UopPopTwoLoadConstInlineBorrow:
			buffer[last].SetOpcode(UopPopTop)
			return opcode
		case UopPopTopLoadConstInline, UopPopTopLoadConstInlineBorrow:
			buffer[last].SetOpcode(UopNop)
			continue
		case UopCopy, UopLoadConstInline, UopLoadConstInlineBorrow,
			UopLoadFast, UopLoadFastBorrow, UopLoadSmallInt:
			buffer[last].SetOpcode(UopNop)
			switch opcode {
			case UopPopTop:
				buffer[pc].SetOpcode(UopNop)
				return UopNop
			case UopPopTopLoadConstInline:
				buffer[pc].SetOpcode(UopLoadConstInline)
				return UopLoadConstInline
			case UopPopTopLoadConstInlineBorrow:
				buffer[pc].SetOpcode(UopLoadConstInlineBorrow)
				return UopLoadConstInlineBorrow
			case UopPopTwoLoadConstInlineBorrow:
				buffer[pc].SetOpcode(UopPopTopLoadConstInlineBorrow)
				opcode = UopPopTopLoadConstInlineBorrow
				continue
			}
			return opcode
		default:
			return opcode
		}
	}
}
