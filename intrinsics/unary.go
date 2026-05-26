// CALL_INTRINSIC_1 dispatch table. The eval loop reads
// UnaryTable[oparg] and calls it. Most helpers in v0.6 are stubs:
// the surface and ID numbering land here so the eval loop can be
// generated against the real table; per-helper bodies fill in as
// the cross-block prereqs land (PEP 695 type runtime in 1689,
// import system, sys.displayhook in 1651, ExceptionGroup in 1686).
//
// CPython: Python/intrinsics.c _PyIntrinsics_UnaryFunctions

package intrinsics

import (
	"errors"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Code-object flag bits for CO_COROUTINE / CO_ASYNC_GENERATOR. Mirror
// the bit values in Include/cpython/code.h so the stopiteration_error
// port can branch on the frame's flags without pulling the compile
// package in (compile imports objects, which would invert the layering).
//
// CPython: Include/cpython/code.h CO_COROUTINE / CO_ASYNC_GENERATOR
const (
	coCoroutine      = 0x0080
	coAsyncGenerator = 0x0200
)

// Unary is one CALL_INTRINSIC_1 entry.
type Unary func(ts *state.Thread, v objects.Object) (objects.Object, error)

// UnaryTable is indexed by INTRINSIC_* id. Order matches
// pycore_intrinsics.h.
var UnaryTable = [MaxUnary + 1]Unary{
	Unary1Invalid:           Unary1InvalidFn,
	UnaryPrintID:            UnaryPrint,
	UnaryImportStarID:       UnaryImportStar,
	UnaryStopIterErrorID:    UnaryStopIterationError,
	UnaryAsyncGenWrapID:     UnaryAsyncGenWrap,
	UnaryPositiveID:         UnaryUnaryPositive,
	UnaryListToTupleID:      UnaryListToTuple,
	UnaryTypevarID:          UnaryTypevar,
	UnaryParamspecID:        UnaryParamspec,
	UnaryTypevartupleID:     UnaryTypevartuple,
	UnarySubscriptGenericID: UnarySubscriptGeneric,
	UnaryTypealiasID:        UnaryTypealias,
}

// Unary1InvalidFn raises if reached. The compiler should never emit
// CALL_INTRINSIC_1 with oparg 0.
//
// CPython: Python/intrinsics.c no_intrinsic1
func Unary1InvalidFn(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, errors.New("intrinsics: invalid unary intrinsic id 0")
}

// PrintExprHook is installed by module/sys.init so CALL_INTRINSIC_1
// PRINT can resolve sys.displayhook without an import cycle from
// intrinsics into module/sys. nil indicates the sys module has not yet
// been wired (e.g. unit tests that build the table in isolation).
//
// CPython: Python/intrinsics.c:28 print_expr
var PrintExprHook func(objects.Object) (objects.Object, error)

// UnaryPrint calls sys.displayhook on v (REPL print path).
//
// CPython: Python/intrinsics.c print_expr
func UnaryPrint(ts *state.Thread, v objects.Object) (objects.Object, error) {
	if PrintExprHook == nil {
		return nil, errors.New("RuntimeError: lost sys.displayhook")
	}
	return PrintExprHook(v)
}

// UnaryImportStar implements `from x import *`.
//
// CPython: Python/intrinsics.c import_star
func UnaryImportStar(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryImportStar", "import system lives in 1683")
}

// UnaryStopIterationError wraps a StopIteration that escaped a
// generator in a RuntimeError (PEP 479). When the surrounding frame is
// an async generator or coroutine the wrapper message changes; when an
// async generator raises StopAsyncIteration that is also wrapped.
// Anything else passes through unchanged, matching CPython's
// Py_NewRef(exc) tail.
//
// CPython: Python/intrinsics.c:142 stopiteration_error
func UnaryStopIterationError(ts *state.Thread, v objects.Object) (objects.Object, error) {
	exc, ok := v.(*pyerrors.Exception)
	if !ok {
		return v, nil
	}
	var flags int
	if objects.CurrentFrameHook != nil {
		if f := objects.CurrentFrameHook(); f != nil {
			if c := f.FrameCode(); c != nil {
				flags = c.Flags
			}
		}
	}
	msg := ""
	switch {
	case pyerrors.Match(exc, pyerrors.PyExc_StopIteration):
		switch {
		case flags&coAsyncGenerator != 0:
			msg = "async generator raised StopIteration"
		case flags&coCoroutine != 0:
			msg = "coroutine raised StopIteration"
		default:
			msg = "generator raised StopIteration"
		}
	case flags&coAsyncGenerator != 0 && pyerrors.Match(exc, pyerrors.PyExc_StopAsyncIteration):
		msg = "async generator raised StopAsyncIteration"
	}
	if msg == "" {
		return v, nil
	}
	wrapped := pyerrors.New(pyerrors.PyExc_RuntimeError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
	wrapped.Cause = exc
	wrapped.Context = exc
	return wrapped, nil
}

// UnaryAsyncGenWrap builds an _PyAsyncGenWrappedValue around v.
//
// CPython: Python/intrinsics.c async_gen_wrap
func UnaryAsyncGenWrap(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryAsyncGenWrap", "async generator object lives in 1687")
}

// UnaryUnaryPositive computes +v via the type's __pos__ slot.
//
// CPython: Python/intrinsics.c:186 unary_pos
func UnaryUnaryPositive(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return objects.NumberPositive(v)
}

// UnaryListToTuple freezes a list (built by a comprehension) into a tuple.
//
// CPython: Python/intrinsics.c list_to_tuple
func UnaryListToTuple(ts *state.Thread, v objects.Object) (objects.Object, error) {
	l, ok := v.(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: LIST_TO_TUPLE expected list, got " + v.Type().Name)
	}
	items := make([]objects.Object, l.Len())
	for i := range items {
		items[i] = l.Item(i)
	}
	return objects.NewTuple(items), nil
}

// UnaryTypevar builds a PEP 695 TypeVar(name) runtime object.
// PEP 695 type parameters always have infer_variance=True.
//
// CPython: Python/intrinsics.c:200 make_typevar
func UnaryTypevar(ts *state.Thread, v objects.Object) (objects.Object, error) {
	name, ok := v.(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: TypeVar name must be a str")
	}
	tv := objects.NewTypeVar(name.Value(), nil, nil)
	tv.InferVariance = true
	tv.Module = objects.CallerModuleName()
	return tv, nil
}

// UnaryParamspec builds a PEP 695 ParamSpec(name) runtime object.
//
// CPython: Python/intrinsics.c _Py_make_paramspec
func UnaryParamspec(ts *state.Thread, v objects.Object) (objects.Object, error) {
	name, ok := v.(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: ParamSpec name must be a str")
	}
	ps := objects.NewParamSpec(name.Value())
	ps.InferVariance = true
	ps.Module = objects.CallerModuleName()
	return ps, nil
}

// UnaryTypevartuple builds a PEP 695 TypeVarTuple(name) runtime object.
//
// CPython: Python/intrinsics.c _Py_make_typevartuple
func UnaryTypevartuple(ts *state.Thread, v objects.Object) (objects.Object, error) {
	name, ok := v.(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: TypeVarTuple name must be a str")
	}
	tvt := objects.NewTypeVarTuple(name.Value())
	tvt.Module = objects.CallerModuleName()
	return tvt, nil
}

// UnarySubscriptGeneric implements Generic[T] subscription. The
// returned GenericAlias has Generic as its origin and the type-param
// tuple as args, so __mro_entries__ pulls Generic itself into the
// derived class's bases.
//
// CPython: Python/intrinsics.c _Py_subscript_generic
func UnarySubscriptGeneric(ts *state.Thread, v objects.Object) (objects.Object, error) {
	params, ok := v.(*objects.Tuple)
	if !ok {
		return nil, errors.New("TypeError: Generic[...] requires a tuple of type parameters")
	}
	return objects.NewGenericAlias(objects.GenericType, params), nil
}

// UnaryTypealias materializes a `type X = ...` runtime alias. The
// argument is a 3-tuple (name, type_params, compute_value). For the
// non-generic form CPython passes type_params=None and compute_value
// is a function that evaluates the alias's value lazily.
//
// CPython: Objects/typevarobject.c:2181 _Py_make_typealias
func UnaryTypealias(ts *state.Thread, v objects.Object) (objects.Object, error) {
	tup, ok := v.(*objects.Tuple)
	if !ok || tup.Len() != 3 {
		return nil, errors.New("TypeError: type alias intrinsic expects a 3-tuple")
	}
	name, ok := tup.Item(0).(*objects.Unicode)
	if !ok {
		return nil, errors.New("TypeError: type alias name must be a str")
	}
	a := objects.NewTypeAlias(name.Value(), tup.Item(1), tup.Item(2))
	a.Module = objects.TypealiasModule()
	return a, nil
}
