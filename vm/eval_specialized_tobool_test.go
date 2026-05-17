package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampToBoolOp rewrites the TO_BOOL at instr to op and primes the
// cooldown counter. Variants without a version cache (BOOL/INT/LIST/
// NONE/STR) need nothing else.
func stampToBoolOp(co *objects.Code, instr int, op compile.Opcode) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
}

func runToBool(t *testing.T, op compile.Opcode, value objects.Object, stamp func(co *objects.Code, instr int)) objects.Object {
	t.Helper()
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.TO_BOOL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{value},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampToBoolOp(co, 1, op)
	if stamp != nil {
		stamp(co, 1)
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	return v
}

func TestFastToBoolBool(t *testing.T) {
	if got := runToBool(t, compile.TO_BOOL_BOOL, objects.True(), nil); got != objects.True() {
		t.Errorf("True: got %v", got)
	}
	if got := runToBool(t, compile.TO_BOOL_BOOL, objects.False(), nil); got != objects.False() {
		t.Errorf("False: got %v", got)
	}
}

func TestFastToBoolInt(t *testing.T) {
	if got := runToBool(t, compile.TO_BOOL_INT, objects.NewInt(0), nil); got != objects.False() {
		t.Errorf("0: got %v", got)
	}
	if got := runToBool(t, compile.TO_BOOL_INT, objects.NewInt(7), nil); got != objects.True() {
		t.Errorf("7: got %v", got)
	}
	if got := runToBool(t, compile.TO_BOOL_INT, objects.NewInt(-3), nil); got != objects.True() {
		t.Errorf("-3: got %v", got)
	}
}

func TestFastToBoolList(t *testing.T) {
	if got := runToBool(t, compile.TO_BOOL_LIST, objects.NewList(nil), nil); got != objects.False() {
		t.Errorf("empty: got %v", got)
	}
	if got := runToBool(t, compile.TO_BOOL_LIST, objects.NewList([]objects.Object{objects.NewInt(1)}), nil); got != objects.True() {
		t.Errorf("nonempty: got %v", got)
	}
}

func TestFastToBoolNone(t *testing.T) {
	if got := runToBool(t, compile.TO_BOOL_NONE, objects.None(), nil); got != objects.False() {
		t.Errorf("None: got %v", got)
	}
}

func TestFastToBoolStr(t *testing.T) {
	if got := runToBool(t, compile.TO_BOOL_STR, objects.NewStr(""), nil); got != objects.False() {
		t.Errorf("empty: got %v", got)
	}
	if got := runToBool(t, compile.TO_BOOL_STR, objects.NewStr("hi"), nil); got != objects.True() {
		t.Errorf("hi: got %v", got)
	}
}

func TestFastToBoolAlwaysTrue(t *testing.T) {
	cls := objects.NewType("C", []*objects.Type{objects.ObjectType()})
	cls.IsUser = true
	inst := objects.NewInstance(cls)
	version := cls.VersionTag()
	got := runToBool(t, compile.TO_BOOL_ALWAYS_TRUE, inst, func(co *objects.Code, idx int) {
		specialize.SetToBoolVersion(co.Code, idx, version)
	})
	if got != objects.True() {
		t.Errorf("user instance: got %v want True", got)
	}
}

// TestFastToBoolGuardMissDeopts plants TO_BOOL_INT but feeds a string
// operand: the guard fails, the dispatcher deopts back to TO_BOOL and
// the generic body still produces the right boolean.
func TestFastToBoolGuardMissDeopts(t *testing.T) {
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.TO_BOOL, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.NewStr("hi")},
		Stacksize: 4,
	}
	specialize.Enable(co)
	stampToBoolOp(co, 1, compile.TO_BOOL_INT)
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.True() {
		t.Errorf("got %v want True", v)
	}
	if op := compile.Opcode(co.Code[2*1]); op == compile.TO_BOOL_INT {
		t.Errorf("guard miss should have deopted, but op stayed TO_BOOL_INT")
	}
}
