// TextIOWrapper wraps a binary buffer (FileIO) with character-level
// encoding and decoding, mirroring the C type in
// cpython/Modules/_io/textio.c.
//
// The Go port uses Go's native UTF-8 handling for the "utf-8" encoding
// (the only encoding currently supported). Reads decode bytes to string;
// writes encode string to bytes. Line-ending translation is deferred.
//
// CPython: Modules/_io/textio.c:2261 textiowrapper (struct definition)
// CPython: Modules/_io/textio.c (textiowrapper_methods)

package io

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// TextIOWrapper wraps a FileIO and performs text encoding/decoding.
//
// CPython: Modules/_io/textio.c:2261 textiowrapper
type TextIOWrapper struct {
	objects.Header

	raw      *FileIO
	encoding string
	errors   string
	name     string
	mode     string
	closed   bool
	scanner  *bufio.Scanner
}

// TextIOWrapperType is the type singleton for _io.TextIOWrapper.
//
// CPython: Modules/_io/textio.c PyTextIOWrapper_Type equivalent
var TextIOWrapperType = objects.NewType("_io.TextIOWrapper", []*objects.Type{objects.ObjectType()})

func init() {
	TextIOWrapperType.Call = textIOWrapperCall
	TextIOWrapperType.Repr = textIOWrapperRepr
	TextIOWrapperType.Str = textIOWrapperRepr
	TextIOWrapperType.Iter = textIOWrapperIter
	TextIOWrapperType.IterNext = textIOWrapperIterNext
	TextIOWrapperType.Getattro = textIOWrapperGetattr
}

// NewTextIOWrapper wraps a FileIO in text mode.
//
// CPython: Modules/_io/textio.c:1163 textiowrapper_init
func NewTextIOWrapper(raw *FileIO, encoding, errors, name, mode string) *TextIOWrapper {
	t := &TextIOWrapper{
		raw:      raw,
		encoding: encoding,
		errors:   errors,
		name:     name,
		mode:     mode,
	}
	t.Init(TextIOWrapperType)
	return t
}

// textIOWrapperCall is the type-call slot.
// TextIOWrapper(buffer, encoding=None, errors=None, newline=None,
//               line_buffering=False, write_through=False)
//
// CPython: Modules/_io/textio.c:1163 textiowrapper_init
func textIOWrapperCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	names := []string{"buffer", "encoding", "errors", "newline", "line_buffering", "write_through"}
	bound := make([]objects.Object, len(names))
	copy(bound, args)
	for k, v := range kwargs {
		idx := -1
		for i, n := range names {
			if n == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("TypeError: TextIOWrapper() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: TextIOWrapper() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return nil, fmt.Errorf("TypeError: TextIOWrapper() missing required argument 'buffer'")
	}
	raw, ok := bound[0].(*FileIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: TextIOWrapper() buffer must be FileIO, not %s", bound[0].Type().Name)
	}
	encoding := "utf-8"
	if bound[1] != nil && !objects.IsNone(bound[1]) {
		s, ok := bound[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: invalid encoding: %s", bound[1].Type().Name)
		}
		encoding = s.Value()
	}
	errors := "strict"
	if bound[2] != nil && !objects.IsNone(bound[2]) {
		s, ok := bound[2].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: invalid errors: %s", bound[2].Type().Name)
		}
		errors = s.Value()
	}
	return NewTextIOWrapper(raw, encoding, errors, raw.name, raw.mode), nil
}

func textIOWrapperRepr(o objects.Object) (string, error) {
	t := o.(*TextIOWrapper)
	state := "open"
	if t.closed {
		state = "closed"
	}
	return fmt.Sprintf("<_io.TextIOWrapper name=%q mode=%q encoding=%q %s>", t.name, t.mode, t.encoding, state), nil
}

// textIOWrapperIter returns the wrapper itself.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func textIOWrapperIter(o objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	if err := t.checkUsable(); err != nil {
		return nil, err
	}
	return t, nil
}

// textIOWrapperIterNext yields lines.
//
// CPython: Modules/_io/textio.c textiowrapper_iternext
func textIOWrapperIterNext(o objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	if err := t.checkUsable(); err != nil {
		return nil, err
	}
	line, err := t.Readline(-1)
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, objects.ErrStopIteration
	}
	return objects.NewStr(line), nil
}

