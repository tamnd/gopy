// Python-visible sys.settrace / sys.setprofile / sys.gettrace /
// sys.getprofile entry points. They thread the user-supplied Python
// callable through a Go trampoline that matches the LegacyTraceFunc
// shape, then defer to SetTrace / SetProfile to install the bridge.
//
// CPython: Python/sysmodule.c:1145 sys_settrace, 1226 sys_setprofile,
//          1202 sys_gettrace_impl, 1283 sys_getprofile_impl

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/module/sys"
	"github.com/tamnd/gopy/objects"
)

func init() {
	sys.SysTraceBuiltinsHook = RegisterSysTraceBuiltins
}

// whatStrings packs the legacy event-kind names sys.settrace /
// sys.setprofile pass to the Python callback as the second argument.
// Index matches the PyTrace_* constant.
//
// CPython: Python/sysmodule.c:1059 whatstrings
var whatStrings = [8]string{
	"call",        // PyTrace_CALL
	"exception",   // PyTrace_EXCEPTION
	"line",        // PyTrace_LINE
	"return",      // PyTrace_RETURN
	"c_call",      // PyTrace_C_CALL
	"c_exception", // PyTrace_C_EXCEPTION
	"c_return",    // PyTrace_C_RETURN
	"opcode",      // PyTrace_OPCODE
}

// callTrampoline invokes a Python-level trace / profile callback
// with (frame, what, arg) and returns whatever the callback returned.
// Mirrors call_trampoline in CPython.
//
// CPython: Python/sysmodule.c:1071 call_trampoline
func callTrampoline(callback objects.Object, f *frame.Frame, what int, arg objects.Object) (objects.Object, error) {
	if arg == nil {
		arg = objects.None()
	}
	if what < 0 || what >= len(whatStrings) {
		return nil, fmt.Errorf("invalid trace what %d", what)
	}
	frameObj := objects.NewFrame(f)
	args := []objects.Object{frameObj, objects.NewStr(whatStrings[what]), arg}
	return objects.Vectorcall(callback, args, uint(len(args)), nil)
}

// profileTrampoline is the LegacyTraceFunc SetProfile installs when
// the Python user calls sys.setprofile(callable). It just relays the
// event into the user's callable and clears the profile state on
// failure, matching CPython's profile_trampoline.
//
// CPython: Python/sysmodule.c:1086 profile_trampoline
func profileTrampoline(callback objects.Object) LegacyTraceFunc {
	return func(_ objects.Object, f *frame.Frame, what int, arg objects.Object) error {
		_, err := callTrampoline(callback, f, what, arg)
		if err != nil {
			ts := currentThread()
			if ts != nil {
				_, _ = SetProfile(ts, nil, nil)
			}
		}
		return err
	}
}

// traceTrampoline is the LegacyTraceFunc SetTrace installs. It is the
// faithful trace_trampoline: on a CALL event the install-time global
// callback runs and its return value becomes that frame's local trace
// function (f_trace); on every other event the frame's own f_trace is
// invoked, and nothing happens when it is NULL. This per-frame gate is
// why the frame that called settrace fires no events (no CALL fired
// for it, so its f_trace stays unset) while a frame whose f_trace was
// set explicitly still receives line and return events.
//
// gopy's frame split keeps f_trace on the Python-level wrapper
// (objects.Frame), so the trampoline reaches it through
// objects.NewFrame, which returns the wrapper already registered on
// the live activation record.
//
// CPython: Python/sysmodule.c:1101 trace_trampoline
func traceTrampoline(callback objects.Object) LegacyTraceFunc {
	return func(_ objects.Object, f *frame.Frame, what int, arg objects.Object) error {
		wrapper := objects.NewFrame(f)
		var cb objects.Object
		if what == PyTraceCall {
			cb = callback
		} else if local := wrapper.Trace(); local != objects.None() {
			cb = local
		}
		if cb == nil {
			return nil
		}
		result, err := callTrampoline(cb, f, what, arg)
		if err != nil {
			ts := currentThread()
			if ts != nil {
				_, _ = SetTrace(ts, nil, nil)
			}
			wrapper.SetTrace(nil)
			return err
		}
		// A None return leaves the existing f_trace in place; any other
		// value installs it as the frame's local trace function.
		//
		// CPython: Python/sysmodule.c:1124 trace_trampoline (Py_XSETREF)
		if result != objects.None() {
			wrapper.SetTrace(result)
		}
		return nil
	}
}

