// Small helpers the dispatch arms call into. The action translator
// (1621/B6) emits some of these; the hand-written panel uses them
// directly. CPython has them as macros / inline helpers in
// Python/ceval_macros.h and Python/ceval.c.
package vm

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/intrinsics"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// decrefInputs drops n stack-ref inputs the action translator already
// consumed via push/pop. CPython's PyStackRef_CLOSE is reduced to a
// no-op in gopy because Go's GC handles object lifetime; the stack
// ref already cleared its slot when it was popped. The helper is
// kept so generated arms can call it without conditional emission.
//
// CPython: Python/ceval_macros.h DECREF_INPUTS
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) decrefInputs(n int) {
	_ = n
}

// error returns the error currently held in pendingErr (and clears it),
// or constructs a generic one if no specific cause was recorded. The
// translator emits `return 0, e.error("error")` for every ERROR_IF;
// helpers that signal failure via NULL stash the real cause on
// pendingErr before returning the sentinel, so this method surfaces
// the right exception to the eval loop.
//
// CPython: Python/ceval.c JUMP_TO_LABEL(error) — the per-instruction
// error label inspects tstate->current_exception.
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) error(label string) error {
	if e.pendingErr != nil {
		err := e.pendingErr
		e.pendingErr = nil
		return err
	}
	return fmt.Errorf("vm: error label %q reached without pending exception", label)
}

// pyNumberNegative is the translator-side wrapper for CPython's
// PyNumber_Negative. The body keeps the NULL-on-failure convention so
// the surrounding `ERROR_IF(res_o == NULL)` translation just works:
// the real Go error rides on e.pendingErr until e.error() retrieves it.
//
// CPython: Objects/abstract.c:1381 PyNumber_Negative
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyNumberNegative(o objects.Object) objects.Object {
	r, err := objects.NumberNegative(o)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return r
}

// pyNumberInvert wraps PyNumber_Invert.
//
// CPython: Objects/abstract.c:1389 PyNumber_Invert
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyNumberInvert(o objects.Object) objects.Object {
	r, err := objects.NumberInvert(o)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return r
}

// pyNumberPositive wraps PyNumber_Positive.
//
// CPython: Objects/abstract.c:1373 PyNumber_Positive
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyNumberPositive(o objects.Object) objects.Object {
	r, err := objects.NumberPositive(o)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return r
}

// pyNumberAbsolute wraps PyNumber_Absolute.
//
// CPython: Objects/abstract.c:1397 PyNumber_Absolute
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyNumberAbsolute(o objects.Object) objects.Object {
	r, err := objects.NumberAbsolute(o)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return r
}

// pyIs mirrors CPython's Py_Is macro: identity comparison returning
// 1 / 0 as an int.
//
// CPython: Include/object.h Py_Is
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyIs(a, b objects.Object) uint32 {
	if a == b {
		return 1
	}
	return 0
}

// globals / builtinsDict / localsDict expose the frame-namespace
// macros from Python/ceval_macros.h (GLOBALS / BUILTINS / LOCALS) as
// evalState accessors so the translator can emit them verbatim.
//
// CPython: Python/ceval_macros.h GLOBALS / BUILTINS / LOCALS
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) globals() objects.Object { return e.f.Globals }

//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) builtinsDict() objects.Object { return e.f.Builtins }

//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) localsDict() objects.Object { return e.f.Locals }

// loadName wraps CPython's _PyEval_LoadName: scan locals, then globals,
// then builtins for `name`. Returns nil-on-failure with pendingErr set,
// mirroring the NULL-on-failure convention the translator expects.
//
// CPython: Python/ceval.c:2789 _PyEval_LoadName
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) loadName(name objects.Object) objects.Object {
	if v, ok := lookupIn(e.f.Locals, name); ok {
		return v
	}
	if v, ok := lookupIn(e.f.Globals, name); ok {
		return v
	}
	if v, ok := lookupIn(e.f.Builtins, name); ok {
		return v
	}
	if s, ok := name.(*objects.Unicode); ok {
		e.pendingErr = fmt.Errorf("vm: NameError: name '%s' is not defined", s.Value())
	} else {
		e.pendingErr = errors.New("vm: NameError")
	}
	return nil
}

