package main

import (
	"strings"
	"testing"
)

func TestPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"NOP", "Nop"},
		{"LOAD_FAST_0", "LoadFast0"},
		{"BINARY_OP_ADD_INT", "BinaryOpAddInt"},
		{"_LEADING", "Leading"},
	}
	for _, c := range cases {
		if got := pascal(strings.TrimPrefix(c.in, "_")); got != c.want {
			t.Errorf("pascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEmitIDsFile_NumericAndAlias(t *testing.T) {
	ids := []UopID{
		{Name: "EXIT_TRACE", Value: 300},
		{Name: "NOP", IsAlias: true, Alias: "NOP"},
	}
	out := EmitIDsFile("optimizer", "abc123", ids, 516)
	if !strings.Contains(out, "// uop header sha256: abc123") {
		t.Error("missing drift marker")
	}
	if !strings.Contains(out, "UopExitTrace uint16 = 300") {
		t.Error("missing numeric uop")
	}
	if !strings.Contains(out, "UopNop uint16 = uint16(compile.NOP)") {
		t.Error("missing alias uop")
	}
	if !strings.Contains(out, "MaxUopID uint16 = 516") {
		t.Error("missing MaxUopID")
	}
	if !strings.Contains(out, `UopExitTrace: "_EXIT_TRACE"`) {
		t.Error("missing UopNames row")
	}
}

func TestEmitMetaFile_FlagsRepPopped(t *testing.T) {
	meta := map[string]*UopMeta{
		"NOP":         {Name: "NOP", Flags: []string{"PURE"}, HasPopped: true, Popped: "0"},
		"LOAD_FAST":   {Name: "LOAD_FAST", Flags: []string{"ARG", "LOCAL", "PURE"}, Replication: 8, HasPopped: true, Popped: "0"},
		"BUILD_TUPLE": {Name: "BUILD_TUPLE", HasPopped: true, Popped: "oparg"},
	}
	out := EmitMetaFile("optimizer", "deadbeef", meta)
	if !strings.Contains(out, `"_LOAD_FAST": {Flags: FlagArg | FlagLocal | FlagPure, Replication: 8, Popped: 0}`) {
		t.Errorf("LOAD_FAST row missing or wrong\n%s", out)
	}
	if !strings.Contains(out, `"_BUILD_TUPLE": {Flags: 0, Replication: 0, Popped: -1}`) {
		t.Errorf("BUILD_TUPLE oparg-dependent row missing\n%s", out)
	}
}
