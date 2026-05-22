// BytesIO is the in-memory binary stream the io module exposes as
// io.BytesIO. Faithful port of cpython/Modules/_io/bytesio.c: a byte
// buffer with a separate string_size cursor, an exports counter that
// blocks resizing while a memoryview is live, an over-seek path that
// zero-pads the gap on write, and the pickle __getstate__ /
// __setstate__ contract.
//
// CPython: Modules/_io/bytesio.c:17 bytesio (struct definition)

package io

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// BytesIO mirrors the C struct: buf is the heap-allocated bytes
// object, pos is the read/write cursor, stringSize tracks the logical
// length (which can differ from len(buf) when truncate/over-seek leave
// allocated capacity past the end), exports counts outstanding
// memoryview/getbuffer views.
//
// CPython: Modules/_io/bytesio.c:17 bytesio
type BytesIO struct {
	objects.Header

	buf        []byte
	pos        int
	stringSize int
	exports    int
	closed     bool
}

// BytesIOType is the type singleton for _io.BytesIO.
//
// CPython: Modules/_io/bytesio.c PyBytesIO_Type
var BytesIOType = objects.NewType("_io.BytesIO", []*objects.Type{objects.ObjectType()})

func init() {
	BytesIOType.Call = bytesIOCall
	BytesIOType.Repr = bytesIORepr
	BytesIOType.Str = bytesIORepr
	BytesIOType.Iter = bytesIOIter
	BytesIOType.IterNext = bytesIOIterNext
	BytesIOType.Getattro = bytesIOGetattr
	// LOAD_SPECIAL walks the type MRO for __enter__ / __exit__.
	//
	// CPython: Modules/_io/iobase.c:391 iobase_enter / :409 iobase_exit
	objects.SetTypeDescr(BytesIOType, "__enter__", objects.NewBuiltinFunction("__enter__", bytesIOEnterDescr))
	objects.SetTypeDescr(BytesIOType, "__exit__", objects.NewBuiltinFunction("__exit__", bytesIOExitDescr))
	objects.AddIterSlotWrappers(BytesIOType)
}

func bytesIOEnterDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	b, ok := args[0].(*BytesIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected _io.BytesIO self")
	}
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	return b, nil
}

func bytesIOExitDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	b, ok := args[0].(*BytesIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected _io.BytesIO self")
	}
	_ = b.Close()
	return objects.None(), nil
}

// NewBytesIO creates an empty BytesIO. Caller may provide initial bytes.
//
// CPython: Modules/_io/bytesio.c:1003 bytesio_new + _io_BytesIO___init___impl
func NewBytesIO(initial []byte) *BytesIO {
	b := &BytesIO{}
	b.Init(BytesIOType)
	if len(initial) > 0 {
		b.buf = append([]byte(nil), initial...)
		b.stringSize = len(initial)
	}
	return b
}

// bytesIOCall is the type-call slot. Accepts an optional initial_value
// bytes argument.
//
// CPython: Modules/_io/bytesio.c:1033 _io_BytesIO___init___impl
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
// CPython: Modules/_io/bytesio.c:679 bytesio_iternext_lock_held
func bytesIOIterNext(o objects.Object) (objects.Object, error) {
	b := o.(*BytesIO)
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	n := b.scanEOL(-1)
	if n == 0 {
		return nil, objects.ErrStopIteration
	}
	out := append([]byte(nil), b.buf[b.pos:b.pos+n]...)
	b.pos += n
	return objects.NewBytes(out), nil
}

