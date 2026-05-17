package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// runUnpackSequence drives LOAD_CONST seq; UNPACK_SEQUENCE n;
// BUILD_TUPLE n; RETURN_VALUE. After UNPACK the stack from bottom to
// top is items[n-1], …, items[0]; BUILD_TUPLE n folds it back so the
// returned tuple should equal the original sequence.
func runUnpackSequence(t *testing.T, op compile.Opcode, seq objects.Object, n byte) *objects.Tuple {
	t.Helper()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.UNPACK_SEQUENCE, n),
			instr(compile.BUILD_TUPLE, n),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{seq},
		Stacksize: 8,
	}
	specialize.Enable(co)
	idx := 1
	specialize.SetOpcode(co.Code, idx, op)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	tup, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("result not tuple: %T", v)
	}
	return tup
}

func tupleEqualsList(t *objects.Tuple, want []objects.Object) bool {
	if t.Len() != len(want) {
		return false
	}
	for i, w := range want {
		if t.Item(i) != w {
			return false
		}
	}
	return true
}

func TestFastUnpackSequenceTwoTuple(t *testing.T) {
	a := objects.NewInt(1)
	b := objects.NewInt(2)
	seq := objects.NewTuple([]objects.Object{a, b})
	got := runUnpackSequence(t, compile.UNPACK_SEQUENCE_TWO_TUPLE, seq, 2)
	// After UNPACK push items[1],items[0]; BUILD_TUPLE 2 produces
	// (items[1], items[0]).
	if !tupleEqualsList(got, []objects.Object{b, a}) {
		t.Errorf("got %v want (2,1)", got)
	}
}

func TestFastUnpackSequenceTuple(t *testing.T) {
	a := objects.NewInt(10)
	b := objects.NewInt(20)
	c := objects.NewInt(30)
	seq := objects.NewTuple([]objects.Object{a, b, c})
	got := runUnpackSequence(t, compile.UNPACK_SEQUENCE_TUPLE, seq, 3)
	if !tupleEqualsList(got, []objects.Object{c, b, a}) {
		t.Errorf("got %v want (30,20,10)", got)
	}
}

func TestFastUnpackSequenceList(t *testing.T) {
	a := objects.NewInt(7)
	b := objects.NewInt(8)
	seq := objects.NewList([]objects.Object{a, b})
	got := runUnpackSequence(t, compile.UNPACK_SEQUENCE_LIST, seq, 2)
	if !tupleEqualsList(got, []objects.Object{b, a}) {
		t.Errorf("got %v want (8,7)", got)
	}
}

func TestFastUnpackSequenceSizeMismatchDeopts(t *testing.T) {
	// Plant UNPACK_SEQUENCE_TUPLE expecting 3 but pass a 2-tuple.
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.UNPACK_SEQUENCE, 2),
			instr(compile.BUILD_TUPLE, 2),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.NewTuple([]objects.Object{objects.NewInt(1), objects.NewInt(2)})},
		Stacksize: 8,
	}
	specialize.Enable(co)
	idx := 1
	// Stamp _LIST variant: tuple operand fails the type guard.
	specialize.SetOpcode(co.Code, idx, compile.UNPACK_SEQUENCE_LIST)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if _, ok := v.(*objects.Tuple); !ok {
		t.Errorf("expected a tuple result, got %T", v)
	}
	if op := compile.Opcode(co.Code[2*idx]); op == compile.UNPACK_SEQUENCE_LIST {
		t.Errorf("guard miss should have deopted, but op stayed UNPACK_SEQUENCE_LIST")
	}
}
