// CALL_INTRINSIC_2 dispatch table. Pop two values, push one. Same
// stub-with-real-numbering shape as unary.go.
//
// CPython: Python/intrinsics.c _PyIntrinsics_BinaryFunctions

package intrinsics

import (
	"errors"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Binary is one CALL_INTRINSIC_2 entry.
type Binary func(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error)

// BinaryTable is indexed by INTRINSIC_2_* id.
var BinaryTable = [MaxBinary + 1]Binary{
	Binary2Invalid:                 Binary2InvalidFn,
	BinaryPrepReraiseStarID:        BinaryPrepReraiseStar,
	BinaryTypevarWithBoundID:       BinaryTypevarWithBound,
	BinaryTypevarWithConstraintsID: BinaryTypevarWithConstraints,
	BinarySetFunctionTypeParamsID:  BinarySetFunctionTypeParams,
	BinarySetTypeparamDefaultID:    BinarySetTypeparamDefault,
}

// Binary2InvalidFn raises if reached.
//
// CPython: Python/intrinsics.c no_intrinsic2
func Binary2InvalidFn(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, errors.New("intrinsics: invalid binary intrinsic id 0")
}

// BinaryPrepReraiseStar reconstructs an ExceptionGroup for `raise except*`.
//
// CPython: Python/intrinsics.c prep_reraise_star
func BinaryPrepReraiseStar(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, notImplemented("BinaryPrepReraiseStar", "ExceptionGroup type lives in 1686")
}

// BinaryTypevarWithBound builds TypeVar(name, bound=...).
//
// CPython: Python/intrinsics.c typevar_with_bound
func BinaryTypevarWithBound(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, notImplemented("BinaryTypevarWithBound", "PEP 695 typevar lives in 1689")
}

// BinaryTypevarWithConstraints builds TypeVar(name, *constraints).
//
// CPython: Python/intrinsics.c typevar_with_constraints
func BinaryTypevarWithConstraints(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, notImplemented("BinaryTypevarWithConstraints", "PEP 695 typevar lives in 1689")
}

// BinarySetFunctionTypeParams attaches __type_params__ to a function.
//
// CPython: Python/intrinsics.c set_function_type_params
func BinarySetFunctionTypeParams(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, notImplemented("BinarySetFunctionTypeParams", "function object __type_params__ slot lives in 1685")
}

// BinarySetTypeparamDefault sets a TypeVar's default. Added in 3.13.
//
// CPython: Python/intrinsics.c set_typeparam_default
func BinarySetTypeparamDefault(ts *state.Thread, lhs, rhs objects.Object) (objects.Object, error) {
	return nil, notImplemented("BinarySetTypeparamDefault", "PEP 695 typevar default lives in 1689")
}
