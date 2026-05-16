package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

func runContainsOp(t *testing.T, op compile.Opcode, key, container objects.Object, oparg byte) objects.Object {
	t.Helper()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.CONTAINS_OP, oparg),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{key, container},
		Stacksize: 4,
	}
	specialize.Enable(co)
	idx := 2
	specialize.SetOpcode(co.Code, idx, op)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	return v
}

func TestFastContainsOpDict(t *testing.T) {
	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("a"), objects.NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_DICT, objects.NewStr("a"), d, 0); got != objects.True() {
		t.Errorf("a in d: got %v want True", got)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_DICT, objects.NewStr("b"), d, 0); got != objects.False() {
		t.Errorf("b in d: got %v want False", got)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_DICT, objects.NewStr("b"), d, 1); got != objects.True() {
		t.Errorf("b not in d: got %v want True", got)
	}
}

func TestFastContainsOpSet(t *testing.T) {
	s := objects.NewSet()
	if err := s.Add(objects.NewInt(7)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_SET, objects.NewInt(7), s, 0); got != objects.True() {
		t.Errorf("7 in s: got %v want True", got)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_SET, objects.NewInt(9), s, 0); got != objects.False() {
		t.Errorf("9 in s: got %v want False", got)
	}
	if got := runContainsOp(t, compile.CONTAINS_OP_SET, objects.NewInt(9), s, 1); got != objects.True() {
		t.Errorf("9 not in s: got %v want True", got)
	}
}

func TestFastContainsOpGuardMissDeopts(t *testing.T) {
	// Plant CONTAINS_OP_DICT but feed a list as the container — guard
	// fails, dispatcher deopts back to CONTAINS_OP.
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.CONTAINS_OP, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts: []any{
			objects.NewInt(1),
			objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)}),
		},
		Stacksize: 4,
	}
	specialize.Enable(co)
	idx := 2
	specialize.SetOpcode(co.Code, idx, compile.CONTAINS_OP_DICT)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v want True", v)
	}
	if op := compile.Opcode(co.Code[2*idx]); op == compile.CONTAINS_OP_DICT {
		t.Errorf("guard miss should have deopted, but op stayed CONTAINS_OP_DICT")
	}
}
