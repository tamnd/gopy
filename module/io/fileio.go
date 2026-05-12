// FileIO is the raw file I/O type. It wraps Go's os.File and provides the
// _io.FileIO interface: read, readall, readinto, write, seek, tell, truncate,
// close, and capability predicates.
//
// CPython: Modules/_io/fileio.c:65 fileio (struct definition)
// CPython: Modules/_io/fileio.c:1245 PyFileIO_Type equivalent

package io

import (
	"fmt"
	"io"
	"os"

	"github.com/tamnd/gopy/objects"
)

// FileIO wraps an *os.File and records whether the file was opened for
// reading or writing (or both in update mode).
//
// CPython: Modules/_io/fileio.c:65 fileio
type FileIO struct {
	objects.Header

	name     string
	mode     string
	closed   bool
	readable bool
	writable bool
	f        *os.File
}

// FileIOType is the type singleton for _io.FileIO.
//
// CPython: Modules/_io/fileio.c:1245 PyFileIO_Type equivalent
var FileIOType = objects.NewType("_io.FileIO", []*objects.Type{objects.ObjectType()})

func init() {
	FileIOType.Call = fileIOCall
	FileIOType.Repr = fileIORepr
	FileIOType.Str = fileIORepr
	FileIOType.Getattro = fileIOGetattr
}

// NewFileIO constructs a FileIO around an already-opened *os.File.
//
// CPython: Modules/_io/fileio.c:243 fileio_init (analog)
func NewFileIO(f *os.File, name, mode string, readable, writable bool) *FileIO {
	fi := &FileIO{
		name:     name,
		mode:     mode,
		readable: readable,
		writable: writable,
		f:        f,
	}
	fi.Init(FileIOType)
	return fi
}

// fileIOCall is the type-call slot.
// FileIO(name, mode='r', closefd=True, opener=None)
//
// CPython: Modules/_io/fileio.c:243 fileio_init
func fileIOCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	names := []string{"name", "mode", "closefd", "opener"}
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
			return nil, fmt.Errorf("TypeError: FileIO() got an unexpected keyword argument %q", k)
		}
		if bound[idx] != nil {
			return nil, fmt.Errorf("TypeError: FileIO() got multiple values for argument %q", k)
		}
		bound[idx] = v
	}
	if bound[0] == nil {
		return nil, fmt.Errorf("TypeError: FileIO() missing required argument 'name'")
	}

	name, err := fileIONameArg(bound[0])
	if err != nil {
		return nil, err
	}
	mode := "r"
	if bound[1] != nil && !objects.IsNone(bound[1]) {
		s, ok := bound[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: invalid mode: %s", bound[1].Type().Name)
		}
		mode = s.Value()
	}

	flag, readable, writable, err := fileIOModeFlags(mode)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(name, flag, 0o600)
	if err != nil {
		return nil, fmt.Errorf("OSError: %s", err.Error())
	}
	return NewFileIO(f, name, mode, readable, writable), nil
}

// fileIONameArg extracts a file-path string from the name argument.
//
// CPython: Modules/_io/fileio.c:243 fileio_init (path extraction)
func fileIONameArg(o objects.Object) (string, error) {
	switch v := o.(type) {
	case *objects.Unicode:
		return v.Value(), nil
	case *objects.Bytes:
		return string(v.Bytes()), nil
	}
	return "", fmt.Errorf("TypeError: invalid file: %s", o.Type().Name)
}

