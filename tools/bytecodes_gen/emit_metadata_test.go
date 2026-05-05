package main

import (
	"strings"
	"testing"
)

// mkInst is a small helper for building an InstDef in tests without
// going through the parser. Inputs/outputs are just slot names with no
// type or size annotation.
func mkInst(name string, ins, outs []string, body string) *InstDef {
	d := &InstDef{Kind: "inst", Name: name}
	for _, n := range ins {
		d.Inputs = append(d.Inputs, Input{Stack: &StackEffect{Name: n}})
	}
	for _, n := range outs {
		d.Outputs = append(d.Outputs, Output{Stack: &StackEffect{Name: n}})
	}
	for _, t := range strings.Fields(body) {
		d.Body = append(d.Body, dslTok{Kind: tokIdentifier, Text: t})
	}
	return d
}

func TestStackCountsFixed(t *testing.T) {
	d := mkInst("ADD", []string{"a", "b"}, []string{"c"}, "")
	pops, pushes := stackCounts(d.Inputs, d.Outputs)
	if pops != 2 || pushes != 1 {
		t.Errorf("got pops=%d pushes=%d, want 2/1", pops, pushes)
	}
}

func TestStackCountsVariadic(t *testing.T) {
	d := &InstDef{
		Kind: "inst", Name: "BUILD_LIST",
		Inputs:  []Input{{Stack: &StackEffect{Name: "items", Size: "oparg"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "lst"}}},
	}
	pops, pushes := stackCounts(d.Inputs, d.Outputs)
	if pops != -1 || pushes != 1 {
		t.Errorf("got pops=%d pushes=%d, want -1/1", pops, pushes)
	}
}

func TestCacheCount(t *testing.T) {
	in := []Input{
		{Cache: &CacheEffect{Name: "version", Size: 2}},
		{Cache: &CacheEffect{Name: "func", Size: 4}},
	}
	if got := cacheCount(in); got != 6 {
		t.Errorf("cacheCount = %d, want 6", got)
	}
}

func TestHasOpargPositive(t *testing.T) {
	d := mkInst("LOAD_CONST", nil, []string{"v"}, "v = consts [ oparg ] ;")
	if !hasOparg(d.Body) {
		t.Error("hasOparg should be true when body mentions oparg")
	}
}

func TestHasOpargNegative(t *testing.T) {
	d := mkInst("NOP", nil, nil, "/* nothing */")
	if hasOparg(d.Body) {
		t.Error("hasOparg should be false when body doesn't mention oparg")
	}
}

func TestCollectMetadataSkipsOpFragments(t *testing.T) {
	defs := []Def{
		mkInst("CONCRETE", []string{"a"}, []string{"b"}, "use oparg"),
		&InstDef{Kind: "op", Name: "_FRAGMENT", Inputs: nil, Outputs: nil},
	}
	got := CollectMetadata(defs)
	if len(got) != 1 || got[0].Name != "CONCRETE" {
		t.Fatalf("got %v, want one entry for CONCRETE", got)
	}
	if !got[0].HasOparg || got[0].Pops != 1 || got[0].Pushes != 1 {
		t.Errorf("entry shape wrong: %+v", got[0])
	}
}

func TestCollectMetadataAttachesFamily(t *testing.T) {
	defs := []Def{
		mkInst("LOAD_FAST", nil, []string{"v"}, ""),
		&FamilyDef{Name: "LOAD_FAST_FAMILY", Members: []string{"LOAD_FAST", "LOAD_FAST_CHECK"}},
	}
	got := CollectMetadata(defs)
	if len(got) != 1 || got[0].Family != "LOAD_FAST_FAMILY" {
		t.Errorf("family not attached: %+v", got)
	}
}

func TestEmitMetadataFile(t *testing.T) {
	entries := []MetadataEntry{
		{Name: "NOP", Pushes: 0, Pops: 0, CacheSize: 0, HasOparg: false, Family: ""},
		{Name: "BUILD_LIST", Pushes: 1, Pops: -1, CacheSize: 0, HasOparg: true, Family: ""},
	}
	src := EmitMetadataFile("compile", "deadbeef", entries)
	for _, want := range []string{
		"DO NOT EDIT",
		"// bytecodes.c sha256: deadbeef",
		"package compile",
		`{"NOP", 0, 0, 0, false, ""}`,
		`{"BUILD_LIST", 1, -1, 0, true, ""}`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, src)
		}
	}
}
