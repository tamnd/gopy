// FOR_ITER family specializer.
//
// _Py_Specialize_ForIter narrows FOR_ITER based on the iterator type
// on top of stack. The list/tuple/range arms read the underlying
// sequence directly; the generator arm punches into the generator
// frame and only fires when PEP 523 is not active and the jump
// target fits in a 16-bit oparg.
//
// CPython: Python/specialize.c:2909 _Py_Specialize_ForIter

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"math"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// ForIter specializes the FOR_ITER at instr based on the iterator on
// the stack. oparg is the codeunit's argument (used for the gen-arm
// jump-fits check).
//
// CPython: Python/specialize.c:2909 _Py_Specialize_ForIter
func ForIter(iter objects.Object, code []byte, instr int, oparg int32) {
	switch {
	case objects.IsListIter(iter):
		Specialize(code, instr, compile.FOR_ITER_LIST)
		return
	case objects.IsTupleIter(iter):
		Specialize(code, instr, compile.FOR_ITER_TUPLE)
		return
	case objects.IsRangeIter(iter):
		Specialize(code, instr, compile.FOR_ITER_RANGE)
		return
	case objects.IsGenerator(iter):
		// CPython gates on `oparg <= SHRT_MAX` because the FOR_ITER_GEN
		// dispatch must be able to walk past the loop body and hit
		// END_FOR. gopy mirrors that bound.
		if oparg <= math.MaxInt16 {
			Specialize(code, instr, compile.FOR_ITER_GEN)
			return
		}
	}
	Unspecialize(code, instr)
}
