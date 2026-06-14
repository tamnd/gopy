// Small helpers the dispatch arms call into. The action translator
// (1621/B6) emits some of these; the hand-written panel uses them
// directly. CPython has them as macros / inline helpers in
// Python/ceval_macros.h and Python/ceval.c.
package vm

import (
	"errors"
	"fmt"
	"strings"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
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
// `args[oparg]`. The returned slice aliases LocalsPlus directly so
// hot opcodes like BUILD_SLICE / BUILD_LIST / BUILD_TUPLE skip the
// per-instruction allocation; callers read but never mutate it, and
// every consumer copies into its own buffer before the stack moves.
//
// CPython: Tools/cases_generator/stack.py Local.from_memory_effect —
// sized inputs bind to a stack_pointer slice without copying.
func (e *evalState) peekSliceBottomFirst(topOffset, n int) []stackref.Ref {
	if n == 0 {
		return nil
	}
	f := e.f
	top := f.StackBase + f.StackTop - topOffset
	return f.LocalsPlus[top-n : top]
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
func (e *evalState) error(label string) error { //nolint:unparam // label reserved for ERROR_NO_POP / RERAISE arms the translator will add
	if e.pendingErr != nil {
		err := e.pendingErr
		e.pendingErr = nil
		return err
	}
	return fmt.Errorf("vm: error label %q reached without pending exception", label)
}

// errOccurred mirrors CPython's _PyErr_Occurred(tstate): true when a
// pending exception is on the thread state. Under gopy the eval loop
// stashes pending failures on evalState.pendingErr, so the wrapper just
// reports whether that slot is populated.
//
// CPython: Python/errors.c _PyErr_Occurred.
func (e *evalState) errOccurred() bool {
	return e.pendingErr != nil
}

// errExceptionMatches mirrors CPython's _PyErr_ExceptionMatches: true
// when the running exception is an instance (or subclass) of t.
// Reads pendingErr. *pyerrors.Exception carries the exact type;
// generic Go errors get classified via synthesizeException's prefix
// table so a `try: ... except ValueError:` still catches a
// "ValueError: ..." Go error.
//
// CPython: Python/errors.c PyErr_ExceptionMatches (calls
// PyErr_GivenExceptionMatches with tstate->current_exception).
func (e *evalState) errExceptionMatches(t *objects.Type) bool {
	if e.pendingErr == nil || t == nil {
		return false
	}
	// pendingErr is always a Go error (Exception itself does not
	// implement error). Promote to a typed Exception via the same
	// prefix table eval_unwind uses, then ask IsSubtype.
	exc := synthesizeException(e.pendingErr)
	return pyerrors.IsSubtype(exc.ExcType, t)
}

// pyNumberNegative is the translator-side wrapper for CPython's
// PyNumber_Negative. The body keeps the NULL-on-failure convention so
// the surrounding `ERROR_IF(res_o == NULL)` translation just works:
// the real Go error rides on e.pendingErr until e.error() retrieves it.
//
// CPython: Objects/abstract.c:1381 PyNumber_Negative
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
func (e *evalState) globals() objects.Object { return e.f.Globals }

func (e *evalState) builtinsDict() objects.Object { return e.f.Builtins }

func (e *evalState) localsDict() objects.Object { return e.f.Locals }

// commonConsts returns the interpreter's LOAD_COMMON_CONSTANT lookup
// table. CPython populates this during interpreter init in
// _PyInterpreterState_InitConsts; gopy delays the fill to the first
// read because the frame builtins (source of `all` / `any`) are not
// finalized until execution starts. After the first call the array
// is cached on state.Interpreter and reused.
//
// CPython: Python/pylifecycle.c:815 _PyInterpreterState_InitConsts
func (e *evalState) commonConsts() [state.NumCommonConstants]objects.Object {
	interp := e.ts.Interp()
	var out [state.NumCommonConstants]objects.Object
	ready := true
	for i := 0; i < state.NumCommonConstants; i++ {
		v, ok := interp.CommonConsts[i].(objects.Object)
		if !ok || v == nil {
			ready = false
			break
		}
		out[i] = v
	}
	if ready {
		return out
	}
	interp.CommonConsts[state.ConstantAssertionError] = pyerrors.PyExc_AssertionError
	interp.CommonConsts[state.ConstantNotImplementedError] = pyerrors.PyExc_NotImplementedError
	interp.CommonConsts[state.ConstantBuiltinTuple] = objects.TupleType
	if v, ok := lookupIn(e.f.Builtins, objects.NewStr("all")); ok {
		interp.CommonConsts[state.ConstantBuiltinAll] = v
	}
	if v, ok := lookupIn(e.f.Builtins, objects.NewStr("any")); ok {
		interp.CommonConsts[state.ConstantBuiltinAny] = v
	}
	for i := 0; i < state.NumCommonConstants; i++ {
		if v, ok := interp.CommonConsts[i].(objects.Object); ok {
			out[i] = v
		}
	}
	return out
}

// loadName wraps CPython's _PyEval_LoadName: scan locals, then globals,
// then builtins for `name`. Returns nil-on-failure with pendingErr set,
// mirroring the NULL-on-failure convention the translator expects.
//
// CPython: Python/ceval.c:2789 _PyEval_LoadName
func (e *evalState) loadName(name objects.Object) objects.Object {
	if v, found, err := e.loadFromScope(e.f.Locals, name); err != nil {
		e.pendingErr = err
		return nil
	} else if found {
		return v
	}
	if v, found, err := e.loadFromScope(e.f.Globals, name); err != nil {
		e.pendingErr = err
		return nil
	} else if found {
		return v
	}
	if v, found, err := objects.MappingGetOptionalItem(e.f.Builtins, name); err != nil {
		e.pendingErr = err
		return nil
	} else if found {
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
func (e *evalState) importName(name, fromlist, level objects.Object) objects.Object {
	s, ok := name.(*objects.Unicode)
	if !ok {
		e.pendingErr = errors.New("TypeError: import name must be str")
		return nil
	}
	modname := s.Value()
	lvl := importLevel(level)
	pkgname := globalName(e.f.Globals)

	exec := &vmExecutor{ts: e.ts, builtins: callerBuiltins(e.f)}
	mod, ierr := imp.ImportModuleLevel(exec, modname, pkgname, lvl)
	if ierr != nil {
		if errors.Is(ierr, imp.ErrModuleNotFound) {
			pyerrors.SetModuleNotFound(e.ts, modname)
		}
		e.pendingErr = ierr
		return nil
	}
	// A non-empty fromlist drives _handle_fromlist: force-import any
	// submodule named in the fromlist that is not already an attribute,
	// so a later IMPORT_FROM/import_all_from finds it via plain getattr.
	// CPython runs this inside __import__ before returning the module.
	//
	// CPython: Lib/importlib/_bootstrap.py:1463 _handle_fromlist
	if !isEmptyFromlist(fromlist) {
		if herr := e.handleFromlist(mod, fromlist, false); herr != nil {
			e.pendingErr = herr
			return nil
		}
	}

	// When fromlist is empty (`import a.b.c`) return the top-level
	// package; otherwise return the deepest module so IMPORT_FROM can
	// extract attributes.
	//
	// CPython: Python/bytecodes.c IMPORT_NAME "return the head of the
	// dotted name" when fromlist is empty.
	if isEmptyFromlist(fromlist) && strings.Contains(modname, ".") {
		top := strings.SplitN(modname, ".", 2)[0]
		if tm, ok := imp.GetModule(top); ok {
			return tm
		}
	}
	return mod
}

// importFrom wraps _PyEval_ImportFrom: fetch `name` as an attribute of
// the already-imported module `from`.
//
// CPython: Python/ceval.c:3154 _PyEval_ImportFrom
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
func (e *evalState) dictSetItem(scope, key, value objects.Object) int32 {
	if err := storeIn(scope, key, value); err != nil {
		e.pendingErr = err
		return 1
	}
	return 0
}

// stackrefTypeFlags returns the boxed object's tp_flags. Translates the
// CPython compound `PyStackRef_TYPE(x)->tp_flags` so MATCH_MAPPING and
// MATCH_SEQUENCE can `& Py_TPFLAGS_MAPPING` etc. without exposing the
// PyTypeObject indirection at the bytecode action level.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_TYPE
func (e *evalState) stackrefTypeFlags(r stackref.Ref) uint64 {
	o := r.AsObject()
	if o == nil {
		return 0
	}
	t := o.Type()
	if t == nil {
		return 0
	}
	return t.TpFlags
}

// objectSetItem wraps PyObject_SetItem: container[sub] = val.
//
// CPython: Objects/abstract.c:175 PyObject_SetItem
func (e *evalState) objectSetItem(container, sub, val objects.Object) int32 {
	if err := objects.SetItem(container, sub, val); err != nil {
		e.pendingErr = err
		return 1
	}
	return 0
}

// objectDelItem wraps PyObject_DelItem: del container[sub].
//
// CPython: Objects/abstract.c:191 PyObject_DelItem
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
// CPython: Python/ceval.c:728 _PyEval_MatchKeys
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
	get, gerr := objects.LookupAttrString(subject, "get")
	if gerr != nil {
		e.pendingErr = gerr
		return nil
	}
	if get == nil {
		e.pendingErr = fmt.Errorf("TypeError: %s object has no attribute 'get'", subject.Type().Name)
		return nil
	}
	dummy := objects.NewInstance(objects.ObjectType())
	seen := objects.NewSet()
	values := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		k := keysTup.Item(i)
		contains, cerr := seen.Contains(k)
		if cerr != nil {
			e.pendingErr = cerr
			return nil
		}
		if contains {
			rep, _ := objects.Repr(k)
			e.pendingErr = fmt.Errorf("ValueError: mapping pattern checks duplicate key (%s)", rep)
			return nil
		}
		if aerr := seen.Add(k); aerr != nil {
			e.pendingErr = aerr
			return nil
		}
		v, cerr := objects.Call(get, objects.NewTuple([]objects.Object{k, dummy}), nil)
		if cerr != nil {
			e.pendingErr = cerr
			return nil
		}
		if v == dummy {
			return objects.None()
		}
		values[i] = v
	}
	return objects.NewTuple(values)
}

// matchClass wraps _PyEval_MatchClass: returns a tuple of extracted
// attributes, or None on no match, or nil on a real error.
//
// CPython: Python/ceval.c _PyEval_MatchClass
func (e *evalState) matchClass(subject, typeObj objects.Object, oparg uint32, names objects.Object) objects.Object {
	namesTup, ok := names.(*objects.Tuple)
	if !ok {
		e.pendingErr = errors.New("MATCH_CLASS: names not a tuple")
		return nil
	}
	tp, ok := typeObj.(*objects.Type)
	if !ok {
		e.pendingErr = errors.New("TypeError: called match pattern must be a class")
		return nil
	}
	if !isInstance(subject, tp) {
		return objects.None()
	}
	npos := int(oparg)
	nkw := namesTup.Len()
	attrs := make([]objects.Object, 0, npos+nkw)
	seen := make(map[string]struct{}, npos+nkw)
	if npos > 0 {
		posAttrs, noMatch, mcErr := resolvePositionalAttrs(subject, typeObj, tp, npos, seen)
		if mcErr != nil {
			e.pendingErr = mcErr
			return nil
		}
		if noMatch {
			return objects.None()
		}
		attrs = append(attrs, posAttrs...)
	}
	for i := 0; i < nkw; i++ {
		name, isStr := namesTup.Item(i).(*objects.Unicode)
		if !isStr {
			e.pendingErr = fmt.Errorf("TypeError: keyword sub-pattern names must be strings (got %s)", namesTup.Item(i).Type().Name)
			return nil
		}
		val, mcErr := matchClassAttr(subject, tp, name.Value(), seen)
		if mcErr != nil {
			e.pendingErr = mcErr
			return nil
		}
		if val == nil {
			return objects.None()
		}
		attrs = append(attrs, val)
	}
	return objects.NewTuple(attrs)
}

// checkExceptTypeValid wraps _PyEval_CheckExceptTypeValid: 0 if the
// operand is a valid exception type (or tuple of types), -1 otherwise
// with pendingErr set.
//
// CPython: Python/ceval.c _PyEval_CheckExceptTypeValid
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
func (e *evalState) checkExceptStarTypeValid(right objects.Object) int32 {
	return e.checkExceptTypeValid(right)
}

// exceptionMatches wraps PyErr_GivenExceptionMatches: 1 if exc is an
// instance of (or matches) the type operand, else 0.
//
// CPython: Python/errors.c:299 PyErr_GivenExceptionMatches
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
	it, ierr := objects.Iter(o)
	if ierr != nil {
		return nil, ierr
	}
	if it.Type().IterNext == nil {
		return nil, errors.New("TypeError: iter() returned non-iterator of type '" + it.Type().Name + "'")
	}
	var out []objects.Object
	for {
		v, nerr := objects.IterNext(it)
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
	case *objects.Unicode:
		sl := objects.NewSlice(start, stop, nil)
		return objects.StrGetSlice(c, sl)
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

// dictLoadGlobal wraps _PyDict_LoadGlobal: scan globals then builtins
// for name. Returns the bound value on hit, nil with no pendingErr on a
// pure miss (so the caller can synthesize NameError), and nil with
// pendingErr set on a real lookup failure.
//
// CPython: Objects/dictobject.c _PyDict_LoadGlobal
func (e *evalState) dictLoadGlobal(globals, builtins, name objects.Object) objects.Object {
	if v, found, err := objects.MappingGetOptionalItem(globals, name); err != nil {
		e.pendingErr = err
		return nil
	} else if found {
		return v
	}
	if v, found, err := objects.MappingGetOptionalItem(builtins, name); err != nil {
		e.pendingErr = err
		return nil
	} else if found {
		return v
	}
	return nil
}

// dictNew returns a fresh empty dict. Mirrors CPython's PyDict_New,
// which can fail with MemoryError; under Go's GC NewDict is infallible
// so the helper never stashes a pendingErr.
//
// CPython: Objects/dictobject.c PyDict_New
func (e *evalState) dictNew() objects.Object {
	return objects.NewDict()
}

// dictFromItems builds a dict from an interleaved key/value array.
// values holds 2*n entries; even indices are keys, odd are values.
// Mirrors CPython's _PyDict_FromItems, which the bytecodes.c BUILD_MAP
// body calls with `(values_o, 2, values_o+1, 2, oparg)` (both ptr args
// alias the same array, just offset by one with stride 2).
//
// CPython: Objects/dictobject.c _PyDict_FromItems
// CPython: Python/bytecodes.c BUILD_MAP
func (e *evalState) dictFromItems(values []objects.Object, n uint32) objects.Object {
	if uint32(len(values)) < n*2 {
		e.pendingErr = fmt.Errorf("BUILD_MAP: values has %d entries, want %d", len(values), n*2)
		return nil
	}
	d := objects.NewDict()
	for i := uint32(0); i < n; i++ {
		if err := d.SetItem(values[i*2], values[i*2+1]); err != nil {
			e.pendingErr = err
			return nil
		}
	}
	return d
}

// handledException returns the currently-handled exception as an
// objects.Object, or nil if no handler is active. Mirrors the rvalue
// read `tstate->exc_info->exc_value` that PUSH_EXC_INFO uses to stash
// the outer handler before installing the new one.
//
// CPython: Python/bytecodes.c PUSH_EXC_INFO (read of exc_info->exc_value)
func (e *evalState) handledException() objects.Object {
	h := e.ts.HandledException()
	if h == nil {
		return nil
	}
	exc, ok := h.(*pyerrors.Exception)
	if !ok {
		return nil
	}
	return exc
}

// setHandledException installs obj as the currently-handled exception,
// or clears the slot when obj is nil. Mirrors the lvalue write
// `tstate->exc_info->exc_value = ...` plus the Py_XSETREF macro that
// CPython uses in POP_EXCEPT / PUSH_EXC_INFO. A non-Exception object
// (e.g. None, which CPython stores as a sentinel) clears the slot,
// matching the hand-written POP_EXCEPT arm in eval_simple.go.
//
// CPython: Python/bytecodes.c POP_EXCEPT, PUSH_EXC_INFO
func (e *evalState) setHandledException(obj objects.Object) {
	if obj == nil {
		pyerrors.SetHandled(e.ts, nil)
		return
	}
	exc, ok := obj.(*pyerrors.Exception)
	if !ok {
		pyerrors.SetHandled(e.ts, nil)
		return
	}
	pyerrors.SetHandled(e.ts, exc)
}

// callIntrinsic1 dispatches `_PyIntrinsics_UnaryFunctions[oparg].func(tstate, x)`.
// On error returns nil with pendingErr set; the translator's
// ERROR_IF(res == NULL) pattern picks that up.
//
// CPython: Python/bytecodes.c CALL_INTRINSIC_1
func (e *evalState) callIntrinsic1(oparg uint32, value objects.Object) objects.Object {
	if int(oparg) >= len(intrinsicsUnary) || intrinsicsUnary[oparg] == nil {
		e.pendingErr = fmt.Errorf("CALL_INTRINSIC_1: unknown id %d", oparg)
		return nil
	}
	res, err := intrinsicsUnary[oparg](e.ts, value)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return res
}

// callIntrinsic2 dispatches `_PyIntrinsics_BinaryFunctions[oparg].func(tstate, v2, v1)`.
// Argument order matches the C body byte-for-byte.
//
// CPython: Python/bytecodes.c CALL_INTRINSIC_2
func (e *evalState) callIntrinsic2(oparg uint32, value2, value1 objects.Object) objects.Object {
	if int(oparg) >= len(intrinsicsBinary) || intrinsicsBinary[oparg] == nil {
		e.pendingErr = fmt.Errorf("CALL_INTRINSIC_2: unknown id %d", oparg)
		return nil
	}
	res, err := intrinsicsBinary[oparg](e.ts, value2, value1)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return res
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
// "raise on duplicate key" (DICT_MERGE semantics). The dispatchGen path
// calls this for kwargs unpacking; trySimple's DICT_MERGE arm bypasses
// it and goes through dictMergeKwargs + formatKwargsError so the error
// message picks up the function's qualname.
//
// CPython: Objects/dictobject.c:3232 _PyDict_MergeEx
func (e *evalState) dictMergeEx(a, b objects.Object, override int32) int32 {
	d, ok := a.(*objects.Dict)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyDict_MergeEx expected dict")
		return -1
	}
	if merr := dictMergeKwargs(d, b); merr != nil {
		var dup *kwargsDuplicateErr
		if errors.As(merr, &dup) && override == 2 {
			e.pendingErr = fmt.Errorf("%s", dup.Error())
			return -1
		}
		e.pendingErr = merr
		return -1
	}
	return 0
}

// kwargsDuplicateErr is the sentinel dictMergeKwargs returns when the
// override-on-duplicate path trips. The DICT_MERGE arm catches this
// and reformats it with the function name through formatKwargsError,
// mirroring CPython's KeyError-percolation in _PyEval_FormatKwargsError.
type kwargsDuplicateErr struct {
	key objects.Object
}

func (e *kwargsDuplicateErr) Error() string {
	if s, ok := e.key.(*objects.Unicode); ok {
		return "TypeError: got multiple values for keyword argument '" + s.Value() + "'"
	}
	r, _ := objects.Repr(e.key)
	if r != "" {
		return "TypeError: got multiple values for keyword argument '" + r + "'"
	}
	return "TypeError: got multiple values for keyword argument"
}

// kwargsNotMappingErr is returned when the source has no keys() method.
// The DICT_MERGE arm rewrites it to "X argument after ** must be a
// mapping, not Y" so the function name is part of the message.
type kwargsNotMappingErr struct {
	src objects.Object
}

func (e *kwargsNotMappingErr) Error() string {
	return "TypeError: argument after ** must be a mapping, not " + e.src.Type().Name
}

// dictMergeKwargs is the DICT_MERGE / _PyDict_MergeEx slow path with
// override-on-duplicate semantics. Tries the dict fast path first,
// then falls back to b.keys() + b[key] iteration so mapping subclasses
// (and the CrazyDict-style mid-iteration mutation guard) work.
//
// CPython: Objects/dictobject.c:3247 dict_merge (override == 2)
func dictMergeKwargs(d *objects.Dict, b objects.Object) error {
	// Fast path for exact dict instances: iterate internal order directly.
	// Dict subclasses (e.g. OrderedDict) may override keys() to return a
	// different order than the underlying storage; fall through to the slow
	// path so their Python-level keys() is called.
	//
	// CPython: Objects/dictobject.c:3207 dict_merge (exact-dict fast path)
	if bd, ok := b.(*objects.Dict); ok && bd.Type() == objects.DictType {
		for _, k := range bd.Keys() {
			if existing, _ := d.GetItem(k); existing != nil {
				return &kwargsDuplicateErr{key: k}
			}
			v, _ := bd.GetItem(k)
			if serr := d.SetItem(k, v); serr != nil {
				return serr
			}
		}
		return nil
	}
	keysFn, gerr := objects.GetAttr(b, objects.NewStr("keys"))
	if gerr != nil {
		return &kwargsNotMappingErr{src: b}
	}
	keysObj, cerr := objects.CallNoArgs(keysFn)
	if cerr != nil {
		return cerr
	}
	it, ierr := objects.Iter(keysObj)
	if ierr != nil {
		return ierr
	}
	for {
		k, nerr := objects.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			return nil
		}
		if nerr != nil {
			return nerr
		}
		if existing, _ := d.GetItem(k); existing != nil {
			return &kwargsDuplicateErr{key: k}
		}
		v, ierr := objects.GetItem(b, k)
		if ierr != nil {
			return ierr
		}
		if serr := d.SetItem(k, v); serr != nil {
			return serr
		}
	}
}

// formatKwargsError wraps a dictMergeKwargs error in the same shape
// CPython's _PyEval_FormatKwargsError produces. Duplicate-key and
// not-a-mapping errors pick up the function's qualname prefix; any
// other error percolates unchanged.
//
// CPython: Python/ceval.c:3410 _PyEval_FormatKwargsError
func formatKwargsError(callable objects.Object, err error) error {
	funcstr := objectFunctionStr(callable)
	var dup *kwargsDuplicateErr
	if errors.As(err, &dup) {
		var keystr string
		if s, ok := dup.key.(*objects.Unicode); ok {
			keystr = s.Value()
		} else {
			keystr, _ = objects.Repr(dup.key)
		}
		if keystr != "" {
			return fmt.Errorf("TypeError: %s got multiple values for keyword argument '%s'", funcstr, keystr)
		}
		return fmt.Errorf("TypeError: %s got multiple values for keyword argument", funcstr)
	}
	var notMap *kwargsNotMappingErr
	if errors.As(err, &notMap) {
		return fmt.Errorf("TypeError: %s argument after ** must be a mapping, not %s", funcstr, notMap.src.Type().Name)
	}
	return err
}

// objectFunctionStr mirrors _PyObject_FunctionStr: returns
// "<module>.<qualname>()" when both are set and module != 'builtins',
// "<qualname>()" otherwise, falling back to str(x) when __qualname__
// is unset.
//
// CPython: Objects/object.c:973 _PyObject_FunctionStr
func objectFunctionStr(x objects.Object) string {
	if x == nil {
		return ""
	}
	qn, _ := objects.GetAttr(x, objects.NewStr("__qualname__"))
	if qn == nil {
		s, _ := objects.Str(x)
		return s
	}
	qstr, _ := objects.Str(qn)
	mod, _ := objects.GetAttr(x, objects.NewStr("__module__"))
	if mod != nil && mod != objects.None() {
		mstr, _ := objects.Str(mod)
		if mstr != "" && mstr != "builtins" {
			return mstr + "." + qstr + "()"
		}
	}
	return qstr + "()"
}

// listExtend wraps _PyList_Extend: appends every item from iter to list.
//
// CPython: Objects/listobject.c:1029 _PyList_Extend
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

// listAppendTakeRef wraps _PyList_AppendTakeRef. CPython steals the
// item ref. gopy's dispatch arm drop(1) after the call will Decref the
// item slot, so we Incref here to keep the list's owned reference alive.
//
// CPython: Objects/listobject.c:362 _PyList_AppendTakeRef
func (e *evalState) listAppendTakeRef(list, item objects.Object) int32 {
	l, ok := list.(*objects.List)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyList_AppendTakeRef expected list")
		return -1
	}
	// List.Append now takes its own counted reference on the item
	// (PyList_SET_ITEM + Py_INCREF), so the dispatch arm's following
	// drop(1) Closes the stack slot and the list keeps its own reference.
	// No extra Incref here.
	l.Append(item)
	return 0
}

// setAddTakeRef wraps _PySet_AddTakeRef. Steal semantics; balance the
// dispatch arm's drop(1) by Incref'ing the stored element so the set's
// reference outlives the popped stack slot.
//
// CPython: Objects/setobject.c:2433 _PySet_AddTakeRef
func (e *evalState) setAddTakeRef(set, elem objects.Object) int32 {
	s, ok := set.(*objects.Set)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PySet_AddTakeRef expected set")
		return -1
	}
	objects.Incref(elem)
	if err := s.Add(elem); err != nil {
		e.pendingErr = err
		return -1
	}
	return 0
}

