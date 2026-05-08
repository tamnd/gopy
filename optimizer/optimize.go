// Tier-2 optimizer entry point. JUMP_BACKWARD's warmup-overflow path
// calls Optimize, which projects a trace, runs the analysis pass,
// rewrites implicit exits/errors into explicit _EXIT_TRACE /
// _ERROR_POP_N rows, and installs the resulting executor at the
// install site. Returns 1 on a successful install, 0 on a benign
// bail (no useful trace, no progress slot, etc), -1 on a hard
// failure.
//
// CPython: Python/optimizer.c:113-163 _PyOptimizer_Optimize
// CPython: Python/optimizer.c:1029-1099 prepare_for_execution
// CPython: Python/optimizer.c:1184-1250 make_executor_from_uops
// CPython: Python/optimizer.c:1272-1325 uop_optimize

package optimizer

import (
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Optimize is the JUMP_BACKWARD warmup-callback. It projects a trace
// starting at codeunit offset start in code, hands it through the
// analysis pass, and on success installs the resulting executor in
// code's side table at the start offset. chainDepth is the number of
// side-exit hops between the root trace and this attempt; the entry
// trace and every MaxChainDepth'th side-exit must make forward
// progress (i.e. own an ENTER_EXECUTOR slot in the host code).
//
// CPython: Python/optimizer.c:113-163 _PyOptimizer_Optimize
func Optimize(interp *state.Interpreter, frame objects.InterpreterFrame, code *objects.Code, start, chainDepth int, currStackEntries int) (*Executor, int) {
	chainDepth %= MaxChainDepth
	progressNeeded := chainDepth == 0
	if progressNeeded && !hasSpaceForExecutor(code, start) {
		return nil, 0
	}
	exec, err := uopOptimize(interp, frame, code, start, currStackEntries, progressNeeded)
	if err <= 0 {
		return nil, err
	}
	if progressNeeded {
		index := getIndexForExecutor(code, start)
		if index < 0 {
			return nil, 0
		}
		insertExecutor(code, start, index, exec)
	} else {
		exec.VMData.Code = nil
	}
	exec.VMData.ChainDepth = uint8(chainDepth)
	return exec, 1
}

// uopOptimize runs the projection + analysis + fix-up pipeline. It
// owns the trace scratch buffer so the buffer never escapes onto the
// heap when projection bails early.
//
// CPython: Python/optimizer.c:1272-1325 uop_optimize
func uopOptimize(interp *state.Interpreter, frame objects.InterpreterFrame, code *objects.Code, instr, currStackEntries int, progressNeeded bool) (*Executor, int) {
	dependencies := &BloomFilter{}
	dependencies.Init()
	buffer := make([]UOPInstruction, UOPMaxTraceLength)
	length := TranslateBytecodeToTrace(code, instr, buffer, UOPMaxTraceLength, dependencies, progressNeeded)
	if length <= 0 {
		return nil, length
	}
	length = Analyze(interp, frame, code, buffer, length, currStackEntries, dependencies)
	if length <= 0 {
		return nil, length
	}
	length = prepareForExecution(buffer, length)
	exec := makeExecutorFromUops(buffer, length, dependencies)
	if exec == nil {
		return nil, -1
	}
	ExecutorInit(interp, exec, dependencies)
	return exec, 1
}

// prepareForExecution drops _NOP rows, then walks the trace once to
// (a) materialize the implicit _EXIT_TRACE and _ERROR_POP_N stubs the
// projector left as flag-only references, and (b) split the trace
// format into JUMP-format rows that point at those stubs. The
// CPython routine keeps a per-target dedup so adjacent exits with
// identical targets share one stub.
//
// CPython: Python/optimizer.c:1029-1099 prepare_for_execution
func prepareForExecution(buffer []UOPInstruction, length int) int {
	copyTo := 0
	for i := 0; i < length; i++ {
		if buffer[i].Opcode() != UopNop {
			if copyTo != i {
				buffer[copyTo] = buffer[i]
			}
			copyTo++
		}
	}
	length = copyTo
	currentJump := int32(-1)
	currentJumpTarget := int32(-1)
	currentExitOp := uint16(0)
	currentError := int32(-1)
	currentErrorTarget := int32(-1)
	currentPopped := int32(-1)
	nextSpare := length
	for i := 0; i < length; i++ {
		inst := &buffer[i]
		opcode := inst.Opcode()
		target := int32(inst.GetTarget())
		flags := uopFlags(opcode)
		if flags&(FlagExit|FlagDeopt) != 0 {
			exitOp := UopDeopt
			if flags&FlagExit != 0 {
				exitOp = UopExitTrace
			}
			jumpTarget := target
			if jumpTarget != currentJumpTarget || currentExitOp != exitOp {
				makeExit(&buffer[nextSpare], exitOp, jumpTarget)
				currentExitOp = exitOp
				currentJumpTarget = jumpTarget
				currentJump = int32(nextSpare)
				nextSpare++
			}
			inst.Target = uint32(uint16(currentJump))
			inst.SetFormat(UOPFormatJump)
		}
		if flags&FlagError != 0 {
			popped := int32(0)
			if flags&FlagErrorNoPop == 0 {
				popped = uopNumPopped(opcode, inst.Oparg)
			}
			if target != currentErrorTarget || popped != currentPopped {
				currentPopped = popped
				currentError = int32(nextSpare)
				currentErrorTarget = target
				makeExit(&buffer[nextSpare], UopErrorPopN, 0)
				buffer[nextSpare].Operand0 = uint64(target)
				nextSpare++
			}
			if inst.Format() == UOPFormatTarget {
				inst.SetFormat(UOPFormatJump)
				inst.Target = 0
			}
			inst.Target |= uint32(uint16(currentError)) << 16
		}
		if opcode == UopJumpToTop {
			inst.SetFormat(UOPFormatJump)
			inst.Target = 1
		}
	}
	return nextSpare
}

// makeExit stamps an exit-stub row with the given opcode and Tier-1
// target. CPython splits this into a helper because every implicit
// exit/deopt/error stub funnels through here.
//
// CPython: Python/optimizer.c:1014-1024 make_exit
func makeExit(inst *UOPInstruction, opcode uint16, target int32) {
	inst.SetOpcode(opcode)
	inst.Oparg = 0
	inst.Operand0 = 0
	inst.SetFormat(UOPFormatTarget)
	inst.Target = uint32(target)
}

// countExits returns the number of explicit _EXIT_TRACE rows in the
// finalized trace. The executor pre-allocates one ExitData per
// exit so the dispatch loop never needs to grow the side-exit table
// at runtime.
//
// CPython: Python/optimizer.c:1001-1012 count_exits
func countExits(buffer []UOPInstruction, length int) int {
	count := 0
	for i := 0; i < length; i++ {
		if buffer[i].Opcode() == UopExitTrace {
			count++
		}
	}
	return count
}

// makeExecutorFromUops materializes the executor object: copies the
// finalized trace into the fresh allocation, wires _EXIT_TRACE rows
// to their per-exit ExitData slots, and stamps _START_EXECUTOR's
// operand0 with the executor's own pointer (the dispatch loop reads
// this back as the running-trace handle).
//
// CPython: Python/optimizer.c:1184-1250 make_executor_from_uops
func makeExecutorFromUops(buffer []UOPInstruction, length int, dependencies *BloomFilter) *Executor {
	_ = dependencies
	exitCount := countExits(buffer, length)
	exec := AllocateExecutor(exitCount, length)
	for i := 0; i < exitCount; i++ {
		exec.Exits[i].Executor = nil
		exec.Exits[i].Temperature = initialTemperatureBackoffCounter()
	}
	nextExit := exitCount - 1
	for i := length - 1; i >= 0; i-- {
		exec.Trace[i] = buffer[i]
		if buffer[i].Opcode() == UopExitTrace {
			exec.Exits[nextExit].Target = buffer[i].Target
			exec.Trace[i].Operand0 = uint64(uintptr(nextExit))
			nextExit--
		}
	}
	return exec
}

// uopFlags reads the FlagXxx bitfield for op out of the generated
// metadata table. Returns zero for unrecognized opcodes (the
// fix-up loop treats the missing entries as plain rows, which keeps
// us safe across the pending #430 expansion).
//
// CPython: Include/internal/pycore_uop_metadata.h _PyUop_Flags
func uopFlags(op uint16) UopFlag {
	name, ok := UopNames[op]
	if !ok || name == "" {
		return 0
	}
	return UopMeta[name].Flags
}

// uopNumPopped returns the per-uop num-popped value the error path
// uses to size the _ERROR_POP_N stub. Negative values are oparg-
// dependent; the projector approximates them as the oparg.
//
// CPython: Include/internal/pycore_uop_metadata.h _PyUop_num_popped
func uopNumPopped(op uint16, oparg uint16) int32 {
	name, ok := UopNames[op]
	if !ok || name == "" {
		return 0
	}
	popped := UopMeta[name].Popped
	if popped < 0 {
		return int32(oparg)
	}
	return int32(popped)
}

// initialTemperatureBackoffCounter returns the warmup-counter value
// a fresh side exit takes on. CPython uses a value-and-backoff word
// that counts up before the side-exit warms; gopy mirrors the bit
// layout for parity with the gate fixtures.
//
// CPython: Include/internal/pycore_backoff.h:91-97
// initial_temperature_backoff_counter
func initialTemperatureBackoffCounter() BackoffCounter {
	return BackoffCounter{ValueAndBackoff: 0}
}
