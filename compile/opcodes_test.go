package compile

import "testing"

func TestOpcodeNumbers(t *testing.T) {
	// Cross-checks against cpython 3.14 _opcode_metadata.py.
	cases := []struct {
		op   Opcode
		want int
		name string
	}{
		{NOP, 27, "NOP"},
		{LOAD_CONST, 82, "LOAD_CONST"},
		{LOAD_FAST, 84, "LOAD_FAST"},
		{POP_JUMP_IF_TRUE, 103, "POP_JUMP_IF_TRUE"},
		{JUMP_FORWARD, 77, "JUMP_FORWARD"},
		{JUMP_BACKWARD, 75, "JUMP_BACKWARD"},
		{RETURN_VALUE, 35, "RETURN_VALUE"},
		{EXTENDED_ARG, 69, "EXTENDED_ARG"},
		{RESUME, 128, "RESUME"},
	}
	for _, c := range cases {
		if int(c.op) != c.want {
			t.Errorf("%s = %d, want %d", c.name, int(c.op), c.want)
		}
		if c.op.Name() != c.name {
			t.Errorf("Name(%d) = %q, want %q", int(c.op), c.op.Name(), c.name)
		}
	}
}

func TestOpcodeFlags(t *testing.T) {
	// LOAD_CONST has HAS_ARG and HAS_CONST.
	if !LOAD_CONST.HasArg() {
		t.Error("LOAD_CONST should have HAS_ARG")
	}
	if !LOAD_CONST.HasConst() {
		t.Error("LOAD_CONST should have HAS_CONST")
	}
	// LOAD_NAME has HAS_ARG and HAS_NAME.
	if !LOAD_NAME.HasName() {
		t.Error("LOAD_NAME should have HAS_NAME")
	}
	// JUMP_FORWARD has HAS_JUMP.
	if !JUMP_FORWARD.HasJump() {
		t.Error("JUMP_FORWARD should have HAS_JUMP")
	}
	// NOP has nothing.
	if NOP.HasArg() || NOP.HasJump() || NOP.HasConst() {
		t.Error("NOP should have no flags")
	}
	// LOAD_FAST has HAS_LOCAL.
	if !LOAD_FAST.HasLocal() {
		t.Error("LOAD_FAST should have HAS_LOCAL")
	}
}

func TestHasTargetMatchesHasJump(t *testing.T) {
	jumps := []Opcode{
		JUMP_FORWARD, JUMP_BACKWARD, POP_JUMP_IF_TRUE,
		POP_JUMP_IF_FALSE, POP_JUMP_IF_NONE, POP_JUMP_IF_NOT_NONE,
		FOR_ITER, SEND,
	}
	for _, op := range jumps {
		if !HasTarget(op) {
			t.Errorf("HasTarget(%s) = false, want true", op.Name())
		}
	}
	if HasTarget(NOP) {
		t.Error("HasTarget(NOP) = true, want false")
	}
}

func TestNameOutOfRange(t *testing.T) {
	if Opcode(-1).Name() != "" {
		t.Error("negative opcode Name should be empty")
	}
	if Opcode(9999).Name() != "" {
		t.Error("oversized opcode Name should be empty")
	}
}
