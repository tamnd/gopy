package sys

import (
	"fmt"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

// exitShim is the inittab-time form of sys.exit. It installs SystemExit
// on the active thread (read via CurrentThreadHook, which vm wires on
// init) and returns errSystemExit so the VM unwinds. The thread-bound
// variant in helpers.go is the Bind-time form used when lifecycle
// constructs the module; the shim covers callers that come through the
// inittab path instead.
//
// CPython: Python/sysmodule.c:915 sys_exit_impl
func exitShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	status := objects.None()
	if len(args) > 0 {
		status = args[0]
	}
	if CurrentThreadHook != nil {
		if ts := CurrentThreadHook(); ts != nil {
			errors.Set(ts, errors.PyExc_SystemExit, objects.NewTuple([]objects.Object{status}))
		}
	}
	return nil, errSystemExit
}

// setRecursionLimitShim is the inittab-time form of sys.setrecursionlimit.
// Reads the active thread via CurrentThreadHook so the ValueError path can
// land on the thread the way the lifecycle-bound form does.
//
// CPython: Python/sysmodule.c:1352 sys_setrecursionlimit_impl
func setRecursionLimitShim(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: setrecursionlimit() takes exactly one argument (%d given)", len(args))
	}
	n, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[0].Type().Name)
	}
	v, _ := n.Int64()
	if v < 1 {
		if CurrentThreadHook != nil {
			if ts := CurrentThreadHook(); ts != nil {
				errors.SetString(ts, errors.PyExc_ValueError, "recursion limit must be greater or equal than 1")
			}
		}
		return nil, fmt.Errorf("ValueError: recursion limit must be greater or equal than 1")
	}
	recursionLimit.Store(int32(v))
	return objects.None(), nil
}
