package optimizer

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

func TestTranslateBytecodeToTrace_BailsOnEntryReturn(t *testing.T) {
	// RETURN_VALUE on the entry frame has nothing to return to, so
	// the projector bails per CPython semantics.
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.RETURN_VALUE), 0,
		},
	}
	buf := make([]UOPInstruction, UOPMaxTraceLength)
	deps := &BloomFilter{}
	deps.Init()
	n := TranslateBytecodeToTrace(co, 0, buf, len(buf), deps, false)
	if n != 0 {
		t.Errorf("expected bail (0), got %d", n)
	}
}

func TestTranslateBytecodeToTrace_LoopClosesWithJumpToTop(t *testing.T) {
	// A loop that jumps back to the entry produces a trace closed
	// by _JUMP_TO_TOP. Layout (codeunits): LOAD_CONST POP_TOP
	// JUMP_BACKWARD <cache>.
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			0, 0, // JUMP_BACKWARD cache slot
		},
	}
	buf := make([]UOPInstruction, UOPMaxTraceLength)
	deps := &BloomFilter{}
	deps.Init()
	n := TranslateBytecodeToTrace(co, 0, buf, len(buf), deps, false)
	if n <= 0 {
		t.Fatalf("expected positive trace length, got %d", n)
	}
	if buf[0].Opcode() != UopStartExecutor || buf[1].Opcode() != UopMakeWarm {
		t.Errorf("trace prelude = (%d, %d), want (UopStartExecutor, UopMakeWarm)",
			buf[0].Opcode(), buf[1].Opcode())
	}
	if buf[n-1].Opcode() != UopJumpToTop {
		t.Errorf("trace[%d] = %d, want UopJumpToTop", n-1, buf[n-1].Opcode())
	}
}

func TestTranslateBytecodeToTrace_StampsBloom(t *testing.T) {
	// Every projection adds the entry code to the dependencies bloom
	// so the invalidation walk knows to revisit this trace if the
	// code changes.
	co := &objects.Code{
		Code: []byte{
			byte(compile.LOAD_CONST), 0,
			byte(compile.POP_TOP), 0,
			byte(compile.JUMP_BACKWARD), 4,
			0, 0,
		},
	}
	buf := make([]UOPInstruction, UOPMaxTraceLength)
	deps := &BloomFilter{}
	deps.Init()
	TranslateBytecodeToTrace(co, 0, buf, len(buf), deps, false)
	empty := BloomFilter{}
	if *deps == empty {
		t.Errorf("dependencies bloom is still zero after projection")
	}
}
