// StringIO is the in-memory text stream the io module exposes as
// io.StringIO. Mirrors the C type defined in
// cpython/Modules/_io/stringio.c: a UCS-4 buffer plus a cursor that
// read / write / seek / truncate / getvalue all index into.
//
// The CPython source carries an "accumulating vs realized" split that
// lets writes go through a PyUnicodeWriter until the first read so
// repeated str concatenation stays cheap. The Go port keeps it
// simpler: the buffer is a single []rune, every operation works on
// it directly, and getvalue copies the live slice into a new str.
// The behavior the user-visible methods publish is the same; only
// the internal optimisation differs.
//
// CPython: Modules/_io/stringio.c:20 stringio (struct definition)
// CPython: Modules/_io/stringio.c:1073 stringio_slots (type slots)

package pyio

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// StringIO carries the rune buffer, the read/write cursor (in
// characters, not bytes), and the closed flag. readNL stores the
// `newline` argument from the constructor so future read paths can
// reflect translation rules; the current port defers newline
// translation but tracks the value so the constructor type-checks
// match CPython.
//
// CPython: Modules/_io/stringio.c:20 stringio
type StringIO struct {
	objects.Header

	buf    []rune
	pos    int
	closed bool

	readNL    string
	hasReadNL bool
}

// StringIOType is the type singleton for _io.StringIO. Bases is just
// objects.ObjectType for now; the io.py shim re-registers it as a
// virtual subclass of TextIOBase once that lands.
//
// CPython: Modules/_io/stringio.c:1073 stringio_slots
var StringIOType = objects.NewType("_io.StringIO", []*objects.Type{objects.ObjectType()})

func init() {
	StringIOType.Call = stringIOCall
	StringIOType.Repr = stringIORepr
	StringIOType.Str = stringIORepr
	StringIOType.Iter = stringIOIter
	StringIOType.IterNext = stringIOIterNext
	StringIOType.Getattro = stringIOGetattr
}

// NewStringIO mirrors PyStringIO_New: an empty buffer, cursor at 0,
// not closed. Caller code populates the initial value through the
// constructor path.
//
// CPython: Modules/_io/stringio.c:637 stringio_new
func NewStringIO() *StringIO {
	s := &StringIO{}
	s.Init(StringIOType)
	return s
}

// stringIOCall is the type-call slot. Mirrors the Clinic-generated
// _io_StringIO___init___impl: parse `initial_value` and `newline`,
// reject non-str / non-None types, accept the canonical newline
// values "" / "\n" / "\r" / "\r\n" / None.
//
// CPython: Modules/_io/stringio.c:670 _io_StringIO___init___impl
func stringIOCall(_ objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var (
		initial *objects.Unicode
		nlSet   bool
		nlIsStr bool
		nlVal   string
	)
	// initial_value: str | None, default "".
	if len(args) >= 1 {
		v := args[0]
		if !objects.IsNone(v) {
			s, ok := v.(*objects.Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: initial_value must be str or None, not %s", v.Type().Name)
			}
			initial = s
		}
	}
	// newline: str | None, default "\n".
	if len(args) >= 2 {
		v := args[1]
		nlSet = true
		if objects.IsNone(v) {
			nlIsStr = false
		} else {
			s, ok := v.(*objects.Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: newline must be str or None, not %s", v.Type().Name)
			}
			nlIsStr = true
			nlVal = s.Value()
		}
	}
	for k, v := range kwargs {
		switch k {
		case "initial_value":
			if !objects.IsNone(v) {
				s, ok := v.(*objects.Unicode)
				if !ok {
					return nil, fmt.Errorf("TypeError: initial_value must be str or None, not %s", v.Type().Name)
				}
				initial = s
			}
		case "newline":
			nlSet = true
			if objects.IsNone(v) {
				nlIsStr = false
			} else {
				s, ok := v.(*objects.Unicode)
				if !ok {
					return nil, fmt.Errorf("TypeError: newline must be str or None, not %s", v.Type().Name)
				}
				nlIsStr = true
				nlVal = s.Value()
			}
		default:
			return nil, fmt.Errorf("TypeError: StringIO() got an unexpected keyword argument %q", k)
		}
	}
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: StringIO() takes at most 2 arguments (%d given)", len(args))
	}
	// Reject illegal newline values up front. CPython accepts only
	// "", "\n", "\r", "\r\n", or None.
	//
	// CPython: Modules/_io/stringio.c:696 illegal newline check
	if nlSet && nlIsStr {
		switch nlVal {
		case "", "\n", "\r", "\r\n":
		default:
			return nil, fmt.Errorf("ValueError: illegal newline value: %q", nlVal)
		}
	}
	s := NewStringIO()
	if nlSet && nlIsStr {
		s.readNL = nlVal
		s.hasReadNL = true
	} else if !nlSet {
		s.readNL = "\n"
		s.hasReadNL = true
	}
	if initial != nil {
		s.buf = []rune(initial.Value())
	}
	return s, nil
}

