// _io built-in module. Mirrors the slice of cpython/Modules/_io/
// that the public io module re-exports. The full _io module is a
// large family of types (FileIO, BytesIO, BufferedReader/Writer/Random,
// BufferedRWPair, TextIOWrapper, IncrementalNewlineDecoder, plus the
// _IOBase / _RawIOBase / _BufferedIOBase / _TextIOBase ABC hierarchy);
// this port lands StringIO, BytesIO, FileIO, and TextIOWrapper.
// Buffered layer types remain stubs.
//
// CPython: Modules/_io/_iomodule.c:580 module_methods (module-level fns)
// CPython: Modules/_io/_iomodule.c:782 PyInit__io (the build hook)

package io

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_io", buildModule)
}

// DefaultBufferSize is the value Lib/io.py copies into its module-level
// constant of the same name. Mirrors the macro in _iomodule.h.
//
// CPython: Modules/_io/_iomodule.h:14 DEFAULT_BUFFER_SIZE
const DefaultBufferSize = 8192

// stubType is a placeholder Type used to occupy class names io.py
// imports through `from _io import ...`. They are subclassable and
// instantiable but raise NotImplementedError when called, since the
// underlying implementation has not been ported yet.
//
// CPython: Modules/_io/_iomodule.c:782 PyInit__io (those rows come
// from individual ported types; this is the placeholder until each
// gets its own port).
func stubType(name string) *objects.Type {
	t := objects.NewType(name, []*objects.Type{objects.ObjectType()})
	t.Call = func(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return nil, fmt.Errorf("NotImplementedError: %s is not yet ported", name)
	}
	return t
}

// unsupportedOpType is the _io.UnsupportedOperation exception class.
// CPython makes it a subclass of OSError + ValueError; this port keeps
// the surface narrow (a callable that returns itself wrapped in an
// error message) since no current import path raises it.
//
// CPython: Modules/_io/_iomodule.c:621 _PyIO_get_module_state +
// CPython: Modules/_io/_iomodule.c:709 (UnsupportedOperation registration)
var unsupportedOpType = stubType("_io.UnsupportedOperation")

// buildModule materializes the _io module dict. Mirrors the
// PyInit__io entry: register every type and module-level constant
// the public io module imports through `from _io import ...`.
//
// CPython: Modules/_io/_iomodule.c:782 PyInit__io
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_io")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		// StringIO: ported in stringio.go.
		{"StringIO", StringIOType},

		// BytesIO: ported in bytesio.go.
		{"BytesIO", BytesIOType},

		// FileIO: ported in fileio.go.
		{"FileIO", FileIOType},

		// TextIOWrapper: ported in textiowrapper.go.
		{"TextIOWrapper", TextIOWrapperType},

		// Buffered layer: ported in bufferedio.go.
		{"BufferedReader", BufferedReaderType},
		{"BufferedWriter", BufferedWriterType},
		{"BufferedRWPair", BufferedRWPairType},
		{"BufferedRandom", BufferedRandomType},

		{"IncrementalNewlineDecoder", IncrementalNewlineDecoderType},
		// ABC hierarchy: ported in iobase.go.
		// CPython: Modules/_io/_iomodule.c:683 (type registration order)
		{"_IOBase", IOBaseType},
		{"_RawIOBase", RawIOBaseType},
		{"_BufferedIOBase", BufferedIOBaseType},
		{"_TextIOBase", TextIOBaseType},

		{"UnsupportedOperation", unsupportedOpType},

		// BlockingIOError: CPython's _io module re-exports this exception
		// class so io.py can import it via `from _io import BlockingIOError`.
		// CPython: Modules/_io/_iomodule.c:695 (BlockingIOError registration)
		{"BlockingIOError", errors.PyExc_BlockingIOError},

		{"DEFAULT_BUFFER_SIZE", objects.NewInt(int64(DefaultBufferSize))},

		// open: real implementation in this file.
		// open_code / text_encoding: placeholders.
		//
		// CPython: Modules/_io/_iomodule.c:218 _io_open_impl
		// CPython: Modules/_io/_iomodule.c:432 _io_open_code_impl
		// CPython: Modules/_io/_iomodule.c:475 _io_text_encoding_impl
		{"open", objects.NewBuiltinFunction("open", Open)},
		{"open_code", objects.NewBuiltinFunction("open_code", openCode)},
		{"text_encoding", objects.NewBuiltinFunction("text_encoding", textEncoding)},
	}

	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Open implements _io.open(). It validates the mode string, opens the
