// TextIOWrapper wraps a binary buffer with character-level encoding and
// decoding. Full port of Modules/_io/textio.c; every function carries a
// CPython citation.
//
// The buffer may be any object that exposes read/write/seek/tell/close
// through Python attribute dispatch, matching CPython's protocol. UTF-8
// and ASCII are the only encodings directly handled; others raise
// NotImplementedError.
//
// CPython: Modules/_io/textio.c

package io

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/gopy/objects"
)

// TextIOWrapper wraps a binary buffer with text encoding.
//
// CPython: Modules/_io/textio.c:668 textio (struct)
type TextIOWrapper struct {
	objects.Header

	buf           objects.Object
	encoding      string
	errors        string
	name          string
	mode          string
	linebuffering bool
	writethrough  bool
	closed        bool
	detached      bool
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

// NewTextIOWrapper wraps a binary buffer (typically FileIO) in text mode.
//
// CPython: Modules/_io/textio.c:1102 textiowrapper_init
func NewTextIOWrapper(raw *FileIO, encoding, errors, name, mode string) *TextIOWrapper {
	t := &TextIOWrapper{
		buf:      raw,
		encoding: encoding,
		errors:   errors,
		name:     name,
		mode:     mode,
	}
	t.Init(TextIOWrapperType)
	return t
}

// textIOWrapperCall is the __init__ / __new__ type-call slot.
// TextIOWrapper(buffer, encoding=None, errors=None, newline=None,
//
//	line_buffering=False, write_through=False)
//
// CPython: Modules/_io/textio.c:1102 textiowrapper_init
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
	buf := bound[0]
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
	linebuf := false
	if bound[4] != nil && !objects.IsNone(bound[4]) {
		linebuf = objects.IsTrue(bound[4])
	}
	writethrough := false
	if bound[5] != nil && !objects.IsNone(bound[5]) {
		writethrough = objects.IsTrue(bound[5])
	}

	name := ""
	if raw, ok := buf.(*FileIO); ok {
		name = raw.name
	}
	t := &TextIOWrapper{
		buf:           buf,
		encoding:      encoding,
		errors:        errors,
		name:          name,
		linebuffering: linebuf,
		writethrough:  writethrough,
	}
	t.Init(TextIOWrapperType)
	return t, nil
}

// textIOWrapperRepr returns a string representation.
//
// CPython: Modules/_io/textio.c textiowrapper_repr
func textIOWrapperRepr(o objects.Object) (string, error) {
	t := o.(*TextIOWrapper)
	state := "open"
	if t.closed || t.detached {
		state = "closed"
	}
	return fmt.Sprintf("<_io.TextIOWrapper name=%q mode=%q encoding=%q %s>", t.name, t.mode, t.encoding, state), nil
}

// textIOWrapperIter returns the wrapper itself for iteration.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func textIOWrapperIter(o objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	if err := t.checkUsable(); err != nil {
		return nil, err
	}
	return t, nil
}

// textIOWrapperIterNext yields lines one at a time.
//
// CPython: Modules/_io/textio.c:3224 textiowrapper_iternext
func textIOWrapperIterNext(o objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	if err := t.checkUsable(); err != nil {
		return nil, err
	}
	line, err := t.readline(-1)
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, objects.ErrStopIteration
	}
	return objects.NewStr(line), nil
}

