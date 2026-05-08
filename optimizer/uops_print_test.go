package optimizer

import "testing"

func TestPrintUOp_NamedTargetFormat(t *testing.T) {
	u := &UOPInstruction{Oparg: 5, Target: 12, Operand0: 0xdead, Operand1: 0xbeef}
	u.SetOpcode(UopExitTrace)
	u.SetFormat(UOPFormatTarget)
	got := PrintUOp(u)
	want := "_EXIT_TRACE (5, target=12, operand0=0xdead, operand1=0xbeef)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrintUOp_UnknownOpcode(t *testing.T) {
	u := &UOPInstruction{}
	u.SetOpcode(0x7FFE) // top of opcode range, no name
	got := PrintUOp(u)
	if got[:6] != "<uop 3" {
		t.Errorf("got %q, want prefix '<uop 3'", got)
	}
}

func TestPrintUOp_JumpFormatWithErrorTarget(t *testing.T) {
	// _BINARY_OP_ADD_INT carries HAS_ERROR_FLAG; encode a JUMP-format
	// instruction so error_target packs into the high half of Target.
	u := &UOPInstruction{Oparg: 1, Target: (3 << 16) | 7}
	u.SetOpcode(UopBinaryOpAddInt)
	u.SetFormat(UOPFormatJump)
	got := PrintUOp(u)
	if !contains(got, "_BINARY_OP_ADD_INT (1, jump_target=7,") {
		t.Errorf("got %q, want jump_target=7 prefix", got)
	}
	if !contains(got, "error_target=3") {
		t.Errorf("got %q, want error_target=3", got)
	}
}

func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
