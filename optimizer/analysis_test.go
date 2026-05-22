package optimizer

import (
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func makeUop(op uint16) UOPInstruction {
	var u UOPInstruction
	u.SetOpcode(op)
	return u
}

func TestRemoveUnneededUops_DropsRedundantSetIPAndCheckValidity(t *testing.T) {
	buf := []UOPInstruction{
		makeUop(UopStartExecutor),
		makeUop(UopSetIp),
		makeUop(UopCheckValidity),
		makeUop(UopLoadFast),
		makeUop(UopExitTrace),
	}
	n := removeUnneededUops(buf, len(buf))
	if n != 5 {
		t.Fatalf("length = %d, want 5", n)
	}
	if buf[1].Opcode() != UopNop {
		t.Errorf("buf[1] = %s, want NOP (no escape since _START_EXECUTOR)", UopNames[buf[1].Opcode()])
	}
	if buf[2].Opcode() != UopNop {
		t.Errorf("buf[2] = %s, want NOP (no escape upstream)", UopNames[buf[2].Opcode()])
	}
}

func TestRemoveUnneededUops_KeepsSetIPBeforeEscape(t *testing.T) {
	// _BINARY_OP_ADD_INT carries FlagEscapes per the metadata table.
	const escaping = uint16(45) // _BINARY_OP_ADD_INT
	if uopFlags(escaping)&FlagEscapes == 0 {
		t.Fatalf("test fixture: opcode %d is not flagged FlagEscapes", escaping)
	}
	buf := []UOPInstruction{
		makeUop(UopStartExecutor),
		makeUop(UopSetIp),
		makeUop(escaping),
		makeUop(UopExitTrace),
	}
	n := removeUnneededUops(buf, len(buf))
	if n != 4 {
		t.Fatalf("length = %d, want 4", n)
	}
	if buf[1].Opcode() != UopSetIp {
		t.Errorf("buf[1] = %s, want _SET_IP (resurrected for escaping op)", UopNames[buf[1].Opcode()])
	}
}

func TestRemoveUnneededUops_CollapsesLoadThenPop(t *testing.T) {
	buf := []UOPInstruction{
		makeUop(UopStartExecutor),
		makeUop(UopLoadFast),
		makeUop(UopPopTop),
		makeUop(UopExitTrace),
	}
	n := removeUnneededUops(buf, len(buf))
	if n != 4 {
		t.Fatalf("length = %d, want 4", n)
	}
	if buf[1].Opcode() != UopNop {
		t.Errorf("buf[1] = %s, want NOP (paired with POP_TOP)", UopNames[buf[1].Opcode()])
	}
	if buf[2].Opcode() != UopNop {
		t.Errorf("buf[2] = %s, want NOP (collapsed)", UopNames[buf[2].Opcode()])
	}
}

// stubFrame is a minimal InterpreterFrame for the removeGlobals test
// path. Only FrameGlobals/FrameBuiltins/FrameFunc are read.
type stubFrame struct {
	globals  objects.Object
	builtins objects.Object
	fn       objects.Object
}

func (s *stubFrame) FrameCode() *objects.Code            { return nil }
func (s *stubFrame) FrameGlobals() objects.Object        { return s.globals }
func (s *stubFrame) FrameBuiltins() objects.Object       { return s.builtins }
func (s *stubFrame) FrameLocals() objects.Object         { return nil }
func (s *stubFrame) FrameBack() objects.InterpreterFrame { return nil }
func (s *stubFrame) FrameLasti() int                     { return 0 }
func (s *stubFrame) FrameNumLocals() int                 { return 0 }
func (s *stubFrame) FrameFastLocal(int) objects.Object   { return nil }
func (s *stubFrame) FrameNumCells() int                  { return 0 }
func (s *stubFrame) FrameCellLocal(int) objects.Object   { return nil }
func (s *stubFrame) FrameNumFrees() int                       { return 0 }
func (s *stubFrame) FrameFreeLocal(int) objects.Object        { return nil }
func (s *stubFrame) FrameLocalsPlusItem(int) objects.Object   { return nil }
func (s *stubFrame) FrameFunc() objects.Object                { return s.fn }

// TestRemoveGlobals_RewritesGuardToCheckFunction confirms that the
// first _GUARD_GLOBALS_VERSION on a fresh trace becomes _CHECK_FUNCTION
// (stamped with the function version) and registers the dict with the
// globals watcher.
func TestRemoveGlobals_RewritesGuardToCheckFunction(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)
	globals := objects.NewDict()
	builtins := objects.NewDict()
	interp.Builtins = builtins
	code := &objects.Code{}
	fn := objects.NewFunction("f", code, globals)
	fn.Builtins = builtins
	fn.Version = 42
	frame := &stubFrame{globals: globals, builtins: builtins, fn: fn}

	keysVer := globals.GetKeysVersion()
	if keysVer == 0 {
		t.Fatal("globals keys version is zero; allocator wrap?")
	}

	buf := []UOPInstruction{
		makeUop(UopStartExecutor),
		makeUop(UopGuardGlobalsVersion),
		makeUop(UopExitTrace),
	}
	buf[1].Operand0 = uint64(keysVer)
	deps := &BloomFilter{}
	deps.Init()

	rc := removeGlobals(interp, frame, buf, len(buf), deps)
	if rc != 1 {
		t.Fatalf("removeGlobals = %d, want 1 (terminator hit after rewrite)", rc)
	}
	if buf[1].Opcode() != UopCheckFunction {
		t.Errorf("buf[1] = %s, want _CHECK_FUNCTION", UopNames[buf[1].Opcode()])
	}
	if buf[1].Operand0 != 42 {
		t.Errorf("buf[1].Operand0 = %d, want 42 (function version)", buf[1].Operand0)
	}
}

