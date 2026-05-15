// sys.excepthook is invoked by the interpreter when an exception goes
// uncaught at the top level, and by threading.py to display exceptions
// raised in worker threads. CPython routes through PyErr_Display which
// formats a traceback. The gopy port writes a minimal "type: value"
// line to sys.stderr; full traceback formatting can plug in once the
// stack-walk hook is exposed.
//
// CPython: Python/sysmodule.c sys_excepthook_impl
// CPython: Python/pythonrun.c PyErr_Display

package sys

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/objects"
)

func excepthookShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return objects.None(), nil
	}
	exc := args[1]
	repr, err := objects.Str(exc)
	if err != nil {
		return objects.None(), nil
	}
	tp := exc.Type().Name
	fmt.Fprintf(os.Stderr, "%s: %s\n", tp, repr)
	return objects.None(), nil
}
