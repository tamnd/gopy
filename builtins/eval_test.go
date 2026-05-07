// Coverage for the eval() and exec() builtins. Argument routing and
// type validation live in this package; the actual eval pipeline
// (compile + EvalCode) gets exercised in vm/eval_builtin_test.go where
// the real evaluator hook is installed.

package builtins

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// captureEvaluator stashes the args eval/exec hand to the evaluator
// hook so a test can inspect them without spinning up the vm. The
// returned closure also lets the test inject the value the hook
// pretends to produce.
func captureEvaluator(t *testing.T, ret objects.Object, retErr error) *evalCall {
	t.Helper()
	prev := currentEvaluator
	t.Cleanup(func() { SetEvaluator(prev) })

	got := &evalCall{}
	SetEvaluator(func(code *objects.Code, globals, locals objects.Object) (objects.Object, error) {
		got.code = code
		got.globals = globals
		got.locals = locals
		return ret, retErr
	})
	return got
}

type evalCall struct {
	code    *objects.Code
	globals objects.Object
	locals  objects.Object
}

func TestEvalCompilesStringInExpressionMode(t *testing.T) {
	got := captureEvaluator(t, objects.NewInt(42), nil)
	g := objects.NewDict()
	out, err := Eval([]objects.Object{
		objects.NewStr("1 + 2"),
		g,
	}, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.code == nil {
		t.Fatalf("evaluator hook saw no code object")
	}
	if got.globals != g {
		t.Fatalf("globals = %v, want injected dict", got.globals)
	}
	if got.locals != g {
		t.Fatalf("locals defaulted to %v, want globals (%v)", got.locals, g)
	}
	n, _ := out.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("eval returned %d, want 42 (from hook)", n)
	}
}

func TestEvalAcceptsCodeObject(t *testing.T) {
	got := captureEvaluator(t, objects.None(), nil)
	co := &objects.Code{}
	g := objects.NewDict()
	if _, err := Eval([]objects.Object{co, g}, nil); err != nil {
		t.Fatalf("eval(code): %v", err)
	}
	if got.code != co {
		t.Fatalf("evaluator saw code %v, want injected %v", got.code, co)
	}
}

func TestEvalLocalsKwargOverride(t *testing.T) {
	got := captureEvaluator(t, objects.None(), nil)
	g := objects.NewDict()
	l := objects.NewDict()
	_, err := Eval(
		[]objects.Object{objects.NewStr("1")},
		map[string]objects.Object{
			"globals": g,
			"locals":  l,
		},
	)
	if err != nil {
		t.Fatalf("eval kwargs: %v", err)
	}
	if got.globals != g {
		t.Fatalf("globals = %v, want %v", got.globals, g)
	}
	if got.locals != l {
		t.Fatalf("locals = %v, want %v", got.locals, l)
	}
}

func TestEvalMissingSourceTypeError(t *testing.T) {
	captureEvaluator(t, nil, nil)
	_, err := Eval([]objects.Object{}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError", err)
	}
}

func TestEvalGlobalsMustBeDict(t *testing.T) {
	captureEvaluator(t, nil, nil)
	_, err := Eval([]objects.Object{
		objects.NewStr("1"),
		objects.NewInt(1),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on non-dict globals", err)
	}
}

func TestEvalSourceMustBeStrOrCode(t *testing.T) {
	captureEvaluator(t, nil, nil)
	_, err := Eval([]objects.Object{
		objects.NewInt(1),
		objects.NewDict(),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError on non-str source", err)
	}
}

func TestEvalNoFrameNoGlobals(t *testing.T) {
	captureEvaluator(t, nil, nil)
	prevScope := currentScope
	t.Cleanup(func() { SetCurrentScope(prevScope) })
	SetCurrentScope(func() (objects.Object, objects.Object) { return nil, nil })

	_, err := Eval([]objects.Object{objects.NewStr("1")}, nil)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError when no frame and no globals", err)
	}
}

func TestExecReturnsNoneOnSuccess(t *testing.T) {
	captureEvaluator(t, objects.NewInt(99), nil)
	g := objects.NewDict()
	out, err := Exec([]objects.Object{
		objects.NewStr("x = 1\n"),
		g,
	}, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !objects.IsNone(out) {
		t.Fatalf("exec returned %v, want None", out)
	}
}

func TestExecCompilesStringInFileMode(t *testing.T) {
	got := captureEvaluator(t, objects.None(), nil)
	g := objects.NewDict()
	if _, err := Exec([]objects.Object{
		objects.NewStr("y = 1\nz = 2\n"),
		g,
	}, nil); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got.code == nil {
		t.Fatalf("evaluator hook saw no code")
	}
}

func TestExecUnknownKwarg(t *testing.T) {
	captureEvaluator(t, objects.None(), nil)
	_, err := Exec(
		[]objects.Object{objects.NewStr("x = 1\n")},
		map[string]objects.Object{"closure": objects.None()},
	)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("err = %v, want TypeError for unknown kwarg", err)
	}
}
