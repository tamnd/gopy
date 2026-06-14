package errors

import (
	"fmt"
	"io"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// unhandledKeyboardInterrupt records that a KeyboardInterrupt reached
// the top-level print path. Modules/main.c reads the matching runtime
// flag after Py_RunMain and re-raises SIGINT so the process dies by
// signal (exit status -SIGINT) rather than a plain non-zero code.
//
// CPython: Python/pythonrun.c:625 unhandled_keyboard_interrupt store
var unhandledKeyboardInterrupt bool

// UnhandledKeyboardInterrupt reports whether a KeyboardInterrupt was
// surfaced to the top-level handler since the last reset. The CLI
// entry point consults it to decide whether to exit via SIGINT.
//
// CPython: Modules/main.c:786 _PyRuntime.signals.unhandled_keyboard_interrupt
func UnhandledKeyboardInterrupt() bool {
	return unhandledKeyboardInterrupt
}

// HandleSystemExit inspects the current exception. If it is a
// SystemExit, the exit code is read off the args and the exception
// is cleared; the caller propagates the code. KeyboardInterrupt is
// surfaced as (-1, false) so the lifecycle layer can flip the
// runtime's unhandled_keyboard_interrupt flag (1639 wires that hook
// in once the signal table lands).
//
// The bool return matches CPython's int return: true means the
// caller should exit with code; false means fall through to the
// normal print path.
//
// CPython: Python/pythonrun.c:622 _Py_HandleSystemExitAndKeyboardInterrupt
func HandleSystemExit(ts *state.Thread) (code int, handled bool) {
	exc := Occurred(ts)
	if exc == nil {
		return 0, false
	}
	if Match(exc, PyExc_KeyboardInterrupt) {
		unhandledKeyboardInterrupt = true
		return 0, false
	}
	if !Match(exc, PyExc_SystemExit) {
		return 0, false
	}

	value := exitValue(exc)
	if c, ok := parseExitCode(value); ok {
		Clear(ts)
		return c, true
	}
	return 0, false
}

// exitValue returns SystemExit's `code` attribute. For the v0.7
// surface that is just args[0] when args has one element, args
// otherwise. Once 1651-builtins lands the SystemExit init slot it
// will populate exc.code directly; until then the args mirror the
// CPython init contract.
//
// CPython: Objects/exceptions.c:819 SystemExit_init
func exitValue(exc *Exception) objects.Object {
	if exc.Args == nil {
		return objects.None()
	}
	switch exc.Args.Len() {
	case 0:
		return objects.None()
	case 1:
		return exc.Args.Item(0)
	default:
		return exc.Args
	}
}

// parseExitCode mirrors CPython's parse_exit_code: int -> int,
// None -> 0, anything else -> not-an-exit-code so the caller falls
// through to print the value.
//
// CPython: Python/pythonrun.c:598 parse_exit_code
func parseExitCode(value objects.Object) (int, bool) {
	if value == nil || value == objects.None() {
		return 0, true
	}
	if i, ok := value.(*objects.Int); ok {
		v, _ := i.Int64()
		return int(v), true
	}
	return 0, false
}

// PrintEx is the closer port of _PyErr_PrintEx: handle SystemExit
// first, then print the exception chain. Returns the exit code the
// caller should propagate.
//
// CPython: Python/pythonrun.c:694 _PyErr_PrintEx
func PrintEx(ts *state.Thread, w io.Writer) int {
	if code, ok := HandleSystemExit(ts); ok {
		return code
	}
	exc := Occurred(ts)
	if exc == nil {
		return 0
	}
	if w == nil {
		Clear(ts)
		return 1
	}
	if _, err := io.WriteString(w, FormatException(exc)); err != nil {
		fmt.Fprintln(w, err)
	}
	Clear(ts)
	return 1
}
