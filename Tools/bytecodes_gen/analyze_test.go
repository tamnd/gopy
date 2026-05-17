package main

import "testing"

func TestAnalyzeInstFixed(t *testing.T) {
	inst := mkInst("BINARY_ADD", []string{"a", "b"}, []string{"c"}, "c = a + b")
	a, err := AnalyzeInst(inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Inputs) != 2 || a.Inputs[0].Name != "a" || a.Inputs[1].Name != "b" {
		t.Errorf("inputs wrong: %+v", a.Inputs)
	}
	if len(a.Outputs) != 1 || a.Outputs[0].Name != "c" {
		t.Errorf("outputs wrong: %+v", a.Outputs)
	}
	if a.Variadic {
		t.Error("Variadic should be false")
	}
}

func TestAnalyzeInstVariadic(t *testing.T) {
	inst := &InstDef{
		Kind: "inst", Name: "BUILD_LIST",
		Inputs:  []Input{{Stack: &StackEffect{Name: "items", Size: "oparg"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "lst"}}},
	}
	a, err := AnalyzeInst(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Variadic {
		t.Error("Variadic should be true with sized input")
	}
	if a.Inputs[0].SizeExpr != "oparg" {
		t.Errorf("SizeExpr = %q, want oparg", a.Inputs[0].SizeExpr)
	}
}

func TestAnalyzeInstCacheOffsets(t *testing.T) {
	inst := &InstDef{
		Kind: "inst", Name: "LOAD_ATTR",
		Inputs: []Input{
			{Stack: &StackEffect{Name: "owner"}},
			{Cache: &CacheEffect{Name: "version", Size: 2}},
			{Cache: &CacheEffect{Name: "func", Size: 4}},
		},
		Outputs: []Output{{Stack: &StackEffect{Name: "attr"}}},
	}
	a, err := AnalyzeInst(inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Caches) != 2 {
		t.Fatalf("got %d caches, want 2", len(a.Caches))
	}
	if a.Caches[0].Offset != 0 || a.Caches[1].Offset != 2 {
		t.Errorf("cache offsets %d/%d, want 0/2", a.Caches[0].Offset, a.Caches[1].Offset)
	}
}

func TestAnalyzeInstDuplicateOutputName(t *testing.T) {
	inst := &InstDef{
		Kind: "inst", Name: "BAD",
		Outputs: []Output{
			{Stack: &StackEffect{Name: "x"}},
			{Stack: &StackEffect{Name: "x"}},
		},
	}
	if _, err := AnalyzeInst(inst); err == nil {
		t.Error("expected error for duplicate output name")
	}
}

func TestAnalyzeAllSkipsOpFragments(t *testing.T) {
	defs := []Def{
		mkInst("REAL", []string{"a"}, []string{"b"}, ""),
		&InstDef{Kind: "op", Name: "FRAG"},
		mkInst("REAL2", nil, []string{"c"}, ""),
	}
	got, err := AnalyzeAll(defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "REAL" || got[1].Name != "REAL2" {
		t.Errorf("got %v, want [REAL, REAL2]", got)
	}
}
