package compile

import (
	"strings"
	"testing"
)

// TestDisassembleBasic verifies the listing shape: one line per
// instruction with the opname and oparg.
func TestDisassembleBasic(t *testing.T) {
	co := &Code{
		Name: "<m>",
		Code: []byte{
			byte(LOAD_CONST), 0,
			byte(RETURN_VALUE), 0,
		},
	}
	got := Disassemble(co)
	if !strings.Contains(got, "LOAD_CONST") {
		t.Errorf("disasm missing LOAD_CONST:\n%s", got)
	}
	if !strings.Contains(got, "RETURN_VALUE") {
		t.Errorf("disasm missing RETURN_VALUE:\n%s", got)
	}
}

// TestDisassembleExtendedArg recombines EXTENDED_ARG bytes into the
// final 32-bit oparg.
func TestDisassembleExtendedArg(t *testing.T) {
	co := &Code{
		Name: "<m>",
		Code: []byte{
			byte(EXTENDED_ARG), 0x12,
			byte(LOAD_CONST), 0x34,
		},
	}
	got := Disassemble(co)
	if !strings.Contains(got, "4660") {
		t.Errorf("disasm missing recombined oparg 4660 (0x1234):\n%s", got)
	}
}