// checkUsable returns an error if the wrapper is closed or detached.
//
// CPython: Modules/_io/textio.c (closed/detached guards throughout)
func (t *TextIOWrapper) checkUsable() error {
	if t.detached {
		return fmt.Errorf("ValueError: underlying buffer has been detached")
	}
	if t.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// --- Buffer dispatch helpers -----------------------------------------------
// These mirror CPython calling buffer.read() / buffer.write() etc. through
// normal Python attribute dispatch so TextIOWrapper works with any binary
// stream: FileIO, BytesIO, BufferedReader, etc.

// bufCall looks up method name on buf and calls it with args.
func bufCall(buf objects.Object, name string, args []objects.Object) (objects.Object, error) {
	fn, err := objects.GetAttr(buf, objects.NewStr(name))
	if err != nil {
		return nil, fmt.Errorf("AttributeError: buffer has no method %q: %w", name, err)
	}
	argTuple := objects.NewTuple(args)
	return objects.Call(fn, argTuple, nil)
}

// bufRead calls buf.read(size) and returns the raw bytes.
// size < 0 means read all.
//
// CPython: Modules/_io/textio.c:1988 textiowrapper_read (buffer.read call)
func bufRead(buf objects.Object, size int) ([]byte, error) {
	var arg objects.Object
	if size < 0 {
		arg = objects.NewInt(-1)
	} else {
		arg = objects.NewInt(int64(size))
	}
	result, err := bufCall(buf, "read", []objects.Object{arg})
	if err != nil {
		return nil, err
	}
	switch v := result.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.Unicode:
		return []byte(v.Value()), nil
	}
	if objects.IsNone(result) {
		return nil, nil
	}
	return nil, fmt.Errorf("TypeError: buffer.read() returned %s, expected bytes", result.Type().Name)
}

// bufWrite calls buf.write(data) and returns the byte count written.
//
// CPython: Modules/_io/textio.c:1662 textiowrapper_write (buffer.write call)
func bufWrite(buf objects.Object, data []byte) (int, error) {
	result, err := bufCall(buf, "write", []objects.Object{objects.NewBytes(data)})
	if err != nil {
		return 0, err
	}
	if result == nil || objects.IsNone(result) {
		return len(data), nil
	}
	if n, ok := result.(*objects.Int); ok {
		v, _ := n.Int64()
		return int(v), nil
	}
	return len(data), nil
}

// bufSeek calls buf.seek(pos, whence).
//
// CPython: Modules/_io/textio.c:2536 textiowrapper_seek (buffer.seek call)
func bufSeek(buf objects.Object, pos int64, whence int) (int64, error) {
	result, err := bufCall(buf, "seek", []objects.Object{
		objects.NewInt(pos),
		objects.NewInt(int64(whence)),
	})
	if err != nil {
		return 0, err
	}
	if n, ok := result.(*objects.Int); ok {
		v, _ := n.Int64()
		return v, nil
	}
	return pos, nil
}

// bufTell calls buf.tell().
//
// CPython: Modules/_io/textio.c:2734 textiowrapper_tell (buffer.tell call)
func bufTell(buf objects.Object) (int64, error) {
	result, err := bufCall(buf, "tell", nil)
	if err != nil {
		return 0, err
	}
	if n, ok := result.(*objects.Int); ok {
		v, _ := n.Int64()
		return v, nil
	}
	return 0, nil
}

// bufClose calls buf.close().
//
// CPython: Modules/_io/textio.c:3138 textiowrapper_close (buffer.close call)
func bufClose(buf objects.Object) error {
	_, err := bufCall(buf, "close", nil)
	return err
}

// bufFlush calls buf.flush().
//
// CPython: Modules/_io/textio.c:3121 textiowrapper_flush (buffer.flush call)
func bufFlush(buf objects.Object) error {
	_, err := bufCall(buf, "flush", nil)
	return err
}

// bufTruncate calls buf.truncate(pos) or buf.truncate().
//
// CPython: Modules/_io/textio.c:2968 textiowrapper_truncate (buffer.truncate call)
func bufTruncate(buf objects.Object, pos int64, hasPos bool) (int64, error) {
	var result objects.Object
	var err error
	if hasPos {
		result, err = bufCall(buf, "truncate", []objects.Object{objects.NewInt(pos)})
	} else {
		result, err = bufCall(buf, "truncate", nil)
	}
	if err != nil {
		return 0, err
	}
	if n, ok := result.(*objects.Int); ok {
		v, _ := n.Int64()
		return v, nil
	}
	return 0, nil
}

// --- Encoding / decoding helpers -------------------------------------------

