// Pins WITH_EXCEPT_START's exit-function dispatch: the opcode peeks
// the 5-element WITH block on the stack, calls exit_fn(type, val, tb),
// and pushes the result on top while leaving the original frame intact.
//
// CPython: Python/bytecodes.c:3524 WITH_EXCEPT_START

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func TestEvalWithExceptStartDispatchesExitFn(t *testing.T) {
	var got struct {
		called    bool
		excType   objects.Object
		excVal    objects.Object
		traceback objects.Object
	}
	exitFn := objects.NewBuiltinFunction("__exit__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		got.called = true
		if len(args) >= 3 {
			got.excType = args[0]
			got.excVal = args[1]
			got.traceback = args[2]
		}
		return objects.NewStr("exit-result"), nil
	})

	excVal := objects.NewStr("boom")
	ts := state.NewThread()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0), // exit_fn
			instr(compile.LOAD_CONST, 1), // exit_self
			instr(compile.LOAD_CONST, 2), // lasti
			instr(compile.LOAD_CONST, 1), // unused
			instr(compile.LOAD_CONST, 3), // exc_val (TOS)
			instr(compile.WITH_EXCEPT_START, 0),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{exitFn, nil, int64(0), excVal},
		Stacksize: 8,
	}

	v, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode err: %v", err)
	}
	if !got.called {
		t.Fatal("exit_fn was not called")
	}
	if got.traceback != objects.None() {
		t.Errorf("traceback arg = %v, want None", got.traceback)
	}
	if got.excVal != excVal {
		t.Errorf("exc val arg = %v, want the original exc instance", got.excVal)
	}
	if got.excType != excVal.Type() {
		t.Errorf("exc type arg = %v, want type(exc_val) = %v", got.excType, excVal.Type())
	}
	s, sErr := objects.Str(v)
	if sErr != nil {
		t.Fatalf("Str(return): %v", sErr)
	}
	if s != "exit-result" {
		t.Errorf("return value = %q, want %q", s, "exit-result")
	}
}
