package main

import "testing"

func TestExpandMacroLinearComposition(t *testing.T) {
	// op A: (a -- x), op B: (x -- y). macro M = A + B should be (a -- y).
	a := &InstDef{
		Kind: "op", Name: "A",
		Inputs:  []Input{{Stack: &StackEffect{Name: "a"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "x"}}},
	}
	b := &InstDef{
		Kind: "op", Name: "B",
		Inputs:  []Input{{Stack: &StackEffect{Name: "x"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "y"}}},
	}
	m := &MacroDef{Name: "M", UOps: []UOp{{Name: "A"}, {Name: "B"}}}
	defs := &Defs{Insts: []*InstDef{a, b}, Macros: []*MacroDef{m}}

	got, err := ExpandMacros(defs)
	if err != nil {
		t.Fatal(err)
	}
	// No real inst, just M.
	if len(got) != 1 || got[0].Name != "M" {
		t.Fatalf("got %v, want one inst named M", got)
	}
	exp := got[0]
	if len(exp.Inputs) != 1 || exp.Inputs[0].Stack.Name != "a" {
		t.Errorf("expanded inputs %+v, want [a]", exp.Inputs)
	}
	if len(exp.Outputs) != 1 || exp.Outputs[0].Stack.Name != "y" {
		t.Errorf("expanded outputs %+v, want [y]", exp.Outputs)
	}
}

func TestExpandMacroExtraInputsLiftToMacroSignature(t *testing.T) {
	// op A: (a, b -- r). macro M = A. M's signature should be (a, b -- r).
	a := &InstDef{
		Kind: "op", Name: "A",
		Inputs: []Input{
			{Stack: &StackEffect{Name: "a"}},
			{Stack: &StackEffect{Name: "b"}},
		},
		Outputs: []Output{{Stack: &StackEffect{Name: "r"}}},
	}
	m := &MacroDef{Name: "M", UOps: []UOp{{Name: "A"}}}
	defs := &Defs{Insts: []*InstDef{a}, Macros: []*MacroDef{m}}

	got, err := ExpandMacros(defs)
	if err != nil {
		t.Fatal(err)
	}
	exp := got[0]
	if len(exp.Inputs) != 2 || exp.Inputs[0].Stack.Name != "a" || exp.Inputs[1].Stack.Name != "b" {
		t.Errorf("inputs %+v want [a b]", exp.Inputs)
	}
}

func TestExpandMacroCacheBetweenOps(t *testing.T) {
	a := &InstDef{
		Kind: "op", Name: "A",
		Outputs: []Output{{Stack: &StackEffect{Name: "v"}}},
	}
	b := &InstDef{
		Kind: "op", Name: "B",
		Inputs: []Input{{Stack: &StackEffect{Name: "v"}}},
	}
	m := &MacroDef{Name: "M", UOps: []UOp{
		{Name: "A"},
		{Cache: &CacheEffect{Name: "version", Size: 2}},
		{Name: "B"},
	}}
	defs := &Defs{Insts: []*InstDef{a, b}, Macros: []*MacroDef{m}}

	got, err := ExpandMacros(defs)
	if err != nil {
		t.Fatal(err)
	}
	exp := got[0]
	var cacheCount int
	for _, in := range exp.Inputs {
		if in.Cache != nil {
			cacheCount++
		}
	}
	if cacheCount != 1 {
		t.Errorf("cache count %d, want 1", cacheCount)
	}
}

func TestExpandMacrosKeepsRealInsts(t *testing.T) {
	realInst := mkInst("REAL", []string{"x"}, []string{"y"}, "")
	frag := &InstDef{
		Kind: "op", Name: "FRAG",
		Inputs:  []Input{{Stack: &StackEffect{Name: "x"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "y"}}},
	}
	mac := &MacroDef{Name: "MAC", UOps: []UOp{{Name: "FRAG"}}}
	defs := &Defs{Insts: []*InstDef{realInst, frag}, Macros: []*MacroDef{mac}}

	got, err := ExpandMacros(defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "REAL" || got[1].Name != "MAC" {
		t.Errorf("got %v, want [REAL, MAC]", got)
	}
}

func TestExpandMacroUnknownOp(t *testing.T) {
	mac := &MacroDef{Name: "BAD", UOps: []UOp{{Name: "MISSING"}}}
	defs := &Defs{Macros: []*MacroDef{mac}}
	if _, err := ExpandMacros(defs); err == nil {
		t.Error("expected error for missing op")
	}
}