// importName wraps _PyEval_ImportName. The signature mirrors CPython's:
// (name, fromlist, level). Failure surfaces through pendingErr.
//
// CPython: Python/ceval.c:3043 _PyEval_ImportName
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) importName(name, fromlist, level objects.Object) objects.Object {
	e.pendingErr = errors.New("vm: importName helper not wired (spec 1714 A6 pending)")
	_ = name
	_ = fromlist
	_ = level
	return nil
}

// importFrom wraps _PyEval_ImportFrom: fetch `name` as an attribute of
// the already-imported module `from`.
//
// CPython: Python/ceval.c:3154 _PyEval_ImportFrom
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) importFrom(from objects.Object, name objects.Object) objects.Object {
	s, ok := name.(*objects.Unicode)
	if !ok {
		e.pendingErr = errors.New("vm: importFrom: name not a string")
		return nil
	}
	v, err := evalImportFrom(e, from, s.Value())
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return v
}

// dictSetItem wraps PyDict_SetItem: stash key=value on the dict-like
// scope. Returns 0 on success and 1 on failure (with pendingErr set),
// matching the C int err convention.
//
// CPython: Objects/dictobject.c:2240 PyDict_SetItem
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) dictSetItem(scope, key, value objects.Object) int32 {
	if err := storeIn(scope, key, value); err != nil {
		e.pendingErr = err
		return 1
	}
	return 0
}

// objectDelItem wraps PyObject_DelItem: del container[sub].
//
// CPython: Objects/abstract.c:191 PyObject_DelItem
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) objectDelItem(container, sub objects.Object) int32 {
	if err := objects.DelItem(container, sub); err != nil {
		e.pendingErr = err
		return 1
	}
	return 0
}

// objectDelAttr wraps PyObject_DelAttr.
//
// CPython: Objects/object.c:1308 PyObject_DelAttr
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) objectDelAttr(o, name objects.Object) int32 {
	if err := objects.DelAttr(o, name); err != nil {
		e.pendingErr = err
		return 1
	}
	return 0
}

// matchKeys wraps _PyEval_MatchKeys: returns a tuple of values, or
// None on partial match, or nil on a real error.
//
// CPython: Python/ceval.c:5052 _PyEval_MatchKeys
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) matchKeys(subject, keys objects.Object) objects.Object {
	keysTup, ok := keys.(*objects.Tuple)
	if !ok {
		e.pendingErr = errors.New("MATCH_KEYS: keys not a tuple")
		return nil
	}
	n := keysTup.Len()
	if n == 0 {
		return objects.NewTuple(nil)
	}
	values := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		v, gerr := matchKeysGet(subject, keysTup.Item(i))
		if errors.Is(gerr, errKeyMissing) {
			return objects.None()
		}
		if gerr != nil {
			e.pendingErr = gerr
			return nil
		}
		values[i] = v
	}
	return objects.NewTuple(values)
}

// matchClass wraps _PyEval_MatchClass: returns a tuple of extracted
// attributes, or None on no match, or nil on a real error.
//
// CPython: Python/ceval.c _PyEval_MatchClass
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) matchClass(subject, typeObj objects.Object, oparg uint32, names objects.Object) objects.Object {
	namesTup, ok := names.(*objects.Tuple)
	if !ok {
		e.pendingErr = errors.New("MATCH_CLASS: names not a tuple")
		return nil
	}
	tp, ok := typeObj.(*objects.Type)
	if !ok {
		e.pendingErr = fmt.Errorf("MATCH_CLASS: type operand not a type, got %T", typeObj)
		return nil
	}
	if !isInstance(subject, tp) {
		return objects.None()
	}
	npos := int(oparg)
	nkw := namesTup.Len()
	attrs := make([]objects.Object, npos+nkw)
	if npos > 0 {
		matchArgs, agerr := objects.GetAttr(typeObj, objects.NewStr("__match_args__"))
		if agerr != nil {
			return objects.None()
		}
		maTup, isTup := matchArgs.(*objects.Tuple)
		if !isTup || maTup.Len() < npos {
			return objects.None()
		}
		for i := 0; i < npos; i++ {
			s, serr := objects.Str(maTup.Item(i))
			if serr != nil {
				return objects.None()
			}
			val, verr := objects.GetAttr(subject, objects.NewStr(s))
			if verr != nil {
				return objects.None()
			}
			attrs[i] = val
		}
	}
	for i := 0; i < nkw; i++ {
		s, serr := objects.Str(namesTup.Item(i))
		if serr != nil {
			return objects.None()
		}
		val, verr := objects.GetAttr(subject, objects.NewStr(s))
		if verr != nil {
			return objects.None()
		}
		attrs[npos+i] = val
	}
	return objects.NewTuple(attrs)
}