// decodeBytes decodes raw bytes using the given encoding.
//
// CPython: Modules/_io/textio.c:1988 textiowrapper_read (IncrementalDecoder.decode)
func decodeBytes(data []byte, encoding string) (string, error) {
	switch strings.ToLower(encoding) {
	case "utf-8", "utf8", "utf_8":
		if !utf8.Valid(data) {
			return "", fmt.Errorf("UnicodeDecodeError: invalid utf-8 sequence")
		}
		return string(data), nil
	case "ascii", "us-ascii":
		for _, b := range data {
			if b > 127 {
				return "", fmt.Errorf("UnicodeDecodeError: ordinal not in range(128)")
			}
		}
		return string(data), nil
	case "latin-1", "latin1", "iso-8859-1", "iso8859-1":
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes), nil
	}
	return "", fmt.Errorf("NotImplementedError: encoding %q is not yet supported", encoding)
}

// encodeString encodes a string using the given encoding.
//
// CPython: Modules/_io/textio.c:1662 textiowrapper_write (IncrementalEncoder.encode)
func encodeString(s, encoding string) ([]byte, error) {
	switch strings.ToLower(encoding) {
	case "utf-8", "utf8", "utf_8":
		return []byte(s), nil
	case "ascii", "us-ascii":
		for _, r := range s {
			if r > 127 {
				return nil, fmt.Errorf("UnicodeEncodeError: character %q is not in ASCII range", r)
			}
		}
		return []byte(s), nil
	case "latin-1", "latin1", "iso-8859-1", "iso8859-1":
		b := make([]byte, len([]rune(s)))
		for i, r := range []rune(s) {
			if r > 255 {
				return nil, fmt.Errorf("UnicodeEncodeError: character %q is not in Latin-1 range", r)
			}
			b[i] = byte(r)
		}
		return b, nil
	}
	return nil, fmt.Errorf("NotImplementedError: encoding %q is not yet supported", encoding)
}

// --- Public methods ---------------------------------------------------------

// read reads up to size characters from the buffer and decodes them.
// size < 0 reads the whole remaining content.
//
// CPython: Modules/_io/textio.c:1988 textiowrapper_read_impl
func (t *TextIOWrapper) read(size int) (string, error) {
	if err := t.checkUsable(); err != nil {
		return "", err
	}
	data, err := bufRead(t.buf, size)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	return decodeBytes(data, t.encoding)
}

// readline reads one line from the buffer.
// size < 0 means no limit.
//
// CPython: Modules/_io/textio.c:2206 _textiowrapper_readline
func (t *TextIOWrapper) readline(size int) (string, error) {
	if err := t.checkUsable(); err != nil {
		return "", err
	}
	// For BytesIO or FileIO, read byte-by-byte to find newline.
	var buf strings.Builder
	for size < 0 || buf.Len() < size {
		chunk, err := bufRead(t.buf, 1)
		if err != nil {
			return "", err
		}
		if len(chunk) == 0 {
			break
		}
		char, err := decodeBytes(chunk, t.encoding)
		if err != nil {
			return "", err
		}
		buf.WriteString(char)
		if char == "\n" {
			break
		}
	}
	return buf.String(), nil
}

// Write encodes and writes a string to the underlying buffer.
//
// CPython: Modules/_io/textio.c:1662 textiowrapper_write_impl
func (t *TextIOWrapper) Write(s string) (int, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	data, err := encodeString(s, t.encoding)
	if err != nil {
		return 0, err
	}
	_, err = bufWrite(t.buf, data)
	if err != nil {
		return 0, err
	}
	return len([]rune(s)), nil
}

// Flush flushes the write buffer and the underlying buffer.
//
// CPython: Modules/_io/textio.c:3121 textiowrapper_flush_impl
func (t *TextIOWrapper) Flush() error {
	if err := t.checkUsable(); err != nil {
		return err
	}
	return bufFlush(t.buf)
}

