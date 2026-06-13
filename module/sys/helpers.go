package sys

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	pegen "github.com/tamnd/gopy/parser/pegen"
	"github.com/tamnd/gopy/state"
)

// DefaultRecursionLimit is CPython's Py_DEFAULT_RECURSION_LIMIT. The
// runtime sets this on every fresh interpreter; gopy keeps the same
// default until the per-thread recursion counter lands.
//
// CPython: Include/cpython/pystate.h Py_DEFAULT_RECURSION_LIMIT
const DefaultRecursionLimit = 1000

// recursionLimit holds the current recursion ceiling. CPython carries
// this on PyThreadState (py_recursion_limit); gopy parks it as a
// process-wide atomic until the per-thread counter is wired through
// the eval loop. Set/get round-trip through atomic so concurrent
// callers see a consistent value.
//
// CPython: Python/sysmodule.c:1352 sys_setrecursionlimit_impl,
// Python/sysmodule.c:1612 sys_getrecursionlimit_impl
var recursionLimit atomic.Int32

// intMaxStrDigits guards integer↔string conversion length. CPython added
// this in 3.11 to mitigate quadratic-time attacks via enormous int repr.
// Default is 4300 (CPython's _Py_STR_MAX_LEN).
//
// CPython: Python/sysmodule.c:2001 sys_get_int_max_str_digits_impl,
// Python/sysmodule.c:2026 sys_set_int_max_str_digits_impl
var intMaxStrDigits atomic.Int32

func init() {
	recursionLimit.Store(DefaultRecursionLimit)
	intMaxStrDigits.Store(4300)
	// Wire the parser hook so decimal integer literals exceeding the
	// current ceiling get a SyntaxError at parse/compile time.
	//
	// CPython: Objects/longobject.c:30 _MAX_STR_DIGITS_ERROR_FMT_TO_INT
	pegen.IntMaxStrDigitsHook = intMaxStrDigits.Load
	// Wire the runtime hook so int(str) and str(int) at the objects
	// layer reject values past the ceiling, matching what the parser
	// rejects at compile time.
	//
	// CPython: Objects/longobject.c:2049 long_to_decimal_string_internal,
	// Objects/longobject.c:2943 long_from_string_base
	objects.IntMaxStrDigitsHook = intMaxStrDigits.Load
}

// Bind stamps the runtime helpers onto d: exit, setrecursionlimit,
// getrecursionlimit, getrefcount, intern. They live in a separate
// step because they capture the current thread (for SystemExit and
// the type-error path on intern). Init populates the static surface
// that does not need a thread.
//
// CPython: Python/sysmodule.c:3892 _PySys_InitCore (function rows)
func Bind(d *objects.Dict, ts *state.Thread) error {
	helpers := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"exit", makeExit(ts)},
		{"setrecursionlimit", makeSetRecursionLimit(ts)},
		{"getrecursionlimit", getRecursionLimit},
		{"getrefcount", getRefcount},
		{"intern", makeIntern(ts)},
		{"get_int_max_str_digits", getIntMaxStrDigits},
		{"set_int_max_str_digits", setIntMaxStrDigits},
		{"get_coroutine_origin_tracking_depth", getCoroutineOriginTrackingDepth},
		{"set_coroutine_origin_tracking_depth", setCoroutineOriginTrackingDepth},
		{"get_asyncgen_hooks", getAsyncgenHooks},
		{"set_asyncgen_hooks", setAsyncgenHooks},
	}
	for _, h := range helpers {
		if err := setItem(d, h.name, objects.NewBuiltinFunction(h.name, h.fn)); err != nil {
			return err
		}
	}
	return nil
}

// makeExit ports sys.exit(status). The status is wrapped in a 1-tuple
// and installed on the thread as a SystemExit; the caller (pythonrun)
// interprets the int / None / other shape via _PyErr_PrintEx.
//
// CPython: Python/sysmodule.c:915 sys_exit_impl
func makeExit(ts *state.Thread) func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		status := objects.None()
		if len(args) > 0 {
			status = args[0]
		}
		errors.Set(ts, errors.PyExc_SystemExit, objects.NewTuple([]objects.Object{status}))
		return nil, errSystemExit
	}
}

// errSystemExit is the sentinel returned by sys.exit so the VM
// propagates a Go-side failure while the thread carries the
// SystemExit object the printer reads. The string is informational;
// the printer never renders it because HandleSystemExit catches the
// exception first.
var errSystemExit = fmt.Errorf("SystemExit")

