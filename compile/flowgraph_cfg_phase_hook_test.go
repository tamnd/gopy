package compile

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestCfgOptimizeCodeUnitFiresEveryPhase(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)

	consts := []any{int64(1)}
	var seen []string
	hook := func(phase string, _ *cfgBuilder) {
		seen = append(seen, phase)
	}
	if err := cfgOptimizeCodeUnitWithHook(g, &consts, 0, 0, 1, hook); err != nil {
		t.Fatalf("cfgOptimizeCodeUnitWithHook: %v", err)
	}
	want := []string{
		"entry",
		"translate_jump_labels_to_targets",
		"mark_except_handlers",
		"label_exception_targets",
		"optimize_cfg",
		"remove_unused_consts",
		"add_checks_for_loads_of_uninitialized_variables",
		"insert_superinstructions",
		"push_cold_blocks_to_end",
		"resolve_line_numbers",
	}
	if len(seen) != len(want) {
		t.Fatalf("phase count = %d, want %d (seen=%v)", len(seen), len(want), seen)
	}
	for i, p := range want {
		if seen[i] != p {
			t.Fatalf("phase[%d] = %q, want %q (full seen=%v)", i, seen[i], p, seen)
		}
	}
}

func TestCfgPhaseHookCanDumpAtEveryBoundary(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)

	consts := []any{int64(1)}
	var dumps strings.Builder
	hook := func(phase string, g *cfgBuilder) {
		dumps.WriteString(DumpCfg(g, CfgDumpHeader{Phase: phase, FirstLineno: 1}))
		dumps.WriteString("---\n")
	}
	if err := cfgOptimizeCodeUnitWithHook(g, &consts, 0, 0, 1, hook); err != nil {
		t.Fatalf("cfgOptimizeCodeUnitWithHook: %v", err)
	}
	out := dumps.String()
	for _, phase := range []string{
		"translate_jump_labels_to_targets",
		"optimize_cfg",
		"resolve_line_numbers",
	} {
		if !strings.Contains(out, "# cfg dump: "+phase) {
			t.Errorf("missing dump for phase %q in:\n%s", phase, out)
		}
	}
}
