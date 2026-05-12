// BytesIO is the in-memory binary stream the io module exposes as
// io.BytesIO. Mirrors the C type defined in
// cpython/Modules/_io/bytesio.c: a []byte buffer plus a cursor that
// read / write / seek / truncate / getvalue all index into.
//
// CPython: Modules/_io/bytesio.c:17 bytesio (struct definition)
// CPython: Modules/_io/bytesio.c (type slots)

package io

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// BytesIO carries the byte buffer, the read/write cursor (in bytes),
// and the closed flag.
//
// CPython: Modules/_io/bytesio.c:17 bytesio
type BytesIO struct {
	objects.Header

	buf    []byte
	pos    int
	closed bool
}

// BytesIOType is the type singleton for _io.BytesIO.
//
// CPython: Modules/_io/bytesio.c (PyBytesIO_Type equivalent)
var BytesIOType = objects.NewType("_io.BytesIO", []*objects.Type{objects.ObjectType()})

func init() {
	BytesIOType.Call = bytesIOCall
	BytesIOType.Repr = bytesIORepr
	BytesIOType.Str = bytesIORepr
	BytesIOType.Iter = bytesIOIter
	BytesIOType.IterNext = bytesIOIterNext
	BytesIOType.Getattro = bytesIOGetattr
}

// NewBytesIO creates an empty BytesIO. Caller may provide initial bytes.
//
// CPython: Modules/_io/bytesio.c bytesio_new
func NewBytesIO(initial []byte) *BytesIO {
	b := &BytesIO{}
	b.Init(BytesIOType)
	if len(initial) > 0 {
		b.buf = append([]byte(nil), initial...)
	}
	return b
}

// bytesIOCall is the type-call slot. Accepts an optional initial_value
// bytes argument.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO___init___impl
func bytesIOCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: BytesIO() takes at most 1 argument (%d given)", len(args))
	}
	var initial []byte
	src := positional(args, 0)
	for k, v := range kwargs {
		if k == "initial_bytes" {
			src = v
		} else {
			return nil, fmt.Errorf("TypeError: BytesIO() got an unexpected keyword argument %q", k)
		}
	}
	if src != nil && !objects.IsNone(src) {
		switch v := src.(type) {
		case *objects.Bytes:
			initial = append([]byte(nil), v.Bytes()...)
		case *objects.ByteArray:
			initial = append([]byte(nil), v.Bytes()...)
		default:
			return nil, fmt.Errorf("TypeError: initial_bytes must be bytes-like object, not %s", src.Type().Name)
		}
	}
	return NewBytesIO(initial), nil
}

func bytesIORepr(_ objects.Object) (string, error) {
	return "<_io.BytesIO object>", nil
}

// bytesIOIter returns the BytesIO itself.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func bytesIOIter(o objects.Object) (objects.Object, error) {
	b := o.(*BytesIO)
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	return b, nil
}

// bytesIOIterNext yields lines one at a time.
//
// CPython: Modules/_io/iobase.c:740 iobase_iternext
func bytesIOIterNext(o objects.Object) (objects.Object, error) {
	b := o.(*BytesIO)
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	line := b.readline(-1)
	if len(line) == 0 {
		return nil, objects.ErrStopIteration
	}
	return objects.NewBytes(line), nil
}

