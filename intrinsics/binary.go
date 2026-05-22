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

// BinaryTypevarWithBound builds TypeVar(name, bound=...). CPython
// passes the bound as a lazy-eval function (the evaluate_bound thunk);
// gopy stores whatever the caller pushed so __bound__ returns it.
//
// CPython: Python/intrinsics.c:244 make_typevar_with_bound
func BinaryTypevarWithBound(ts *state.Thread, name, evaluateBound objects.Object) (objects.Object, error) {
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: TypeVar name must be a str")
	}
	return objects.NewTypeVar(n.Value(), evaluateBound, nil), nil
}

// BinaryTypevarWithConstraints builds TypeVar(name, *constraints).
//
// CPython: Python/intrinsics.c:252 make_typevar_with_constraints
func BinaryTypevarWithConstraints(ts *state.Thread, name, evaluateConstraints objects.Object) (objects.Object, error) {
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: TypeVar name must be a str")
	}
	return objects.NewTypeVar(n.Value(), nil, evaluateConstraints), nil
}

// BinarySetFunctionTypeParams attaches __type_params__ to a function.
// CPython stamps the tuple directly into func->func_typeparams; the
// gopy port walks Function.Dict and writes "__type_params__" there.
//
// CPython: Python/intrinsics.c set_function_type_params
func BinarySetFunctionTypeParams(ts *state.Thread, fn, params objects.Object) (objects.Object, error) {
	f, ok := fn.(*objects.Function)
	if !ok {
		return nil, errors.New("TypeError: __type_params__ target is not a function")
	}
	tup, ok := params.(*objects.Tuple)
	if !ok {
		return nil, errors.New("TypeError: __type_params__ must be a tuple")
	}
	f.Typeparams = tup
	return fn, nil
}

// BinarySetTypeparamDefault sets a TypeVar/ParamSpec/TypeVarTuple's
// __default__ slot. Added in 3.13 alongside PEP 696.
//
// CPython: Python/intrinsics.c set_typeparam_default
func BinarySetTypeparamDefault(ts *state.Thread, typeparam, defaultVal objects.Object) (objects.Object, error) {
	switch tp := typeparam.(type) {
	case *objects.TypeVar:
		tp.Default = defaultVal
		tp.HasDefault = true
	case *objects.ParamSpec:
		tp.Default = defaultVal
		tp.HasDefault = true
	case *objects.TypeVarTuple:
		tp.Default = defaultVal
		tp.HasDefault = true
	default:
		return nil, errors.New("TypeError: set_typeparam_default expects TypeVar/ParamSpec/TypeVarTuple")
	}
	return typeparam, nil
}
