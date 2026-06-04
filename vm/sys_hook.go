// vm-side wiring for sys.exc_info. The builtin needs to read the
// running thread's handled-exception slot, but module/sys cannot
// import vm without cycling back through the import machinery.
// Installing the hook from the vm side keeps the dependency arrow
// pointing vm -> module/sys.

package vm

import (
	"github.com/tamnd/gopy/module/sys"
	"github.com/tamnd/gopy/objects"
)

func init() {
	sys.CurrentThreadHook = currentThread
	sys.IsFinalizingHook = func() bool {
		ts := currentThread()
		if ts == nil {
			return false
		}
		return ts.Interp().Finalizing != 0
	}
	// Expose the running thread's async-generator hooks to the objects
	// package so async_gen_init_hooks can capture the finalizer and fire
	// the firstiter hook. The slots are stored on the thread as any by
	// sys.set_asyncgen_hooks; cast them back to Object here.
	//
	// CPython: Objects/genobject.c:130 async_gen_init_hooks
	objects.AsyncGenHooksHook = func() (objects.Object, objects.Object) {
		ts := currentThread()
		if ts == nil {
			return nil, nil
		}
		fi, _ := ts.AsyncGenFirstIter.(objects.Object)
		fin, _ := ts.AsyncGenFinalizer.(objects.Object)
		return fi, fin
	}
}
