package vm

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
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