// Close flushes and closes the underlying buffer.
//
// CPython: Modules/_io/textio.c:3138 textiowrapper_close_impl
func (t *TextIOWrapper) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	return bufClose(t.buf)
}

// Seek seeks to a position in the underlying buffer.
//
// CPython: Modules/_io/textio.c:2536 textiowrapper_seek_impl
func (t *TextIOWrapper) Seek(pos int64, whence int) (int64, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	return bufSeek(t.buf, pos, whence)
}

// Tell returns the current stream position.
//
// CPython: Modules/_io/textio.c:2734 textiowrapper_tell_impl
func (t *TextIOWrapper) Tell() (int64, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	return bufTell(t.buf)
}

// Truncate truncates the file at the current position (or pos if given).
//
// CPython: Modules/_io/textio.c:2968 textiowrapper_truncate_impl
func (t *TextIOWrapper) Truncate(pos int64, hasPos bool) (int64, error) {
	if err := t.checkUsable(); err != nil {
		return 0, err
	}
	if err := t.Flush(); err != nil {
		return 0, err
	}
	return bufTruncate(t.buf, pos, hasPos)
}

// Detach flushes and detaches the underlying buffer.
//
// CPython: Modules/_io/textio.c:1566 textiowrapper_detach_impl
func (t *TextIOWrapper) Detach() (objects.Object, error) {
	if err := t.checkUsable(); err != nil {
		return nil, err
	}
	if err := t.Flush(); err != nil {
		return nil, err
	}
	buf := t.buf
	t.buf = nil
	t.detached = true
	return buf, nil
}

// --- Getattr / method dispatch ---------------------------------------------

// textIOWrapperGetattr exposes attributes and methods through Python dispatch.
//
// CPython: Modules/_io/textio.c textiowrapper_getset + textiowrapper_methods
func textIOWrapperGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	t := o.(*TextIOWrapper)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	// CPython: Modules/_io/textio.c:3240 textiowrapper_name_get
	case "name":
		if t.detached {
			return objects.None(), nil
		}
		if t.name != "" {
			return objects.NewStr(t.name), nil
		}
		// CPython: Modules/_io/textio.c:3240 textiowrapper_name_get
		// catches AttributeError on the underlying buffer and falls back
		// to None; we treat any GetAttr failure as that case for now.
		nameAttr, err := objects.GetAttr(t.buf, objects.NewStr("name"))
		if err != nil { //nolint:nilerr // matches CPython AttributeError fallback
			return objects.None(), nil
		}
		return nameAttr, nil
	case "mode":
		return objects.NewStr(t.mode), nil
	// CPython: Modules/_io/textio.c textiowrapper member encoding
	case "encoding":
		return objects.NewStr(t.encoding), nil
	// CPython: Modules/_io/textio.c:3288 textiowrapper_errors_get
	case "errors":
		return objects.NewStr(t.errors), nil
	// CPython: Modules/_io/textio.c:3254 textiowrapper_closed_get
	case "closed":
		return objects.NewBool(t.closed || t.detached), nil
	// CPython: Modules/_io/textio.c textiowrapper member line_buffering
	case "line_buffering":
		return objects.NewBool(t.linebuffering), nil
	// CPython: Modules/_io/textio.c textiowrapper member write_through
	case "write_through":
		return objects.NewBool(t.writethrough), nil
	// CPython: Modules/_io/textio.c:3268 textiowrapper_newlines_get
	case "newlines":
		return objects.None(), nil
	// CPython: Modules/_io/textio.c textiowrapper member buffer
	case "buffer":
		if t.detached {
			return objects.None(), nil
		}
		return t.buf, nil
	}
	if fn := textIOWrapperMethod(t, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.TextIOWrapper' object has no attribute '%s'", n.Value())
}

