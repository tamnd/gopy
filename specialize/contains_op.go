// CONTAINS_OP family specializer.
//
// _Py_Specialize_ContainsOp narrows CONTAINS_OP based on the right
// operand (the container). Dict and set/frozenset both use the
// generic membership protocol, but the inline arms can skip the
// dispatch by reading the hash table directly.
//
// CPython: Python/specialize.c:3108 _Py_Specialize_ContainsOp

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// ContainsOp specializes the CONTAINS_OP at instr based on the
// container operand (the right-hand side of `x in container`).
//
// CPython: Python/specialize.c:3108 _Py_Specialize_ContainsOp
func ContainsOp(container objects.Object, code []byte, instr int) {
	if objects.IsExactDict(container) {
		Specialize(code, instr, compile.CONTAINS_OP_DICT)
		return
	}
	if objects.IsExactSet(container) || objects.IsExactFrozenSet(container) {
		Specialize(code, instr, compile.CONTAINS_OP_SET)
		return
	}
	Unspecialize(code, instr)
}
