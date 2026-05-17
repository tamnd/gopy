// UNPACK_SEQUENCE family specializer.
//
// _Py_Specialize_UnpackSequence narrows UNPACK_SEQUENCE based on the
// sequence on top of stack. The arms only fire when the sequence
// length matches oparg, since the inline dispatch fans out the items
// directly without re-checking the count.
//
// CPython: Python/specialize.c:2802 _Py_Specialize_UnpackSequence

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// UnpackSequence specializes the UNPACK_SEQUENCE at instr based on
// the sequence and target count (oparg).
//
// CPython: Python/specialize.c:2802 _Py_Specialize_UnpackSequence
func UnpackSequence(seq objects.Object, code []byte, instr int, oparg int32) {
	if objects.IsExactTuple(seq) {
		t := seq.(*objects.Tuple)
		if int32(t.Len()) != oparg {
			Unspecialize(code, instr)
			return
		}
		if t.Len() == 2 {
			Specialize(code, instr, compile.UNPACK_SEQUENCE_TWO_TUPLE)
			return
		}
		Specialize(code, instr, compile.UNPACK_SEQUENCE_TUPLE)
		return
	}
	if objects.IsExactList(seq) {
		l := seq.(*objects.List)
		if int32(l.Len()) != oparg {
			Unspecialize(code, instr)
			return
		}
		Specialize(code, instr, compile.UNPACK_SEQUENCE_LIST)
		return
	}
	Unspecialize(code, instr)
}