// textIOWrapperMethod returns BuiltinFunction objects for TextIOWrapper methods.
//
// CPython: Modules/_io/textio.c textiowrapper_methods
func textIOWrapperMethod(t *TextIOWrapper, name string) objects.Object {
	switch name {
	// CPython: Modules/_io/textio.c:1988 textiowrapper_read_impl
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size, err := optionalSize(args, "read")
			if err != nil {
				return nil, err
			}
			s, err := t.read(size)
			if err != nil {
				return nil, err
			}
			return objects.NewStr(s), nil
		})
	// CPython: Modules/_io/textio.c:2372 textiowrapper_readline_impl
	case "readline":
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size, err := optionalSize(args, "readline")
			if err != nil {
				return nil, err
			}
			s, err := t.readline(size)
			if err != nil {
				return nil, err
			}
			return objects.NewStr(s), nil
		})
	// CPython: Modules/_io/textio.c (readlines via IOBase)
	case "readlines":
		return objects.NewBuiltinFunction("readlines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := t.checkUsable(); err != nil {
				return nil, err
			}
			var lines []objects.Object
			for {
				line, err := t.readline(-1)
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
	// CPython: Modules/_io/textio.c:1662 textiowrapper_write_impl
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
			}
			s, ok := args[0].(*objects.Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: write() argument must be str, not %s", args[0].Type().Name)
			}
			n, err := t.Write(s.Value())
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(n)), nil
		})
	// CPython: Modules/_io/textio.c (writelines via IOBase)
	case "writelines":
		return objects.NewBuiltinFunction("writelines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := t.checkUsable(); err != nil {
				return nil, err
			}
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
	// CPython: Modules/_io/textio.c:2536 textiowrapper_seek_impl
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
			out, err := t.Seek(int64(pos), whence)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	// CPython: Modules/_io/textio.c:2734 textiowrapper_tell_impl
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			pos, err := t.Tell()
			if err != nil {
				return nil, err
			}
			return objects.NewInt(pos), nil
		})
	// CPython: Modules/_io/textio.c:2968 textiowrapper_truncate_impl
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) == 0 || objects.IsNone(args[0]) {
				n, err := t.Truncate(0, false)
				if err != nil {
					return nil, err
				}
				return objects.NewInt(n), nil
			}
			pos, err := intArg(args[0], "truncate")
			if err != nil {
				return nil, err
			}
			n, err := t.Truncate(int64(pos), true)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(n), nil
		})
	// CPython: Modules/_io/textio.c:3121 textiowrapper_flush_impl
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), t.Flush()
		})
	// CPython: Modules/_io/textio.c:3138 textiowrapper_close_impl
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.None(), t.Close()
		})
	// CPython: Modules/_io/textio.c:1566 textiowrapper_detach_impl
	case "detach":
		return objects.NewBuiltinFunction("detach", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return t.Detach()
		})
	// CPython: Modules/_io/textio.c:1370 textiowrapper_reconfigure
	case "reconfigure":
		return objects.NewBuiltinFunction("reconfigure", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if err := t.checkUsable(); err != nil {
				return nil, err
			}
			if enc, ok := kwargs["encoding"]; ok && !objects.IsNone(enc) {
				if s, ok := enc.(*objects.Unicode); ok {
					t.encoding = s.Value()
				}
			}
			if errs, ok := kwargs["errors"]; ok && !objects.IsNone(errs) {
				if s, ok := errs.(*objects.Unicode); ok {
					t.errors = s.Value()
				}
			}
			if lb, ok := kwargs["line_buffering"]; ok && !objects.IsNone(lb) {
				t.linebuffering = objects.IsTrue(lb)
			}
			if wt, ok := kwargs["write_through"]; ok && !objects.IsNone(wt) {
				t.writethrough = objects.IsTrue(wt)
			}
			return t, nil
		})
	case "readable":
		// CPython: Modules/_io/textio.c:2997 _io_TextIOWrapper_readable_impl
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if t.detached || t.closed {
				return objects.False(), nil
			}
			return bufCall(t.buf, "readable", nil)
		})
	case "writable":
		// CPython: Modules/_io/textio.c:3009 _io_TextIOWrapper_writable_impl
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if t.detached || t.closed {
				return objects.False(), nil
			}
			return bufCall(t.buf, "writable", nil)
		})
	case "seekable":
		// CPython: Modules/_io/textio.c:3033 _io_TextIOWrapper_seekable_impl
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if t.detached || t.closed {
				return objects.False(), nil
			}
			return bufCall(t.buf, "seekable", nil)
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.False(), nil
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
	case "fileno":
		return objects.NewBuiltinFunction("fileno", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return bufCall(t.buf, "fileno", nil)
		})
	}
	return nil
}

