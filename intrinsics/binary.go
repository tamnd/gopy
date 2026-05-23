// CALL_INTRINSIC_2 dispatch table. Pop two values, push one. Same
// stub-with-real-numbering shape as unary.go.
//
// CPython: Python/intrinsics.c _PyIntrinsics_BinaryFunctions

package intrinsics

import (
	"errors"

	pyerrors "github.com/tamnd/gopy/errors"
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

// BinaryPrepReraiseStar reconstructs the exception (or group of
// exceptions) that should propagate after a `try/except*` finishes.
// orig is the original raised exception that drove the unwind; excs
// is the accumulated list of leftovers (each `rest` partition from
// CHECK_EG_MATCH plus any exception raised inside an except* arm).
// None entries are filtered out. Returns None when nothing remains.
//
// Lifecycle mirrors CPython: a naked exception that was wrapped into
// an ExceptionGroup only for matching purposes surfaces as itself
// when at most one leaf survives; a group input collapses back into
// a derived ExceptionGroup over the survivors.
//
// CPython: Python/intrinsics.c:237 prep_reraise_star
// CPython: Objects/exceptions.c:1618 _PyExc_PrepReraiseStar
func BinaryPrepReraiseStar(ts *state.Thread, orig, excs objects.Object) (objects.Object, error) {
	list, ok := excs.(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: PrepReraiseStar excs must be a list")
	}
	var leftovers []*pyerrors.Exception
	for i := 0; i < list.Len(); i++ {
		item := list.Item(i)
		if item == objects.None() {
			continue
		}
		exc, eok := item.(*pyerrors.Exception)
		if !eok {
			return nil, errors.New("TypeError: PrepReraiseStar item is not an exception")
		}
		leftovers = append(leftovers, exc)
	}
	if len(leftovers) == 0 {
		return objects.None(), nil
	}
	origExc, _ := orig.(*pyerrors.Exception)
	// Naked-exception input: only one leaf could have survived. Return
	// it bare so user code sees the original exception, not a wrapper.
	if origExc != nil && !pyerrors.IsExceptionGroup(origExc.ExcType) {
		return leftovers[0], nil
	}
	if len(leftovers) == 1 {
		return leftovers[0], nil
	}
	items := make([]objects.Object, len(leftovers))
	for i, e := range leftovers {
		items[i] = e
	}
	message := objects.NewStr("")
	if origExc != nil && origExc.Args != nil && origExc.Args.Len() >= 1 {
		message = origExc.Args.Item(0).(*objects.Unicode)
	}
	group := pyerrors.New(pyerrors.PyExc_ExceptionGroup, objects.NewTuple([]objects.Object{
		message, objects.NewTuple(items),
	}))
	return group, nil
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
