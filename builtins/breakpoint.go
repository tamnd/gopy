// Port of builtin_breakpoint and sys_breakpointhook. breakpoint()
// looks up sys.breakpointhook on every call and forwards its arguments
// to it; the default hook (sys.__breakpointhook__) reads the
// PYTHONBREAKPOINT environment variable to decide which callable to run,
// defaulting to pdb.set_trace. The default lives here rather than in
// module/sys because its body imports modules and calls warnings.warn,
// which module/sys cannot reach without an import cycle.
//
// CPython: Python/bltinmodule.c:466 builtin_breakpoint
// CPython: Python/sysmodule.c:585 sys_breakpointhook

package builtins

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	_warnings "github.com/tamnd/gopy/module/_warnings"
	"github.com/tamnd/gopy/objects"
)

// Breakpoint implements the breakpoint() builtin: resolve
// sys.breakpointhook and call it with the verbatim args/kwargs. A
// missing hook is the "lost sys.breakpointhook" RuntimeError, matching
// _PySys_GetRequiredAttrString returning NULL.
//
// CPython: Python/bltinmodule.c:466 builtin_breakpoint
func Breakpoint(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	hook := liveSysAttr("breakpointhook")
	if hook == nil {
		return nil, fmt.Errorf("RuntimeError: lost sys.breakpointhook")
	}
	return objects.Call(hook, objects.NewTuple(args), kwargsDict(kwargs))
}

// breakpointHook is the default sys.breakpointhook: read
// PYTHONBREAKPOINT, then import and call the named callable. An unset or
// empty value means pdb.set_trace; "0" is an explicit no-op; any other
// value is split on the last dot into a module path and attribute name.
// Import or attribute failures warn (RuntimeWarning) and return None.
//
// CPython: Python/sysmodule.c:585 sys_breakpointhook
func breakpointHook(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	envar := pythonBreakpointEnv()

	switch envar {
	case "":
		envar = "pdb.set_trace"
	case "0":
		// The breakpoint is explicitly no-op'd.
		return objects.None(), nil
	}

	lastDot := strings.LastIndex(envar, ".")
	var modulepath, attrname string
	switch {
	case lastDot < 0:
		// The breakpoint is a built-in, e.g. PYTHONBREAKPOINT=int.
		modulepath, attrname = "builtins", envar
	case lastDot != 0:
		// Split on the last dot.
		modulepath, attrname = envar[:lastDot], envar[lastDot+1:]
	default:
		// A leading dot (e.g. PYTHONBREAKPOINT=.foo) has no module.
		return breakpointWarn(envar)
	}

	module, err := importBreakpointModule(modulepath)
	if err != nil {
		// ModuleNotFoundError is a subclass of ImportError, so either
		// category ends the breakpoint with a warning. The import hook
		// wraps the sentinel ("imp: ModuleNotFoundError: ..."), so match
		// anywhere in the message rather than at the front.
		if strings.Contains(err.Error(), "ImportError") ||
			strings.Contains(err.Error(), "ModuleNotFoundError") {
			return breakpointWarn(envar)
		}
		return nil, err
	}

	hook, err := objects.GetAttr(module, objects.NewStr(attrname))
	if err != nil {
		if strings.HasPrefix(err.Error(), "AttributeError") {
			return breakpointWarn(envar)
		}
		return nil, err
	}
	return objects.Call(hook, objects.NewTuple(args), kwargsDict(kwargs))
}

// importBreakpointModule imports the dotted module path and returns the
// deepest module, the same object PyImport_Import yields. A non-empty
// fromlist makes the import hook hand back the submodule instead of the
// top-level package.
//
// CPython: Python/sysmodule.c:622 PyImport_Import(modulepath)
func importBreakpointModule(modulepath string) (objects.Object, error) {
	return Import([]objects.Object{
		objects.NewStr(modulepath),
		objects.None(),
		objects.None(),
		objects.NewTuple([]objects.Object{objects.NewStr("__doc__")}),
		objects.NewInt(0),
	}, nil)
}

// breakpointWarn issues the RuntimeWarning CPython raises when the
// PYTHONBREAKPOINT path cannot be imported, then returns None. The
// warning flows through the Python warnings module so catch_warnings /
// check_warnings record it.
//
// CPython: Python/sysmodule.c:658 sys_breakpointhook warn label
func breakpointWarn(envar string) (objects.Object, error) {
	// The import or attribute lookup that brought us here raised a Python
	// exception (ModuleNotFoundError / AttributeError) which gopy swallows
	// in favor of a warning. CPython's sys_breakpointhook calls PyErr_Clear
	// before warning, so clear the lingering thread exception too; otherwise
	// a later contextmanager exit or except handler observes the stale error.
	//
	// CPython: Python/sysmodule.c:658 sys_breakpointhook (PyErr_Clear before warn)
	if objects.ClearCurrentExceptionHook != nil {
		objects.ClearCurrentExceptionHook()
	}
	message := fmt.Sprintf("Ignoring unimportable $PYTHONBREAKPOINT: %q", envar)
	if err := emitRuntimeWarning(message); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// emitRuntimeWarning routes message through the _warnings machinery
// under RuntimeWarning so the emission walks the live filter list and
// any recording context manager. Like CPython's sys_breakpointhook it
// calls PyErr_WarnFormat (the C _warnings entry) rather than re-entering
// the import hook for "warnings", so the warning path stays independent
// of whatever import the breakpoint hook was trying to resolve.
//
// CPython: Python/sysmodule.c:658 sys_breakpointhook (PyErr_WarnFormat PyExc_RuntimeWarning, stacklevel=1)
func emitRuntimeWarning(message string) error {
	return _warnings.WarnEx(errors.PyExc_RuntimeWarning, message, 1)
}

// pythonBreakpointEnv reads PYTHONBREAKPOINT from os.environ. gopy's
// os.environ is a plain dict with no writethrough to the Go process
// environment, and EnvironmentVarGuard mutates that dict directly, so
// the value must come from there rather than os.Getenv. A missing key
// or any lookup failure reads as unset ("").
//
// CPython: Python/sysmodule.c:589 Py_GETENV("PYTHONBREAKPOINT")
func pythonBreakpointEnv() string {
	mod, ok := imp.GetModule("os")
	if !ok || mod == nil {
		return ""
	}
	environ, err := objects.GetAttr(mod, objects.NewStr("environ"))
	if err != nil {
		return ""
	}
	v, err := objects.GetItem(environ, objects.NewStr("PYTHONBREAKPOINT"))
	if err != nil || v == nil {
		return ""
	}
	s, err := objects.Str(v)
	if err != nil {
		return ""
	}
	return s
}

// liveSysAttr reads an attribute off the live sys module dict, or nil
// when sys is absent or the name is unset. breakpoint() uses it to pick
// up sys.breakpointhook on every call so reassignments take effect.
//
// CPython: Python/sysmodule.c PySys_GetAttrString
func liveSysAttr(name string) objects.Object {
	mod, ok := imp.GetModule("sys")
	if !ok || mod == nil {
		return nil
	}
	d := mod.Dict()
	if d == nil {
		return nil
	}
	v, err := d.GetItem(objects.NewStr(name))
	if err != nil {
		return nil
	}
	return v
}

// kwargsDict turns the Go kwargs map into a *Dict for Call, or nil when
// empty so the callee sees no keyword arguments.
func kwargsDict(kwargs map[string]objects.Object) *objects.Dict {
	if len(kwargs) == 0 {
		return nil
	}
	d := objects.NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}