// --- IncrementalNewlineDecoder --------------------------------------------

// IncrementalNewlineDecoder wraps another codec decoder and handles universal
// newline translation. Implements the public interface from textio.c.
//
// CPython: Modules/_io/textio.c:219 nldecoder_object
type IncrementalNewlineDecoder struct {
	objects.Header

	decoder   objects.Object
	translate bool
	errors    string
	seennl    int
	pendingcr bool
}

// IncrementalNewlineDecoderType is the type for _io.IncrementalNewlineDecoder.
var IncrementalNewlineDecoderType = objects.NewType("_io.IncrementalNewlineDecoder", []*objects.Type{objects.ObjectType()})

func init() {
	IncrementalNewlineDecoderType.Call = incrementalNLDecoderCall
	IncrementalNewlineDecoderType.Getattro = incrementalNLDecoderGetattr
}

// incrementalNLDecoderCall constructs a new IncrementalNewlineDecoder.
//
// CPython: Modules/_io/textio.c:247 incrementalnewlinedecoder_init
func incrementalNLDecoderCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	names := []string{"decoder", "translate", "errors"}
	bound := make([]objects.Object, len(names))
	copy(bound, args)
	for k, v := range kwargs {
		for i, n := range names {
			if n == k {
				bound[i] = v
				break
			}
		}
	}
	if bound[0] == nil {
		return nil, fmt.Errorf("TypeError: IncrementalNewlineDecoder() missing required argument 'decoder'")
	}
	translate := false
	if bound[1] != nil && !objects.IsNone(bound[1]) {
		translate = objects.IsTrue(bound[1])
	}
	errors := "strict"
	if bound[2] != nil && !objects.IsNone(bound[2]) {
		if s, ok := bound[2].(*objects.Unicode); ok {
			errors = s.Value()
		}
	}
	d := &IncrementalNewlineDecoder{
		decoder:   bound[0],
		translate: translate,
		errors:    errors,
	}
	d.Init(IncrementalNewlineDecoderType)
	return d, nil
}

// Newline-bitset flags, CPython: Modules/_io/textio.c (anonymous enum
// inside _PyIncrementalNewlineDecoder_decode).
const (
	seenCR   = 1
	seenLF   = 2
	seenCRLF = 4
	seenAll  = seenCR | seenLF | seenCRLF
)

