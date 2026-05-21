// Tier-2 trace executor. Hand-written driver loop plus the contract
// per-uop methods speak: Tier2Status return, *UOPInstruction argument,
// shared Tier2State receiver. The dispatch switch in
// uops_dispatch_gen.go fans the per-uop names into method calls;
// per-uop bodies live in uops_impl.go (hand-ported) or
// uops_stubs_gen.go (StatusDeopt stubs for un-ported uops).
//
// CPython: Python/ceval.c:1240-1358 enter_tier_two
// CPython: Python/executor_cases.c.h (generated case bodies)

package optimizer

// DEPRECATED (spec 1714): Spec 1714 phase 7: fully deleted; uop definitions come from optimizer/uop_meta_gen.go (regenerated through tools/cases_generator).
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/state"
)

// Tier2Status is the return contract for per-uop methods. The driver
// loop branches on this value to decide what to do next.
//
// CPython: Python/ceval.c:1335-1356 jump_to_error_target /
// jump_to_jump_target labels
type Tier2Status int

const (
	// StatusContinue advances to NextUop. Methods that take a
	// JUMP_TO_JUMP_TARGET / JUMP_TO_TOP path mutate s.NextUop in
	// place and still return StatusContinue, since the loop's "next"
	// is whatever index was just written.
	StatusContinue Tier2Status = iota
	// StatusError signals the per-uop body set an exception on
	// s.Thread; the driver hands control back to the Tier-1 unwind
	// path.
	StatusError
	// StatusDeopt signals the trace cannot run further: the install
	// site Tier-1 instruction should run instead. Stubs return this
	// to keep partial-port traces runtime-safe.
	StatusDeopt
	// StatusExit corresponds to _EXIT_TRACE: the trace finished
	// normally; the caller resumes Tier-1 at the side-exit target.
	StatusExit
)

// Tier2State carries the per-call locals the dispatch switch reads
// and mutates. Field set is intentionally small: the C loop keeps
// frame, stack pointer, oparg, next_uop and current_executor in
// registers, and gopy mirrors that with a stack-allocated value.
//
// CPython: Python/ceval.c:1240-1288 enter_tier_two prologue
type Tier2State struct {
	Interp   *state.Interpreter
	Thread   *state.Thread
	Frame    *frame.Frame
	Executor *Executor
	// NextUop is the index into Executor.Trace of the next instruction
	// to dispatch. Mutated by JUMP_TO_TOP / JUMP_TO_JUMP_TARGET helpers
	// before the method returns StatusContinue.
	NextUop int
	// Oparg carries the current uop's oparg (low 16 bits of
	// UOPInstruction.Oparg). Refreshed before each dispatch.
	Oparg uint32
}

// RunExecutor is the entry point from vm.enterExecutor: build a
// Tier2State around the trace and run the dispatch loop. Returns the
// terminal status so the caller can deopt, propagate an error, or
// resume Tier-1 at the exit target.
//
// CPython: Python/ceval.c:1290 jump to enter_tier_two
func RunExecutor(thread *state.Thread, fr *frame.Frame, exec *Executor) Tier2Status {
	s := Tier2State{
		Thread:   thread,
		Frame:    fr,
		Executor: exec,
	}
	if thread != nil {
		s.Interp = thread.Interp()
	}
	tier2Log("trace_enter", "executor=%p trace_len=%d", exec, len(exec.Trace))
	return s.Run()
}

// Run is the dispatch driver. Mirrors enter_tier_two's for(;;) over
// the switch: load opcode, advance, dispatch by uop ID, branch on the
// returned status. JUMP_TO_TOP / JUMP_TO_JUMP_TARGET are encoded as
// in-method mutations of NextUop, not separate status values, so the
// switch stays small.
//
// CPython: Python/ceval.c:1291-1333 tier2_dispatch loop
func (s *Tier2State) Run() Tier2Status {
	for {
		if s.NextUop < 0 || s.NextUop >= len(s.Executor.Trace) {
			return StatusExit
		}
		inst := &s.Executor.Trace[s.NextUop]
		idx := s.NextUop
		s.NextUop++
		s.Oparg = uint32(inst.Oparg)
		tier2Log("dispatch", "idx=%d uop=%s oparg=%d target=%d operand0=%d",
			idx, tier2UopName(inst.Opcode()), inst.Oparg, inst.Target, inst.Operand0)
		st := s.executeUop(inst)
		switch st {
		case StatusContinue:
			continue
		case StatusError, StatusDeopt, StatusExit:
			tier2Log("exit", "kind=%s idx=%d uop=%s",
				tier2StatusName(st), idx, tier2UopName(inst.Opcode()))
			return st
		default:
			panic("optimizer: unknown Tier2Status")
		}
	}
}

func tier2StatusName(st Tier2Status) string {
	switch st {
	case StatusContinue:
		return "continue"
	case StatusError:
		return "error"
	case StatusDeopt:
		return "deopt"
	case StatusExit:
		return "exit"
	}
	return "?"
}

// jumpToJumpTarget mirrors JUMP_TO_JUMP_TARGET(): redirect the
// dispatcher to the just-executed uop's split jump-target field.
// Returns StatusContinue so the driver re-enters the loop.
//
// _JUMP_TO_TOP shares this path because upstream encodes it as a
// JUMP_TO_JUMP_TARGET() call, not a literal rewind.
//
// CPython: Python/ceval.c:1350-1354 jump_to_jump_target
// CPython: Python/executor_cases.c.h:6925-6928 _JUMP_TO_TOP
func (s *Tier2State) jumpToJumpTarget(inst *UOPInstruction) Tier2Status {
	s.NextUop = int(inst.GetJumpTarget())
	return StatusContinue
}

// unimplementedUop is the body of every stub method emitted by the
// generator. Bailing to Tier-1 (StatusDeopt) keeps partial-port
// traces runtime-safe: vm.enterExecutor falls back to dispatching the
// install-site Tier-1 instruction.
func (s *Tier2State) unimplementedUop(_ string) Tier2Status {
	return StatusDeopt
}
