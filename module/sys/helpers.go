package sys

import (
	"fmt"
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
