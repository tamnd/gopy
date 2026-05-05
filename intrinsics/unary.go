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

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
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

// UnaryPrint calls sys.displayhook on v (REPL print path).
//
// CPython: Python/intrinsics.c print_expr
func UnaryPrint(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryPrint", "sys.displayhook lives in 1651")
}

// UnaryImportStar implements `from x import *`.
//
// CPython: Python/intrinsics.c import_star
func UnaryImportStar(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryImportStar", "import system lives in 1683")
}

// UnaryStopIterationError raises a RuntimeError wrapping a
// StopIteration that escaped a generator (PEP 479).
//
// CPython: Python/intrinsics.c stopiteration_error
func UnaryStopIterationError(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryStopIterationError", "RuntimeError wrap needs exception module 1686")
}

// UnaryAsyncGenWrap builds an _PyAsyncGenWrappedValue around v.
//
// CPython: Python/intrinsics.c async_gen_wrap
func UnaryAsyncGenWrap(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryAsyncGenWrap", "async generator object lives in 1687")
}

// UnaryUnaryPositive computes +v via the type's __pos__ slot.
//
// CPython: Python/intrinsics.c unary_pos
func UnaryUnaryPositive(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryUnaryPositive", "number protocol slot dispatch in 1684")
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
//
// CPython: Python/intrinsics.c make_typevar
func UnaryTypevar(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryTypevar", "PEP 695 typevar lives in 1689")
}

// UnaryParamspec builds a PEP 695 ParamSpec(name) runtime object.
//
// CPython: Python/intrinsics.c make_paramspec
func UnaryParamspec(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryParamspec", "PEP 695 paramspec lives in 1689")
}

// UnaryTypevartuple builds a PEP 695 TypeVarTuple(name) runtime object.
//
// CPython: Python/intrinsics.c make_typevartuple
func UnaryTypevartuple(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryTypevartuple", "PEP 695 typevartuple lives in 1689")
}

// UnarySubscriptGeneric implements Generic[T] subscription.
//
// CPython: Python/intrinsics.c subscript_generic
func UnarySubscriptGeneric(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnarySubscriptGeneric", "Generic[...] lives in 1689")
}

// UnaryTypealias materializes a `type X = ...` runtime alias.
//
// CPython: Python/intrinsics.c type_alias
func UnaryTypealias(ts *state.Thread, v objects.Object) (objects.Object, error) {
	return nil, notImplemented("UnaryTypealias", "PEP 695 type aliases live in 1689")
}