// SysSetTrace returns the builtin function that backs sys.settrace.
// Calling it installs callable as the trace function on the current
// thread, or clears tracing when callable is None.
//
// CPython: Python/sysmodule.c:1145 sys_settrace
func SysSetTrace() objects.Object {
	return objects.NewBuiltinFunction("settrace", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("settrace() takes 1 argument (got %d)", len(args))
		}
		ts := currentThread()
		if ts == nil {
			return nil, fmt.Errorf("settrace() requires an active thread state")
		}
		if args[0] == objects.None() {
			if _, err := SetTrace(ts, nil, nil); err != nil {
				return nil, err
			}
			return objects.None(), nil
		}
		if _, err := SetTrace(ts, traceTrampoline(args[0]), args[0]); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
}

// SysSetProfile returns the builtin function that backs
// sys.setprofile.
//
// CPython: Python/sysmodule.c:1226 sys_setprofile
func SysSetProfile() objects.Object {
	return objects.NewBuiltinFunction("setprofile", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("setprofile() takes 1 argument (got %d)", len(args))
		}
		ts := currentThread()
		if ts == nil {
			return nil, fmt.Errorf("setprofile() requires an active thread state")
		}
		if args[0] == objects.None() {
			if _, err := SetProfile(ts, nil, nil); err != nil {
				return nil, err
			}
			return objects.None(), nil
		}
		if _, err := SetProfile(ts, profileTrampoline(args[0]), args[0]); err != nil {
			return nil, err
		}
		return objects.None(), nil
	})
}

// SysGetTrace returns the builtin that backs sys.gettrace. It hands
// back the user-installed trace callable, or None when no trace is
// active.
//
// CPython: Python/sysmodule.c:1202 sys_gettrace_impl
func SysGetTrace() objects.Object {
	return objects.NewBuiltinFunction("gettrace", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		ts := currentThread()
		if ts == nil {
			return objects.None(), nil
		}
		_, obj := CTraceFunc(ts)
		if obj == nil {
			return objects.None(), nil
		}
		return obj, nil
	})
}

// SysGetProfile returns the builtin that backs sys.getprofile.
//
// CPython: Python/sysmodule.c:1283 sys_getprofile_impl
func SysGetProfile() objects.Object {
	return objects.NewBuiltinFunction("getprofile", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		ts := currentThread()
		if ts == nil {
			return objects.None(), nil
		}
		_, obj := CProfileFunc(ts)
		if obj == nil {
			return objects.None(), nil
		}
		return obj, nil
	})
}

// CallTracing calls func(*args) with tracing temporarily disabled and
// the prior tracing depth restored afterwards. A debugger drives this
// from a checkpoint to recursively trace some other code without the
// trace machinery re-entering itself on the nested call.
//
// CPython: Python/ceval.c:2621 _PyEval_CallTracing
func CallTracing(fn objects.Object, funcArgs *objects.Tuple) (objects.Object, error) {
	ts := currentThread()
	if ts == nil {
		return nil, fmt.Errorf("SystemError: call_tracing() requires an active thread state")
	}
	// Save and disable tracing for the duration of the call.
	saveTracing := ts.Tracing
	ts.Tracing = 0
	n := funcArgs.Len()
	call := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		call[i] = funcArgs.Item(i)
	}
	result, err := objects.Vectorcall(fn, call, uint(n), nil)
	// Restore tracing.
	ts.Tracing = saveTracing
	return result, err
}

// SysCallTracing returns the builtin that backs sys.call_tracing.
// It calls func(*args) (args must be a tuple) with the tracing state
// saved and restored around the call.
//
// CPython: Python/sysmodule.c:2172 sys_call_tracing_impl
func SysCallTracing() objects.Object {
	return objects.NewBuiltinFunction("call_tracing", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: call_tracing expected 2 arguments, got %d", len(args))
		}
		funcArgs, ok := args[1].(*objects.Tuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: call_tracing() argument 2 must be tuple, not %s", args[1].Type().Name)
		}
		return CallTracing(args[0], funcArgs)
	})
}

// RegisterSysTraceBuiltins installs settrace, setprofile, gettrace,
// getprofile, and call_tracing in the supplied sys dict. Lifecycle
// wiring calls this once it has built the per-interpreter sys module.
//
// CPython: Python/sysmodule.c:3360 sys_methods (settrace / setprofile rows)
func RegisterSysTraceBuiltins(d *objects.Dict) error {
	for _, e := range []struct {
		name string
		obj  objects.Object
	}{
		{"settrace", SysSetTrace()},
		{"setprofile", SysSetProfile()},
		{"gettrace", SysGetTrace()},
		{"getprofile", SysGetProfile()},
		{"call_tracing", SysCallTracing()},
	} {
		if err := d.SetItem(objects.NewStr(e.name), e.obj); err != nil {
			return err
		}
	}
	return nil
}
