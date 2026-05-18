package compile

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/parser"
)

func TestCompileWithCfgPhaseHookFiresEveryUnitAndPhase(t *testing.T) {
	src := "def f(x):\n    return x + 1\n\nf(2)\n"
	mod, err := parser.ParseString(src, "<test>", parser.ModeFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type key struct{ unit, phase string }
	seen := map[key]int{}
	var firstDump string
	hook := func(unit, phase, dump string) {
		seen[key{unit, phase}]++
		if firstDump == "" {
			firstDump = dump
		}
	}
	if err := CompileWithCfgPhaseHook(mod, "<test>", 0, hook); err != nil {
		t.Fatalf("CompileWithCfgPhaseHook: %v", err)
	}
	wantPhases := []string{
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
	units := map[string]bool{}
	for k := range seen {
		units[k.unit] = true
	}
	if len(units) < 2 {
		t.Fatalf("expected at least 2 units (module + f), got %d: %v", len(units), units)
	}
	for unit := range units {
		for _, phase := range wantPhases {
			if seen[key{unit, phase}] != 1 {
				t.Errorf("unit=%q phase=%q fire count = %d, want 1", unit, phase, seen[key{unit, phase}])
			}
		}
	}
	if !strings.Contains(firstDump, "[EH=") {
		t.Errorf("first dump should include block header, got:\n%s", firstDump)
	}
}

func TestCompileWithCfgPhaseHookRequiresHook(t *testing.T) {
	src := "x = 1\n"
	mod, err := parser.ParseString(src, "<test>", parser.ModeFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := CompileWithCfgPhaseHook(mod, "<test>", 0, nil); err == nil {
		t.Fatal("nil hook should return an error")
	}
}