// raw FileIO, and wraps it in TextIOWrapper for text mode or returns the
// FileIO directly for binary mode. Mirrors the CPython _io_open_impl logic:
// create raw, optionally wrap in buffered, optionally wrap in text.
//
// CPython: Modules/_io/_iomodule.c:218 _io_open_impl
func Open(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, err := bindIOOpenArgs(args, kwargs)
	if err != nil {
		return nil, err
	}
	return ioOpen(a)
}

// ioOpenArgs holds the decoded parameters for _io.open.
//
// CPython: Modules/_io/_iomodule.c:199 _io_open_impl signature.
type ioOpenArgs struct {
	fileObj   objects.Object
	file      string
	fileFd    int
	fileIsFd  bool
	mode      string
	buffering int
	encoding  string
	errors    string
	newline   string
	closefd   bool
	opener    objects.Object
	creating  bool
	reading   bool
	writing   bool
	appending bool
	updating  bool
	text      bool
	binary    bool

	hasBuffering bool
	hasNewline   bool
}

// bindIOOpenArgs parses the argument list for _io.open.
//
// CPython: Modules/_io/_iomodule.c:218 _io_open_impl (clinic-generated binder)
func bindIOOpenArgs(args []objects.Object, kwargs map[string]objects.Object) (*ioOpenArgs, error) {
	names := []string{"file", "mode", "buffering", "encoding", "errors", "newline", "closefd", "opener"}
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
			return nil, fmt.Errorf("TypeError: open() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: open() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return nil, fmt.Errorf("TypeError: open() missing required argument: 'file'")
	}
	a := &ioOpenArgs{fileObj: bound[0]}
	var err error
	if fd, ok := bound[0].(*objects.Int); ok {
		// CPython open() accepts an existing file descriptor in addition
		// to a path; FileIO's __init__ does the actual fdopen below.
		//
		// CPython: Modules/_io/_iomodule.c:319 raw creation with fd path
		n, fits := fd.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: open() file descriptor too large")
		}
		a.fileIsFd = true
		a.fileFd = int(n)
	} else if a.file, err = ioFileArg(bound[0]); err != nil {
		return nil, err
	}
	if a.mode, err = ioModeArg(bound[1]); err != nil {
		return nil, err
	}
	if a.encoding, err = ioOptStrArg(bound[3], "encoding"); err != nil {
		return nil, err
	}
	if a.errors, err = ioOptStrArg(bound[4], "errors"); err != nil {
		return nil, err
	}
	if bound[5] != nil && !objects.IsNone(bound[5]) {
		a.hasNewline = true
	}
	if a.newline, err = ioOptStrArg(bound[5], "newline"); err != nil {
		return nil, err
	}
	a.buffering = -1
	a.closefd = true
	if bound[2] != nil && !objects.IsNone(bound[2]) {
		n, ok := bound[2].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: buffering must be an int")
		}
		v, _ := n.Int64()
		a.buffering = int(v)
		a.hasBuffering = true
	}
	if bound[6] != nil && !objects.IsNone(bound[6]) {
		a.closefd = objects.IsTrue(bound[6])
	}
	if bound[7] != nil && !objects.IsNone(bound[7]) && objects.Callable(bound[7]) {
		a.opener = bound[7]
	}
	if err := ioParseMode(a); err != nil {
		return nil, err
	}
	return a, nil
}

// stripTextBit removes 't' from the mode string before passing it to
// the raw FileIO constructor, which only understands the binary-mode
// letters. Mirrors CPython's `raw_mode` derivation in _io_open_impl.
//
// CPython: Modules/_io/_iomodule.c:309 raw_mode buffer build
func stripTextBit(mode string) string {
	out := make([]byte, 0, len(mode))
	for i := 0; i < len(mode); i++ {
		if mode[i] == 't' {
			continue
		}
		out = append(out, mode[i])
	}
	return string(out)
}

// ioFileArg extracts the path string from the file argument.
//
// CPython: Modules/_io/_iomodule.c:214 (path_or_fd handling)
func ioFileArg(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	// os.PathLike: call __fspath__() and recurse.
	//
	// CPython: Modules/_io/fileio.c:303 PyUnicode_FSConverter calls
	// PyOS_FSPath which invokes __fspath__.
	if fspath, err := objects.GetAttr(o, objects.NewStr("__fspath__")); err == nil {
		result, err := objects.CallNoArgs(fspath)
		if err != nil {
			return "", err
		}
		return ioFileArg(result)
	}
	return "", fmt.Errorf("TypeError: invalid file: %s", o.Type().Name)
}