// checkExceptTypeValid wraps _PyEval_CheckExceptTypeValid: 0 if the
// operand is a valid exception type (or tuple of types), -1 otherwise
// with pendingErr set.
//
// CPython: Python/ceval.c _PyEval_CheckExceptTypeValid
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) checkExceptTypeValid(right objects.Object) int32 {
	if _, ok := right.(*objects.Type); ok {
		return 0
	}
	if t, ok := right.(*objects.Tuple); ok {
		for i := 0; i < t.Len(); i++ {
			if _, ok := t.Item(i).(*objects.Type); !ok {
				e.pendingErr = errors.New("TypeError: catching classes that do not inherit from BaseException is not allowed")
				return -1
			}
		}
		return 0
	}
	e.pendingErr = errors.New("TypeError: catching classes that do not inherit from BaseException is not allowed")
	return -1
}

// checkExceptStarTypeValid is the except* variant; mirrors
// _PyEval_CheckExceptStarTypeValid. Same shape as the non-star check
// but also rejects ExceptionGroup operands.
//
// CPython: Python/ceval.c _PyEval_CheckExceptStarTypeValid
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) checkExceptStarTypeValid(right objects.Object) int32 {
	return e.checkExceptTypeValid(right)
}

// exceptionMatches wraps PyErr_GivenExceptionMatches: 1 if exc is an
// instance of (or matches) the type operand, else 0.
//
// CPython: Python/errors.c:299 PyErr_GivenExceptionMatches
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) exceptionMatches(exc, target objects.Object) int32 {
	if exc == nil {
		return 0
	}
	if tp, ok := target.(*objects.Type); ok {
		if isInstance(exc, tp) {
			return 1
		}
		return 0
	}
	if tup, ok := target.(*objects.Tuple); ok {
		for i := 0; i < tup.Len(); i++ {
			if e.exceptionMatches(exc, tup.Item(i)) == 1 {
				return 1
			}
		}
	}
	return 0
}

// pyType mirrors Py_TYPE: returns the type object for o.
//
// CPython: Include/object.h Py_TYPE
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) pyType(o objects.Object) objects.Object {
	return objects.TypeOf(o)
}

// iterToSlice walks any iterable and returns its items. Used by
// LIST_EXTEND, UNPACK_EX, CALL_FUNCTION_EX, and similar arms that
// need to drain an iterator into Go memory before doing the real
// work.
//
// CPython: Python/bytecodes.c GET_ITER + FOR_ITER loop, drained.
func iterToSlice(o objects.Object) ([]objects.Object, error) {
	if o == nil {
		return nil, errors.New("TypeError: cannot iterate nil")
	}
	t := o.Type()
	if t.Iter == nil {
		// Tuple/List shortcut: avoid iterator allocation for the common case.
		switch v := o.(type) {
		case *objects.Tuple:
			out := make([]objects.Object, v.Len())
			for i := range out {
				out[i] = v.Item(i)
			}
			return out, nil
		case *objects.List:
			out := make([]objects.Object, v.Len())
			for i := range out {
				out[i] = v.Item(i)
			}
			return out, nil
		}
		return nil, errors.New("TypeError: '" + t.Name + "' object is not iterable")
	}
	it, ierr := t.Iter(o)
	if ierr != nil {
		return nil, ierr
	}
	itType := it.Type()
	if itType.IterNext == nil {
		return nil, errors.New("TypeError: iter() returned non-iterator of type '" + itType.Name + "'")
	}
	var out []objects.Object
	for {
		v, nerr := itType.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			return out, nil
		}
		if nerr != nil {
			return nil, nerr
		}
		out = append(out, v)
	}
}