// setUpdate wraps _PySet_Update.
//
// CPython: Objects/setobject.c:1942 _PySet_Update
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
func (e *evalState) cellNew(initial objects.Object) objects.Object {
	return objects.NewCell(initial)
}

// mappingGetOptionalItem wraps PyMapping_GetOptionalItem: look up key in
// o, returning (value, status) where status is CPython's int contract
// (1 found, 0 missing, -1 error). The looked-up value is nil when
// status is 0 or -1; the failure cause is stashed on pendingErr for the
// surrounding ERROR_IF to surface.
//
// CPython: Objects/abstract.c:207 PyMapping_GetOptionalItem
func (e *evalState) mappingGetOptionalItem(o, key objects.Object) (objects.Object, int32) {
	v, found, err := objects.MappingGetOptionalItem(o, key)
	if err != nil {
		e.pendingErr = err
		return nil, -1
	}
	if !found {
		return nil, 0
	}
	return v, 1
}

// cellSwapTakeRef wraps PyCell_SwapTakeRef: atomically replaces the
// cell's contents and returns the previous value (nil if unbound).
// The returned ref is "taken" from the cell, callers must Decref.
//
// CPython: Objects/cellobject.c:60 PyCell_SwapTakeRef
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

// sliceNew wraps PySlice_New. Build a slice object from start/stop/step.
// CPython surfaces NULL for "absent" step; the Go side maps the nil
// trio to Python None implicitly through objects.NewSlice.
//
// CPython: Objects/sliceobject.c PySlice_New
func (e *evalState) sliceNew(start, stop, step objects.Object) objects.Object {
	return objects.NewSlice(start, stop, step)
}

