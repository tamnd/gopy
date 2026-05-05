package vm

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// codeWithBytecode builds a minimal Code object whose Code blob is bc.
func codeWithBytecode(bc []byte) *objects.Code {
	return &objects.Code{Code: bc, Stacksize: 4}
}

// instr packs an opcode + oparg byte pair.
func instr(op compile.Opcode, arg byte) []byte { return []byte{byte(op), arg} }

func TestEvalNotImplementedSurface(t *testing.T) {
	ts := state.NewThread()
	// BINARY_OP is not in the hand-written panel, so dispatch should
	// fall through to ErrNotImplemented.
	co := codeWithBytecode(instr(compile.BINARY_OP, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected ErrNotImplemented for ungenerated dispatch")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("err = %v, want wraps ErrNotImplemented", err)
	}
}

func TestEvalErrorMentionsOpcodeName(t *testing.T) {
	ts := state.NewThread()
	co := codeWithBytecode(instr(compile.BINARY_OP, 0))
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !contains(got, "BINARY_OP") {
		t.Errorf("error %q should mention BINARY_OP", got)
	}
}

func TestEvalEmptyCodeFails(t *testing.T) {
	ts := state.NewThread()
	co := codeWithBytecode(nil)
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestThreadVMLazyInit(t *testing.T) {
	ts := state.NewThread()
	b1 := BreakerFor(ts)
	b2 := BreakerFor(ts)
	if b1 != b2 {
		t.Error("BreakerFor must return the same breaker for the same thread")
	}
	p := PendingFor(ts)
	if p == nil {
		t.Error("PendingFor must not be nil")
	}
}

func TestEvalExtendedArgFetch(t *testing.T) {
	ts := state.NewThread()
	// EXTENDED_ARG 0x01, then BINARY_OP 0x02 -> oparg should be 0x0102.
	// BINARY_OP is unimplemented so we expect ErrNotImplemented to bubble.
	bc := append(instr(compile.EXTENDED_ARG, 1), instr(compile.BINARY_OP, 2)...)
	co := codeWithBytecode(bc)

	_, err := EvalCode(ts, co, nil, nil)
	if err == nil || !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented after EXTENDED_ARG fetch, got %v", err)
	}
	// We can't observe oparg directly without a real arm, but at least the
	// fetch must not have looped past the end.
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