func (t *TextIOWrapper) checkUsable() error {
	if t.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// Read reads up to size characters (bytes in UTF-8) from the underlying
// file and decodes them as text. size<0 reads the whole file.
//
// CPython: Modules/_io/textio.c:1804 textiowrapper_read_chunk
func (t *TextIOWrapper) Read(size int) (string, error) {
	if err := t.checkUsable(); err != nil {
		return "", err
	}
	data, err := t.raw.Read(size)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Readline reads one text line from the underlying raw file.
//
// CPython: Modules/_io/textio.c:1885 textiowrapper_readline
func (t *TextIOWrapper) Readline(size int) (string, error) {
	if err := t.checkUsable(); err != nil {
		return "", err
	}
	var buf strings.Builder
	for size < 0 || buf.Len() < size {
		chunk := make([]byte, 1)
		n, err := t.raw.f.Read(chunk)
		if n == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("OSError: %s", err.Error())
		}
		buf.WriteByte(chunk[0])
		if chunk[0] == '\n' {
			break
		}
	}
	return buf.String(), nil
}

// Write encodes the string and writes it to the underlying raw file.
//
// CPython: Modules/_io/textio.c:1499 textiowrapper_write
func (t *TextIOWrapper) Write(s string) (int, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	if !t.raw.writable {
		return 0, fmt.Errorf("io.UnsupportedOperation: not writable")
	}
	data := []byte(s)
	n, err := t.raw.Write(data)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Flush flushes the underlying raw file.
//
// CPython: Modules/_io/textio.c textiowrapper_flush
func (t *TextIOWrapper) Flush() error {
	if err := t.checkUsable(); err != nil {
		return err
	}
	return nil
}

// Close flushes and closes the underlying raw file.
//
// CPython: Modules/_io/textio.c textiowrapper_close
func (t *TextIOWrapper) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	return t.raw.Close()
}

// textIOWrapperGetattr exposes attributes and methods.
//
// CPython: Modules/_io/textio.c textiowrapper_getset + textiowrapper_methods
func textIOWrapperGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		return objects.NewStr(t.name), nil
	case "mode":
		return objects.NewStr(t.mode), nil
	case "encoding":
		return objects.NewStr(t.encoding), nil
	case "errors":
		return objects.NewStr(t.errors), nil
	case "closed":
		return objects.NewBool(t.closed), nil
	case "line_buffering":
		return objects.NewBool(false), nil
	case "newlines":
		return objects.None(), nil
	case "buffer":
		return t.raw, nil
	}
	if fn := textIOWrapperMethod(t, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.TextIOWrapper' object has no attribute '%s'", n.Value())
}

// textIOWrapperMethod maps method names to BuiltinFunctions.
//
// CPython: Modules/_io/textio.c textiowrapper_methods
func textIOWrapperMethod(t *TextIOWrapper, name string) objects.Object {
	switch name {
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size, err := optionalSize(args, "read")
			if err != nil {
				return nil, err
			}
			s, err := t.Read(size)
			if err != nil {
				return nil, err
			}
			return objects.NewStr(s), nil
		})
	case "readline":
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size, err := optionalSize(args, "readline")
			if err != nil {
				return nil, err
			}
			s, err := t.Readline(size)
			if err != nil {
				return nil, err
			}
			return objects.NewStr(s), nil
		})
	case "readlines":
		return objects.NewBuiltinFunction("readlines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := t.checkUsable(); err != nil {
				return nil, err
			}
			var lines []objects.Object
			for {
				line, err := t.Readline(-1)
				if err != nil {
					return nil, err
				}
				if line == "" {
					break
				}
				lines = append(lines, objects.NewStr(line))
			}
			return objects.NewList(lines), nil
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
			}
			s, ok := args[0].(*objects.Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: write() argument must be str, not %s", args[0].Type().Name)
			}
			if _, err := t.Write(s.Value()); err != nil {
				return nil, err
			}
			// Return character count (rune count) matching CPython behavior.
			// CPython: Modules/_io/textio.c:1499 textiowrapper_write returns
			// the number of characters written to the text layer.
			return objects.NewInt(int64(len([]rune(s.Value())))), nil
		})
	case "writelines":
		return objects.NewBuiltinFunction("writelines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: writelines() takes exactly 1 argument (%d given)", len(args))
			}
			lst, ok := args[0].(*objects.List)
			if !ok {
				return nil, fmt.Errorf("TypeError: writelines() argument must be list")
			}
			for i := 0; i < lst.Len(); i++ {
				item := lst.Item(i)
				s, ok := item.(*objects.Unicode)
				if !ok {
					return nil, fmt.Errorf("TypeError: write() argument must be str, not %s", item.Type().Name)
				}
				if _, err := t.Write(s.Value()); err != nil {
					return nil, err
				}
			}
			return objects.None(), nil
		})
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("TypeError: seek() takes 1 or 2 arguments (%d given)", len(args))
			}
			pos, err := intArg(args[0], "seek")
			if err != nil {
				return nil, err
			}
			whence := 0
			if len(args) == 2 {
				w, err := intArg(args[1], "seek")
				if err != nil {
					return nil, err
				}
				whence = w
			}
			out, err := t.raw.Seek(int64(pos), whence)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			pos, err := t.raw.Tell()
			if err != nil {
				return nil, err
			}
			return objects.NewInt(pos), nil
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), t.Flush()
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), t.Close()
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(t.raw.readable && !t.closed), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(t.raw.writable && !t.closed), nil
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(!t.closed), nil
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(false), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := t.checkUsable(); err != nil {
				return nil, err
			}
			return t, nil
		})
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), t.Close()
		})
	}
	return nil
}