// TestRemoveGlobals_BailsOnIncorrectKeys confirms that a stale keys
// version stamped on a guard uop returns 0 so the orchestrator drops
// the trace.
func TestRemoveGlobals_BailsOnIncorrectKeys(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	WatcherInit(interp)
	globals := objects.NewDict()
	builtins := objects.NewDict()
	interp.Builtins = builtins
	fn := objects.NewFunction("f", &objects.Code{}, globals)
	fn.Builtins = builtins
	frame := &stubFrame{globals: globals, builtins: builtins, fn: fn}

	buf := []UOPInstruction{
		makeUop(UopGuardGlobalsVersion),
		makeUop(UopExitTrace),
	}
	buf[0].Operand0 = 0xBAD0BAD // does not match globals.GetKeysVersion()
	deps := &BloomFilter{}
	deps.Init()

	rc := removeGlobals(interp, frame, buf, len(buf), deps)
	if rc != 0 {
		t.Errorf("removeGlobals = %d, want 0 (drop trace)", rc)
	}
}

// TestRemoveGlobals_BailsOnCustomBuiltins confirms a frame whose
// f_builtins differs from interp.builtins short-circuits to 1 (treat
// as no-op pass).
func TestRemoveGlobals_BailsOnCustomBuiltins(t *testing.T) {
	interp := &state.Interpreter{JIT: true}
	interp.Builtins = objects.NewDict() // canonical
	customBuiltins := objects.NewDict() // someone else's
	globals := objects.NewDict()
	fn := objects.NewFunction("f", &objects.Code{}, globals)
	fn.Builtins = customBuiltins
	frame := &stubFrame{globals: globals, builtins: customBuiltins, fn: fn}

	buf := []UOPInstruction{makeUop(UopExitTrace)}
	deps := &BloomFilter{}
	deps.Init()
	if rc := removeGlobals(interp, frame, buf, len(buf), deps); rc != 1 {
		t.Errorf("removeGlobals = %d, want 1 (custom builtins bail)", rc)
	}
}

func TestAnalyze_PassesThroughEmptyTrace(t *testing.T) {
	buf := []UOPInstruction{
		makeUop(UopStartExecutor),
		makeUop(UopJumpToTop),
	}
	deps := &BloomFilter{}
	deps.Init()
	co := &objects.Code{Stacksize: 4}
	n := Analyze(nil, nil, co, buf, len(buf), 0, deps)
	if n != 2 {
		t.Errorf("Analyze length = %d, want 2", n)
	}
}
