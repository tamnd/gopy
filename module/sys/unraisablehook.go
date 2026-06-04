// sys.unraisablehook(unraisable) is invoked by the runtime when an
// exception occurs in a destructor, in a generator close path, or
// during garbage collection, where there is no caller to receive the
// exception. The default hook formats a short message to sys.stderr.
//
// CPython: Python/sysmodule.c:893 sys_unraisablehook
// CPython: Python/errors.c:1600 _PyErr_WriteUnraisableDefaultHook

package sys

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/objects"
)

func unraisableHookShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return objects.None(), nil
	}
	u := args[0]
	excType, _ := objects.GetAttr(u, objects.NewStr("exc_type"))
	excValue, _ := objects.GetAttr(u, objects.NewStr("exc_value"))
	errMsg, _ := objects.GetAttr(u, objects.NewStr("err_msg"))

	prefix := "Exception ignored in"
	if errMsg != nil && errMsg != objects.None() {
		if s, err := objects.Str(errMsg); err == nil {
			prefix = s
		}
	}
	fmt.Fprintln(os.Stderr, prefix+":")
	if excType != nil {
		name := "?"
		if t, ok := excType.(*objects.Type); ok {
			name = t.Name
		}
		repr := ""
		if excValue != nil && excValue != objects.None() {
			if s, err := objects.Str(excValue); err == nil {
				repr = ": " + s
			}
		}
		fmt.Fprintf(os.Stderr, "%s%s\n", name, repr)
	}
	return objects.None(), nil
}