// cellSetTakeRef wraps PyCell_SetTakeRef: writes a value into a cell,
// stealing the caller's reference and Decref'ing whatever the cell
// previously held. Used by STORE_DEREF / DELETE_DEREF (the NULL form).
//
// CPython: Objects/cellobject.c PyCell_SetTakeRef
//
//	(Py_XDECREF(old_obj) before storing)
func (e *evalState) cellSetTakeRef(cell, newVal objects.Object) {
	c, ok := cell.(*objects.Cell)
	if !ok {
		e.pendingErr = errors.New("TypeError: PyCell_SetTakeRef expected cell")
		return
	}
	old := c.Contents
	c.Contents = newVal
	if old != nil {
		objects.Decref(old)
	}
}

// getANext wraps _PyEval_GetANext. Returns the awaitable for iter's
// next async value, or nil on error.
//
// CPython: Python/ceval.c:3562 _PyEval_GetANext
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
			// Mirror _PyErr_FormatFromCause: lift the underlying failure
			// off the thread state, chain it under a TypeError that names
			// the offender, and reinstall the TypeError. Without the
			// Clear+Raise dance, handleException sees the inner exception
			// already pinned on the thread state and never installs the
			// TypeError, so `err.__cause__` stays unset upstream.
			//
			// CPython: Python/ceval.c:3593 _PyEval_GetANext
			// CPython: Python/errors.c:1438 _PyErr_FormatFromCause
			cause := pyerrors.Occurred(e.ts)
			if cause == nil {
				cause = synthesizeException(werr)
			}
			pyerrors.Clear(e.ts)
			msg := fmt.Sprintf(
				"TypeError: 'async for' received an invalid object from __anext__: %s",
				next.Type().Name)
			exc := pyerrors.New(pyerrors.PyExc_TypeError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
			exc.Cause = cause
			exc.Context = cause
			exc.Suppress = true
			pyerrors.Raise(e.ts, exc)
			e.pendingErr = objects.NewRaisedError(exc, msg)
			return nil
		}
		next = wrapped
	}
	return next
}

