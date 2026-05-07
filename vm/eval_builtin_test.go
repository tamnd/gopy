// End-to-end check that builtins.eval() and builtins.exec() drive the
// real vm evaluator wired through builtins.SetEvaluator.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
)

func TestEvalBuiltinComputesExpression(t *testing.T) {
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	out, err := builtins.Eval([]objects.Object{
		objects.NewStr("3 * 14"),
		g,
	}, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	n, _ := out.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("eval result = %d, want 42", n)
	}
}

func TestExecBuiltinMutatesGlobals(t *testing.T) {
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if _, err := builtins.Exec([]objects.Object{
		objects.NewStr("answer = 6 * 7\n"),
		g,
	}, nil); err != nil {
		t.Fatalf("exec: %v", err)
	}
	v, err := g.GetItem(objects.NewStr("answer"))
	if err != nil {
		t.Fatalf("globals[answer]: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("answer = %d, want 42", n)
	}
}

func TestEvalBuiltinAcceptsCompiledCode(t *testing.T) {
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	co, err := builtins.Compile([]objects.Object{
		objects.NewStr("21 + 21"),
		objects.NewStr("<test>"),
		objects.NewStr("eval"),
	}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := builtins.Eval([]objects.Object{co, g}, nil)
	if err != nil {
		t.Fatalf("eval(code): %v", err)
	}
	n, _ := out.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("eval(code) = %d, want 42", n)
	}
}
