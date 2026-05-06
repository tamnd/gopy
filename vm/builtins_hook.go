// vm-side wiring for builtins.globals() and builtins.locals(). The
// builtin needs to read the running frame, but builtins doesn't import
// vm or frame. This file installs a hook from the vm side so the
// dependency arrow points the right way: builtins -> objects, vm ->
// builtins.

package vm

import (
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
)

func init() {
	builtins.SetCurrentScope(currentScope)
}

// currentScope yields the running frame's (globals, locals). The
// locals branch matches PyEval_GetFrameLocals: if the frame has an
// explicit f_locals dict (set by EvalCode for module / class bodies)
// it is returned as-is; otherwise the fast locals are materialized
// into a fresh dict via FrameFastToLocals.
//
// CPython: Python/bltinmodule.c:1267 builtin_globals_impl
// CPython: Python/bltinmodule.c:1933 builtin_locals_impl
func currentScope() (objects.Object, objects.Object) {
	ts := currentThread()
	if ts == nil {
		return nil, nil
	}
	f := frameStackFor(ts).Top()
	if f == nil {
		return nil, nil
	}
	if f.Locals != nil {
		return f.Globals, f.Locals
	}
	d, err := objects.FrameFastToLocals(objects.NewFrame(f))
	if err != nil {
		return f.Globals, nil
	}
	return f.Globals, d
}
