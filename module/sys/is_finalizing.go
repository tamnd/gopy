package sys

import "github.com/tamnd/gopy/objects"

// IsFinalizingHook is installed by vm so sys.is_finalizing() can read the
// running interpreter's Finalizing flag without module/sys importing the
// state/vm packages (which would cycle). When unset the interpreter is
// not tearing down, so the call reports False.
var IsFinalizingHook func() bool

// isFinalizing implements sys.is_finalizing(). Returns True while the
// main interpreter is being finalized by Py_Finalize, False otherwise.
//
// CPython: Python/sysmodule.c:2262 sys_is_finalizing_impl
func isFinalizing(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if IsFinalizingHook != nil && IsFinalizingHook() {
		return objects.True(), nil
	}
	return objects.False(), nil
}
