// Port of Python/bltinmodule.c builtin_print_impl and its sys.stdout
// resolution. CPython looks up sys.stdout on every call so that
// `sys.stdout = io.StringIO(); print('x')` redirects into the swapped
// stream; gopy mirrors the dynamic lookup so support.captured_stdout()
// works as the test suite expects.
//
// CPython: Python/bltinmodule.c:2224 builtin_print_impl

package builtins

import (
	"fmt"
	"io"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// Print is the Python-callable print(*args, sep=' ', end='\n',
// file=None, flush=False). Mirrors builtin_print_impl one-for-one.
// When file is omitted or None, CPython calls
// _PySys_GetRequiredAttr(&_Py_ID(stdout)); gopy resolves the same path
// via imp.GetModule("sys").Dict().GetItem("stdout") on every call.
//
// CPython: Python/bltinmodule.c:2224 builtin_print_impl
// CPython: Python/bltinmodule.c:2231 (file == Py_None branch)
func Print(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	file, err := resolveFileKwarg(kwargs["file"])
	if err != nil {
		return nil, err
	}
	// CPython lets sys.stdout be None when FILE* stdout isn't connected;
	// the call becomes a no-op in that case.
	//
	// CPython: Python/bltinmodule.c:2238 (file == Py_None after lookup)
	if file == nil {
		return objects.None(), nil
	}

	var sep, end *string
	flush := false
	if v, ok := kwargs["sep"]; ok && !objects.IsNone(v) {
		s, err := unicodeArg("sep", v)
		if err != nil {
			return nil, err
		}
		sep = &s
	}
	if v, ok := kwargs["end"]; ok && !objects.IsNone(v) {
		s, err := unicodeArg("end", v)
		if err != nil {
			return nil, err
		}
		end = &s
	}
	if v, ok := kwargs["flush"]; ok {
		b, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		flush = b
	}

	for i, a := range args {
		if i > 0 {
			s := " "
			if sep != nil {
				s = *sep
			}
			if _, err := io.WriteString(file, s); err != nil {
				return nil, err
			}
		}
		s, err := objects.Str(a)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(file, s); err != nil {
			return nil, err
		}
	}

	tail := "\n"
	if end != nil {
		tail = *end
	}
	if _, err := io.WriteString(file, tail); err != nil {
		return nil, err
	}

	if flush {
		// Call the Python-level flush() method on the file object, matching
		// CPython's builtin_print_impl which calls PyObject_CallMethodNoArgs(file, "flush").
		//
		// CPython: Python/bltinmodule.c:2278 PyObject_CallMethodNoArgs(file, &_Py_ID(flush))
		fileObj := kwargs["file"]
		if fileObj == nil || objects.IsNone(fileObj) {
			fileObj = sysStdout()
		}
		if fileObj != nil && !objects.IsNone(fileObj) {
			flushAttr, ferr := objects.GetAttr(fileObj, objects.NewStr("flush"))
			if ferr == nil {
				if _, ferr = objects.Call(flushAttr, objects.NewTuple(nil), nil); ferr != nil {
					return nil, ferr
				}
			}
		}
	}
	return objects.None(), nil
}

// resolveFileKwarg returns the writer print() should write into. The
// kwarg value of nil / None means "look up sys.stdout on this call".
// CPython performs the same resolution at the top of
// builtin_print_impl.
//
// CPython: Python/bltinmodule.c:2231 _PySys_GetRequiredAttr stdout
func resolveFileKwarg(v objects.Object) (io.Writer, error) {
	if v != nil && !objects.IsNone(v) {
		return writerFromObject(v)
	}
	stdout := sysStdout()
	if stdout == nil || objects.IsNone(stdout) {
		return nil, nil
	}
	return writerFromObject(stdout)
}

// sysStdout reads sys.stdout from the sys module on every call.
// Mirrors PySys_GetObject("stdout") exactly: returns nil when sys is
// missing or sys.stdout is absent / None. CPython's builtin_print_impl
// treats the nil/None case as a no-op (see the Py_None branch around
// Python/bltinmodule.c:2238).
//
// CPython: Python/sysmodule.c PySys_GetObject("stdout")
func sysStdout() objects.Object {
	mod, ok := imp.GetModule("sys")
	if !ok || mod == nil {
		return nil
	}
	d := mod.Dict()
	if d == nil {
		return nil
	}
	v, err := d.GetItem(objects.NewStr("stdout"))
	if err != nil {
		return nil
	}
	return v
}

// unicodeArg validates that o is a str (or None handled by caller)
// and returns its Go-string contents. Mirrors the
// "X must be None or a string, not %.200s" check in builtin_print_impl.
//
// CPython: Python/bltinmodule.c:2250 builtin_print_impl sep/end check
func unicodeArg(name string, o objects.Object) (string, error) {
	if o.Type() != objects.StrType() {
		return "", fmt.Errorf("TypeError: %s must be None or a string, not %s",
			name, o.Type().Name)
	}
	return objects.Str(o)
}

// writerFromObject extracts an io.Writer from a Python file object.
// Accepts a direct io.Writer (Go-tunneled), an *objects.File, or any
// object that defines a write() method (matches PyFile_WriteObject's
// duck-typed path through PyObject_CallMethodOneArg).
//
// CPython: Python/fileobject.c:42 PyFile_WriteObject
func writerFromObject(o objects.Object) (io.Writer, error) {
	if w, ok := o.(io.Writer); ok {
		return w, nil
	}
	if f, ok := o.(*objects.File); ok {
		return &fileWriter{f: f}, nil
	}
	// Duck-typed write(): Python-level objects like io.StringIO or any
	// user class with a write method land here.
	writeAttr, err := objects.GetAttr(o, objects.NewStr("write"))
	if err == nil {
		return &callableWriter{write: writeAttr}, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute 'write'", o.Type().Name)
}

// fileWriter adapts an objects.File to io.Writer by funneling raw byte
// chunks through File.Write as a str payload. print() only writes UTF-8
// strings (separators, formatted args, and the terminating newline), so
// reconstructing a Unicode argument here is exact.
type fileWriter struct{ f *objects.File }

func (w *fileWriter) Write(p []byte) (int, error) {
	return w.f.Write(objects.NewStr(string(p)))
}

// callableWriter adapts a Python object with a write() method to
// io.Writer. PyFile_WriteString calls write(str(p)) on the underlying
// object; gopy mirrors the same call shape so io.StringIO, custom
// classes with __write__, and the captured_stdout helper all behave
// identically.
//
// CPython: Python/fileobject.c:80 PyFile_WriteString
type callableWriter struct{ write objects.Object }

func (c *callableWriter) Write(p []byte) (int, error) {
	_, err := objects.Call(c.write, objects.NewTuple([]objects.Object{objects.NewStr(string(p))}), nil)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
