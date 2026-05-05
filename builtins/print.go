// Port of Python/bltinmodule.c print and the small piece of
// _PyBuiltin_Init that stamps print plus the named singletons into
// the builtins dict. The full bltinmodule (input, len, abs, ord, ...)
// arrives in follow-on commits.
//
// CPython: Python/bltinmodule.c builtin_print_impl + _PyBuiltin_Init

package builtins

import (
	"fmt"
	"io"

	"github.com/tamnd/gopy/objects"
)

// Print is the Python-callable print(*args, sep=' ', end='\n',
// file=None, flush=False). Mirrors builtin_print_impl one-for-one.
// The file kwarg defaults to defaultFile when omitted; CPython would
// resolve None through sys.stdout, which gopy does not yet have.
//
// CPython: Python/bltinmodule.c:2224 builtin_print_impl
func Print(defaultFile io.Writer) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		file := defaultFile
		var sep, end *string
		flush := false

		if v, ok := kwargs["file"]; ok && !objects.IsNone(v) {
			w, err := writerFromObject(v)
			if err != nil {
				return nil, err
			}
			file = w
		}
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
			if f, ok := file.(interface{ Flush() error }); ok {
				if err := f.Flush(); err != nil {
					return nil, err
				}
			}
		}
		return objects.None(), nil
	}
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
// gopy does not yet have a generic file-like protocol, so until the
// io module port lands we accept Go writers tunneled through the
// builtin facade.
//
// CPython: Python/bltinmodule.c:2231 sys.stdout fallback path
func writerFromObject(o objects.Object) (io.Writer, error) {
	if w, ok := o.(io.Writer); ok {
		return w, nil
	}
	return nil, fmt.Errorf("TypeError: %s object has no write() method", o.Type().Name)
}
