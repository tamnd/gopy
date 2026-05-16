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
	"github.com/tamnd/gopy/stackref"
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

// peekSliceBottomFirst returns the n stack slots that sit between
// depth `topOffset` and `topOffset+n-1` from TOS, ordered bottom-first
// so the result reads like a CPython sized-input region: index 0 is the
// stack-bottom slot (the first input the bytecode compiler pushed),
// index n-1 is the slot closest to TOS.
//
// Used by translated arms whose body indexes a sized input like
// `args[oparg]`. The slice is a copy; mutating an entry does not affect
// the underlying stack.
//
// CPython: Tools/cases_generator/stack.py Local.from_memory_effect —
// sized inputs bind to a stack_pointer slice without copying, but the
// gopy refcount-only path doesn't need the aliasing.
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) peekSliceBottomFirst(topOffset, n int) []stackref.Ref {
	out := make([]stackref.Ref, n)
	for i := 0; i < n; i++ {
		out[i] = e.peek(topOffset + n - 1 - i)
	}
	return out
}

// setPendingErr stashes a generic synthetic exception on pendingErr,
// keyed by the CPython helper name that set it. Translator-emitted
// statements for `PyErr_Format(...)` / `_PyEval_FormatExcUnbound(...)`
// and friends route through here; the surrounding ERROR_NO_POP /
// ERROR_IF picks the error up via e.error().
//
// CPython: Python/errors.c PyErr_Format — sets tstate->current_exception
// with a formatted message; gopy preserves the failure signal but
// drops the format string since the eval loop only inspects existence.
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) setPendingErr(name string) {
	e.pendingErr = fmt.Errorf("vm: %s", name)
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

// dictPop wraps PyDict_Pop: removes key from dict. Returns 1 if found
// and removed, 0 if missing, -1 on error (with pendingErr set). The
// third arg models the C output pointer; gopy ignores it because the
// only DELETE_* caller passes NULL.
//
// CPython: Objects/dictobject.c:5044 PyDict_Pop
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) dictPop(dict, key, _ objects.Object) int32 {
	d, ok := dict.(*objects.Dict)
	if !ok {
		e.pendingErr = errors.New("TypeError: PyDict_Pop expected dict")
		return -1
	}
	v, err := d.GetItem(key)
	if err != nil {
		e.pendingErr = err
		return -1
	}
	if v == nil {
		return 0
	}
	if derr := d.DelItem(key); derr != nil {
		e.pendingErr = derr
		return -1
	}
	return 1
}

// dictMergeEx wraps _PyDict_MergeEx: merges b into a. override==2 means
// "raise on duplicate key" (DICT_MERGE semantics).
//
// CPython: Objects/dictobject.c:3232 _PyDict_MergeEx
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) dictMergeEx(a, b objects.Object, override int32) int32 {
	d, ok := a.(*objects.Dict)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyDict_MergeEx expected dict")
		return -1
	}
	items, err := iterToSlice(b)
	if err != nil {
		// Fallback: maybe b is a mapping. Walk its items() via keys().
		if bd, isd := b.(*objects.Dict); isd {
			for _, k := range bd.Keys() {
				v, _ := bd.GetItem(k)
				if override == 2 {
					if existing, _ := d.GetItem(k); existing != nil {
						if s, sok := k.(*objects.Unicode); sok {
							e.pendingErr = fmt.Errorf("TypeError: got multiple values for keyword argument '%s'", s.Value())
						} else {
							e.pendingErr = errors.New("TypeError: duplicate keyword argument")
						}
						return -1
					}
				}
				if serr := d.SetItem(k, v); serr != nil {
					e.pendingErr = serr
					return -1
				}
			}
			return 0
		}
		e.pendingErr = err
		return -1
	}
	_ = items
	return 0
}

// listExtend wraps _PyList_Extend: appends every item from iter to list.
//
// CPython: Objects/listobject.c:1029 _PyList_Extend
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) listExtend(list, iter objects.Object) int32 {
	l, ok := list.(*objects.List)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyList_Extend expected list")
		return -1
	}
	items, err := iterToSlice(iter)
	if err != nil {
		e.pendingErr = err
		return -1
	}
	for _, it := range items {
		l.Append(it)
	}
	return 0
}

