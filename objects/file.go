// File is the gopy stand-in for the io module's IOBase / FileIO /
// BufferedReader / BufferedWriter / TextIOWrapper layer cake. The full
// CPython tree splits raw, buffered, and text I/O across distinct types
// (Modules/_io/fileio.c, bufferedio.c, textio.c) wired together by
// io.open. v0.10.1 collapses them into one Python-visible type backed
// by *os.File: read modes use a bufio.Reader, write/append modes use a
// bufio.Writer, text vs. binary is a flag that decides whether read /
// write trade in str or bytes. The split into RawIOBase / Buffered /
// Text remains a follow-up and matches the structure 1689 will land in
// full.
//
// CPython: Modules/_io/fileio.c:35 fileio (_io.FileIO)
// CPython: Modules/_io/bufferedio.c:1929 bufferedreader (_io.BufferedReader)
// CPython: Modules/_io/bufferedio.c:2196 bufferedwriter (_io.BufferedWriter)
// CPython: Modules/_io/textio.c:2261 textiowrapper (_io.TextIOWrapper)

package objects

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// File mirrors the union of FileIO + the buffer + TextIOWrapper. The
// read/write side is decided at open time and does not change; mixing
// '+' modes wires both rd and wr at construction.
type File struct {
	Header

	name   string
	mode   string
	binary bool
	closed bool

	f  *os.File
	rd *bufio.Reader
	wr *bufio.Writer
}

// FileType is the type singleton for File. CPython exposes three or
// four sibling types depending on mode; gopy's collapsed layer surfaces
// as the single name "_io.File".
//
// CPython: Modules/_io/fileio.c:1245 PyFileIO_Type and friends
var FileType = NewType("_io.File", []*Type{objectType})

func init() {
	FileType.Repr = fileRepr
	FileType.Str = fileRepr
	FileType.Iter = fileIter
	FileType.IterNext = fileIterNext
	FileType.Getattro = fileGetattr
	FileType.Setattro = fileSetattr
}

// NewFile constructs a File around an already-opened *os.File. The
// (name, mode, binary) trio describes how the file was opened so the
// Python-visible attributes match. readable and writable wire up the
// buffered reader / writer; an updating mode passes both true.
//
// CPython: Modules/_io/fileio.c:243 fileio_init (analog)
func NewFile(f *os.File, name, mode string, binary, readable, writable bool) *File {
	fi := &File{
		name:   name,
		mode:   mode,
		binary: binary,
		f:      f,
	}
	if readable {
		fi.rd = bufio.NewReader(f)
	}
	if writable {
		fi.wr = bufio.NewWriter(f)
	}
	fi.init(FileType)
	return fi
}

func fileRepr(o Object) (string, error) {
	fi := o.(*File)
	state := "open"
	if fi.closed {
		state = "closed"
	}
	return fmt.Sprintf("<%s file %q mode=%q %s>", FileType.Name, fi.name, fi.mode, state), nil
}

// fileIter returns the file itself: iterating a file yields lines, and
// the iterator is the file. Mirrors IOBase.__iter__.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func fileIter(o Object) (Object, error) {
	return o, nil
}

// fileIterNext yields one line per call, raising StopIteration at EOF.
// Mirrors IOBase.__next__'s readline-then-empty-check.
//
// CPython: Modules/_io/iobase.c:740 iobase_iternext
func fileIterNext(o Object) (Object, error) {
	fi := o.(*File)
	line, err := fi.readline(-1)
	if err != nil {
		return nil, err
	}
	if isEmptyLine(line, fi.binary) {
		return nil, ErrStopIteration
	}
	return line, nil
}

func isEmptyLine(o Object, binary bool) bool {
	if binary {
		return o.(*Bytes).Len() == 0
	}
	return o.(*Unicode).Value() == ""
}

// Read returns up to size bytes / characters; size<0 reads until EOF.
// Binary mode returns Bytes, text mode returns Unicode (decoded as
// UTF-8).
//
// CPython: Modules/_io/fileio.c:706 fileio_read (raw layer)
// CPython: Modules/_io/textio.c:1804 textiowrapper_read (text layer)
func (fi *File) Read(size int) (Object, error) {
	if err := fi.requireRead(); err != nil {
		return nil, err
	}
	if size == 0 {
		return fi.emptyResult(), nil
	}
	var buf []byte
	var err error
	if size < 0 {
		buf, err = io.ReadAll(fi.rd)
	} else {
		buf = make([]byte, size)
		n, rerr := io.ReadFull(fi.rd, buf)
		buf = buf[:n]
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			rerr = nil
		}
		err = rerr
	}
	if err != nil {
		return nil, ioErr(err)
	}
	return fi.wrapBytes(buf), nil
}

