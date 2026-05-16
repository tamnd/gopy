package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// emitWithCache appends one instruction (op, arg) plus the right
// number of cache codeunits zeroed out, mirroring what the assembler
// will eventually emit for adaptive opcodes.
func emitWithCache(buf []byte, op compile.Opcode, arg byte) []byte {
	buf = append(buf, byte(op), arg)
	for i := 0; i < specialize.CacheCount(op); i++ {
		buf = append(buf, 0, 0)
	}
	return buf
}

// quickenedCode builds a Code object with Quickened = true and runs
// specialize.Quicken so the cache cells carry the warmup counter
// shape. Used by the wiring tests.
func quickenedCode(bc []byte, consts []any, names []string) *objects.Code {
	code := make([]byte, len(bc))
	copy(code, bc)
	specialize.Quicken(code, true)
	co := &objects.Code{
		Code:      code,
		Consts:    consts,
		Names:     names,
		Quickened: true,
		Stacksize: 8,
	}
	return co
}

// TestDispatchAdaptiveSpecializesToBool exercises the round trip:
// Quickened bytecode runs TO_BOOL, the adaptive driver decrements
// the counter, on trigger the specializer rewrites TO_BOOL ->
// TO_BOOL_INT, and the fast-path arm handles the int operand
// producing True. The opcode stays specialized after the run.
func TestDispatchAdaptiveSpecializesToBool(t *testing.T) {
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.TO_BOOL, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := quickenedCode(bc, []any{int64(7)}, nil)

	// Drive the counter down to zero so the next dispatch tick
	// actually triggers the specializer. Quicken seeds value=1 in
	// the lowest 12 bits so a single Advance brings it to 0.
	idx := 1 // codeunit index of TO_BOOL
	cnt := specialize.LoadCounter(co.Code, idx)
	specialize.StoreCounter(co.Code, idx, specialize.MakeBackoffCounter(0, cnt.ValueAndBackoff&15))

	ts := state.NewThread()
	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if out != objects.True() {
		t.Fatalf("result: got %v want True", out)
	}
	// After running, the TO_BOOL slot should read TO_BOOL_INT: the
	// adaptive driver triggered specialization and the fast-path arm
	// handled the int operand without deopting.
	got := compile.Opcode(co.Code[2*idx])
	if got != compile.TO_BOOL_INT {
		t.Fatalf("opcode after dispatch: got %s want TO_BOOL_INT", got.Name())
	}
}

// TestDispatchAdaptiveTicksCounter checks that on the slow path
// (counter not yet zero) the adaptive driver decrements the counter
// without calling the specializer.
func TestDispatchAdaptiveTicksCounter(t *testing.T) {
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.TO_BOOL, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := quickenedCode(bc, []any{int64(7)}, nil)

	idx := 1
	// Force a fat counter (value 100) so one tick stays above zero.
	specialize.StoreCounter(co.Code, idx, specialize.MakeBackoffCounter(100, 0))
	before := specialize.LoadCounter(co.Code, idx)

	ts := state.NewThread()
	if _, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict()); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}

	after := specialize.LoadCounter(co.Code, idx)
	if after.ValueAndBackoff >= before.ValueAndBackoff {
		t.Fatalf("counter did not decrement: before=%#x after=%#x",
			before.ValueAndBackoff, after.ValueAndBackoff)
	}
	// The opcode should still be TO_BOOL (no specialization).
	if got := compile.Opcode(co.Code[2*idx]); got != compile.TO_BOOL {
		t.Fatalf("opcode: got %s want TO_BOOL", got.Name())
	}
}

// TestDispatchDeoptsSpecialized sanity-checks the deopt arm directly:
// hand-place TO_BOOL_STR in the bytecode while the operand is an int,
// the fast-path arm refuses the type guard so dispatch should rewrite
// the opcode to TO_BOOL and still produce True via the generic body.
func TestDispatchDeoptsSpecialized(t *testing.T) {
	var bc []byte
	bc = emitWithCache(bc, compile.LOAD_CONST, 0)
	bc = emitWithCache(bc, compile.TO_BOOL, 0)
	bc = emitWithCache(bc, compile.RETURN_VALUE, 0)
	co := quickenedCode(bc, []any{int64(7)}, nil)

	idx := 1
	co.Code[2*idx] = byte(compile.TO_BOOL_STR)
	// Stamp cooldown counter so adaptiveTick won't re-specialize
	// before we observe the deopt.
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())

	ts := state.NewThread()
	out, err := EvalCode(ts, co, objects.NewDict(), objects.NewDict())
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if out != objects.True() {
		t.Fatalf("result: got %v want True", out)
	}
	if got := compile.Opcode(co.Code[2*idx]); got != compile.TO_BOOL {
		t.Fatalf("opcode after deopt: got %s want TO_BOOL", got.Name())
	}
}
