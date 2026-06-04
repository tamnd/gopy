// Port of builtin_input_impl. input() resolves sys.stdin / sys.stdout /
// sys.stderr on every call, flushes stderr, writes the prompt to stdout,
// and reads one line from stdin via its readline() method. gopy does not
// ship GNU readline, so the interactive (tty) branch of the C function
// is collapsed into the file path: every call routes through
// sys.stdin.readline(), which is what PyFile_GetLine does for the
// non-interactive case. The encoding handshake the tty branch performs
// is unnecessary because gopy's text streams already hand back str.
//
// CPython: Python/bltinmodule.c:2327 builtin_input_impl
// CPython: Objects/fileobject.c:54 PyFile_GetLine

package builtins

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// Input implements input(prompt=None, /). The prompt, when supplied, is
// written to sys.stdout; the line is read from sys.stdin with one
// trailing newline stripped. A clean EOF raises EOFError; a missing or
// None sys.stdin / stdout / stderr raises RuntimeError.
//
// CPython: Python/bltinmodule.c:2327 builtin_input_impl
func Input(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: input() takes no keyword arguments")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: input expected at most 1 argument, got %d", len(args))
	}

	fin, err := requiredSysStream("stdin")
	if err != nil {
		return nil, err
	}
	fout, err := requiredSysStream("stdout")
	if err != nil {
		return nil, err
	}
	ferr, err := requiredSysStream("stderr")
	if err != nil {
		return nil, err
	}

	// First of all, flush stderr. Errors here are swallowed, matching
	// the PyErr_Clear() after _PyFile_Flush(ferr).
	flushStream(ferr)

	// Fallback path: write the prompt to stdout, flush it, then pull a
	// line off stdin. gopy always takes this path because it has no GNU
	// readline binding for the interactive branch.
	if len(args) == 1 {
		if err := writeStream(fout, args[0]); err != nil {
			return nil, err
		}
	}
	flushStream(fout)

	return fileGetLine(fin)
}

// requiredSysStream reads sys.<name> and rejects a missing or None
// value with RuntimeError "lost sys.<name>", mirroring
// _PySys_GetRequiredAttr followed by the Py_None guard.
//
// CPython: Python/bltinmodule.c:2337 builtin_input_impl stream checks
func requiredSysStream(name string) (objects.Object, error) {
	s := liveSysAttr(name)
	if s == nil || objects.IsNone(s) {
		return nil, fmt.Errorf("RuntimeError: lost sys.%s", name)
	}
	return s, nil
}

// flushStream calls stream.flush() and discards any error, the way the
// C function wraps _PyFile_Flush in PyErr_Clear().
func flushStream(stream objects.Object) {
	flush, err := objects.GetAttr(stream, objects.NewStr("flush"))
	if err != nil {
		return
	}
	_, _ = objects.Call(flush, objects.NewTuple(nil), nil)
}

// writeStream writes str(prompt) to the stream via its write() method,
// the Py_PRINT_RAW form of PyFile_WriteObject.
//
// CPython: Objects/fileobject.c:166 PyFile_WriteObject (Py_PRINT_RAW)
func writeStream(stream, prompt objects.Object) error {
	s, err := objects.Str(prompt)
	if err != nil {
		return err
	}
	write, err := objects.GetAttr(stream, objects.NewStr("write"))
	if err != nil {
		return err
	}
	_, err = objects.Call(write, objects.NewTuple([]objects.Object{objects.NewStr(s)}), nil)
	return err
}

// fileGetLine calls stream.readline() and applies PyFile_GetLine's n<0
// semantics: a non-string return is a TypeError, an empty line is the
// EOFError, and a single trailing newline is stripped.
//
// CPython: Objects/fileobject.c:54 PyFile_GetLine (n < 0 branch)
func fileGetLine(stream objects.Object) (objects.Object, error) {
	readline, err := objects.GetAttr(stream, objects.NewStr("readline"))
	if err != nil {
		return nil, err
	}
	result, err := objects.Call(readline, objects.NewTuple(nil), nil)
	if err != nil {
		return nil, err
	}
	line, ok := result.(*objects.Unicode)
	if !ok {
		if _, isBytes := result.(*objects.Bytes); !isBytes {
			return nil, fmt.Errorf("TypeError: object.readline() returned non-string")
		}
	}
	if line == nil {
		// bytes readline: leave the raw object untouched, matching the
		// PyBytes branch which only resizes a trailing newline.
		return result, nil
	}
	s := line.Value()
	if s == "" {
		return nil, fmt.Errorf("EOFError: EOF when reading a line")
	}
	return objects.NewStr(strings.TrimSuffix(s, "\n")), nil
}