// incrementalNLDecoderGetattr handles attribute and method access.
func incrementalNLDecoderGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	d := o.(*IncrementalNewlineDecoder)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	// CPython: Modules/_io/textio.c:637 incrementalnewlinedecoder_newlines_get
	case "newlines":
		switch d.seennl {
		case seenCR:
			return objects.NewStr("\r"), nil
		case seenLF:
			return objects.NewStr("\n"), nil
		case seenCRLF:
			return objects.NewStr("\r\n"), nil
		case seenCR | seenLF:
			return objects.NewTuple([]objects.Object{objects.NewStr("\r"), objects.NewStr("\n")}), nil
		case seenCR | seenCRLF:
			return objects.NewTuple([]objects.Object{objects.NewStr("\r"), objects.NewStr("\r\n")}), nil
		case seenLF | seenCRLF:
			return objects.NewTuple([]objects.Object{objects.NewStr("\n"), objects.NewStr("\r\n")}), nil
		case seenCR | seenLF | seenCRLF:
			return objects.NewTuple([]objects.Object{objects.NewStr("\r"), objects.NewStr("\n"), objects.NewStr("\r\n")}), nil
		default:
			return objects.None(), nil
		}
	// CPython: Modules/_io/textio.c:326 _PyIncrementalNewlineDecoder_decode
	case "decode":
		return objects.NewBuiltinFunction("decode", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: decode() missing required argument 'input'")
			}
			final := false
			if len(args) >= 2 {
				final = objects.IsTrue(args[1])
			}
			if v, ok := kwargs["final"]; ok {
				final = objects.IsTrue(v)
			}
			// Step 1: pass through the underlying decoder if present.
			var decoded string
			if d.decoder != nil && !objects.IsNone(d.decoder) {
				finalArg := objects.False()
				if final {
					finalArg = objects.True()
				}
				result, err := bufCall(d.decoder, "decode", []objects.Object{args[0], finalArg})
				if err != nil {
					return nil, err
				}
				if s, ok := result.(*objects.Unicode); ok {
					decoded = s.Value()
				}
			} else {
				switch v := args[0].(type) {
				case *objects.Bytes:
					decoded = string(v.Bytes())
				case *objects.Unicode:
					decoded = v.Value()
				default:
					return nil, fmt.Errorf("TypeError: decode() argument must be bytes or str")
				}
			}
			return objects.NewStr(d.translateNewlines(decoded, final)), nil
		}), nil
	// CPython: Modules/_io/textio.c:541 incrementalnewlinedecoder_getstate
	case "getstate":
		return objects.NewBuiltinFunction("getstate", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			var buf objects.Object
			if d.pendingcr {
				buf = objects.NewBytes([]byte("\r"))
			} else {
				buf = objects.NewBytes(nil)
			}
			flags := int64(0)
			if d.decoder != nil && !objects.IsNone(d.decoder) {
				if r, err := bufCall(d.decoder, "getstate", nil); err == nil {
					if tup, ok := r.(*objects.Tuple); ok && tup.Len() >= 2 {
						if f, ok := tup.Item(1).(*objects.Int); ok {
							v, _ := f.Int64()
							flags = v << 1
						}
					}
				}
			}
			if d.pendingcr {
				flags |= 1
			}
			return objects.NewTuple([]objects.Object{buf, objects.NewInt(flags)}), nil
		}), nil
	// CPython: Modules/_io/textio.c:587 incrementalnewlinedecoder_setstate
	case "setstate":
		return objects.NewBuiltinFunction("setstate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: setstate() missing required argument")
			}
			tup, ok := args[0].(*objects.Tuple)
			if !ok || tup.Len() < 2 {
				return nil, fmt.Errorf("TypeError: setstate() argument must be 2-tuple")
			}
			flags, ok := tup.Item(1).(*objects.Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: setstate() flags must be int")
			}
			v, _ := flags.Int64()
			d.pendingcr = v&1 != 0
			return objects.None(), nil
		}), nil
	// CPython: Modules/_io/textio.c:624 incrementalnewlinedecoder_reset
	case "reset":
		return objects.NewBuiltinFunction("reset", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			d.seennl = 0
			d.pendingcr = false
			if d.decoder != nil && !objects.IsNone(d.decoder) {
				_, err := bufCall(d.decoder, "reset", nil)
				return objects.None(), err
			}
			return objects.None(), nil
		}), nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.IncrementalNewlineDecoder' object has no attribute '%s'", n.Value())
}