// checkUsable raises ValueError if the BytesIO is closed.
//
// CPython: Modules/_io/bytesio.c:44 check_closed
func (b *BytesIO) checkUsable() error {
	if b.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// Read returns up to size bytes; size<0 reads until end-of-buffer.
//
// CPython: Modules/_io/bytesio.c:183 _io_BytesIO_read_impl
func (b *BytesIO) Read(size int) []byte {
	remaining := len(b.buf) - b.pos
	if remaining < 0 {
		remaining = 0
	}
	if size < 0 || size > remaining {
		size = remaining
	}
	if size <= 0 {
		return nil
	}
	out := append([]byte(nil), b.buf[b.pos:b.pos+size]...)
	b.pos += size
	return out
}

// readline reads one line including the trailing '\n'.
//
// CPython: Modules/_io/bytesio.c:80 scan_eol_lock_held
func (b *BytesIO) readline(limit int) []byte {
	if b.pos >= len(b.buf) {
		return nil
	}
	end := len(b.buf)
	if limit >= 0 && b.pos+limit < end {
		end = b.pos + limit
	}
	for i := b.pos; i < end; i++ {
		if b.buf[i] == '\n' {
			out := append([]byte(nil), b.buf[b.pos:i+1]...)
			b.pos = i + 1
			return out
		}
	}
	out := append([]byte(nil), b.buf[b.pos:end]...)
	b.pos = end
	return out
}

// Write appends p to the buffer at the current cursor.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO_write_impl
func (b *BytesIO) Write(p []byte) int {
	n := len(p)
	if n == 0 {
		return 0
	}
	end := b.pos + n
	if end > len(b.buf) {
		grow := make([]byte, end)
		copy(grow, b.buf)
		b.buf = grow
	}
	copy(b.buf[b.pos:end], p)
	b.pos = end
	return n
}

// Seek changes the cursor position.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO_seek_impl
func (b *BytesIO) Seek(pos, whence int) (int, error) {
	switch whence {
	case 0:
		if pos < 0 {
			return 0, fmt.Errorf("ValueError: Negative seek position %d", pos)
		}
		b.pos = pos
	case 1:
		newPos := b.pos + pos
		if newPos < 0 {
			return 0, fmt.Errorf("ValueError: Negative seek position %d", newPos)
		}
		b.pos = newPos
	case 2:
		newPos := len(b.buf) + pos
		if newPos < 0 {
			return 0, fmt.Errorf("ValueError: Negative seek position %d", newPos)
		}
		b.pos = newPos
	default:
		return 0, fmt.Errorf("ValueError: Invalid whence (%d, should be 0, 1 or 2)", whence)
	}
	return b.pos, nil
}

// Tell returns the current cursor position.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO_tell_impl
func (b *BytesIO) Tell() int { return b.pos }

// Truncate cuts the buffer to at most size bytes.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO_truncate_impl
func (b *BytesIO) Truncate(size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("ValueError: Negative size value %d", size)
	}
	if size < len(b.buf) {
		b.buf = b.buf[:size]
	}
	if b.pos > len(b.buf) {
		b.pos = len(b.buf)
	}
	return len(b.buf), nil
}

// GetValue returns the entire buffer contents as a new bytes object.
//
// CPython: Modules/_io/bytesio.c _io_BytesIO_getvalue_impl
func (b *BytesIO) GetValue() []byte {
	return append([]byte(nil), b.buf...)
}

// Close marks the BytesIO as closed.
//
// CPython: Modules/_io/bytesio.c (close inherited from iobase)
func (b *BytesIO) Close() {
	b.closed = true
	b.buf = nil
}

// bytesIOGetattr exposes properties and methods.
//
// CPython: Modules/_io/bytesio.c bytesio_getset + bytesio_methods
func bytesIOGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	b := o.(*BytesIO)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "closed":
		return objects.NewBool(b.closed), nil
	}
	if fn := bytesIOMethod(b, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '_io.BytesIO' object has no attribute '%s'", n.Value())
}

// bytesIOMethod maps method names to BuiltinFunctions.
func bytesIOMethod(b *BytesIO, name string) objects.Object {
	switch name {
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			size, err := optionalSize(args, "read")
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(b.Read(size)), nil
		})
	case "read1":
		return objects.NewBuiltinFunction("read1", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			size, err := optionalSize(args, "read1")
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(b.Read(size)), nil
		})
	case "readline":
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			size, err := optionalSize(args, "readline")
			if err != nil {
				return nil, err
			}
			return objects.NewBytes(b.readline(size)), nil
		})
	case "readlines":
		return objects.NewBuiltinFunction("readlines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			var lines []objects.Object
			for {
				line := b.readline(-1)
				if len(line) == 0 {
					break
				}
				lines = append(lines, objects.NewBytes(line))
			}
			return objects.NewList(lines), nil
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
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
			n := b.Write(data)
			return objects.NewInt(int64(n)), nil
		})
	case "writelines":
		return objects.NewBuiltinFunction("writelines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
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
				var data []byte
				switch v := item.(type) {
				case *objects.Bytes:
					data = v.Bytes()
				case *objects.ByteArray:
					data = v.Bytes()
				default:
					return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", item.Type().Name)
				}
				b.Write(data)
			}
			return objects.None(), nil
		})
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
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
			out, err := b.Seek(pos, whence)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(out)), nil
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewInt(int64(b.Tell())), nil
		})
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			size := b.pos
			if len(args) >= 1 && !objects.IsNone(args[0]) {
				v, err := intArg(args[0], "truncate")
				if err != nil {
					return nil, err
				}
				size = v
			}
			out, err := b.Truncate(size)
			if err != nil {
				return nil, err
			}
			return objects.NewInt(int64(out)), nil
		})
	case "getvalue":
		return objects.NewBuiltinFunction("getvalue", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBytes(b.GetValue()), nil
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			b.Close()
			return objects.None(), nil
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(false), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			return b, nil
		})
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			b.Close()
			return objects.None(), nil
		})
	}
	return nil
}
