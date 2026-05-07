// COMPARE_OP family specializer.
//
// _Py_Specialize_CompareOp narrows COMPARE_OP based on the operand
// types. The oparg encodes the comparison operator in bits 5..7
// (shift 5); the str variant only fires for == and != since the
// generic less-than path on str runs Python-level slots.
//
// CPython: Python/specialize.c:2740 _Py_Specialize_CompareOp

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// CompareOp specializes the COMPARE_OP at instr.
//
// CPython: Python/specialize.c:2740 _Py_Specialize_CompareOp
func CompareOp(lhs, rhs objects.Object, code []byte, instr int, oparg int32) {
	if !sameExactType(lhs, rhs) {
		Unspecialize(code, instr)
		return
	}
	if objects.IsExactFloat(lhs) {
		Specialize(code, instr, compile.COMPARE_OP_FLOAT)
		return
	}
	if objects.IsExactInt(lhs) {
		// CPython narrows to compact ints; gopy's *Int is always
		// big.Int but the inline arms still use Int64() which
		// returns ok only when the value fits in 63 bits.
		li, _ := lhs.(*objects.Int)
		ri, _ := rhs.(*objects.Int)
		_, lok := li.Int64()
		_, rok := ri.Int64()
		if lok && rok {
			Specialize(code, instr, compile.COMPARE_OP_INT)
			return
		}
		Unspecialize(code, instr)
		return
	}
	if objects.IsExactStr(lhs) {
		cmp := objects.CompareOp(oparg >> 5)
		if cmp == objects.CompareEQ || cmp == objects.CompareNE {
			Specialize(code, instr, compile.COMPARE_OP_STR)
			return
		}
		Unspecialize(code, instr)
		return
	}
	Unspecialize(code, instr)
}