// makeSetRecursionLimit ports sys.setrecursionlimit. Negative or
// zero limits raise ValueError; CPython also rejects new_limit <
// current depth (RecursionError) but gopy has no per-thread depth
// counter yet, so that check parks until the eval-loop port lands.
//
// CPython: Python/sysmodule.c:1352 sys_setrecursionlimit_impl
func makeSetRecursionLimit(ts *state.Thread) func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: setrecursionlimit() takes exactly one argument (%d given)", len(args))
		}
		n, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[0].Type().Name)
		}
		v, _ := n.Int64()
		if v < 1 {
			errors.SetString(ts, errors.PyExc_ValueError, "recursion limit must be greater or equal than 1")
			return nil, fmt.Errorf("ValueError: recursion limit must be greater or equal than 1")
		}
		recursionLimit.Store(int32(v))
		return objects.None(), nil
	}
}

// getRecursionLimit ports sys.getrecursionlimit. Reads the atomic
// ceiling shared by every thread.
//
// CPython: Python/sysmodule.c:1612 sys_getrecursionlimit_impl
func getRecursionLimit(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(recursionLimit.Load())), nil
}

// RecursionLimit returns the current process-wide recursion ceiling.
// Called from vm/eval_call.go to check depth before pushing a frame.
//
// CPython: Include/cpython/pystate.h py_recursion_limit field
func RecursionLimit() int { return int(recursionLimit.Load()) }

// getIntMaxStrDigits ports sys.get_int_max_str_digits().
//
// CPython: Python/sysmodule.c:2001 sys_get_int_max_str_digits_impl
func getIntMaxStrDigits(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewInt(int64(intMaxStrDigits.Load())), nil
}

// setIntMaxStrDigits ports sys.set_int_max_str_digits(maxdigits).
//
// CPython: Python/sysmodule.c:2026 sys_set_int_max_str_digits_impl
func setIntMaxStrDigits(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: set_int_max_str_digits() takes exactly one argument")
	}
	iv, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	v, _ := iv.Int64()
	if v != 0 && v < 640 {
		return nil, fmt.Errorf("ValueError: set_int_max_str_digits() limit must be 0 or >= 640")
	}
	intMaxStrDigits.Store(int32(v))
	return objects.None(), nil
}

// resolveThread returns the active thread via CurrentThreadHook, which
// the vm installs once the interpreter starts. inittab-built sys has no
// thread of its own, so every coroutine-origin and asyncgen-hook entry
// point routes through this hook.
func resolveThread() *state.Thread {
	if CurrentThreadHook != nil {
		return CurrentThreadHook()
	}
	return nil
}

// getCoroutineOriginTrackingDepth ports
// sys.get_coroutine_origin_tracking_depth() -> int. CPython reads the
// per-thread depth via _PyEval_GetCoroutineOriginTrackingDepth.
//
// CPython: Python/sysmodule.c:1402 sys_get_coroutine_origin_tracking_depth_impl
// CPython: Python/ceval.c:2690 _PyEval_GetCoroutineOriginTrackingDepth
func getCoroutineOriginTrackingDepth(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ts := resolveThread()
	if ts == nil {
		return objects.NewInt(0), nil
	}
	return objects.NewInt(int64(ts.CoroutineOriginTrackingDepth)), nil
}

// setCoroutineOriginTrackingDepth ports
// sys.set_coroutine_origin_tracking_depth(depth). Negative depths raise
// ValueError, matching CPython.
//
// CPython: Python/sysmodule.c:1379 sys_set_coroutine_origin_tracking_depth_impl
// CPython: Python/ceval.c:2677 _PyEval_SetCoroutineOriginTrackingDepth
func setCoroutineOriginTrackingDepth(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: set_coroutine_origin_tracking_depth() takes exactly one argument (%d given)", len(args))
	}
	iv, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	v, _ := iv.Int64()
	if v < 0 {
		return nil, fmt.Errorf("ValueError: depth must be >= 0")
	}
	ts := resolveThread()
	if ts == nil {
		return objects.None(), nil
	}
	ts.CoroutineOriginTrackingDepth = int(v)
	return objects.None(), nil
}

