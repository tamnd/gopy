package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// TestOptimizeRunsPanel: the public Optimize entry runs the panel
// end-to-end and produces a sane stack depth on a trivial sequence.
func TestOptimizeRunsPanel(t *testing.T) {
	seq := &Sequence{}
	seq.Addop(RESUME, 0, ast.Pos{Lineno: 1})
	seq.Addop(LOAD_CONST, 0, ast.Pos{Lineno: 1})
	seq.Addop(RETURN_VALUE, 0, ast.Pos{Lineno: 1})
	seq.Addop(NOP, 0, ast.Pos{Lineno: 1}) // dead
	consts := []any{nil}
	info, err := Optimize(seq, &consts, 0, 0)
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if info.MaxStackDepth < 1 {
		t.Errorf("MaxStackDepth = %d, want >= 1", info.MaxStackDepth)
	}
	// Tail NOP should be gone after the panel.
	for _, ins := range seq.Instrs {
		if ins.Op == NOP {
			t.Errorf("residual NOP after Optimize: %+v", seq.Instrs)
			break
		}
	}
}
