// BINARY_OP family specializer.
//
// _Py_Specialize_BinaryOp walks the (lhs, rhs, oparg) triple and
// rewrites BINARY_OP into one of the per-type specialized variants
// when both operands are exactly the right type. NB_SUBSCR has its
// own subtree because the second operand drives the specialization
// (list[int] vs dict[any] vs str[int] etc.). Anything that does not
// match a known shape unspecializes back to BINARY_OP.
//
// CPython: Python/specialize.c:2578 _Py_Specialize_BinaryOp

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// BinaryOp specializes the BINARY_OP at instr based on the two
// operands and the NB_* oparg. nextOp / nextArg are the opcode and
// arg of the *following* codeunit (after the cache cells); the
// INPLACE_ADD_UNICODE arm peeks at that to detect the
// `s = s + ""` pattern and pick the in-place variant when the
// store target is the same local.
//
// CPython: Python/specialize.c:2578 _Py_Specialize_BinaryOp
func BinaryOp(lhs, rhs objects.Object, code []byte, instr int, oparg int32, nextOp compile.Opcode, nextArg int32, locals []objects.Object) {
	switch oparg {
	case compile.NB_ADD, compile.NB_INPLACE_ADD:
		if !sameExactType(lhs, rhs) {
			break
		}
		if objects.IsExactStr(lhs) {
			if nextOp == compile.STORE_FAST && int(nextArg) < len(locals) && locals[nextArg] == lhs {
				Specialize(code, instr, compile.BINARY_OP_INPLACE_ADD_UNICODE)
				return
			}
			Specialize(code, instr, compile.BINARY_OP_ADD_UNICODE)
			return
		}
		if objects.IsExactInt(lhs) {
			Specialize(code, instr, compile.BINARY_OP_ADD_INT)
			return
		}
		if objects.IsExactFloat(lhs) {
			Specialize(code, instr, compile.BINARY_OP_ADD_FLOAT)
			return
		}
	case compile.NB_MULTIPLY, compile.NB_INPLACE_MULTIPLY:
		if !sameExactType(lhs, rhs) {
			break
		}
		if objects.IsExactInt(lhs) {
			Specialize(code, instr, compile.BINARY_OP_MULTIPLY_INT)
			return
		}
		if objects.IsExactFloat(lhs) {
			Specialize(code, instr, compile.BINARY_OP_MULTIPLY_FLOAT)
			return
		}
	case compile.NB_SUBTRACT, compile.NB_INPLACE_SUBTRACT:
		if !sameExactType(lhs, rhs) {
			break
		}
		if objects.IsExactInt(lhs) {
			Specialize(code, instr, compile.BINARY_OP_SUBTRACT_INT)
			return
		}
		if objects.IsExactFloat(lhs) {
			Specialize(code, instr, compile.BINARY_OP_SUBTRACT_FLOAT)
			return
		}
	case compile.NB_SUBSCR:
		if specializeBinarySubscr(lhs, rhs, code, instr) {
			return
		}
	}
	Unspecialize(code, instr)
}

// sameExactType returns true when both operands have the same exact
// (non-subclass) type. CPython spells the same check as
// `Py_IS_TYPE(lhs, Py_TYPE(rhs))`.
func sameExactType(lhs, rhs objects.Object) bool {
	return objects.ExactType(lhs) == objects.ExactType(rhs)
}

// specializeBinarySubscr handles the NB_SUBSCR sub-tree. Returns
// true if it picked a variant; false signals the caller to fall
// through to Unspecialize.
//
// CPython: Python/specialize.c:2644 (NB_SUBSCR case in
// _Py_Specialize_BinaryOp)
func specializeBinarySubscr(lhs, rhs objects.Object, code []byte, instr int) bool {
	if objects.IsExactInt(rhs) && intIsNonNegative(rhs) {
		if objects.IsExactList(lhs) {
			Specialize(code, instr, compile.BINARY_OP_SUBSCR_LIST_INT)
			return true
		}
		if objects.IsExactTuple(lhs) {
			Specialize(code, instr, compile.BINARY_OP_SUBSCR_TUPLE_INT)
			return true
		}
		if objects.IsExactStr(lhs) {
			Specialize(code, instr, compile.BINARY_OP_SUBSCR_STR_INT)
			return true
		}
	}
	if objects.IsExactDict(lhs) {
		Specialize(code, instr, compile.BINARY_OP_SUBSCR_DICT)
		return true
	}
	if objects.IsExactList(lhs) && objects.IsExactSlice(rhs) {
		Specialize(code, instr, compile.BINARY_OP_SUBSCR_LIST_SLICE)
		return true
	}
	return false
}

// intIsNonNegative is the gopy stand-in for
// _PyLong_IsNonNegativeCompact. Returns true when o is an exact
// int with a value the LIST_INT and TUPLE_INT arms can use as a
// raw Py_ssize_t index.
func intIsNonNegative(o objects.Object) bool {
	i, ok := o.(*objects.Int)
	if !ok {
		return false
	}
	v, ok := i.Int64()
	return ok && v >= 0
}