// Write pushes the source bytes / characters into the buffer. Returns
// the number of bytes written from the underlying buffer's perspective
// (matches CPython's _io._BufferedIOBase.write contract).
//
// CPython: Modules/_io/fileio.c:887 fileio_write (raw layer)
// CPython: Modules/_io/textio.c:1499 textiowrapper_write (text layer)
func (fi *File) Write(o Object) (int, error) {
	if err := fi.requireWrite(); err != nil {
		return 0, err
	}
	data, err := writeBytes(o, fi.binary)
	if err != nil {
		return 0, err
	}
	n, werr := fi.wr.Write(data)
	if werr != nil {
		return 0, ioErr(werr)
	}
	return n, nil
}

// writeBytes coerces the argument to the byte payload that gets pushed
// to the buffer. Binary mode requires bytes / bytearray; text mode
// requires str. Mirrors the type checks in textiowrapper_write and
// fileio_write.
func writeBytes(o Object, binary bool) ([]byte, error) {
	if binary {
		switch v := o.(type) {
		case *Bytes:
			return append([]byte(nil), v.Bytes()...), nil
		case *ByteArray:
			return append([]byte(nil), v.Bytes()...), nil
		}
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", o.Type().Name)
	}
	s, ok := o.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: write() argument must be str, not %s", o.Type().Name)
	}
	return []byte(s.Value()), nil
}

// Readline returns one line including the terminating '\n'. size>=0
// caps the read length. Returns an empty string / bytes at EOF.
//
// CPython: Modules/_io/iobase.c:830 iobase_readline (driver)
func (fi *File) Readline(size int) (Object, error) {
	if err := fi.requireRead(); err != nil {
		return nil, err
	}
	return fi.readline(size)
}

func (fi *File) readline(size int) (Object, error) {
	var buf []byte
	for size < 0 || len(buf) < size {
		b, err := fi.rd.ReadByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ioErr(err)
		}
		buf = append(buf, b)
		if b == '\n' {
			break
		}
	}
	return fi.wrapBytes(buf), nil
}

// Close flushes the writer (if any) and releases the underlying file
// descriptor. Calling Close on an already-closed File is a no-op.
//
// CPython: Modules/_io/fileio.c:432 fileio_close
func (fi *File) Close() error {
	if fi.closed {
		return nil
	}
	fi.closed = true
	var firstErr error
	if fi.wr != nil {
		if err := fi.wr.Flush(); err != nil {
			firstErr = ioErr(err)
		}
	}
	if fi.f != nil {
		if err := fi.f.Close(); err != nil && firstErr == nil {
			firstErr = ioErr(err)
		}
	}
	return firstErr
}

// Flush pushes any buffered writes to the underlying file. Read-only
// files accept the call as a no-op (matches IOBase.flush).
//
// CPython: Modules/_io/bufferedio.c:519 bufferedwriter_flush_unlocked
func (fi *File) Flush() error {
	if fi.closed {
		return errClosed()
	}
	if fi.wr != nil {
		if err := fi.wr.Flush(); err != nil {
			return ioErr(err)
		}
	}
	return nil
}

func (fi *File) requireRead() error {
	if fi.closed {
		return errClosed()
	}
	if fi.rd == nil {
		return errors.New("io.UnsupportedOperation: not readable")
	}
	return nil
}

func (fi *File) requireWrite() error {
	if fi.closed {
		return errClosed()
	}
	if fi.wr == nil {
		return errors.New("io.UnsupportedOperation: not writable")
	}
	return nil
}

func (fi *File) emptyResult() Object {
	if fi.binary {
		return EmptyBytes()
	}
	return NewStr("")
}

func (fi *File) wrapBytes(buf []byte) Object {
	if fi.binary {
		return NewBytes(buf)
	}
	return NewStr(string(buf))
}

// errClosed mirrors the ValueError IOBase raises for any I/O attempted
// on a closed file.
//
// CPython: Modules/_io/iobase.c:339 iobase_check_closed
func errClosed() error {
	return errors.New("ValueError: I/O operation on closed file")
}

// ioErr maps a Go I/O error to a Python-style OSError. The minimal
// port keeps the original message; once OSError carries errno + strerror
// the wrapper extracts them properly.
func ioErr(err error) error {
	return fmt.Errorf("OSError: %s", err.Error())
}

