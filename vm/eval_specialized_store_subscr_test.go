package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// runStoreSubscr drives:
//
//	LOAD_CONST value; LOAD_CONST container; LOAD_CONST key;
//	STORE_SUBSCR; LOAD_CONST container; RETURN_VALUE.
//
// Returns the (mutated) container.
func runStoreSubscr(t *testing.T, op compile.Opcode, value, container, key objects.Object) objects.Object {
	t.Helper()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0), // value
			instr(compile.LOAD_CONST, 1), // container
			instr(compile.LOAD_CONST, 2), // key
			instr(compile.STORE_SUBSCR, 0),
			instr(compile.LOAD_CONST, 1), // container again
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{value, container, key},
		Stacksize: 8,
	}
	specialize.Enable(co)
	idx := 3 // codeunit index of STORE_SUBSCR
	specialize.SetOpcode(co.Code, idx, op)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	return v
}

func TestFastStoreSubscrListInt(t *testing.T) {
	lst := objects.NewList([]objects.Object{objects.NewInt(0), objects.NewInt(0), objects.NewInt(0)})
	got := runStoreSubscr(t, compile.STORE_SUBSCR_LIST_INT, objects.NewInt(42), lst, objects.NewInt(1)).(*objects.List)
	v, _ := got.Item(1).(*objects.Int).Int64()
	if v != 42 {
		t.Errorf("got list[1]=%d want 42", v)
	}
}

func TestFastStoreSubscrDict(t *testing.T) {
	d := objects.NewDict()
	got := runStoreSubscr(t, compile.STORE_SUBSCR_DICT, objects.NewInt(99), d, objects.NewStr("k")).(*objects.Dict)
	v, err := got.GetItem(objects.NewStr("k"))
	if err != nil || v == nil {
		t.Fatalf("GetItem k: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 99 {
		t.Errorf("got %d want 99", n)
	}
}

func TestFastStoreSubscrGuardMissDeopts(t *testing.T) {
	// Plant STORE_SUBSCR_LIST_INT, but feed a dict — guard fails.
	d := objects.NewDict()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.LOAD_CONST, 2),
			instr(compile.STORE_SUBSCR, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.NewInt(5), d, objects.NewStr("x")},
		Stacksize: 8,
	}
	specialize.Enable(co)
	idx := 3
	specialize.SetOpcode(co.Code, idx, compile.STORE_SUBSCR_LIST_INT)
	specialize.StoreCounter(co.Code, idx, specialize.AdaptiveCounterCooldown())
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if _, ok := v.(*objects.Dict); !ok {
		t.Errorf("expected dict, got %T", v)
	}
	if op := compile.Opcode(co.Code[2*idx]); op == compile.STORE_SUBSCR_LIST_INT {
		t.Errorf("guard miss should have deopted, op stayed STORE_SUBSCR_LIST_INT")
	}
}
