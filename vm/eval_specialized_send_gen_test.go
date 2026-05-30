package vm

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/state"
)

// stampSendGen rewrites the SEND at instr to SEND_GEN and sets the
// adaptive counter to cooldown so the dispatcher routes through the
// fast arm instead of the adaptive tick. Mirrors what
// _Py_Specialize_Send does once it has picked SEND_GEN.
//
// CPython: Python/specialize.c:2964 _Py_Specialize_Send writes the
// opcode and ResetCounter() into the cache cells.
func stampSendGen(co *objects.Code, instr int) {
	specialize.SetOpcode(co.Code, instr, compile.SEND_GEN)
	specialize.StoreCounter(co.Code, instr, specialize.AdaptiveCounterCooldown())
}

// genYieldOnce builds a generator that yields v once on the first
// send(None), then signals StopIteration. The body runs on its own
// goroutine driven by yieldCh / sendCh, matching the structure
// execReturnGenerator produces at runtime.
func genYieldOnce(v objects.Object) *objects.Generator {
	g := objects.NewGenerator("once", "once")
	go func() {
		first := <-g.SendCh
		if first.Err != nil {
			g.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
			return
		}
		g.YieldCh <- objects.GenMsg{Val: v}
		<-g.SendCh
		g.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
	}()
	return g
}

// genStopImmediately builds a generator that signals StopIteration on
// the first send. Used to drive the StopIteration path of SEND_GEN.
func genStopImmediately() *objects.Generator {
	g := objects.NewGenerator("done", "done")
	go func() {
		<-g.SendCh
		g.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
	}()
	return g
}

// sendHitProgram drives one SEND on a generator that yields a value.
//
//	idx 0  LOAD_CONST 0   ; push generator
//	idx 1  LOAD_CONST 1   ; push None (the value sent)
//	idx 2  SEND 0         ; SEND_GEN by stamp; on hit pushes yielded val
//	idx 3  (cache cell, owned by SEND)
//	idx 4  RETURN_VALUE   ; returns TOS (the yielded value)
//
// The stack on return is [..., receiver, retval]; RETURN_VALUE picks
// up TOS regardless of items beneath it.
func sendHitProgram(gen objects.Object) *objects.Code {
	return &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.SEND, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{gen, objects.None()},
		Stacksize: 4,
	}
}

// sendStopProgram drives one SEND on a generator that signals
// StopIteration immediately. The SEND oparg of 2 makes the fast arm
// jump cacheAdvance(SEND)+2*2 = 4 codeunits past idx 2's cache cell,
// landing on idx 6 (the "took the exhaustion branch" sentinel).
//
//	idx 0  LOAD_CONST 0   ; push generator
//	idx 1  LOAD_CONST 1   ; push None
//	idx 2  SEND 2         ; SEND_GEN by stamp; oparg=2 jumps to idx 6
//	idx 3  (cache cell)
//	idx 4  LOAD_CONST 2   ; "yielded" sentinel (unreached)
//	idx 5  RETURN_VALUE 0
//	idx 6  LOAD_CONST 3   ; "exhausted" sentinel
//	idx 7  RETURN_VALUE 0
//
// CPython lands the jump on END_SEND. The test uses LOAD_CONST +
// RETURN_VALUE in place so the sentinel surfaces without needing the
// END_SEND opcode (which has its own arm that requires the right
// stack shape).
func sendStopProgram(gen objects.Object) *objects.Code {
	return &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.SEND, 2),
			instr(compile.LOAD_CONST, 2),
			instr(compile.RETURN_VALUE, 0),
			instr(compile.LOAD_CONST, 3),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{gen, objects.None(), objects.NewInt(99), objects.NewInt(-1)},
		Stacksize: 4,
	}
}