func stringIORepr(_ objects.Object) (string, error) {
	return "<_io.StringIO object>", nil
}

// stringIOIter returns the StringIO itself, matching IOBase.__iter__.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func stringIOIter(o objects.Object) (objects.Object, error) {
	s := o.(*StringIO)
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return s, nil
}

// stringIOIterNext yields the next line, signalling EOF with
// ErrStopIteration. Mirrors stringio_iternext.
//
// CPython: Modules/_io/stringio.c:407 stringio_iternext
func stringIOIterNext(o objects.Object) (objects.Object, error) {
	s := o.(*StringIO)
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	line := s.readline(-1)
	if line == "" {
		return nil, objects.ErrStopIteration
	}
	return objects.NewStr(line), nil
}

// Write appends s to the buffer at the current cursor, overwriting
// existing characters and extending the buffer as needed. Returns
// the number of characters written.
//
// CPython: Modules/_io/stringio.c:189 write_str (the buffer side)
// CPython: Modules/_io/stringio.c:547 _io_StringIO_write_impl (the wrapper)
func (s *StringIO) Write(text string) int {
	r := []rune(text)
	n := len(r)
	if n == 0 {
		return 0
	}
	end := s.pos + n
	if end > len(s.buf) {
		// Extend with NULs (matches CPython's resize_buffer + memset
		// path for an over-seek write).
		grow := make([]rune, end)
		copy(grow, s.buf)
		s.buf = grow
	}
	copy(s.buf[s.pos:end], r)
	s.pos = end
	return n
}

// Read returns up to size characters starting at the cursor; size<0
// reads until end-of-buffer.
//
// CPython: Modules/_io/stringio.c:324 _io_StringIO_read_impl
func (s *StringIO) Read(size int) string {
	remaining := len(s.buf) - s.pos
	if remaining < 0 {
		remaining = 0
	}
	if size < 0 || size > remaining {
		size = remaining
	}
	if size <= 0 {
		return ""
	}
	out := string(s.buf[s.pos : s.pos+size])
	s.pos += size
	return out
}

// readline returns one line including the trailing '\n' if present.
// Returns the empty string at end-of-buffer.
//
// CPython: Modules/_io/stringio.c:357 _stringio_readline
func (s *StringIO) readline(limit int) string {
	if s.pos >= len(s.buf) {
		return ""
	}
	end := len(s.buf)
	if limit >= 0 && s.pos+limit < end {
		end = s.pos + limit
	}
	for i := s.pos; i < end; i++ {
		if s.buf[i] == '\n' {
			out := string(s.buf[s.pos : i+1])
			s.pos = i + 1
			return out
		}
	}
	out := string(s.buf[s.pos:end])
	s.pos = end
	return out
}

// Seek changes the cursor position.
//
// CPython: Modules/_io/stringio.c:497 _io_StringIO_seek_impl
func (s *StringIO) Seek(pos, whence int) (int, error) {
	switch whence {
	case 0:
		if pos < 0 {
			return 0, fmt.Errorf("ValueError: Negative seek position %d", pos)
		}
		s.pos = pos
	case 1:
		if pos != 0 {
			return 0, fmt.Errorf("OSError: Can't do nonzero cur-relative seeks")
		}
	case 2:
		if pos != 0 {
			return 0, fmt.Errorf("OSError: Can't do nonzero end-relative seeks")
		}
		s.pos = len(s.buf)
	default:
		return 0, fmt.Errorf("ValueError: Invalid whence (%d, should be 0, 1 or 2)", whence)
	}
	return s.pos, nil
}

// Tell returns the cursor position in characters.
//
// CPython: Modules/_io/stringio.c:303 _io_StringIO_tell_impl
func (s *StringIO) Tell() int { return s.pos }

// Truncate cuts the buffer down to size characters, leaving the
// cursor unchanged. size<0 is rejected; size>=current length is a
// no-op. Returns the new buffer length.
//
// CPython: Modules/_io/stringio.c:458 _io_StringIO_truncate_impl
func (s *StringIO) Truncate(size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("ValueError: Negative size value %d", size)
	}
	if size < len(s.buf) {
		s.buf = s.buf[:size]
	}
	return size, nil
}

// GetValue returns the entire buffer as a Go string. Allocates fresh,
// matching the immutability guarantee CPython makes for the returned
// PyUnicode object.
//
// CPython: Modules/_io/stringio.c:284 _io_StringIO_getvalue_impl
func (s *StringIO) GetValue() string { return string(s.buf) }

// Close marks the StringIO as closed and drops the buffer.
//
// CPython: Modules/_io/stringio.c:580 _io_StringIO_close_impl
func (s *StringIO) Close() {
	s.closed = true
	s.buf = nil
}

// Closed reports whether the StringIO has been closed.
func (s *StringIO) Closed() bool { return s.closed }