// ioModeArg returns the mode string, defaulting to "r".
func ioModeArg(o objects.Object) (string, error) {
	if o == nil || objects.IsNone(o) {
		return "r", nil
	}
	s, ok := o.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: invalid mode: %s", o.Type().Name)
	}
	return s.Value(), nil
}

// ioOptStrArg enforces "None or str" on optional string arguments.
func ioOptStrArg(o objects.Object, name string) (string, error) {
	if o == nil || objects.IsNone(o) {
		return "", nil
	}
	s, ok := o.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: invalid %s: %s", name, o.Type().Name)
	}
	return s.Value(), nil
}

// ioParseMode decodes the mode string into booleans on a.
//
// CPython: Modules/_io/_iomodule.c:232 (mode loop)
func ioParseMode(a *ioOpenArgs) error {
	allowed := "axrwb+t"
	seen := map[byte]bool{}
	for i := 0; i < len(a.mode); i++ {
		c := a.mode[i]
		found := false
		for j := 0; j < len(allowed); j++ {
			if allowed[j] == c {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("ValueError: invalid mode: %q", a.mode)
		}
		if seen[c] {
			return fmt.Errorf("ValueError: invalid mode: %q", a.mode)
		}
		seen[c] = true
	}
	a.creating = seen['x']
	a.reading = seen['r']
	a.writing = seen['w']
	a.appending = seen['a']
	a.updating = seen['+']
	a.text = seen['t']
	a.binary = seen['b']
	if a.text && a.binary {
		return fmt.Errorf("ValueError: can't have text and binary mode at once")
	}
	count := 0
	for _, v := range []bool{a.creating, a.reading, a.writing, a.appending} {
		if v {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("ValueError: can't have read/write/append mode at once")
	}
	if count == 0 {
		return fmt.Errorf("ValueError: must have exactly one of read/write/append mode")
	}
	if a.binary && a.encoding != "" {
		return fmt.Errorf("ValueError: binary mode doesn't take an encoding argument")
	}
	if a.binary && a.errors != "" {
		return fmt.Errorf("ValueError: binary mode doesn't take an errors argument")
	}
	if a.binary && a.newline != "" {
		return fmt.Errorf("ValueError: binary mode doesn't take a newline argument")
	}
	return nil
}

// ioOpenFlags maps the mode booleans to os.OpenFile flags.
//
// CPython: Modules/_io/_iomodule.c:319 (FileIO construction with rawmode)
func ioOpenFlags(a *ioOpenArgs) (flag int, readable, writable bool) {
	readable = a.reading || a.updating
	writable = a.writing || a.appending || a.creating || a.updating
	switch {
	case a.reading:
		flag = os.O_RDONLY
	case a.writing:
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case a.appending:
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case a.creating:
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	if a.updating {
		flag = (flag &^ (os.O_RDONLY | os.O_WRONLY)) | os.O_RDWR
	}
	return flag, readable, writable
}

// ioOpen creates the appropriate stream object for the mode, mirroring
// CPython's open() chain: raw FileIO -> Buffered{Reader,Writer,Random}
// -> optional TextIOWrapper.
//
// CPython: Modules/_io/_iomodule.c:319 raw creation through :440 wrapper
func ioOpen(a *ioOpenArgs) (objects.Object, error) {
	flag, readable, writable := ioOpenFlags(a)
	rawMode := a.mode
	var raw objects.Object
	if a.fileIsFd {
		// Construct FileIO via its type-call slot so the existing
		// integer-fd arm in fileIOCall runs (fdopen path).
		//
		// CPython: Modules/_io/_iomodule.c:329 PyObject_CallFunction(FileIO, ...)
		obj, err := fileIOCall(nil, []objects.Object{a.fileObj, objects.NewStr(stripTextBit(a.mode))}, nil)
		if err != nil {
			return nil, err
		}
		raw = obj
	} else if a.opener != nil {
		// opener(path, flags) returns a file descriptor.
		// CPython: Modules/_io/fileio.c:399 opener call path
		flagObj := objects.NewInt(int64(flag))
		nameObj := objects.NewStr(a.file)
		fdObj, callErr := objects.Call(a.opener, objects.NewTuple([]objects.Object{nameObj, flagObj}), nil)
		if callErr != nil {
			return nil, callErr
		}
		fdInt, ok := fdObj.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected integer from opener, got %s", fdObj.Type().Name)
		}
		fdVal, _ := fdInt.Int64()
		if fdVal < 0 {
			return nil, fmt.Errorf("ValueError: opener returned invalid file descriptor")
		}
		f := os.NewFile(uintptr(fdVal), a.file)
		if f == nil {
			return nil, fmt.Errorf("OSError: bad file descriptor from opener")
		}
		raw = NewFileIO(f, a.file, rawMode, readable, writable)
	} else {
		f, err := os.OpenFile(a.file, flag, 0o600)
		if err != nil {
			return nil, fmt.Errorf("OSError: %s", err.Error())
		}
		raw = NewFileIO(f, a.file, rawMode, readable, writable)
	}

	buffering := a.buffering
	lineBuffering := false
	if a.text && buffering == 1 {
		lineBuffering = true
		buffering = -1
	}
	if buffering < 0 {
		buffering = DefaultBufferSize
	}
	// CPython: Modules/_io/_iomodule.c:330 buffering=0 only valid in binary mode.
	if buffering == 0 {
		if a.binary {
			return raw, nil
		}
		return nil, fmt.Errorf("ValueError: can't have unbuffered text I/O")
	}

	// Choose buffered class by readable/writable.
	// CPython: Modules/_io/_iomodule.c:392 buffered selection.
	var buffered objects.Object
	var berr error
	switch {
	case readable && writable:
		buffered, berr = bufferedRandomCall(nil, []objects.Object{raw, objects.NewInt(int64(buffering))}, nil)
	case readable:
		buffered, berr = bufferedReaderCall(nil, []objects.Object{raw, objects.NewInt(int64(buffering))}, nil)
	case writable:
		buffered, berr = bufferedWriterCall(nil, []objects.Object{raw, objects.NewInt(int64(buffering))}, nil)
	default:
		return nil, fmt.Errorf("ValueError: unknown mode: %q", a.mode)
	}
	if berr != nil {
		return nil, berr
	}
	if a.binary {
		return buffered, nil
	}

	// Text mode: wrap in TextIOWrapper.
	// CPython: Modules/_io/_iomodule.c:427 TextIOWrapper construction.
	enc := a.encoding
	if enc == "" || enc == "locale" {
		// "locale" is CPython's sentinel for the preferred locale encoding.
		// On modern POSIX systems this is UTF-8; gopy defaults to UTF-8.
		//
		// CPython: Modules/_io/_iomodule.c:463 _io_text_encoding_impl locale branch
		enc = "utf-8"
	}
	errHandler := a.errors
	if errHandler == "" {
		errHandler = "strict"
	}
	var newlineArg objects.Object
	if a.hasNewline {
		newlineArg = objects.NewStr(a.newline)
	} else {
		newlineArg = objects.None()
	}
	wrapper, err := textIOWrapperCall(nil, []objects.Object{
		buffered,
		objects.NewStr(enc),
		objects.NewStr(errHandler),
		newlineArg,
		objects.NewBool(lineBuffering),
	}, nil)
	if err != nil {
		return nil, err
	}
	return wrapper, nil
}

// openCode implements _io.open_code(path). CPython routes through
// PyFile_OpenCodeObject which by default just calls open(path, 'rb');
// gopy follows that default.
//
// CPython: Modules/_io/_iomodule.c:513 _io_open_code_impl
func openCode(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	path := firstArg(args, kwargs, "path")
	if path == nil {
		return nil, fmt.Errorf("TypeError: open_code() missing required argument 'path'")
	}
	if _, ok := path.(*objects.Unicode); !ok {
		return nil, fmt.Errorf("TypeError: open_code() argument 'path' must be str, not %s", path.Type().Name)
	}
	return Open([]objects.Object{path, objects.NewStr("rb")}, nil)
}

// textEncoding implements _io.text_encoding(encoding, stacklevel=2). It
// returns the encoding unchanged when explicitly given. When None,
// returns "locale" so callers can pass the sentinel to open() (gopy's
// open then maps the missing encoding to utf-8).
//
// CPython: Modules/_io/_iomodule.c:476 _io_text_encoding_impl
func textEncoding(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	enc := firstArg(args, kwargs, "encoding")
	if enc == nil {
		return nil, fmt.Errorf("TypeError: text_encoding() missing required argument 'encoding'")
	}
	if objects.IsNone(enc) {
		return objects.NewStr("locale"), nil
	}
	if _, ok := enc.(*objects.Unicode); !ok {
		return nil, fmt.Errorf("TypeError: text_encoding() argument 'encoding' must be str or None, not %s", enc.Type().Name)
	}
	return enc, nil
}

// firstArg returns the first positional argument, falling back to the
// keyword of the given name.
func firstArg(args []objects.Object, kwargs map[string]objects.Object, name string) objects.Object {
	if len(args) >= 1 {
		return args[0]
	}
	if v, ok := kwargs[name]; ok {
		return v
	}
	return nil
}