// fileGetattr exposes name / mode / closed as plain attributes and
// every method as a bound built-in function. This sidesteps the
// type-method registration plumbing pending the full _io port.
//
// CPython: Modules/_io/iobase.c (iobase + per-class slot tables) +
// PyObject_GenericGetAttr resolution
func fileGetattr(o Object, name Object) (Object, error) {
	fi := o.(*File)
	n, ok := name.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.v {
	case "name":
		return NewStr(fi.name), nil
	case "mode":
		return NewStr(fi.mode), nil
	case "closed":
		return NewBool(fi.closed), nil
	}
	if fn := fileMethod(fi, n.v); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", FileType.Name, n.v)
}

// fileSetattr supports a single mutable attribute today: the mode
// string, which TextIOWrapper writes to in _pyio.open after wrapping.
// Anything else raises AttributeError to match read-only-by-default.
//
// CPython: Modules/_io/textio.c (textiowrapper_setmode) + the implicit
// __dict__-less fall-through.
func fileSetattr(o Object, name Object, value Object) error {
	fi := o.(*File)
	n, ok := name.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: attribute name must be string")
	}
	if n.v == "mode" {
		v, ok := value.(*Unicode)
		if !ok {
			return fmt.Errorf("TypeError: mode must be str, not %s", value.Type().Name)
		}
		fi.mode = v.Value()
		return nil
	}
	return fmt.Errorf("AttributeError: '%s' object attribute '%s' is read-only", FileType.Name, n.v)
}

// fileMethod maps a name to the BuiltinFunction backing the matching
// File method. nil is returned for an unknown name so fileGetattr can
// raise AttributeError with the right message.
func fileMethod(fi *File, name string) Object {
	switch name {
	case "read":
		return NewBuiltinFunction("read", func(args []Object, _ map[string]Object) (Object, error) {
			return fileReadCall(fi, args)
		})
	case "readline":
		return NewBuiltinFunction("readline", func(args []Object, _ map[string]Object) (Object, error) {
			return fileReadlineCall(fi, args)
		})
	case "write":
		return NewBuiltinFunction("write", func(args []Object, _ map[string]Object) (Object, error) {
			return fileWriteCall(fi, args)
		})
	case "close":
		return NewBuiltinFunction("close", func(_ []Object, _ map[string]Object) (Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return None(), nil
		})
	case "flush":
		return NewBuiltinFunction("flush", func(_ []Object, _ map[string]Object) (Object, error) {
			if err := fi.Flush(); err != nil {
				return nil, err
			}
			return None(), nil
		})
	case "__enter__":
		return NewBuiltinFunction("__enter__", func(_ []Object, _ map[string]Object) (Object, error) {
			if fi.closed {
				return nil, errClosed()
			}
			return fi, nil
		})
	case "__exit__":
		return NewBuiltinFunction("__exit__", func(_ []Object, _ map[string]Object) (Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return None(), nil
		})
	case "readable":
		return NewBuiltinFunction("readable", func(_ []Object, _ map[string]Object) (Object, error) {
			return NewBool(fi.rd != nil && !fi.closed), nil
		})
	case "writable":
		return NewBuiltinFunction("writable", func(_ []Object, _ map[string]Object) (Object, error) {
			return NewBool(fi.wr != nil && !fi.closed), nil
		})
	}
	return nil
}

// fileReadCall is the size-arg unpacker for File.read. read(-1) and
// read() share semantics; any value beyond an int is a TypeError.
func fileReadCall(fi *File, args []Object) (Object, error) {
	size, err := optionalSize(args, "read")
	if err != nil {
		return nil, err
	}
	return fi.Read(size)
}

func fileReadlineCall(fi *File, args []Object) (Object, error) {
	size, err := optionalSize(args, "readline")
	if err != nil {
		return nil, err
	}
	return fi.Readline(size)
}

func fileWriteCall(fi *File, args []Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: write() takes exactly one argument (%d given)", len(args))
	}
	n, err := fi.Write(args[0])
	if err != nil {
		return nil, err
	}
	return NewInt(int64(n)), nil
}

// optionalSize pulls a size int off the args slice. Missing or None
// means -1 (read everything). Mirrors the clinic spec for io.read.
func optionalSize(args []Object, fnName string) (int, error) {
	switch len(args) {
	case 0:
		return -1, nil
	case 1:
		if IsNone(args[0]) {
			return -1, nil
		}
		i, ok := args[0].(*Int)
		if !ok {
			return 0, fmt.Errorf("TypeError: %s() argument must be int, not %s", fnName, args[0].Type().Name)
		}
		v, ok := i.Int64()
		if !ok {
			return 0, fmt.Errorf("OverflowError: %s() argument too large", fnName)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("TypeError: %s() takes at most 1 argument (%d given)", fnName, len(args))
	}
}