// checkUsable maps the CHECK_INITIALIZED + CHECK_CLOSED preamble
// every public StringIO method runs. The Go port does not separate
// "initialized" from "open" because tp_alloc + the constructor are
// fused; only the closed flag survives.
//
// CPython: Modules/_io/stringio.c:57 CHECK_INITIALIZED / 64 CHECK_CLOSED
func (s *StringIO) checkUsable() error {
	if s.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// stringIOGetattr exposes properties (closed, newlines, line_buffering)
// and methods as bound builtin functions. Same approach File.go uses
// while a real method-descriptor surface is pending.
//
// CPython: Modules/_io/stringio.c:1035 stringio_methods +
// CPython: Modules/_io/stringio.c:1054 stringio_getset
func stringIOGetattr(o objects.Object, name objects.Object) (objects.Object, error) {
	s := o.(*StringIO)
	n, ok := name.(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.Value() {
	case "closed":
		return objects.NewBool(s.closed), nil
	case "newlines":
		// Until newline tracking lands in read(), report None: CPython
		// fills this from the IncrementalNewlineDecoder, which the
		// port has not wired yet.
		return objects.None(), nil
	case "line_buffering":
		return objects.NewBool(false), nil
	}
	if fn := stringIOMethod(s, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", StringIOType.Name, n.Value())
}

// stringIOMethod returns the built-in function backing the named
// method. Mirrors the row pick CPython does in stringio_methods at
// stringio_member_descriptor lookup time.
//
// CPython: Modules/_io/stringio.c:1035 stringio_methods
func stringIOMethod(s *StringIO, name string) objects.Object {
	switch name {
	case "write":
		return objects.NewBuiltinFunction("write", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOWrite(s, args)
		})
	case "read":
		return objects.NewBuiltinFunction("read", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIORead(s, args)
		})
	case "readline":
		return objects.NewBuiltinFunction("readline", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOReadlineCall(s, args)
		})
	case "seek":
		return objects.NewBuiltinFunction("seek", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOSeekCall(s, args)
		})
	case "tell":
		return objects.NewBuiltinFunction("tell", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewInt(int64(s.Tell())), nil
		})
	case "truncate":
		return objects.NewBuiltinFunction("truncate", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOTruncateCall(s, args)
		})
	case "getvalue":
		return objects.NewBuiltinFunction("getvalue", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewStr(s.GetValue()), nil
		})
	case "close":
		return objects.NewBuiltinFunction("close", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			s.Close()
			return objects.None(), nil
		})
	case "flush":
		return objects.NewBuiltinFunction("flush", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "readable":
		return objects.NewBuiltinFunction("readable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "writable":
		return objects.NewBuiltinFunction("writable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "seekable":
		return objects.NewBuiltinFunction("seekable", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(true), nil
		})
	case "isatty":
		return objects.NewBuiltinFunction("isatty", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(false), nil
		})
	case "__enter__":
		return objects.NewBuiltinFunction("__enter__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return s, nil
		})
	case "__exit__":
		return objects.NewBuiltinFunction("__exit__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			s.Close()
			return objects.None(), nil
		})
	}
	return nil
}

func stringIOWrite(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: write() takes exactly 1 argument (%d given)", len(args))
	}
	u, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: string argument expected, got '%s'", args[0].Type().Name)
	}
	n := s.Write(u.Value())
	return objects.NewInt(int64(n)), nil
}

func stringIORead(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	size, err := optionalSize(args, "read")
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s.Read(size)), nil
}

func stringIOReadlineCall(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	size, err := optionalSize(args, "readline")
	if err != nil {
		return nil, err
	}
	return objects.NewStr(s.readline(size)), nil
}

func stringIOSeekCall(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
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
	out, err := s.Seek(pos, whence)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(out)), nil
}

func stringIOTruncateCall(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	size := s.pos
	if len(args) >= 1 && !objects.IsNone(args[0]) {
		v, err := intArg(args[0], "truncate")
		if err != nil {
			return nil, err
		}
		size = v
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: truncate() takes at most 1 argument (%d given)", len(args))
	}
	out, err := s.Truncate(size)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(out)), nil
}

// optionalSize unpacks a single int / None argument into a Go int.
// Missing or None means -1 (read everything). Mirrors the size handler
// CPython generates for the read / readline clinic specs.
func optionalSize(args []objects.Object, fn string) (int, error) {
	switch len(args) {
	case 0:
		return -1, nil
	case 1:
		if objects.IsNone(args[0]) {
			return -1, nil
		}
		return intArg(args[0], fn)
	default:
		return 0, fmt.Errorf("TypeError: %s() takes at most 1 argument (%d given)", fn, len(args))
	}
}

func intArg(o objects.Object, fn string) (int, error) {
	i, ok := o.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument must be int, not %s", fn, o.Type().Name)
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: %s() argument out of range", fn)
	}
	return int(v), nil
}
