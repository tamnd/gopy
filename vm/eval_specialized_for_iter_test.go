package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampForIter rewrites the FOR_ITER at instr to op and sets the
// adaptive counter to cooldown so the dispatcher routes through the
// fast arm instead of the adaptive tick. Mirrors what
// _Py_Specialize_ForIter does once it has picked a target variant.
//
// CPython: Python/specialize.c _Py_Specialize_ForIter writes the
// opcode and ResetCounter() into the cache cells.
func stampForIter(co *objects.Code, instr int, op compile.Opcode) {
	specialize.SetOpcode(co.Code, instr, op)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
}

// listIterOver builds the iterator returned by iter(list(items)). The
// fast arm type-asserts against this concrete iterator type.
func listIterOver(t *testing.T, items []objects.Object) objects.Object {
	t.Helper()
	it, err := objects.Iter(objects.NewList(items))
	if err != nil {
		t.Fatalf("Iter(list): %v", err)
	}
	return it
}

func tupleIterOver(t *testing.T, items []objects.Object) objects.Object {
	t.Helper()
	it, err := objects.Iter(objects.NewTuple(items))
	if err != nil {
		t.Fatalf("Iter(tuple): %v", err)
	}
	return it
}

func rangeIterOver(t *testing.T, start, stop, step int64) objects.Object {
	t.Helper()
	r, err := objects.NewRange(objects.NewInt(start), objects.NewInt(stop), objects.NewInt(step))
	if err != nil {
		t.Fatalf("NewRange: %v", err)
	}
	it, err := objects.Iter(r)
	if err != nil {
		t.Fatalf("Iter(range): %v", err)
	}
	return it
}

// hitProgram lays out
//
//	LOAD_CONST 0     (push iterator)
//	FOR_ITER_<x> 0   (advance on hit, push next value above iter)
//	RETURN_VALUE     (returns the value at TOS)
//
// On a hit the value pushed by the fast arm is at TOS, so RETURN_VALUE
// hands it back to the test.
func hitProgram(op compile.Opcode, iter objects.Object) *objects.Code {
	return &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(op, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{iter},
		Stacksize: 4,
	}
}

// exhaustionProgram lays out
//
//	idx 0  LOAD_CONST 0   ; push (already-exhausted) iterator
//	idx 1  FOR_ITER_<x> 2 ; oparg=2 -> jumpBy(3) lands at idx 5
//	idx 3  LOAD_CONST 1   ; "took the body" sentinel (unreached)
//	idx 4  RETURN_VALUE
//	idx 5  POP_TOP        ; drop the iterator that FOR_ITER left on stack
//	idx 6  LOAD_CONST 2   ; "exhausted" sentinel
//	idx 7  RETURN_VALUE
//
// CPython's macro(FOR_ITER_LIST) jumps `oparg + 1` codeunits, matching
// jumpBy(oparg+1) here (Python/bytecodes.c:3349).
func exhaustionProgram(op compile.Opcode, iter objects.Object) *objects.Code {
	return &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(op, 2),
			instr(compile.LOAD_CONST, 1),
			instr(compile.RETURN_VALUE, 0),
			instr(compile.POP_TOP, 0),
			instr(compile.LOAD_CONST, 2),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{iter, objects.NewInt(42), objects.NewInt(-1)},
		Stacksize: 4,
	}
}

func runInt(t *testing.T, co *objects.Code) int64 {
	t.Helper()
	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	return got
}

