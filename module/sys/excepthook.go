// sys.excepthook is invoked by the interpreter when an exception goes
// uncaught at the top level, by threading.py to display exceptions
// raised in worker threads, and by code.InteractiveConsole to render a
// traceback. CPython routes through PyErr_Display, which formats the
// full traceback (chained causes included) and writes it through the
// live sys.stderr object so a redirected or mocked stream captures it.
//
// CPython: Python/sysmodule.c sys_excepthook_impl
// CPython: Python/pythonrun.c PyErr_Display
package sys

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func excepthookShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return objects.None(), nil
	}
	// PyErr_Display formats the value argument (args[1]); the type and
	// traceback are derived from it. code.InteractiveConsole already
	// stitched the traceback onto the value via with_traceback before
	// calling the hook, so FormatException(value) renders the same frames.
	text := excepthookText(args[1])

	// Write through the live sys.stderr the way _PyErr_Display does, so a
	// caller that swapped sys.stderr (tests mocking the stream, the REPL's
	// captured stderr) sees the output instead of the process fd.
	//
	// CPython: Python/pythonrun.c _PyErr_Display (PySys_GetObject "stderr")
	d := liveSysDict()
	if d != nil {
		if errf, _ := d.GetItem(objects.NewStr("stderr")); errf != nil && errf != objects.None() {
			if write, err := objects.GetAttr(errf, objects.NewStr("write")); err == nil {
				if _, err := objects.Call(write, objects.NewTuple([]objects.Object{objects.NewStr(text)}), nil); err == nil {
					return objects.None(), nil
				}
			}
		}
	}
	// Fall back to the process stderr only when sys.stderr is missing or
	// unusable, mirroring CPython's last-resort write to the C-level
	// stderr in _PyErr_Display.
	fmt.Fprint(os.Stderr, text)
	return objects.None(), nil
}

// excepthookText renders the traceback string for the exception value,
// falling back to a "Type: repr" line when the object is not a gopy
// Exception (the hook must never raise while reporting an error).
func excepthookText(value objects.Object) string {
	if exc, ok := value.(*errors.Exception); ok {
		return errors.FormatException(exc)
	}
	repr, err := objects.Str(value)
	if err != nil {
		return value.Type().Name + "\n"
	}
	return value.Type().Name + ": " + repr + "\n"
}
