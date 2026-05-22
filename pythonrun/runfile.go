package pythonrun

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
)

// ErrFileEmpty is returned by RunFile when the source file is empty.
// CPython treats this as a no-op module; the gopy port keeps the
// same shape but surfaces the case so callers can distinguish.
var ErrFileEmpty = errors.New("pythonrun: source file is empty")

// RunFile reads filename, parses it as a Python module, and runs the
// resulting code object against globals/locals. Returns the value the
// frame produced (None for module mode) or the error that escaped.
//
// The caller owns the globals dict; RunFile sets `__file__` on it
// for the duration of the run if the slot is missing, mirroring
// _PyRun_SimpleFileObject. Restoration on the way out matches the
// CPython "set_file_name" branch.
//
// CPython: Python/pythonrun.c:1276 pyrun_file
func RunFile(ts *state.Thread, filename string, globals, locals objects.Object) (objects.Object, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(src) == 0 {
		return objects.None(), nil
	}

	dict, _ := globals.(*objects.Dict)
	restore, err := installFileName(dict, filename)
	if err != nil {
		return nil, err
	}
	defer restore()

	// Stamp __name__ = "__main__" on the globals if absent, so
	// `if __name__ == "__main__"` in the script works, and register
	// the script as the __main__ module in sys.modules so code that
	// does __import__("__main__") (unittest.main, doctest, ...) sees
	// the test classes the script defined.
	//
	// CPython: Python/pythonrun.c:472 set_file_name (paired branch)
	if dict != nil {
		nameKey := objects.NewStr("__name__")
		if has, _ := dict.Contains(nameKey); !has {
			_ = dict.SetItem(nameKey, objects.NewStr("__main__"))
			defer func() { _ = dict.DelItem(nameKey) }()
		}
		if _, ok := imp.GetModule("__main__"); !ok {
			imp.AddModule("__main__", objects.NewModuleWithDict("__main__", dict))
			defer imp.RemoveModule("__main__")
		}
	}

	return RunBytes(ts, src, filename, parser.ModeFile, globals, locals)
}

// RunSimpleFile parses, compiles, and runs filename as a module
// against globals. The result is discarded; on failure the traceback
// (or Go-level error) is rendered to stderr.
//
// Returns the exit code: 0 on success, 1 on a generic exception, or
// the int the caller passed to SystemExit. See RunSimpleString for
// the rationale on returning the exit code instead of CPython's
// 0/-1 plus Py_Exit long jump.
//
// The .pyc detect-and-dispatch branch and the interactive-tty
// fallback are deferred to 1624-C and pyc.go.
//
// CPython: Python/pythonrun.c:462 _PyRun_SimpleFileObject
func RunSimpleFile(ts *state.Thread, filename string, globals objects.Object, stderr io.Writer) int {
	if _, err := RunFile(ts, filename, globals, nil); err != nil {
		return printRunError(ts, err, stderr)
	}
	return 0
}

// RunAnyFile is the top-level dispatch entry. It mirrors
// PyRun_AnyFileExFlags: pick interactive vs simple-file, pass
// through. v0.7 lacks the REPL (1624-C) so the entry collapses to
// RunSimpleFile; the dispatch shape stays so the call site does not
// need to move when 1624-C lands.
//
// CPython: Python/pythonrun.c:91 PyRun_AnyFileExFlags
func RunAnyFile(ts *state.Thread, filename string, globals objects.Object, stderr io.Writer) int {
	return RunSimpleFile(ts, filename, globals, stderr)
}

// installFileName sets __file__ and __cached__ on dict if __file__
// is missing, returning a function that pops both keys back out.
// Mirrors the set_file_name branch of _PyRun_SimpleFileObject.
//
// CPython: Python/pythonrun.c:472 set_file_name
func installFileName(dict *objects.Dict, filename string) (func(), error) {
	if dict == nil {
		return func() {}, nil
	}
	fileKey := objects.NewStr("__file__")
	cachedKey := objects.NewStr("__cached__")

	if has, err := dict.Contains(fileKey); err != nil {
		return nil, fmt.Errorf("dict contains __file__: %w", err)
	} else if has {
		return func() {}, nil
	}
	if err := dict.SetItem(fileKey, objects.NewStr(filename)); err != nil {
		return nil, fmt.Errorf("set __file__: %w", err)
	}
	if err := dict.SetItem(cachedKey, objects.None()); err != nil {
		_ = dict.DelItem(fileKey)
		return nil, fmt.Errorf("set __cached__: %w", err)
	}
	return func() {
		_ = dict.DelItem(fileKey)
		_ = dict.DelItem(cachedKey)
	}, nil
}
