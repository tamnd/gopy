package main

import (
	"strings"
	"testing"
)

func TestEmitTier1ArmFixed(t *testing.T) {
	inst := mkInst("BINARY_ADD", []string{"a", "b"}, []string{"c"}, "")
	a, err := AnalyzeInst(inst)
	if err != nil {
		t.Fatal(err)
	}
	out := EmitTier1Arm(a)
	for _, want := range []string{
		"case compile.BINARY_ADD:",
		"a := e.peek(1)",
		"b := e.peek(0)",
		"body pending (B6)",
		"// outputs: c",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("arm missing %q\n--- arm ---\n%s", want, out)
		}
	}
}

func TestEmitTier1ArmVariadic(t *testing.T) {
	inst := &InstDef{
		Kind: "inst", Name: "BUILD_LIST",
		Inputs:  []Input{{Stack: &StackEffect{Name: "items", Size: "oparg"}}},
		Outputs: []Output{{Stack: &StackEffect{Name: "lst"}}},
	}
	a, err := AnalyzeInst(inst)
	if err != nil {
		t.Fatal(err)
	}
	out := EmitTier1Arm(a)
	// BUILD_LIST has an empty body in the helper, so the translator
	// bails. The arm still has to render the bail stub correctly,
	// including the variadic stack note in the outputs comment.
	for _, want := range []string{
		"case compile.BUILD_LIST:",
		"// outputs: lst",
		"opcodeNotImplemented(op)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("variadic arm missing %q:\n%s", want, out)
		}
	}
}

func TestEmitTier1FileShape(t *testing.T) {
	a := &SignatureAnalysis{
		Name:    "NOP",
		Outputs: nil,
		Inputs:  nil,
	}
	src := EmitTier1File("vm", "deadbeef", []*SignatureAnalysis{a}, nil)
	for _, want := range []string{
		"DO NOT EDIT",
		"// bytecodes.c sha256: deadbeef",
		"package vm",
		"func (e *evalState) dispatchGen(",
		"case compile.NOP:",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("file missing %q\n--- file ---\n%s", want, src)
		}
	}
}