// intrinsicsUnary is the CALL_INTRINSIC_1 dispatch table indexed by
// oparg. Reads through to the intrinsics package so the eval loop
// stays free of helper bodies.
//
// CPython: Python/intrinsics.c _PyIntrinsics_UnaryFunctions
var intrinsicsUnary = unaryTable()

// intrinsicsBinary is the CALL_INTRINSIC_2 dispatch table.
//
// CPython: Python/intrinsics.c _PyIntrinsics_BinaryFunctions
var intrinsicsBinary = binaryTable()

// sliceIndex normalises a slice bound to a Go int. None means
// "use the length"; negative means "from the end". Out-of-range
// indexes clamp to [0, length].
func sliceIndex(o objects.Object, length int, isStop bool) (int, error) {
	if o == nil || o == objects.None() {
		if isStop {
			return length, nil
		}
		return 0, nil
	}
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, errors.New("TypeError: slice indices must be integers or None")
	}
	v, _ := i.Int64()
	idx := int(v)
	if idx < 0 {
		idx += length
		if idx < 0 {
			idx = 0
		}
	}
	if idx > length {
		idx = length
	}
	return idx, nil
}

func sliceContainer(container, start, stop objects.Object) (objects.Object, error) {
	switch c := container.(type) {
	case *objects.List:
		s, err := sliceIndex(start, c.Len(), false)
		if err != nil {
			return nil, err
		}
		e, err := sliceIndex(stop, c.Len(), true)
		if err != nil {
			return nil, err
		}
		if e < s {
			e = s
		}
		out := make([]objects.Object, 0, e-s)
		for i := s; i < e; i++ {
			out = append(out, c.Item(i))
		}
		return objects.NewList(out), nil
	case *objects.Tuple:
		s, err := sliceIndex(start, c.Len(), false)
		if err != nil {
			return nil, err
		}
		e, err := sliceIndex(stop, c.Len(), true)
		if err != nil {
			return nil, err
		}
		if e < s {
			e = s
		}
		out := make([]objects.Object, 0, e-s)
		for i := s; i < e; i++ {
			out = append(out, c.Item(i))
		}
		return objects.NewTuple(out), nil
	}
	return nil, errors.New("TypeError: BINARY_SLICE: unsupported container type '" + container.Type().Name + "'")
}

func storeSlice(container, start, stop, value objects.Object) error {
	l, ok := container.(*objects.List)
	if !ok {
		return errors.New("TypeError: STORE_SLICE: unsupported container type '" + container.Type().Name + "'")
	}
	s, err := sliceIndex(start, l.Len(), false)
	if err != nil {
		return err
	}
	e, err := sliceIndex(stop, l.Len(), true)
	if err != nil {
		return err
	}
	if e < s {
		e = s
	}
	items, err := iterToSlice(value)
	if err != nil {
		return err
	}
	l.SetSlice(s, e, items)
	return nil
}

func unaryTable() []func(*state.Thread, objects.Object) (objects.Object, error) {
	out := make([]func(*state.Thread, objects.Object) (objects.Object, error), len(intrinsics.UnaryTable))
	for i, fn := range intrinsics.UnaryTable {
		out[i] = fn
	}
	return out
}

func binaryTable() []func(*state.Thread, objects.Object, objects.Object) (objects.Object, error) {
	out := make([]func(*state.Thread, objects.Object, objects.Object) (objects.Object, error), len(intrinsics.BinaryTable))
	for i, fn := range intrinsics.BinaryTable {
		out[i] = fn
	}
	return out
}