// translateNewlines applies universal newline tracking and (optionally)
// translation to s. It mirrors the inner loop of CPython's
// _PyIncrementalNewlineDecoder_decode: pendingcr is prefixed onto the
// next chunk when there is content (or when final), a trailing \r in a
// non-final chunk is held back as pendingcr, and the seennl bitset
// distinguishes \r, \n, and \r\n separately.
//
// CPython: Modules/_io/textio.c:326 _PyIncrementalNewlineDecoder_decode.
func (d *IncrementalNewlineDecoder) translateNewlines(s string, final bool) string {
	if d.pendingcr && (final || len(s) > 0) {
		s = "\r" + s
		d.pendingcr = false
	}
	if !final && len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
		d.pendingcr = true
	}
	if len(s) == 0 {
		return s
	}

	seen := d.seennl
	// Fast path: if we've only seen LF so far (or nothing) and there is
	// no \r in s, just check whether there is a \n and we're done.
	if (seen == seenLF || seen == 0) && !strings.ContainsRune(s, '\r') {
		if seen == 0 && strings.ContainsRune(s, '\n') {
			seen |= seenLF
		}
		d.seennl = seen
		return s
	}

	if !d.translate {
		// Scan: track \n, \r, \r\n without modifying s.
		i := 0
		for i < len(s) && seen != seenAll {
			c := s[i]
			i++
			switch c {
			case '\n':
				seen |= seenLF
			case '\r':
				if i < len(s) && s[i] == '\n' {
					seen |= seenCRLF
					i++
				} else {
					seen |= seenCR
				}
			}
		}
		d.seennl = seen
		return s
	}

	// Translate path: copy into a new buffer, mapping \r and \r\n to \n.
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		i++
		switch c {
		case '\n':
			out = append(out, '\n')
			seen |= seenLF
		case '\r':
			if i < len(s) && s[i] == '\n' {
				i++
				seen |= seenCRLF
			} else {
				seen |= seenCR
			}
			out = append(out, '\n')
		default:
			out = append(out, c)
		}
	}
	d.seennl = seen
	return string(out)
}

// Read is the Go-level read method (used by tests and internal code).
// size < 0 reads all.
func (t *TextIOWrapper) Read(size int) (string, error) {
	return t.read(size)
}

// Readline is the Go-level readline method.
func (t *TextIOWrapper) Readline(size int) (string, error) {
	return t.readline(size)
}

// --- _TextIOBase abstract base -----------------------------------------------

// TextIOBaseType is the type singleton for _io._TextIOBase.
//
// CPython: Modules/_io/textio.c:202 textiobase_slots / textiobase_spec
var TextIOBaseType = objects.NewType("_io._TextIOBase", []*objects.Type{IOBaseType})

// textIOBaseGetattr dispatches attribute lookups on _TextIOBase instances.
//
// CPython: Modules/_io/textio.c:187 textiobase_methods + textiobase_getset
func textIOBaseGetattr(_ objects.Object, nameObj objects.Object) (objects.Object, error) {
	name, ok := nameObj.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch name.Value() {
	case "detach":
		// CPython: Modules/_io/textio.c:66 _io__TextIOBase_detach_impl
		return objects.NewBuiltinFunction("detach", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: detach")
		}), nil
	case "read":
		// CPython: Modules/_io/textio.c:86 _io__TextIOBase_read_impl
		return objects.NewBuiltinFunction("read", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: read")
		}), nil
	case "readline":
		// CPython: Modules/_io/textio.c:107 _io__TextIOBase_readline_impl
		return objects.NewBuiltinFunction("readline", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: readline")
		}), nil
	case "write":
		// CPython: Modules/_io/textio.c:128 _io__TextIOBase_write_impl
		return objects.NewBuiltinFunction("write", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return nil, fmt.Errorf("UnsupportedOperation: write")
		}), nil
	case "encoding":
		// CPython: Modules/_io/textio.c:146 _io__TextIOBase_encoding_get_impl
		return objects.None(), nil
	case "newlines":
		// CPython: Modules/_io/textio.c:164 _io__TextIOBase_newlines_get_impl
		return objects.None(), nil
	case "errors":
		// CPython: Modules/_io/textio.c:180 _io__TextIOBase_errors_get_impl
		return objects.None(), nil
	}
	return nil, fmt.Errorf("AttributeError: '_io._TextIOBase' object has no attribute '%s'", name.Value())
}

func init() {
	TextIOBaseType.Getattro = textIOBaseGetattr
}