// TestFastForIterListHit drives the fast arm with a list iterator
// holding one element. The fast path advances by cacheAdvance(FOR_ITER)
// and pushes the next value above the iter; RETURN_VALUE returns it.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_LIST) (Python/bytecodes.c:3349)
func TestFastForIterListHit(t *testing.T) {
	iter := listIterOver(t, []objects.Object{objects.NewInt(11)})
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_LIST)

	if got := runInt(t, co); got != 11 {
		t.Errorf("got %d, want 11", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.FOR_ITER_LIST {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastForIterListExhausted drives the fast arm on an empty list.
// The fast arm reports exhaustion, jumpBy(oparg+1) skips past the loop
// body, and the trailing branch returns the sentinel.
func TestFastForIterListExhausted(t *testing.T) {
	iter := listIterOver(t, nil)
	co := exhaustionProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_LIST)

	if got := runInt(t, co); got != -1 {
		t.Errorf("got %d, want -1 (exhaustion branch)", got)
	}
}

// TestFastForIterListDeoptsOnWrongType plants a tuple iterator under a
// FOR_ITER_LIST stamp. The fast arm's type assertion fails (ok=false),
// the dispatcher rewrites the bytecode back to FOR_ITER, and the
// generic body still produces the right value.
func TestFastForIterListDeoptsOnWrongType(t *testing.T) {
	iter := tupleIterOver(t, []objects.Object{objects.NewInt(11)})
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_LIST)

	if got := runInt(t, co); got != 11 {
		t.Errorf("got %d, want 11", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op == compile.FOR_ITER_LIST {
		t.Errorf("guard miss should have deopted, but op stayed FOR_ITER_LIST")
	}
}

// TestFastForIterTupleHit covers the FOR_ITER_TUPLE fast arm against
// tupleIterator.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_TUPLE) (Python/bytecodes.c:3412)
func TestFastForIterTupleHit(t *testing.T) {
	iter := tupleIterOver(t, []objects.Object{objects.NewInt(22)})
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_TUPLE)

	if got := runInt(t, co); got != 22 {
		t.Errorf("got %d, want 22", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.FOR_ITER_TUPLE {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

func TestFastForIterTupleExhausted(t *testing.T) {
	iter := tupleIterOver(t, nil)
	co := exhaustionProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_TUPLE)

	if got := runInt(t, co); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

func TestFastForIterTupleDeoptsOnWrongType(t *testing.T) {
	iter := listIterOver(t, []objects.Object{objects.NewInt(22)})
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_TUPLE)

	if got := runInt(t, co); got != 22 {
		t.Errorf("got %d, want 22", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op == compile.FOR_ITER_TUPLE {
		t.Errorf("guard miss should have deopted")
	}
}

// TestFastForIterRangeHit covers the FOR_ITER_RANGE fast arm against a
// 1-element range iterator.
//
// CPython: Python/bytecodes.c macro(FOR_ITER_RANGE) (Python/bytecodes.c:3462)
func TestFastForIterRangeHit(t *testing.T) {
	iter := rangeIterOver(t, 7, 8, 1)
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_RANGE)

	if got := runInt(t, co); got != 7 {
		t.Errorf("got %d, want 7", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op != compile.FOR_ITER_RANGE {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

func TestFastForIterRangeExhausted(t *testing.T) {
	iter := rangeIterOver(t, 0, 0, 1)
	co := exhaustionProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_RANGE)

	if got := runInt(t, co); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

// TestFastForIterRangeDescending exercises the desc==false branch of
// RangeIterNextFast: step is negative, cur > stop, so it.asc is false
// and the c <= 0 guard governs exhaustion.
func TestFastForIterRangeDescending(t *testing.T) {
	iter := rangeIterOver(t, 5, 0, -2)
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_RANGE)

	if got := runInt(t, co); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestFastForIterRangeDeoptsOnWrongType(t *testing.T) {
	iter := listIterOver(t, []objects.Object{objects.NewInt(7)})
	co := hitProgram(compile.FOR_ITER, iter)
	specialize.Enable(co)
	stampForIter(co, 1, compile.FOR_ITER_RANGE)

	if got := runInt(t, co); got != 7 {
		t.Errorf("got %d, want 7", got)
	}
	if op := compile.Opcode(co.Code[2*1]); op == compile.FOR_ITER_RANGE {
		t.Errorf("guard miss should have deopted")
	}
}