// fileIOModeFlags maps the mode string to os flags plus capability bits.
//
// CPython: Modules/_io/fileio.c:243 fileio_init (mode parsing block)
func fileIOModeFlags(mode string) (flag int, readable, writable bool, err error) {
	var creating, reading, writing, appending, updating bool
	seen := map[byte]bool{}
	for i := 0; i < len(mode); i++ {
		c := mode[i]
		switch c {
		case 'x':
			creating = true
		case 'r':
			reading = true
		case 'w':
			writing = true
		case 'a':
			appending = true
		case '+':
			updating = true
		case 'b':
			// binary indicator, ignored at raw layer
		default:
			return 0, false, false, fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		if seen[c] {
			return 0, false, false, fmt.Errorf("ValueError: invalid mode: %q", mode)
		}
		seen[c] = true
	}
	count := 0
	for _, v := range []bool{creating, reading, writing, appending} {
		if v {
			count++
		}
	}
	if count > 1 {
		return 0, false, false, fmt.Errorf("ValueError: must have exactly one of create/read/write/append mode")
	}
	if count == 0 {
		return 0, false, false, fmt.Errorf("ValueError: must have exactly one of create/read/write/append mode")
	}
	readable = reading || updating
	writable = writing || appending || creating || updating
	switch {
	case reading:
		flag = os.O_RDONLY
	case writing:
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case appending:
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case creating:
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	if updating {
		flag = (flag &^ (os.O_RDONLY | os.O_WRONLY)) | os.O_RDWR
	}
	return flag, readable, writable, nil
}

func fileIORepr(o objects.Object) (string, error) {
	fi := o.(*FileIO)
	state := "open"
	if fi.closed {
		state = "closed"
	}
	return fmt.Sprintf("<%s name=%q mode=%q %s>", FileIOType.Name, fi.name, fi.mode, state), nil
}

// checkUsable returns an error if the FileIO is closed or the
// requested capability is missing.
//
// CPython: Modules/_io/fileio.c:93 _PyFileIO_closed
func (fi *FileIO) checkUsable() error {
	if fi.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// Read reads up to size bytes from the file. size<0 reads until EOF.
//
// CPython: Modules/_io/fileio.c:706 fileio_read
func (fi *FileIO) Read(size int) ([]byte, error) {
	if err := fi.checkUsable(); err != nil {
		return nil, err
	}
	if !fi.readable {
		return nil, fmt.Errorf("io.UnsupportedOperation: not readable")
	}
	if size == 0 {
		return nil, nil
	}
	if size < 0 {
		data, err := io.ReadAll(fi.f)
		if err != nil {
			return nil, fmt.Errorf("OSError: %s", err.Error())
		}
		return data, nil
	}
	buf := make([]byte, size)
	n, err := fi.f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("OSError: %s", err.Error())
	}
	return buf[:n], nil
}

// Write writes data to the file.
//
// CPython: Modules/_io/fileio.c:887 fileio_write
func (fi *FileIO) Write(data []byte) (int, error) {
	if err := fi.checkUsable(); err != nil {
		return 0, err
	}
	if !fi.writable {
		return 0, fmt.Errorf("io.UnsupportedOperation: not writable")
	}
	n, err := fi.f.Write(data)
	if err != nil {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return n, nil
}

// Seek repositions the file offset.
//
// CPython: Modules/_io/fileio.c:950 fileio_seek
func (fi *FileIO) Seek(pos int64, whence int) (int64, error) {
	if err := fi.checkUsable(); err != nil {
		return 0, err
	}
	newPos, err := fi.f.Seek(pos, whence)
	if err != nil {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return newPos, nil
}

// Tell returns the current file position.
//
// CPython: Modules/_io/fileio.c:1001 fileio_tell
func (fi *FileIO) Tell() (int64, error) {
	return fi.Seek(0, io.SeekCurrent)
}

// Truncate truncates the file to at most size bytes.
//
// CPython: Modules/_io/fileio.c:1023 fileio_truncate
func (fi *FileIO) Truncate(size int64) (int64, error) {
	if err := fi.checkUsable(); err != nil {
		return 0, err
	}
	if err := fi.f.Truncate(size); err != nil {
		return 0, fmt.Errorf("OSError: %s", err.Error())
	}
	return size, nil
}

// Close releases the file descriptor.
//
// CPython: Modules/_io/fileio.c:432 fileio_close
func (fi *FileIO) Close() error {
	if fi.closed {
		return nil
	}
	fi.closed = true
	if fi.f != nil {
		if err := fi.f.Close(); err != nil {
			return fmt.Errorf("OSError: %s", err.Error())
		}
	}
	return nil
}

// fileIOGetattr exposes attributes and methods.
//
// CPython: Modules/_io/fileio.c fileio_getset + fileio_methods
func fileIOGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	fi := o.(*FileIO)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "name":
		return objects.NewStr(fi.name), nil
	case "mode":
		return objects.NewStr(fi.mode), nil
	case "closed":
		return objects.NewBool(fi.closed), nil
	case "closefd":
		return objects.NewBool(true), nil
	}
	if fn := fileIOMethod(fi, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.FileIO' object has no attribute '%s'", n.Value())
}

// fileIOMethod maps method names to BuiltinFunctions.
//
// CPython: Modules/_io/fileio.c fileio_methods
func fileIOMethod(fi *FileIO, name string) objects.Object {
	switch name {
	case "read", "readall":
		return objects.NewBuiltinFunction(name, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			size := -1
			if name == "read" {
				var err error
				size, err = optionalSize(args, "read")
				if err != nil {
					return nil, err
				}
			}
			data, err := fi.Read(size)
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(data), nil
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
			}
			var data []byte
			switch v := args[0].(type) {
			case *objects.Bytes:
				data = v.Bytes()
			case *objects.ByteArray:
				data = v.Bytes()
			default:
				return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", args[0].Type().Name)
			}
			n, err := fi.Write(data)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(n)), nil
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
			out, err := fi.Seek(int64(pos), whence)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			pos, err := fi.Tell()
			if err != nil {
				return nil, err
			}
			return objects.NewInt(pos), nil
		})
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkUsable(); err != nil {
				return nil, err
			}
			cur, err := fi.Tell()
			if err != nil {
				return nil, err
			}
			size := cur
			if len(args) >= 1 && !objects.IsNone(args[0]) {
				v, err := intArg(args[0], "truncate")
				if err != nil {
					return nil, err
				}
				size = int64(v)
			}
			out, err := fi.Truncate(size)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(out), nil
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkUsable(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(fi.readable && !fi.closed), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(fi.writable && !fi.closed), nil
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(!fi.closed), nil
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewBool(false), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.checkUsable(); err != nil {
				return nil, err
			}
			return fi, nil
		})
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := fi.Close(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	}
	return nil
}
