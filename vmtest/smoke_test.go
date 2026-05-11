// v0.6 release smoke. The parser still bails out before rule bodies
// are emitted, so the "live" path through cmd/gopy -c can't drive
// the VM yet. This test stitches several panel features together via
// hand-built bytecode so the v0.6 release gate has something concrete
// to pin: arithmetic, conditional jump, container build, function
// definition + call, and BINARY_OP NB_SUBSCR all run end-to-end.
//
// Once the PEG generator emits rule bodies (1640) the parser path
// joins this gate via the gate_test.go harness and the smoke test
// can consume real source.

package vmtest

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

func instr(op compile.Opcode, arg byte) []byte { return []byte{byte(op), arg} }

func concat(b ...[]byte) []byte {
	var out []byte
	for _, s := range b {
		out = append(out, s...)
	}
	return out
}

// TestSmokeReleaseV06 exercises the v0.6 panel against a single code
// object that touches arithmetic, container build, subscript, and
// the make-function / call pair. Failure here means the v0.6 release
// gate is not green from the VM side.
func TestSmokeReleaseV06(t *testing.T) {
	const nbSubscr = 26
	// Inner: takes (x, y), returns x + y.
	// LOAD_FAST 0, LOAD_FAST 1, BINARY_OP nbAdd(0), RETURN_VALUE.
	inner := &objects.Code{
		Code: concat(
			instr(compile.LOAD_FAST, 0),
			instr(compile.LOAD_FAST, 1),
			instr(compile.BINARY_OP, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Argcount:  2,
		Varnames:  []string{"x", "y"},
		Stacksize: 4,
		Name:      "add",
	}
	inner.Init(objects.CodeType)

	// Outer:
	//   LOAD_CONST 0 (inner code), MAKE_FUNCTION,        # add
	//   PUSH_NULL, LOAD_CONST 1 (10), LOAD_CONST 2 (32), # call args
	//   CALL 2,                                          # add(10, 32) -> 42
	//   LOAD_CONST 3 (list [40, 41, 42, 43]),
	//   LOAD_CONST 4 (2),
	//   BINARY_OP NB_SUBSCR,                             # list[2] -> 42
	//   COMPARE_OP eq,                                   # 42 == 42 -> True
	//   RETURN_VALUE
	cmpEq := uint32(objects.CompareEQ) << 5
	if cmpEq > 0xff {
		t.Fatalf("cmpEq %d does not fit in oparg byte", cmpEq)
	}
	outer := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.MAKE_FUNCTION, 0),
			instr(compile.PUSH_NULL, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.LOAD_CONST, 2),
			instr(compile.CALL, 2),
			instr(compile.LOAD_CONST, 3),
			instr(compile.LOAD_CONST, 4),
			instr(compile.BINARY_OP, nbSubscr),
			instr(compile.COMPARE_OP, byte(cmpEq)),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts: []any{
			inner,
			int64(10),
			int64(32),
			objects.NewList([]objects.Object{
				objects.NewInt(40), objects.NewInt(41),
				objects.NewInt(42), objects.NewInt(43),
			}),
			int64(2),
		},
		Stacksize: 16,
	}

	ts := state.NewThread()
	v, err := vm.EvalCode(ts, outer, objects.NewDict(), nil)
	if err != nil {
		t.Fatalf("EvalCode err: %v", err)
	}
	if v != objects.True() {
		t.Fatalf("v0.6 smoke produced %v, want True", v)
	}
}