// objectGetIter wraps PyObject_GetIter: returns iter(o) or nil on
// error. Drives GET_ITER and friends.
//
// CPython: Objects/abstract.c PyObject_GetIter
func (e *evalState) objectGetIter(o objects.Object) objects.Object {
	it, err := objects.Iter(o)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	if it == nil {
		e.pendingErr = fmt.Errorf("TypeError: iter() returned non-iterator for %s", o.Type().Name)
		return nil
	}
	return it
}

// tupleGetItem mirrors CPython's PyTuple_GET_ITEM macro: borrowed
// reference into a tuple's items array. The macro is unchecked in C,
// but the translator turns invariants into pendingErr fail paths so a
// mis-shaped opcode body surfaces an IndexError instead of a panic.
//
// CPython: Include/cpython/tupleobject.h PyTuple_GET_ITEM
func (e *evalState) tupleGetItem(o objects.Object, i uint32) objects.Object {
	t, ok := o.(*objects.Tuple)
	if !ok {
		e.pendingErr = fmt.Errorf("TypeError: PyTuple_GET_ITEM expected tuple, got %T", o)
		return nil
	}
	if int(i) >= t.Len() {
		e.pendingErr = fmt.Errorf("IndexError: tuple index %d out of range (len %d)", i, t.Len())
		return nil
	}
	return t.Item(int(i))
}