// TestFastSendGenHit drives the fast arm against a generator that
// yields 11. The fast path advances by cacheAdvance(SEND), leaving
// [..., gen, 11] on the stack, and RETURN_VALUE returns the yielded
// value.
//
// CPython: Python/bytecodes.c:1364 SEND_GEN macro (hit path)
func TestFastSendGenHit(t *testing.T) {
	gen := genYieldOnce(objects.NewInt(11))
	co := sendHitProgram(gen)
	specialize.Enable(co)
	stampSendGen(co, 2)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	if got != 11 {
		t.Errorf("got %d, want 11", got)
	}
	if op := compile.Opcode(co.Code[2*2]); op != compile.SEND_GEN {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastSendGenStopIteration drives the fast arm against a generator
// that signals StopIteration on the first send. The fast arm pushes
// None and jumps oparg codeunits past the post-SEND cache cell,
// landing on the exhaustion-branch sentinel (-1).
//
// CPython: Python/bytecodes.c _SEND (StopIteration path)
func TestFastSendGenStopIteration(t *testing.T) {
	gen := genStopImmediately()
	co := sendStopProgram(gen)
	specialize.Enable(co)
	stampSendGen(co, 2)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	if got != -1 {
		t.Errorf("got %d, want -1 (exhaustion branch)", got)
	}
	if op := compile.Opcode(co.Code[2*2]); op != compile.SEND_GEN {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastSendGenDeoptsOnWrongType plants a non-generator receiver
// under a SEND_GEN stamp. The type assertion fails (ok=false), the
// dispatcher rewrites the bytecode back to SEND, and the generic body
// (execSend) routes the call to tp_iternext on the list iterator's
// type, surfacing the first item.
func TestFastSendGenDeoptsOnWrongType(t *testing.T) {
	// iter(list) is not a Generator/Coroutine; the SEND_GEN guard
	// must miss, deopt to SEND, and let the generic body call
	// tp_iternext (which v == None triggers).
	it, err := objects.Iter(objects.NewList([]objects.Object{objects.NewInt(7)}))
	if err != nil {
		t.Fatalf("Iter(list): %v", err)
	}
	co := sendHitProgram(it)
	specialize.Enable(co)
	stampSendGen(co, 2)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	if got != 7 {
		t.Errorf("got %d, want 7 (slow-path tp_iternext)", got)
	}
	if op := compile.Opcode(co.Code[2*2]); op == compile.SEND_GEN {
		t.Errorf("guard miss should have deopted, but op stayed SEND_GEN")
	}
}

// TestFastSendGenCoroutine confirms the SEND_GEN guard accepts a
// *Coroutine receiver in addition to *Generator. The specializer
// stamps SEND_GEN for both (specialize/send.go:25), so the fast arm
// must accept both.
func TestFastSendGenCoroutine(t *testing.T) {
	c := objects.NewCoroutine("c")
	go func() {
		first := <-c.SendCh
		if first.Err != nil {
			c.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
			return
		}
		c.YieldCh <- objects.GenMsg{Val: objects.NewInt(42)}
		<-c.SendCh
		c.YieldCh <- objects.GenMsg{Err: objects.ErrStopIteration}
	}()
	co := sendHitProgram(c)
	specialize.Enable(co)
	stampSendGen(co, 2)

	ts := state.NewThread()
	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	got, ok := v.(*objects.Int).Int64()
	if !ok {
		t.Fatalf("result not int64-representable: %v", v)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if op := compile.Opcode(co.Code[2*2]); op != compile.SEND_GEN {
		t.Errorf("fast path should not deopt: op=%s", op.Name())
	}
}

// TestFastSendGenSurfacesError ensures non-StopIteration errors from
// the generator surface through the fast arm. The body sends a
// RuntimeError sentinel that must propagate as a Go error rather than
// triggering the jump branch.
func TestFastSendGenSurfacesError(t *testing.T) {
	want := errors.New("boom")
	g := objects.NewGenerator("err", "err")
	go func() {
		<-g.SendCh
		g.YieldCh <- objects.GenMsg{Err: want}
	}()
	co := sendHitProgram(g)
	specialize.Enable(co)
	stampSendGen(co, 2)

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("EvalCode: want error, got nil")
	}
	if !errors.Is(err, want) {
		t.Fatalf("EvalCode err=%v, want chain containing %v", err, want)
	}
}
