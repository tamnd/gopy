package main

import "testing"

func TestBuildFamilyMapMapsVariantsToBase(t *testing.T) {
	defs := []Def{
		&FamilyDef{Name: "BINARY_OP", Members: []string{"BINARY_OP_ADD_INT", "BINARY_OP_ADD_STR"}},
	}
	fm := BuildFamilyMap(defs)
	if fm["BINARY_OP_ADD_INT"] != "BINARY_OP" || fm["BINARY_OP_ADD_STR"] != "BINARY_OP" {
		t.Errorf("variants not mapped: %+v", fm)
	}
	if _, ok := fm["BINARY_OP"]; ok {
		t.Errorf("base should not appear as variant key")
	}
}

func TestBuildFamilyMapSkipsBaseInMembers(t *testing.T) {
	defs := []Def{&FamilyDef{Name: "LOAD_CONST", Members: []string{"LOAD_CONST", "LOAD_CONST_MORTAL"}}}
	fm := BuildFamilyMap(defs)
	if _, ok := fm["LOAD_CONST"]; ok {
		t.Errorf("base listed in members should not appear as variant: %+v", fm)
	}
	if fm["LOAD_CONST_MORTAL"] != "LOAD_CONST" {
		t.Errorf("variant not mapped: %+v", fm)
	}
}

func TestResolveBase(t *testing.T) {
	fm := FamilyMap{"LOAD_FAST_CHECK": "LOAD_FAST"}
	if got := fm.ResolveBase("LOAD_FAST_CHECK"); got != "LOAD_FAST" {
		t.Errorf("variant got %q want LOAD_FAST", got)
	}
	if got := fm.ResolveBase("LOAD_FAST"); got != "LOAD_FAST" {
		t.Errorf("base got %q want self", got)
	}
	if got := fm.ResolveBase("UNRELATED"); got != "UNRELATED" {
		t.Errorf("unrelated got %q want self", got)
	}
}