// listAppendTakeRef wraps _PyList_AppendTakeRef.
//
// CPython: Objects/listobject.c:362 _PyList_AppendTakeRef
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) listAppendTakeRef(list, item objects.Object) int32 {
	l, ok := list.(*objects.List)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyList_AppendTakeRef expected list")
		return -1
	}
	l.Append(item)
	return 0
}

// setAddTakeRef wraps _PySet_AddTakeRef.
//
// CPython: Objects/setobject.c:2433 _PySet_AddTakeRef
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) setAddTakeRef(set, elem objects.Object) int32 {
	s, ok := set.(*objects.Set)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PySet_AddTakeRef expected set")
		return -1
	}
	if err := s.Add(elem); err != nil {
		e.pendingErr = err
		return -1
	}
	return 0
}

// setUpdate wraps _PySet_Update.
//
// CPython: Objects/setobject.c:1942 _PySet_Update
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) setUpdate(set, iter objects.Object) int32 {
	s, ok := set.(*objects.Set)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PySet_Update expected set")
		return -1
	}
	items, err := iterToSlice(iter)
	if err != nil {
		e.pendingErr = err
		return -1
	}
	for _, it := range items {
		if aerr := s.Add(it); aerr != nil {
			e.pendingErr = aerr
			return -1
		}
	}
	return 0
}

// setNew wraps PySet_New: build a fresh set, optionally seeded from
// iter. iter==nil means "empty set".
//
// CPython: Objects/setobject.c:2419 PySet_New
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) setNew(iter objects.Object) objects.Object {
	s := objects.NewSet()
	if iter == nil {
		return s
	}
	items, err := iterToSlice(iter)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	for _, it := range items {
		if aerr := s.Add(it); aerr != nil {
			e.pendingErr = aerr
			return nil
		}
	}
	return s
}

// objectLength wraps PyObject_Length: returns len(o) or -1 on error.
//
// CPython: Objects/abstract.c:55 PyObject_Size
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) objectLength(o objects.Object) int32 {
	n, err := objects.Length(o)
	if err != nil {
		e.pendingErr = err
		return -1
	}
	return int32(n)
}

// cellNew wraps PyCell_New: allocates a cell holding initial. initial
// may be nil (unbound cell).
//
// CPython: Objects/cellobject.c:9 PyCell_New
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) cellNew(initial objects.Object) objects.Object {
	return objects.NewCell(initial)
}

// cellSwapTakeRef wraps PyCell_SwapTakeRef: atomically replaces the
// cell's contents and returns the previous value (nil if unbound).
//
// CPython: Objects/cellobject.c:60 PyCell_SwapTakeRef
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) cellSwapTakeRef(cell, newVal objects.Object) objects.Object {
	c, ok := cell.(*objects.Cell)
	if !ok {
		e.pendingErr = errors.New("TypeError: PyCell_SwapTakeRef expected cell")
		return nil
	}
	old := c.Contents
	c.Contents = newVal
	return old
}

// getANext wraps _PyEval_GetANext. Returns the awaitable for iter's
// next async value, or nil on error.
//
// CPython: Python/ceval.c:3562 _PyEval_GetANext
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) getANext(iter objects.Object) objects.Object {
	t := iter.Type()
	if t.Async == nil || t.Async.Anext == nil {
		e.pendingErr = fmt.Errorf(
			"TypeError: 'async for' requires an iterator with __anext__ method, got %s", t.Name)
		return nil
	}
	next, err := t.Async.Anext(iter)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	if _, isAG := iter.(*objects.AsyncGenerator); !isAG {
		wrapped, werr := getAwaitableIter(next)
		if werr != nil {
			e.pendingErr = fmt.Errorf(
				"TypeError: 'async for' received an invalid object from __anext__: %s",
				next.Type().Name)
			return nil
		}
		next = wrapped
	}
	return next
}