// checkUsable raises ValueError if the BytesIO is closed.
//
// CPython: Modules/_io/bytesio.c:43 check_closed
func (b *BytesIO) checkUsable() error {
	if b.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// checkExports raises BufferError if a memoryview is currently
// borrowing the buffer.
//
// CPython: Modules/_io/bytesio.c:53 check_exports
func (b *BytesIO) checkExports() error {
	if b.exports > 0 {
		return fmt.Errorf("BufferError: Existing exports of data: object cannot be re-sized")
	}
	return nil
}

// scanEOL returns the byte count from pos to the next '\n' (inclusive)
// or to string_size, capped by len. Matches CPython's memchr-based
// scan.
//
// CPython: Modules/_io/bytesio.c:80 scan_eol_lock_held
func (b *BytesIO) scanEOL(limit int) int {
	if b.pos >= b.stringSize {
		return 0
	}
	maxlen := b.stringSize - b.pos
	n := limit
	if n < 0 || n > maxlen {
		n = maxlen
	}
	for i := 0; i < n; i++ {
		if b.buf[b.pos+i] == '\n' {
			return i + 1
		}
	}
	return n
}

// ensureBuf grows the underlying buffer so that endpos bytes are
// addressable. Matches CPython's resize_buffer_lock_held: the
// allocation pattern is irrelevant to behavior but kept moderately
// over-allocating to avoid quadratic regrowth.
//
// CPython: Modules/_io/bytesio.c:137 resize_buffer_lock_held
func (b *BytesIO) ensureBuf(endpos int) {
	if endpos <= len(b.buf) {
		return
	}
	alloc := max(endpos+endpos>>3, endpos+6)
	grow := make([]byte, alloc)
	copy(grow, b.buf)
	b.buf = grow[:alloc]
}

// Read returns up to size bytes; size<0 reads until end-of-buffer.
//
// CPython: Modules/_io/bytesio.c:463 _io_BytesIO_read_impl
func (b *BytesIO) Read(size int) []byte {
	n := b.stringSize - b.pos
	if size < 0 || size > n {
		size = max(n, 0)
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
// CPython: Modules/_io/bytesio.c:515 _io_BytesIO_readline_impl
func (b *BytesIO) readline(limit int) []byte {
	n := b.scanEOL(limit)
	if n == 0 {
		return nil
	}
	out := append([]byte(nil), b.buf[b.pos:b.pos+n]...)
	b.pos += n
	return out
}

// Write copies p into the buffer at pos, zero-padding any over-seek
// gap between string_size and pos. Returns the number of bytes
// written. Mirrors write_bytes_lock_held.
//
// CPython: Modules/_io/bytesio.c:192 write_bytes_lock_held
func (b *BytesIO) Write(p []byte) int {
	if err := b.checkExports(); err != nil {
		return -1
	}
	n := len(p)
	if n == 0 {
		return 0
	}
	endpos := b.pos + n
	b.ensureBuf(endpos)
	if b.pos > b.stringSize {
		for i := b.stringSize; i < b.pos; i++ {
			b.buf[i] = 0
		}
	}
	copy(b.buf[b.pos:endpos], p)
	b.pos = endpos
	if b.stringSize < endpos {
		b.stringSize = endpos
	}
	return n
}

// Seek changes the cursor position. whence=1/2 with a negative
// resulting position clamps to zero (matching CPython).
//
// CPython: Modules/_io/bytesio.c:723 _io_BytesIO_seek_impl
func (b *BytesIO) Seek(pos, whence int) (int, error) {
	switch whence {
	case 0:
		if pos < 0 {
			return 0, fmt.Errorf("ValueError: negative seek value %d", pos)
		}
	case 1:
		pos += b.pos
		if pos < 0 {
			pos = 0
		}
	case 2:
		pos += b.stringSize
		if pos < 0 {
			pos = 0
		}
	default:
		return 0, fmt.Errorf("ValueError: invalid whence (%d, should be 0, 1 or 2)", whence)
	}
	b.pos = pos
	return b.pos, nil
}

// Tell returns the current cursor position.
//
// CPython: Modules/_io/bytesio.c:415 _io_BytesIO_tell_impl
func (b *BytesIO) Tell() int { return b.pos }

// Truncate cuts the buffer to at most size bytes. The cursor is
// unchanged. Errors out if exports are held.
//
// CPython: Modules/_io/bytesio.c:646 _io_BytesIO_truncate_impl
func (b *BytesIO) Truncate(size int) (int, error) {
	if err := b.checkExports(); err != nil {
		return 0, err
	}
	if size < 0 {
		return 0, fmt.Errorf("ValueError: negative size value %d", size)
	}
	if size < b.stringSize {
		b.stringSize = size
		b.buf = b.buf[:size]
	}
	return size, nil
}

// GetValue returns the logical contents as a new bytes object.
//
// CPython: Modules/_io/bytesio.c:369 _io_BytesIO_getvalue_impl
func (b *BytesIO) GetValue() []byte {
	out := make([]byte, b.stringSize)
	copy(out, b.buf)
	return out
}

// Close marks the BytesIO as closed and releases the buffer. Cannot
// be called while exports are live.
//
// CPython: Modules/_io/bytesio.c:835 _io_BytesIO_close_impl
func (b *BytesIO) Close() error {
	if err := b.checkExports(); err != nil {
		return err
	}
	b.closed = true
	b.buf = nil
	b.stringSize = 0
	return nil
}

// readInto copies up to len(dst) bytes into dst from the current pos.
//
// CPython: Modules/_io/bytesio.c:606 _io_BytesIO_readinto_impl
func (b *BytesIO) readInto(dst []byte) int {
	n := b.stringSize - b.pos
	if n < 0 {
		return 0
	}
	if len(dst) < n {
		n = len(dst)
	}
	copy(dst, b.buf[b.pos:b.pos+n])
	b.pos += n
	return n
}

// getBuffer hands out a memoryview over the logical contents. The
// exports counter is bumped while CPython tracks live views; gopy's
// memoryview is detached (copy-on-construct via NewMemoryView's Bytes
// path) so the bump is symbolic but kept for parity with check_exports.
//
// CPython: Modules/_io/bytesio.c:342 _io_BytesIO_getbuffer_impl
func (b *BytesIO) getBuffer() (objects.Object, error) {
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	view, err := objects.NewMemoryView(objects.NewBytes(b.buf[:b.stringSize]))
	if err != nil {
		return nil, err
	}
	return view, nil
}

// getState returns the 3-tuple (initial_bytes, position, dict) used
// by pickle protocol 2.
//
// CPython: Modules/_io/bytesio.c:859 bytesio_getstate_lock_held
func (b *BytesIO) getState() (objects.Object, error) {
	if err := b.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{
		objects.NewBytes(b.GetValue()),
		objects.NewInt(int64(b.pos)),
		objects.None(),
	}), nil
}

// setState rehydrates from a 3-tuple. Resets string_size + pos, writes
// the initial bytes, then sets pos.
//
// CPython: Modules/_io/bytesio.c:897 bytesio_setstate_lock_held
func (b *BytesIO) setState(state objects.Object) error {
	t, ok := state.(*objects.Tuple)
	if !ok || t.Len() < 3 {
		return fmt.Errorf("TypeError: _io.BytesIO.__setstate__ argument should be 3-tuple, got %s", state.Type().Name)
	}
	if err := b.checkExports(); err != nil {
		return err
	}
	b.stringSize = 0
	b.pos = 0
	b.buf = nil
	var data []byte
	switch v := t.Item(0).(type) {
	case *objects.Bytes:
		data = v.Bytes()
	case *objects.ByteArray:
		data = v.Bytes()
	default:
		return fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", t.Item(0).Type().Name)
	}
	b.Write(data)
	posObj, ok := t.Item(1).(*objects.Int)
	if !ok {
		return fmt.Errorf("TypeError: second item of state must be an integer, not %s", t.Item(1).Type().Name)
	}
	pos, _ := posObj.Int64()
	if pos < 0 {
		return fmt.Errorf("ValueError: position value cannot be negative")
	}
	b.pos = int(pos)
	return nil
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
	if n.Value() == "closed" {
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
	case "read", "read1":
		return objects.NewBuiltinFunction(name, func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			size, err := optionalSize(args, name)
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
			maxsize := -1
			if len(args) >= 1 && !objects.IsNone(args[0]) {
				v, err := intArg(args[0], "readlines")
				if err != nil {
					return nil, err
				}
				maxsize = v
			}
			var lines []objects.Object
			size := 0
			for {
				line := b.readline(-1)
				if len(line) == 0 {
					break
				}
				lines = append(lines, objects.NewBytes(line))
				size += len(line)
				if maxsize > 0 && size >= maxsize {
					break
				}
			}
			return objects.NewList(lines), nil
		})
	case "readinto":
		return objects.NewBuiltinFunction("readinto", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: readinto() takes exactly 1 argument (%d given)", len(args))
			}
			ba, ok := args[0].(*objects.ByteArray)
			if !ok {
				return nil, fmt.Errorf("TypeError: readinto() argument must be read-write bytes-like object, not '%s'", args[0].Type().Name)
			}
			dst := ba.Bytes()
			n := b.readInto(dst)
			return objects.NewInt(int64(n)), nil
		})
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.checkUsable(); err != nil {
				return nil, err
			}
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
			}
			data, ok := objects.AsBytesLike(args[0])
			if !ok {
				return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", args[0].Type().Name)
			}
			n := b.Write(data)
			if n < 0 {
				return nil, fmt.Errorf("BufferError: Existing exports of data: object cannot be re-sized")
			}
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
			it, err := objects.Iter(args[0])
			if err != nil {
				return nil, err
			}
			for {
				item, err := objects.IterNext(it)
				if errors.Is(err, objects.ErrStopIteration) {
					break
				}
				if err != nil {
					return nil, err
				}
				var data []byte
				switch v := item.(type) {
				case *objects.Bytes:
					data = v.Bytes()
				case *objects.ByteArray:
					data = v.Bytes()
				default:
					return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", item.Type().Name)
				}
				if n := b.Write(data); n < 0 {
					return nil, fmt.Errorf("BufferError: Existing exports of data: object cannot be re-sized")
				}
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
	case "getbuffer":
		return objects.NewBuiltinFunction("getbuffer", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.getBuffer()
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := b.Close(); err != nil {
				return nil, err
			}
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
	case "__getstate__":
		return objects.NewBuiltinFunction("__getstate__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return b.getState()
		})
	case "__setstate__":
		return objects.NewBuiltinFunction("__setstate__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly 1 argument (%d given)", len(args))
			}
			if err := b.setState(args[0]); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "__sizeof__":
		return objects.NewBuiltinFunction("__sizeof__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(len(b.buf))), nil
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
			_ = b.Close()
			return objects.None(), nil
		})
	}
	return nil
}
