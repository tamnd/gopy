// Package pythonrun ports cpython/Python/pythonrun.c. It is the
// consumer layer that sits on top of parser, compile, and vm: take a
// chunk of source, drive it through the pipeline, and surface the
// result or the traceback. v0.7 lands the string-evaluation entries
// (PyRun_SimpleString, PyRun_String); the file and REPL arms join in
// 1624-B and 1624-C.
//
// CPython: Python/pythonrun.c
package pythonrun

import (
	"fmt"
	"io"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// RunString parses src as Python source under mode (file or eval),
// compiles it, and runs the resulting code object against globals
// and locals. Returns the value the frame produced (None for
// ModeFile, the expression result for ModeEval) or the error that
// escaped.
//
// CPython: Python/pythonrun.c:1219 _PyRun_StringFlagsWithName
func RunString(ts *state.Thread, src, filename string, mode parser.Mode, globals, locals objects.Object) (objects.Object, error) {
	if src == "" || src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, filename, mode)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, filename, 0)
	if err != nil {
		return nil, err
	}
	return vm.EvalCode(ts, liftCode(cco), globals, locals)
}

// RunSimpleString parses, compiles, and runs command as a Python
// module against globals. The result is discarded; on failure the
// traceback (or Go-level error from the parse/compile arm) is
// rendered to stderr.
//
// Returns the exit code: 0 on success, 1 on a generic exception, or
// the int the caller passed to SystemExit. CPython's
// PyRun_SimpleStringFlags returns -1 and Py_Exit jumps inside
// PyErr_Print; we surface the exit code through the return so
// lifecycle.Main propagates it without long-jumping the runtime.
//
// CPython owns __main__ in the import system. Once 1623 lands the
// globals argument disappears and RunSimpleString grabs the dict
// itself via PyImport_AddModuleRef("__main__").
//
// CPython: Python/pythonrun.c:592 PyRun_SimpleStringFlags
func RunSimpleString(ts *state.Thread, command string, globals objects.Object, stderr io.Writer) int {
	// Stamp __name__ = "__main__" on the globals if absent. CPython
	// does this through the import-machinery dance in run_command:
	// _PyImport_AddModuleObject builds the __main__ module, and its
	// dict is the dict pymain runs the command against. Until 1623
	// lands the same flow we mirror the visible effect here so class
	// bodies in the -c source can look up __name__.
	//
	// CPython: Modules/main.c:289 pymain_run_command
	// CPython: Python/pythonrun.c:472 _PyImport_AddModuleObject
	if d, ok := globals.(*objects.Dict); ok {
		nameKey := objects.NewStr("__name__")
		if has, _ := d.Contains(nameKey); !has {
			_ = d.SetItem(nameKey, objects.NewStr("__main__"))
		}
	}
	if _, err := RunString(ts, command, "<string>", parser.ModeFile, globals, nil); err != nil {
		return printRunError(ts, err, stderr)
	}
	return 0
}

// printRunError mirrors PyErr_Print: render the thread-state
// exception's traceback. SystemExit short-circuits and returns its
// code; a generic exception returns 1; the parser/compiler still
// surface Go errors directly (no SyntaxError yet) so we fall back
// to the error text and exit 1.
//
// CPython: Python/pythonrun.c:656 PyErr_Print
func printRunError(ts *state.Thread, err error, w io.Writer) int {
	if errors.Occurred(ts) != nil {
		return errors.PrintEx(ts, w)
	}
	fmt.Fprintln(w, err)
	return 1
}

// liftCode adapts compile.Code into objects.Code. The two structs
// will collapse once spec 1687 retires compile.Code.
func liftCode(c *compile.Code) *objects.Code {
	out := &objects.Code{
		Argcount:        c.Argcount,
		PosonlyArgcount: c.PosOnlyArgCount,
		KwonlyArgcount:  c.KwOnlyArgCount,
		Stacksize:       c.Stacksize,
		Flags:           int(c.Flags),
		Code:            c.Code,
		Consts:          c.Consts,
		Names:           c.Names,
		Varnames:        c.VarNames,
		Freevars:        c.FreeVars,
		Cellvars:        c.CellVars,
		Filename:        c.Filename,
		Name:            c.Name,
		Qualname:        c.Qualname,
		Firstlineno:     c.Firstlineno,
		Linetable:       c.Linetable,
		ExceptionTable:  c.ExceptionTable,
	}
	out.Init(objects.CodeType)
	return out
}
