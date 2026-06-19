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
	"errors"
	"fmt"
	"io"

	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	parsererrors "github.com/tamnd/gopy/parser/errors"
	"github.com/tamnd/gopy/specialize"
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
	cco, err := compile.Compile(mod, filename, -1)
	if err != nil {
		return nil, err
	}
	return vm.EvalCode(ts, liftCode(cco), globals, locals)
}

// RunBytes is the bytes-input variant of RunString. The PEP 263
// coding cookie and a UTF-8 BOM are honored by the lexer's bytes
// driver, so a script with `# coding: iso-8859-15` decodes through
// the matching codec before tokenization. CPython routes script
// execution through this path; the str variant is reserved for
// compile(str_source, ...) where PyCF_IGNORE_COOKIE skips the
// cookie scan.
//
// CPython: Python/pythonrun.c:1276 pyrun_file (bytes source)
func RunBytes(ts *state.Thread, src []byte, filename string, mode parser.Mode, globals, locals objects.Object) (objects.Object, error) {
	if len(src) == 0 || src[len(src)-1] != '\n' {
		src = append(src, '\n')
	}
	mod, err := parser.ParseBytes(src, filename, mode)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, filename, -1)
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
	return RunSimpleStringWithName(ts, command, "<string>", globals, stderr)
}

// RunSimpleStringWithName is RunSimpleString with a caller-supplied
// co_filename rather than the "<string>" default. A file driven through
// an in-memory source rewrite (for example a vendored test that gets a
// unittest runner appended) passes its real path here so the compiled
// code object carries the script's filename: frame repr shows it and
// linecache can pull the original source lines for tracebacks.
//
// CPython: Python/pythonrun.c:592 PyRun_SimpleStringFlags
// (_PyRun_SimpleStringFlagsWithName variant)
func RunSimpleStringWithName(ts *state.Thread, command, filename string, globals objects.Object, stderr io.Writer) int {
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
	if _, err := RunString(ts, command, filename, parser.ModeFile, globals, nil); err != nil {
		return printRunError(ts, err, stderr)
	}
	return 0
}

// printRunError mirrors PyErr_Print: render the thread-state
// exception's traceback. SystemExit short-circuits and returns its
// code; a generic exception returns 1; the parser surfaces a Go-side
// *parsererrors.SyntaxError that never reached the VM, so synthesize
// the Python-level SyntaxError exception here and route it through the
// same display path so the `SyntaxError:` prefix + File/line/text/caret
// frame land on stderr.
//
// CPython: Python/pythonrun.c:656 PyErr_Print
func printRunError(ts *state.Thread, err error, w io.Writer) int {
	if pyerrors.Occurred(ts) != nil {
		return pyerrors.PrintEx(ts, w)
	}
	var se *parsererrors.SyntaxError
	if errors.As(err, &se) {
		exc := pyerrors.SyntaxFromParser(se)
		pyerrors.Restore(ts, exc.ExcType, exc, nil)
		return pyerrors.PrintEx(ts, w)
	}
	fmt.Fprintln(w, err)
	return 1
}

// liftCode adapts compile.Code into objects.Code. The two structs
// will collapse once spec 1687 retires compile.Code.
func liftCode(c *compile.Code) *objects.Code {
	out := &objects.Code{
		Version:         objects.AllocCodeVersion(),
		Argcount:        c.Argcount,
		PosonlyArgcount: c.PosOnlyArgCount,
		KwonlyArgcount:  c.KwOnlyArgCount,
		Stacksize:       c.Stacksize,
		Flags:           int(c.Flags),
		Code:            c.Code,
		Consts:          liftRunConsts(c.Consts),
		Names:           c.Names,
		Varnames:        c.VarNames,
		Freevars:        c.FreeVars,
		Cellvars:        c.CellVars,
		LocalsplusNames: c.LocalsPlusNames,
		LocalsplusKinds: c.LocalsPlusKinds,
		Filename:        c.Filename,
		Name:            c.Name,
		Qualname:        c.Qualname,
		Firstlineno:     c.Firstlineno,
		Linetable:       c.Linetable,
		ExceptionTable:  c.ExceptionTable,
	}
	out.Init(objects.CodeType)
	out.SyncNameObjs()
	out.SyncConstObjs()
	out.SyncLocalsplusCounts()
	specialize.Enable(out)
	return out
}

// liftRunConsts mirrors vm.liftConsts: lift nested compile-pipeline
// value types into marshal-friendly forms so a code object produced
// by RunString can later go through marshal.Dump.
//
// CPython: Python/compile.c assemble_consts paths (each child code is
// already a PyObject* so this conversion is implicit there).
func liftRunConsts(consts []any) []any {
	out := make([]any, len(consts))
	for i, v := range consts {
		out[i] = liftRunConst(v)
	}
	return out
}

func liftRunConst(v any) any {
	switch x := v.(type) {
	case *compile.Code:
		return liftCode(x)
	case *compile.ConstTuple:
		items := make([]any, len(x.Values))
		for i, raw := range x.Values {
			items[i] = liftRunConst(raw)
		}
		return items
	}
	return v
}
