// Tests for GET_AWAITABLE / GET_AITER / GET_ANEXT / END_ASYNC_FOR.
// The opcodes were stubs through v0.9; v0.10 fills in the dispatch
// against the Coroutine and AsyncGenerator types and routes
// StopAsyncIteration through END_ASYNC_FOR.
//
// CPython: Python/bytecodes.c GET_AITER, GET_ANEXT, GET_AWAITABLE, END_ASYNC_FOR

package vm

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// TestGetAwaitableCoroutineReturnsSelf mirrors _PyCoro_GetAwaitableIter:
// a coroutine is its own awaitable iterator, no wrapper.
func TestGetAwaitableCoroutineReturnsSelf(t *testing.T) {
	ts := state.NewThread()
	co := objects.NewCoroutine("c")
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_AWAITABLE, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{co},
		Stacksize: 4,
	}
	got, err := EvalCode(ts, c, nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != co {
		t.Fatalf("GET_AWAITABLE returned %v, want the coroutine itself", got)
	}
}

// TestGetAwaitableNonAwaitableTypeError ensures non-awaitable values
// surface a TypeError instead of being silently coerced.
func TestGetAwaitableNonAwaitableTypeError(t *testing.T) {
	ts := state.NewThread()
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_AWAITABLE, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(7)},
		Stacksize: 4,
	}
	_, err := EvalCode(ts, c, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "await") {
		t.Fatalf("err=%v, want TypeError mentioning await", err)
	}
}

// TestGetAiterAsyncGenerator drives the GET_AITER opcode on an async
// generator: the type's am_aiter slot returns the generator itself.
func TestGetAiterAsyncGenerator(t *testing.T) {
	ts := state.NewThread()
	ag := objects.NewAsyncGenerator("g")
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_AITER, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{ag},
		Stacksize: 4,
	}
	got, err := EvalCode(ts, c, nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != ag {
		t.Fatalf("GET_AITER returned %v, want the async generator", got)
	}
}

// TestGetAiterNonAsyncTypeError ensures plain iterables don't satisfy
// the async-for protocol.
func TestGetAiterNonAsyncTypeError(t *testing.T) {
	ts := state.NewThread()
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_AITER, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{int64(0)},
		Stacksize: 4,
	}
	_, err := EvalCode(ts, c, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "__aiter__") {
		t.Fatalf("err=%v, want TypeError mentioning __aiter__", err)
	}
}

// TestGetAnextAsyncGenerator drives GET_ANEXT and verifies the
// awaitable left on the stack is the AsyncGenASend wrapper from
// async_gen_anext.
func TestGetAnextAsyncGenerator(t *testing.T) {
	ts := state.NewThread()
	ag := objects.NewAsyncGenerator("g")
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.GET_ANEXT, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{ag},
		Stacksize: 4,
	}
	got, err := EvalCode(ts, c, nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Type() != objects.AsyncGenASendType {
		t.Fatalf("GET_ANEXT type = %s, want async_generator_asend", got.Type().Name)
	}
}

// TestEndAsyncForSwallowsStopAsyncIteration: the loop ends cleanly
// when the body raised StopAsyncIteration. The opcode pops both the
// awaitable and the exception, leaves nothing behind.
func TestEndAsyncForSwallowsStopAsyncIteration(t *testing.T) {
	ts := state.NewThread()
	stop := pyerrors.New(pyerrors.PyExc_StopAsyncIteration, nil)
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0), // awaitable (placeholder)
			instr(compile.LOAD_CONST, 1), // StopAsyncIteration
			instr(compile.END_ASYNC_FOR, 0),
			instr(compile.LOAD_CONST, 2),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.None(), stop, int64(99)},
		Stacksize: 4,
	}
	got, err := EvalCode(ts, c, nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	iv, ok := got.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", got)
	}
	if n, _ := iv.Int64(); n != 99 {
		t.Fatalf("result = %d, want 99 (loop should have continued past END_ASYNC_FOR)", n)
	}
}

// TestEndAsyncForReraisesOtherException: any exception other than
// StopAsyncIteration must surface back out of the opcode.
func TestEndAsyncForReraisesOtherException(t *testing.T) {
	ts := state.NewThread()
	exc := pyerrors.New(pyerrors.PyExc_ValueError, nil)
	c := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.LOAD_CONST, 1),
			instr(compile.END_ASYNC_FOR, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{objects.None(), exc},
		Stacksize: 4,
	}
	_, err := EvalCode(ts, c, nil, nil)
	if err == nil {
		t.Fatalf("expected re-raise, got nil error")
	}
}