// tupleFromStackRef wraps _PyTuple_FromStackRefStealOnSuccess. The
// translator passes the input scratch name (a Go slice of objects.Object
// since the action body's `values[oparg]` sized input is rendered as a
// peek loop above) and the count.
//
// CPython: Objects/tupleobject.c:226 _PyTuple_FromStackRefStealOnSuccess
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) tupleFromStackRef(values []stackref.Ref, n uint32) objects.Object {
	if int(n) > len(values) {
		e.pendingErr = errors.New("BUILD_TUPLE: count exceeds values slice")
		return nil
	}
	items := make([]objects.Object, n)
	for i := range items {
		items[i] = values[i].AsObject()
	}
	return objects.NewTuple(items)
}

// listFromStackRef wraps _PyList_FromStackRefStealOnSuccess.
//
// CPython: Objects/listobject.c:3146 _PyList_FromStackRefStealOnSuccess
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) listFromStackRef(values []stackref.Ref, n uint32) objects.Object {
	if int(n) > len(values) {
		e.pendingErr = errors.New("BUILD_LIST: count exceeds values slice")
		return nil
	}
	items := make([]objects.Object, n)
	for i := range items {
		items[i] = values[i].AsObject()
	}
	return objects.NewList(items)
}

// longFromSsizeT wraps PyLong_FromSsize_t: boxes a Go int as a Python
// int.
//
// CPython: Objects/longobject.c:1488 PyLong_FromSsize_t
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) longFromSsizeT(n int32) objects.Object {
	return objects.NewInt(int64(n))
}

// longIsZero wraps _PyLong_IsZero. The TO_BOOL_INT body asks whether a
// PyLong's value is exactly 0 to pick between PyStackRef_False and
// PyStackRef_True without a full comparison.
//
// CPython: Objects/longobject.c _PyLong_IsZero
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) longIsZero(o objects.Object) bool {
	if i, ok := o.(*objects.Int); ok {
		return i.Sign() == 0
	}
	return false
}

// cellGetStackRef wraps _PyCell_GetStackRef: returns the cell's contents
// (nil if unbound).
//
// CPython: Include/cpython/cellobject.h _PyCell_GetStackRef
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) cellGetStackRef(cell objects.Object) stackref.Ref {
	c, ok := cell.(*objects.Cell)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyCell_GetStackRef expected cell")
		return stackref.Null
	}
	if c.Contents == nil {
		return stackref.Null
	}
	return stackref.FromObject(c.Contents)
}

// getAwaitable wraps _PyEval_GetAwaitable. opcode is a hint CPython uses
// for tailored error messages; gopy currently ignores it.
//
// CPython: Python/ceval.c:3525 _PyEval_GetAwaitable
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) getAwaitable(iter objects.Object, opcode uint32) objects.Object {
	_ = opcode
	out, err := getAwaitableIter(iter)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return out
}

// dictUpdate wraps PyDict_Update: merges b into a without duplicate-key
// checking.
//
// CPython: Objects/dictobject.c:3354 PyDict_Update
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) dictUpdate(a, b objects.Object) int32 {
	d, ok := a.(*objects.Dict)
	if !ok {
		e.pendingErr = errors.New("TypeError: PyDict_Update expected dict")
		return -1
	}
	if bd, ok := b.(*objects.Dict); ok {
		for _, k := range bd.Keys() {
			v, _ := bd.GetItem(k)
			if serr := d.SetItem(k, v); serr != nil {
				e.pendingErr = serr
				return -1
			}
		}
		return 0
	}
	e.pendingErr = errors.New("TypeError: PyDict_Update expected dict source")
	return -1
}

// templateBuild wraps _PyTemplate_Build: builds a PEP 750 t-string from
// its strings/interpolations tuples.
//
// CPython: Objects/templateobject.c _PyTemplate_Build
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) templateBuild(strings, interpolations objects.Object) objects.Object {
	return objects.NewTemplateStr(strings, interpolations)
}

// objectFormat wraps PyObject_Format. spec may be nil for an empty
// format spec.
//
// CPython: Objects/abstract.c:776 PyObject_Format
//
//nolint:unused // emitted by tools/bytecodes_gen/action.go translator output
func (e *evalState) objectFormat(obj, spec objects.Object) objects.Object {
	specStr := ""
	if spec != nil {
		s, ok := spec.(*objects.Unicode)
		if !ok {
			e.pendingErr = errors.New("TypeError: format spec must be str")
			return nil
		}
		specStr = s.Value()
	}
	out, err := objects.Format(obj, specStr)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return objects.NewStr(out)
}
