// CONTAINS_OP family specializer.
//
// _Py_Specialize_ContainsOp narrows CONTAINS_OP based on the right
// operand (the container). Dict and set/frozenset both use the
// generic membership protocol, but the inline arms can skip the
// dispatch by reading the hash table directly.
//
// CPython: Python/specialize.c:3108 _Py_Specialize_ContainsOp

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

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
