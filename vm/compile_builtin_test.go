// End-to-end check that compile() in the builtins panel produces a
// code object the vm can actually execute. The builtins package can't
// depend on vm (cycle), so this lives here.

package vm

import (
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func TestCompileBuiltinExecutesProducedCode(t *testing.T) {
	g, err := builtins.Init(nil)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	out, err := builtins.Compile([]objects.Object{
		objects.NewStr("y = 41 + 1\n"),
		objects.NewStr("<test>"),
		objects.NewStr("exec"),
	}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	co, ok := out.(*objects.Code)
	if !ok {
		t.Fatalf("compile returned %T, want *objects.Code", out)
	}
	ts := state.NewThread()
	if _, err := EvalCode(ts, co, g, nil); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, err := g.GetItem(objects.NewStr("y"))
	if err != nil {
		t.Fatalf("globals[y]: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 42 {
		t.Fatalf("y = %d, want 42", n)
	}
}
