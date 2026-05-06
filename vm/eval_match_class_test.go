// Pins MATCH_CLASS positional + keyword extraction. CPython routes
// MATCH_CLASS through PyObject_GetAttr on the type for __match_args__
// (positional patterns) and on the subject for the names tuple
// (keyword patterns). The Go port mirrors that routing in
// execMatchClass; this test pins the happy path plus the negative
// branch where the subject is not an instance of the pattern type.
//
// CPython: Python/bytecodes.c:3043 MATCH_CLASS

package vm

import (
	"fmt"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// matchHolder is a minimal *Type-backed Object whose Getattro fields
// can be poked from a test. Mirrors the dummyHolder used in
// objects/protocol_attr_test.go but lives here so vm tests don't have
// to import the unexported helper.
type matchHolder struct {
	objects.Header
}

func newMatchHolder(tp *objects.Type) *matchHolder {
	h := &matchHolder{}
	h.Init(tp)
	return h
}

func TestEvalMatchClassPositionalPlusKeyword(t *testing.T) {
	pointType := objects.NewType("Point", []*objects.Type{objects.ObjectType()})
	pointType.Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		n, _ := objects.Str(name)
		switch n {
		case "x":
			return objects.NewInt(10), nil
		case "y":
			return objects.NewInt(20), nil
		case "color":
			return objects.NewStr("red"), nil
		}
		return nil, fmt.Errorf("AttributeError: 'Point' object has no attribute '%s'", n)
	}

	// Wire the type's meta-getattr so __match_args__ resolves on the
	// type itself. The full type-system port (v0.10) replaces this
	// hook with type_getattro and an MRO walk; for v0.9 a per-test
	// shim is enough to exercise the positional path.
	matchArgs := map[*objects.Type]*objects.Tuple{
		pointType: objects.NewTuple([]objects.Object{objects.NewStr("x"), objects.NewStr("y")}),
	}
	prevMeta := objects.TypeType().Getattro
	objects.TypeType().Getattro = func(o objects.Object, name objects.Object) (objects.Object, error) {
		n, _ := objects.Str(name)
		if n == "__match_args__" {
			if tp, ok := o.(*objects.Type); ok {
				if ma, ok := matchArgs[tp]; ok {
					return ma, nil
				}
			}
		}
		return nil, fmt.Errorf("AttributeError: type has no attribute '%s'", n)
	}
	t.Cleanup(func() { objects.TypeType().Getattro = prevMeta })

	subject := newMatchHolder(pointType)
	keywords := objects.NewTuple([]objects.Object{objects.NewStr("color")})

	// Stack before MATCH_CLASS: [..., subject, type, names]. oparg=2
	// (two positional patterns: x, y).
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0), // subject
			instr(compile.LOAD_CONST, 1), // type
			instr(compile.LOAD_CONST, 2), // names tuple
			instr(compile.MATCH_CLASS, 2),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{subject, pointType, keywords},
		Stacksize: 6,
	}

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Tuple)
	if !ok {
		t.Fatalf("MATCH_CLASS return = %T, want *objects.Tuple (matched)", v)
	}
	if got.Len() != 3 {
		t.Fatalf("attrs len = %d, want 3 (x, y, color)", got.Len())
	}
	x, _ := got.Item(0).(*objects.Int).Int64()
	y, _ := got.Item(1).(*objects.Int).Int64()
	if x != 10 || y != 20 {
		t.Errorf("positional attrs = (%d, %d), want (10, 20)", x, y)
	}
	color, _ := objects.Str(got.Item(2))
	if color != "red" {
		t.Errorf("keyword attr color = %q, want \"red\"", color)
	}
}

func TestEvalMatchClassNotInstancePushesNone(t *testing.T) {
	tp := objects.NewType("OnlyMe", []*objects.Type{objects.ObjectType()})
	other := objects.NewInt(0) // *Int is not an OnlyMe instance.

	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0), // subject (an Int)
			instr(compile.LOAD_CONST, 1), // type (OnlyMe)
			instr(compile.LOAD_CONST, 2), // names (empty)
			instr(compile.MATCH_CLASS, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{other, tp, objects.NewTuple(nil)},
		Stacksize: 6,
	}
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if v != objects.None() {
		t.Errorf("not-an-instance: got %v, want None", v)
	}
}
