// vm-side wiring for builtins.globals() and builtins.locals(). The
// builtin needs to read the running frame, but builtins doesn't import
// vm or frame. This file installs a hook from the vm side so the
// dependency arrow points the right way: builtins -> objects, vm ->
// builtins.

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/builtins"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func init() {
	builtins.SetCurrentScope(currentScope)
	builtins.SetImporter(currentImporter)
	builtins.SetEvaluator(currentEvaluator)
	objects.GenThrowHook = genThrowHook
}

// genThrowHook validates the argument passed to generator.throw(exc),
// instantiates an exception class with no args, and wraps the resulting
// Python exception object in objects.RaisedError so the generator's
// YIELD_VALUE handler can install it on the thread state. The single-arg
// signature mirrors CPython 3.14's preferred form.
//
// CPython: Objects/genobject.c:599 gen_throw / :466 _gen_throw (throw_here
// branch handling the PyExceptionClass_Check and PyExceptionInstance_Check
// cases)
func genThrowHook(arg objects.Object) (error, error) {
	var exc *pyerrors.Exception
	switch v := arg.(type) {
	case *pyerrors.Exception:
		exc = v
	case *objects.Type:
		if !pyerrors.IsSubtype(v, pyerrors.PyExc_BaseException) {
			return nil, fmt.Errorf("TypeError: exceptions must derive from BaseException")
		}
		exc = pyerrors.New(v, objects.NewTuple(nil))
	default:
		return nil, fmt.Errorf(
			"TypeError: exceptions must be classes or instances deriving from BaseException, not %s",
			arg.Type().Name)
	}
	msg := exc.TypeName()
	if m := exc.Message(); m != "" {
		msg = msg + ": " + m
	}
	return objects.NewRaisedError(exc, msg), nil
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

// currentEvaluator is the hook builtins.eval and builtins.exec
// dispatch through. It pulls the active state.Thread off the goroutine
// (or allocates a fresh one when called outside any frame, e.g. from a
// stand-alone program that imports gopy as a library) and calls
// EvalCode against the supplied globals/locals.
//
// CPython: equivalent of PyEval_EvalCode (Python/ceval.c)
func currentEvaluator(code *objects.Code, globals, locals objects.Object) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	return EvalCode(ts, code, globals, locals)
}

// currentImporter is the hook builtins.__import__ delegates to. It
// reuses vmExecutor so the import can run frozen / built-in module
// init code, then forwards to imp.ImportModuleLevel. fromlist is
// accepted for signature parity; the existing IMPORT_NAME arm
// likewise drops it pending fromlist-driven submodule discovery.
//
// CPython: Python/import.c:1561 PyImport_ImportModuleLevelObject
func currentImporter(name, pkgname string, level int, _ []string) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		ts = state.NewThread()
	}
	// Inherit builtins from the running frame so imported modules see
	// __build_class__ without a separate propagation step.
	//
	// CPython: Python/import.c:1759 import_name reads interp->builtins_module.
	var b objects.Object
	if f := frameStackFor(ts).Top(); f != nil {
		b = callerBuiltins(f)
	}
	exec := &vmExecutor{ts: ts, builtins: b}
	mod, err := imp.ImportModuleLevel(exec, name, pkgname, level)
	if err != nil {
		return nil, err
	}
	return mod, nil
}