// getRefcount ports sys.getrefcount. CPython warns the value is one
// higher than expected because the argument hand-off bumps it; gopy
// inherits the same off-by-one because its calling convention also
// hands the argument over via Vectorcall.
//
// CPython: Python/sysmodule.c:2022 sys_getrefcount_impl
func getRefcount(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: getrefcount() takes exactly one argument (%d given)", len(args))
	}
	return objects.NewInt(args[0].Hdr().Refcnt()), nil
}

// getSizeof ports sys.getsizeof(object[, default]). It calls the
// object's __sizeof__ and validates the result is non-negative,
// matching _PySys_GetSizeOf. When the type does not define __sizeof__ a
// TypeError is raised, and the optional default is returned in its
// place when one was supplied. gopy stores objects on the Go heap with
// no PyGC_Head, so the GC-tracked header addition CPython performs has
// no faithful analog here; the reported size is the __sizeof__ value
// for non-GC objects (str, int, float, bytes, ...), which is exactly
// what CPython reports for them.
//
// CPython: Python/sysmodule.c:1841 sys_getsizeof_impl
// CPython: Python/sysmodule.c:1791 _PySys_GetSizeOf
func getSizeof(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: getsizeof() takes at most 2 arguments (%d given)", len(args))
	}
	var obj, dflt objects.Object
	hasDefault := false
	if len(args) >= 1 {
		obj = args[0]
	}
	if len(args) >= 2 {
		dflt = args[1]
		hasDefault = true
	}
	for k, v := range kwargs {
		switch k {
		case "object":
			if obj != nil {
				return nil, fmt.Errorf("TypeError: argument for getsizeof() given by name ('object') and position (1)")
			}
			obj = v
		case "default":
			dflt = v
			hasDefault = true
		default:
			return nil, fmt.Errorf("TypeError: getsizeof() got an unexpected keyword argument '%s'", k)
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("TypeError: getsizeof() missing required argument 'object' (pos 1)")
	}

	size, err := sizeOfObject(obj)
	if err != nil {
		// CPython only substitutes the default when the failure is the
		// "type doesn't define __sizeof__" TypeError; other exceptions
		// (a misbehaving __sizeof__) propagate.
		if hasDefault && isTypeError(err) {
			return dflt, nil
		}
		return nil, err
	}
	return objects.NewInt(size), nil
}

// sizeOfObject calls obj.__sizeof__() and returns the validated
// non-negative size. A type with no __sizeof__ yields a TypeError that
// matches CPython's _PyObject_LookupSpecial-miss message.
//
// CPython: Python/sysmodule.c:1791 _PySys_GetSizeOf
func sizeOfObject(obj objects.Object) (int64, error) {
	// _PySys_GetSizeOf fetches __sizeof__ through _PyObject_LookupSpecial,
	// a TYPE-level MRO lookup that bypasses the instance __getattribute__.
	//
	// CPython: Python/sysmodule.c:1791 _PySys_GetSizeOf (_PyObject_LookupSpecial)
	method, err := objects.LookupSpecial(obj, "__sizeof__")
	if err != nil {
		return 0, err
	}
	if method == nil {
		return 0, fmt.Errorf("TypeError: Type %.100s doesn't define __sizeof__", obj.Type().Name)
	}
	res, err := objects.CallNoArgs(method)
	if err != nil {
		return 0, err
	}
	iv, ok := res.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: 'NoneType' object cannot be interpreted as an integer")
	}
	size, fits := iv.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: cannot fit '%s' into an index-sized integer", res.Type().Name)
	}
	if size < 0 {
		return 0, fmt.Errorf("ValueError: __sizeof__() should return >= 0")
	}
	return size, nil
}

// isTypeError reports whether err is a Python TypeError, so getsizeof
// can decide whether the supplied default should stand in.
func isTypeError(err error) bool {
	return strings.HasPrefix(err.Error(), "TypeError:")
}

// makeIntern ports sys.intern. The interned table lands with the
// unicodeobject port (1616); until then gopy returns the input string
// unchanged so callers see the round-trip semantics. Non-str input
// raises TypeError per CPython.
//
// CPython: Python/sysmodule.c:1004 sys_intern_impl
func makeIntern(ts *state.Thread) func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: intern() takes exactly one argument (%d given)", len(args))
		}
		if args[0].Type() != objects.StrType() {
			msg := fmt.Sprintf("can't intern %.400s", args[0].Type().Name)
			errors.SetString(ts, errors.PyExc_TypeError, msg)
			return nil, fmt.Errorf("TypeError: %s", msg)
		}
		return args[0], nil
	}
}
