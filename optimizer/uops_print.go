// Human-readable formatting for one uop instruction. dis.dis renders
// trace rows by walking executor[i] (which returns a 4-tuple), but
// the gate fixtures and the optimizer's own debug logging want a
// single-line string. Print mirrors CPython's _PyUOpPrint output
// shape exactly: name, then "(oparg, target=..., operand0=0x...,
// operand1=0x...)" for target-format uops or
// "(oparg, jump_target=..., operand0=..., operand1=...)" for jump-
// format uops, then a trailing ", error_target=..." when the uop
// carries the HAS_ERROR_FLAG metadata bit.
//
// CPython: Python/optimizer.c:295-328 _PyUOpPrint

package optimizer

import (
	"fmt"
	"strings"
)

// PrintUOp formats one uop instruction as a single-line string. Out
// of range opcodes render as "<uop N>"; named opcodes use their
// CPython spelling (with leading underscore).
//
// CPython: Python/optimizer.c:295-328 _PyUOpPrint
func PrintUOp(u *UOPInstruction) string {
	var b strings.Builder
	op := u.Opcode()
	if name := UOpName(op); name != "" {
		b.WriteString(name)
	} else {
		fmt.Fprintf(&b, "<uop %d>", op)
	}
	switch u.Format() {
	case UOPFormatTarget:
		fmt.Fprintf(&b, " (%d, target=%d, operand0=%#x, operand1=%#x",
			u.Oparg, u.GetTarget(), u.Operand0, u.Operand1)
	case UOPFormatJump:
		fmt.Fprintf(&b, " (%d, jump_target=%d, operand0=%#x, operand1=%#x",
			u.Oparg, u.GetJumpTarget(), u.Operand0, u.Operand1)
	default:
		fmt.Fprintf(&b, " (%d, Unknown format)", u.Oparg)
		return b.String()
	}
	if hasErrorFlag(op) {
		fmt.Fprintf(&b, ", error_target=%d", u.GetErrorTarget())
	}
	b.WriteString(")")
	return b.String()
}

// hasErrorFlag reports whether the uop's metadata carries
// HAS_ERROR_FLAG. The lookup goes through UopNames so we share the
// table with PrintUOp.
func hasErrorFlag(op uint16) bool {
	name := UopNames[op]
	if name == "" {
		return false
	}
	return UopMeta[name].Flags&FlagError != 0
}