// stackrefsToObjects materializes the first n stackref slots into a
// []objects.Object. Mirrors CPython's STACKREFS_TO_PYOBJECTS macro,
// which builds a borrowed-ref PyObject* array alongside the stack-ref
// array so functions that need a flat PyObject** (e.g.
// _PyUnicode_JoinArray) can consume it. gopy's stackref.Ref already
// wraps an Object so the materialization is a simple slice build.
//
// CPython: Python/ceval_macros.h STACKREFS_TO_PYOBJECTS
func (e *evalState) stackrefsToObjects(refs []stackref.Ref, n uint32) []objects.Object {
	if int(n) > len(refs) {
		e.pendingErr = fmt.Errorf("STACKREFS_TO_PYOBJECTS: count %d exceeds slice (len %d)", n, len(refs))
		return nil
	}
	out := make([]objects.Object, n)
	for i := range out {
		out[i] = refs[i].AsObject()
	}
	return out
}

// unicodeJoinArray wraps CPython's _PyUnicode_JoinArray: concatenate
// `items[:n]` using `sep` as the separator. Returns nil on error with
// pendingErr set so the surrounding ERROR_IF translates as expected.
// Routes through objects.StrJoinUnicode so the writer's Finish() builds
// a *Unicode with kind/ascii/length populated, skipping the classify
// walk NewStr would otherwise force. This is BUILD_STRING's hot path.
//
// CPython: Objects/unicodeobject.c:10278 _PyUnicode_JoinArray
func (e *evalState) unicodeJoinArray(sep objects.Object, items []objects.Object, n uint32) objects.Object {
	sepStr, ok := sep.(*objects.Unicode)
	if !ok {
		e.pendingErr = fmt.Errorf("TypeError: _PyUnicode_JoinArray separator must be str, got %T", sep)
		return nil
	}
	if int(n) > len(items) {
		e.pendingErr = fmt.Errorf("_PyUnicode_JoinArray: count %d exceeds slice (len %d)", n, len(items))
		return nil
	}
	out, err := objects.StrJoinUnicode(sepStr, items[:n])
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return out
}

