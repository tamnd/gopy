// _thread._excepthook and the _ExceptHookArgs struct-sequence back
// threading.excepthook. When a Thread.run() lets an exception escape,
// threading._bootstrap_inner builds an _ExceptHookArgs(exc_type,
// exc_value, exc_traceback, thread) and hands it to threading.excepthook,
// whose C default is _thread._excepthook. The default prints
// "Exception in thread {name}:" followed by the traceback to the thread's
// stderr.
//
// CPython: Modules/_threadmodule.c:2275 thread_excepthook
package _thread

import (
	"fmt"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// exceptHookArgsType is the _thread._ExceptHookArgs struct-sequence type.
//
// CPython: Modules/_threadmodule.c:2266 ExceptHookArgs_desc
var exceptHookArgsType = objects.NewStructSeqTypeDesc(objects.StructSeqDesc{
	Name: "_thread._ExceptHookArgs",
	Fields: []objects.StructSeqField{
		{Name: "exc_type", Doc: "Exception type"},
		{Name: "exc_value", Doc: "Exception value"},
		{Name: "exc_traceback", Doc: "Exception traceback"},
		{Name: "thread", Doc: "Thread"},
	},
	NInSequence: 4,
})

// threadExceptHook is the default threading.excepthook. It expects a
// single _ExceptHookArgs and reports the uncaught thread exception.
//
// CPython: Modules/_threadmodule.c:2275 thread_excepthook
func threadExceptHook(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _thread._excepthook expected 1 argument")
	}
	hookArgs, ok := args[0].(*objects.StructSeq)
	if !ok || hookArgs.Type() != exceptHookArgsType {
		return nil, fmt.Errorf(
			"TypeError: _thread.excepthook argument type must be ExceptHookArgs")
	}
	items := hookArgs.Items()

	// Silently ignore SystemExit, matching the C default.
	excType := items[0]
	if t, ok := excType.(*objects.Type); ok && objects.IsSubtype(t, errors.PyExc_SystemExit) {
		return objects.None(), nil
	}

	excValue := items[1]
	thread := items[3]

	// Resolve the destination stream: sys.stderr, else thread._stderr.
	//
	// CPython: Modules/_threadmodule.c:2298 _PySys_GetOptionalAttr(stderr)
	file := optionalSysStderr()
	if file == nil || file == objects.None() {
		if thread == objects.None() {
			return objects.None(), nil
		}
		f, err := objects.GetAttr(thread, objects.NewStr("_stderr"))
		if err != nil {
			return nil, err
		}
		if f == objects.None() {
			return objects.None(), nil
		}
		file = f
	}

	if err := threadExceptHookFile(file, excValue, thread); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// threadExceptHookFile writes the thread name header and the traceback to
// file, then flushes it.
//
// CPython: Modules/_threadmodule.c:2197 thread_excepthook_file
func threadExceptHookFile(file, excValue, thread objects.Object) error {
	if err := fileWriteString(file, "Exception in thread "); err != nil {
		return err
	}

	name := objects.Object(nil)
	if thread != objects.None() {
		n, err := objects.GetAttr(thread, objects.NewStr("name"))
		if err == nil {
			name = n
		}
	}
	if name != nil {
		s, err := objects.Str(name)
		if err != nil {
			return err
		}
		if err := fileWriteString(file, s); err != nil {
			return err
		}
	} else {
		if err := fileWriteString(file, "<failed to get thread name>"); err != nil {
			return err
		}
	}

	if err := fileWriteString(file, ":\n"); err != nil {
		return err
	}

	// Display the traceback through the same formatter sys.excepthook uses.
	//
	// CPython: Modules/_threadmodule.c:2241 _PyErr_Display
	text := ""
	if exc, ok := excValue.(*errors.Exception); ok {
		text = errors.FormatException(exc)
	} else {
		repr, err := objects.Str(excValue)
		if err == nil {
			text = excValue.Type().Name + ": " + repr + "\n"
		} else {
			text = excValue.Type().Name + "\n"
		}
	}
	if err := fileWriteString(file, text); err != nil {
		return err
	}

	// file.flush(), best effort.
	if flush, err := objects.GetAttr(file, objects.NewStr("flush")); err == nil {
		_, _ = objects.Call(flush, objects.NewTuple(nil), nil)
	}
	return nil
}

// fileWriteString writes s through file.write, mirroring PyFile_WriteString.
//
// CPython: Objects/fileobject.c PyFile_WriteString
func fileWriteString(file objects.Object, s string) error {
	write, err := objects.GetAttr(file, objects.NewStr("write"))
	if err != nil {
		return err
	}
	_, err = objects.Call(write, objects.NewTuple([]objects.Object{objects.NewStr(s)}), nil)
	return err
}

// optionalSysStderr returns sys.stderr, or nil if sys or its stderr is
// unavailable.
//
// CPython: Python/sysmodule.c _PySys_GetOptionalAttr
func optionalSysStderr() objects.Object {
	sysMod, ok := imp.GetModule("sys")
	if !ok {
		return nil
	}
	f, err := objects.GetAttr(sysMod, objects.NewStr("stderr"))
	if err != nil {
		return nil
	}
	return f
}
