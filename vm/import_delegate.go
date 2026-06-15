package vm

import (
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/traceback"
)

// delegateImport routes an import through the live Python importlib
// machinery, the same way CPython's import_name looks up __import__ from
// the frame builtins and calls import_func(name, globals, locals,
// fromlist, level). The builtin __import__ resolves to
// _frozen_importlib.__import__ (interp->import_func, wired at bootstrap),
// which runs _find_and_load / _handle_fromlist and registers the result
// in the shared sys.modules. Delegating here keeps a single import path
// so a monkeypatched loader.exec_module fires and the traceback carries
// the frozen importlib frames.
//
// The returned ok is false when _frozen_importlib is not yet installed
// (early bootstrap, before _bootstrap._install has run), so the caller
// falls back to the Go import driver to load the bootstrap itself.
//
// CPython: Python/ceval.c:2898 import_name
// CPython: Lib/importlib/_bootstrap.py:1390 __import__
func delegateImport(name string, globals, locals, fromlist objects.Object, level int) (objects.Object, bool, error) {
	frozen, ok := imp.GetModule("_frozen_importlib")
	if !ok {
		return nil, false, nil
	}
	importFunc, err := objects.GetAttr(frozen, objects.NewStr("__import__"))
	if err != nil {
		return nil, false, nil //nolint:nilerr // missing __import__ means fall back to the Go driver.
	}
	if globals == nil {
		globals = objects.None()
	}
	if locals == nil {
		locals = objects.None()
	}
	if fromlist == nil {
		fromlist = objects.None()
	}
	args := objects.NewTuple([]objects.Object{
		objects.NewStr(name),
		globals,
		locals,
		fromlist,
		objects.NewInt(int64(level)),
	})
	mod, callErr := objects.Call(importFunc, args, nil)
	if callErr != nil {
		return nil, true, callErr
	}
	return mod, true, nil
}

// importVerbose reports whether the interpreter runs with -v, which
// suppresses the importlib frame trimming so the full machinery shows.
//
// CPython: Python/import.c:3522 _PyInterpreterState_GetConfig(...)->verbose
func importVerbose() bool {
	sysMod, ok := imp.GetModule("sys")
	if !ok {
		return false
	}
	flags, err := objects.GetAttr(sysMod, objects.NewStr("flags"))
	if err != nil {
		return false
	}
	v, err := objects.GetAttr(flags, objects.NewStr("verbose"))
	if err != nil {
		return false
	}
	if i, ok := v.(*objects.Int); ok {
		n, _ := i.Int64()
		return n != 0
	}
	return false
}

// removeImportlibFrames strips importlib frames from the traceback of the
// exception currently on the thread. If it is an ImportError, every
// importlib chunk is trimmed; otherwise only chunks that end with a call
// to _call_with_frames_removed are trimmed. Matches CPython's behavior of
// hiding the import machinery from user tracebacks.
//
// CPython: Python/import.c:3500 remove_importlib_frames
func removeImportlibFrames(ts *state.Thread) {
	exc := pyerrors.Occurred(ts)
	if exc == nil || importVerbose() {
		return
	}
	const (
		removeFrames  = "_call_with_frames_removed"
		bootstrapFile = "<frozen importlib._bootstrap>"
		externalFile  = "<frozen importlib._bootstrap_external>"
	)
	alwaysTrim := exc.ExcType != nil && objects.IsSubtype(exc.ExcType, pyerrors.PyExc_ImportError)

	// A dummy head node lets *outerLink overwrite the chain head uniformly,
	// the way CPython threads prev_link/outer_link through PyObject** slots.
	var dummy traceback.Traceback
	dummy.Next = exc.TB
	prevLink := &dummy.Next
	var outerLink **traceback.Traceback
	inImportlib := false
	for tb := exc.TB; tb != nil; {
		next := tb.Next
		fn := tb.Entry.File
		nowInImportlib := fn == bootstrapFile || fn == externalFile
		if nowInImportlib && !inImportlib {
			outerLink = prevLink
		}
		inImportlib = nowInImportlib

		if nowInImportlib && (alwaysTrim || tb.Entry.Name == removeFrames) {
			*outerLink = next
			prevLink = outerLink
		} else {
			prevLink = &tb.Next
		}
		tb = next
	}
	exc.TB = dummy.Next
}