// tupleFromStackRef wraps _PyTuple_FromStackRefStealOnSuccess. The
// translator passes the input scratch name (a Go slice of objects.Object
// since the action body's `values[oparg]` sized input is rendered as a
// peek loop above) and the count.
//
// NewTuple now takes its own counted reference per item (PyTuple_SET_ITEM
// + the ownership invariant in tuple.go), so the items are read borrowed
// here; the dispatch arm's following drop(oparg) Closes each stack slot.
// No extra Incref, mirroring listFromStackRef.
//
// CPython: Objects/tupleobject.c:226 _PyTuple_FromStackRefStealOnSuccess
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

// listFromStackRef wraps _PyList_FromStackRefStealOnSuccess. Same
// rationale as tupleFromStackRef: balance the unconditional drop()
// that follows in the dispatch arm.
//
// CPython: Objects/listobject.c:3146 _PyList_FromStackRefStealOnSuccess
func (e *evalState) listFromStackRef(values []stackref.Ref, n uint32) objects.Object {
	if int(n) > len(values) {
		e.pendingErr = errors.New("BUILD_LIST: count exceeds values slice")
		return nil
	}
	// Allocating the list object and its item array is a tracked allocation
	// for the _testcapi.set_nomemory fault injector. CPython's BUILD_LIST
	// goes through _PyList_FromStackRefStealOnSuccess -> PyList_New, whose
	// allocation the armed PyMem hook can fail; gopy consumes the same fault
	// counter here so test_list.test_no_memory sees a real MemoryError.
	//
	// CPython: Objects/listobject.c:160 PyList_New (PyMem_Calloc failure)
	if objects.ConsumeAllocFault() {
		e.pendingErr = fmt.Errorf("MemoryError")
		return nil
	}
	// NewList takes its own counted reference per item (PyList_SET_ITEM +
	// Py_INCREF), so the items are read borrowed here; the dispatch arm's
	// following drop(oparg) Closes each stack slot. No extra Incref.
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
func (e *evalState) longFromSsizeT(n int32) objects.Object {
	return objects.NewInt(int64(n))
}

// longIsZero wraps _PyLong_IsZero. The TO_BOOL_INT body asks whether a
// PyLong's value is exactly 0 to pick between PyStackRef_False and
// PyStackRef_True without a full comparison.
//
// CPython: Objects/longobject.c _PyLong_IsZero
func (e *evalState) longIsZero(o objects.Object) bool {
	if i, ok := o.(*objects.Int); ok {
		return i.Sign() == 0
	}
	return false
}

// cellGetStackRef wraps PyCell_GET + PyStackRef_FromPyObjectNew: returns
// a new strong reference to the cell's contents (Null if unbound). The
// cell retains its own strong reference, so the returned ref owns one
// of its own. CPython's LOAD_DEREF / LOAD_FROM_DICT_OR_DEREF use this
// pairing so popping the stack does not drop the cell's contents.
//
// CPython: Python/bytecodes.c LOAD_DEREF (PyStackRef_FromPyObjectNew)
// CPython: Include/cpython/cellobject.h PyCell_GET
func (e *evalState) cellGetStackRef(cell objects.Object) stackref.Ref {
	c, ok := cell.(*objects.Cell)
	if !ok {
		e.pendingErr = errors.New("TypeError: _PyCell_GetStackRef expected cell")
		return stackref.Null
	}
	if c.Contents == nil {
		return stackref.Null
	}
	return stackref.FromObjectNew(c.Contents)
}

// getAwaitable wraps _PyEval_GetAwaitable. opcode is a hint CPython uses
// for tailored error messages; gopy currently ignores it.
//
// CPython: Python/ceval.c:3525 _PyEval_GetAwaitable
func (e *evalState) getAwaitable(iter objects.Object, oparg uint32) objects.Object {
	out, err := getAwaitableIter(iter)
	if err != nil {
		if msg := formatAwaitableError(iter.Type().Name, oparg); msg != nil {
			e.pendingErr = msg
		} else {
			e.pendingErr = err
		}
		return nil
	}
	// Awaited-already gate: when the returned iter is a coroutine that is
	// already suspended yielding from another awaitable, refuse to drive
	// it from a second parent. Mirrors CPython's _PyGen_yf(iter) != NULL
	// branch which captures the FRAME_SUSPENDED_YIELD_FROM state. The
	// suspended-yield-from predicate uses the same gates as cr_await:
	// started, not closed, not running, and a pending YieldFromTarget.
	//
	// CPython: Python/ceval.c:3649 _PyEval_GetAwaitable (PyCoro_CheckExact branch)
	if c, ok := out.(*objects.Coroutine); ok && c.IsSuspendedYieldFrom() {
		exc := pyerrors.New(pyerrors.PyExc_RuntimeError,
			objects.NewTuple([]objects.Object{
				objects.NewStr("coroutine is being awaited already"),
			}))
		pyerrors.Raise(e.ts, exc)
		e.pendingErr = objects.NewRaisedError(exc, "coroutine is being awaited already")
		return nil
	}
	return out
}

// dictUpdate wraps PyDict_Update: merges b into a without duplicate-key
// checking. The slow path mirrors dict_merge: pull b.keys(), iterate,
// and copy each key+value via b[key]. Non-mapping sources surface as
// "'X' object is not a mapping" so `{**1}` / `{**[]}` match CPython.
// Iteration-time RuntimeErrors (a CrazyDict-style "dictionary changed
// size during iteration") percolate up unchanged.
//
// CPython: Objects/dictobject.c:3354 PyDict_Update
// CPython: Objects/dictobject.c:3247 dict_merge (slow path via keys())
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
	keysFn, gerr := objects.GetAttr(b, objects.NewStr("keys"))
	if gerr != nil {
		e.pendingErr = fmt.Errorf("TypeError: '%s' object is not a mapping", b.Type().Name)
		return -1
	}
	keysObj, cerr := objects.CallNoArgs(keysFn)
	if cerr != nil {
		e.pendingErr = cerr
		return -1
	}
	it, ierr := objects.Iter(keysObj)
	if ierr != nil {
		e.pendingErr = ierr
		return -1
	}
	for {
		k, nerr := objects.IterNext(it)
		if errors.Is(nerr, objects.ErrStopIteration) {
			return 0
		}
		if nerr != nil {
			e.pendingErr = nerr
			return -1
		}
		v, gerr := objects.GetItem(b, k)
		if gerr != nil {
			e.pendingErr = gerr
			return -1
		}
		if serr := d.SetItem(k, v); serr != nil {
			e.pendingErr = serr
			return -1
		}
	}
}

// templateBuild wraps _PyTemplate_Build: builds a PEP 750 t-string from
// its strings/interpolations tuples.
//
// CPython: Objects/templateobject.c _PyTemplate_Build
func (e *evalState) templateBuild(strs, interpolations objects.Object) objects.Object {
	return objects.NewTemplateStr(strs, interpolations)
}

// interpolationBuild wraps _PyInterpolation_Build for the BUILD_INTERPOLATION
// arm. conversion is the FVC_* tag carried in oparg>>2; format may be nil for
// an empty format spec.
//
// CPython: Objects/interpolationobject.c:188 _PyInterpolation_Build
func (e *evalState) interpolationBuild(value, str objects.Object, conversion int, format objects.Object) objects.Object {
	ip, err := objects.NewInterpolation(value, str, conversion, format)
	if err != nil {
		e.pendingErr = err
		return nil
	}
	return ip
}

// objectFormat wraps PyObject_Format. spec may be nil for an empty
// format spec.
//
// CPython: Objects/abstract.c:776 PyObject_Format
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
