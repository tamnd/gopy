// StringIO is the in-memory text stream the io module exposes as
// io.StringIO. Faithful port of cpython/Modules/_io/stringio.c:
// rune buffer with a separate string_size cursor, newline translation
// on write (read-translate / write-translate modes), universal newline
// detection on read, and the 4-tuple pickle contract.
//
// CPython: Modules/_io/stringio.c:20 stringio (struct definition)

package io

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// StringIO mirrors the C struct, minus the accumulating-writer
// optimisation (we always work directly against the rune slab).
//
// readTranslate (newline is None): writes have \r\n / \r converted to
// \n, reads return lines terminated by \n.
//
// readUniversal (newline in {None, ""}): readline accepts \r / \n /
// \r\n as terminators.
//
// readNL is the configured newline; writeNL is set when newline is
// "\r" or "\r\n" so writes translate \n to that sequence.
//
// CPython: Modules/_io/stringio.c:20 stringio
type StringIO struct {
	objects.Header

	buf        []rune
	pos        int
	stringSize int
	closed     bool

	readNL        string
	hasReadNL     bool
	writeNL       string
	readUniversal bool
	readTranslate bool

	newlinesSeen uint8 // bit 0: \n, bit 1: \r, bit 2: \r\n
}

// StringIOType is the type singleton for _io.StringIO.
//
// CPython: Modules/_io/stringio.c:1073 stringio_slots
var StringIOType = objects.NewType("_io.StringIO", []*objects.Type{objects.ObjectType()})

func init() {
	StringIOType.Call = stringIOCall
	StringIOType.TpNew = stringIOTpNew
	StringIOType.Repr = stringIORepr
	StringIOType.Str = stringIORepr
	StringIOType.Iter = stringIOIter
	StringIOType.IterNext = stringIOIterNext
	StringIOType.Getattro = stringIOGetattr
	registerStringIODescrs()
	objects.AddIterSlotWrappers(StringIOType)
}

// stringIOTpNew is the tp_new slot. Routing through TpNew means a user
// subclass like doctest._SpoofOut still produces a *StringIO with the
// subclass stamped as ob_type, so descriptor methods registered on
// StringIOType keep resolving via the type assertion.
//
// CPython: Modules/_io/stringio.c:637 stringio_new
func stringIOTpNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return stringIOCall(cls, args, kwargs)
}

// registerStringIODescrs installs the type-level method and getset
// descriptors so unbound calls like `_io.StringIO.getvalue(self)` (the
// pattern doctest._SpoofOut uses when chaining a base-class method)
// resolve through the type MRO.
//
// CPython: Modules/_io/stringio.c:1035 stringio_methods
// CPython: Modules/_io/stringio.c:1054 stringio_getset
func registerStringIODescrs() {
	methods := []struct {
		name string
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"write", stringIOWriteDescr},
		{"read", stringIOReadDescr},
		{"readline", stringIOReadlineDescr},
		{"readlines", stringIOReadlinesDescr},
		{"writelines", stringIOWritelinesDescr},
		{"seek", stringIOSeekDescr},
		{"tell", stringIOTellDescr},
		{"truncate", stringIOTruncateDescr},
		{"getvalue", stringIOGetvalueDescr},
		{"close", stringIOCloseDescr},
		{"flush", stringIOFlushDescr},
		{"readable", stringIOReadableDescr},
		{"writable", stringIOWritableDescr},
		{"seekable", stringIOSeekableDescr},
		{"isatty", stringIOIsattyDescr},
		{"__getstate__", stringIOGetstateDescr},
		{"__setstate__", stringIOSetstateDescr},
		{"__sizeof__", stringIOSizeofDescr},
		{"__enter__", stringIOEnterDescr},
		{"__exit__", stringIOExitDescr},
	}
	for _, m := range methods {
		objects.SetTypeDescr(StringIOType, m.name, objects.NewMethodDescr(StringIOType, m.name, m.fn))
	}
	objects.SetTypeDescr(StringIOType, "closed", objects.NewGetSetDescr("closed",
		func(o objects.Object) (objects.Object, error) {
			return objects.NewBool(o.(*StringIO).closed), nil
		}, nil))
	objects.SetTypeDescr(StringIOType, "newlines", objects.NewGetSetDescr("newlines",
		func(o objects.Object) (objects.Object, error) {
			s := o.(*StringIO)
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return s.newlinesObj(), nil
		}, nil))
	objects.SetTypeDescr(StringIOType, "line_buffering", objects.NewGetSetDescr("line_buffering",
		func(o objects.Object) (objects.Object, error) {
			s := o.(*StringIO)
			if err := s.checkUsable(); err != nil {
				return nil, err
			}
			return objects.NewBool(false), nil
		}, nil))
	// encoding and errors come from _TextIOBase in CPython. gopy's
	// StringIO has no TextIOBase parent so they are served from
	// stringIOGetattr's switch alongside closed / newlines /
	// line_buffering. The matching SetTypeDescr entries also exist so
	// dir() picks them up.
	//
	// CPython: Modules/_io/textio.c:138 _io__TextIOBase_encoding_get_impl
	// CPython: Modules/_io/textio.c:172 _io__TextIOBase_errors_get_impl
	objects.SetTypeDescr(StringIOType, "encoding", objects.NewGetSetDescr("encoding",
		func(_ objects.Object) (objects.Object, error) {
			return objects.None(), nil
		}, nil))
	objects.SetTypeDescr(StringIOType, "errors", objects.NewGetSetDescr("errors",
		func(_ objects.Object) (objects.Object, error) {
			return objects.None(), nil
		}, nil))
}

// stringIOSelf pops args[0] as the StringIO receiver. Mirrors the
// METH_O / METH_NOARGS unwrap CPython does before dispatching into
// the typed C helper.
func stringIOSelf(name string, args []objects.Object) (*StringIO, []objects.Object, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' of '_io.StringIO' object needs an argument", name)
	}
	s, ok := args[0].(*StringIO)
	if !ok {
		return nil, nil, fmt.Errorf("TypeError: descriptor '%s' requires a '_io.StringIO' object but received a '%s'", name, args[0].Type().Name)
	}
	return s, args[1:], nil
}

func stringIOWriteDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("write", args)
	if err != nil {
		return nil, err
	}
	return stringIOWrite(s, rest)
}

func stringIOReadDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("read", args)
	if err != nil {
		return nil, err
	}
	return stringIORead(s, rest)
}

func stringIOReadlineDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("readline", args)
	if err != nil {
		return nil, err
	}
	return stringIOReadlineCall(s, rest)
}

func stringIOReadlinesDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("readlines", args)
	if err != nil {
		return nil, err
	}
	return stringIOReadlinesCall(s, rest)
}

func stringIOWritelinesDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("writelines", args)
	if err != nil {
		return nil, err
	}
	return stringIOWritelinesCall(s, rest)
}

func stringIOSeekDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("seek", args)
	if err != nil {
		return nil, err
	}
	return stringIOSeekCall(s, rest)
}

func stringIOTellDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("tell", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewInt(int64(s.Tell())), nil
}

func stringIOTruncateDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("truncate", args)
	if err != nil {
		return nil, err
	}
	return stringIOTruncateCall(s, rest)
}

func stringIOGetvalueDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("getvalue", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewStr(s.GetValue()), nil
}

func stringIOCloseDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("close", args)
	if err != nil {
		return nil, err
	}
	s.Close()
	return objects.None(), nil
}

func stringIOFlushDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("flush", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

func stringIOReadableDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("readable", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewBool(true), nil
}

func stringIOWritableDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("writable", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewBool(true), nil
}

func stringIOSeekableDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("seekable", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewBool(true), nil
}

func stringIOIsattyDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("isatty", args)
	if err != nil {
		return nil, err
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return objects.NewBool(false), nil
}

func stringIOGetstateDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("__getstate__", args)
	if err != nil {
		return nil, err
	}
	return s.getState()
}

func stringIOSetstateDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, rest, err := stringIOSelf("__setstate__", args)
	if err != nil {
		return nil, err
	}
	if len(rest) != 1 {
		return nil, fmt.Errorf("TypeError: __setstate__() takes exactly 1 argument (%d given)", len(rest))
	}
	if err := s.setState(rest[0]); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

func stringIOSizeofDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	s, _, err := stringIOSelf("__sizeof__", args)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(len(s.buf) * 4)), nil
}

func stringIOEnterDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self argument")
	}
	s, ok := args[0].(*StringIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __enter__() expected _io.StringIO self")
	}
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return s, nil
}

func stringIOExitDescr(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self argument")
	}
	s, ok := args[0].(*StringIO)
	if !ok {
		return nil, fmt.Errorf("TypeError: __exit__() expected _io.StringIO self")
	}
	s.Close()
	return objects.None(), nil
}

// NewStringIO returns an empty StringIO in default-newline mode
// (newline="\n"). Callers populate the initial value through the
// constructor path.
//
// CPython: Modules/_io/stringio.c:637 stringio_new
func NewStringIO() *StringIO {
	return newStringIOForType(StringIOType)
}

// newStringIOForType allocates a fresh *StringIO carrying the given
// Python type. Subclasses (e.g. doctest._SpoofOut) reach this path via
// stringio_new(cls) so `instance.Type()` reports the subclass while
// the underlying Go layout stays *StringIO and the descriptor methods
// resolve.
//
// CPython: Modules/_io/stringio.c:637 stringio_new (tp_alloc(cls, 0))
func newStringIOForType(t *objects.Type) *StringIO {
	s := &StringIO{}
	s.Init(t)
	s.readNL = "\n"
	s.hasReadNL = true
	return s
}

// stringIOCall is the type-call slot, mirroring the Clinic-generated
// _io_StringIO___init___impl: parse `initial_value` and `newline`,
// reject non-str / non-None types, accept "" / "\n" / "\r" / "\r\n" /
// None.
//
// CPython: Modules/_io/stringio.c:670 _io_StringIO___init___impl
func stringIOCall(callable objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 2 {
		return nil, fmt.Errorf("TypeError: StringIO() takes at most 2 arguments (%d given)", len(args))
	}
	initial, err := strOrNone(positional(args, 0), "initial_value")
	if err != nil {
		return nil, err
	}
	nlSet := len(args) >= 2
	nlVal, nlIsStr, err := newlineArg(positional(args, 1))
	if err != nil {
		return nil, err
	}
	for k, v := range kwargs {
		switch k {
		case "initial_value":
			initial, err = strOrNone(v, "initial_value")
			if err != nil {
				return nil, err
			}
		case "newline":
			nlSet = true
			nlVal, nlIsStr, err = newlineArg(v)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("TypeError: StringIO() got an unexpected keyword argument %q", k)
		}
	}
	if nlSet && nlIsStr {
		switch nlVal {
		case "", "\n", "\r", "\r\n":
		default:
			return nil, fmt.Errorf("ValueError: illegal newline value: %q", nlVal)
		}
	}
	t := StringIOType
	if subt, ok := callable.(*objects.Type); ok && subt != nil {
		t = subt
	}
	s := newStringIOForType(t)
	s.applyNewline(nlSet, nlIsStr, nlVal)
	if initial != "" {
		s.Write(initial)
		s.pos = 0
	}
	return s, nil
}

// applyNewline mirrors the newline-mode bookkeeping inside __init__.
// nlSet=false means default (newline="\n"). nlSet=true && !nlIsStr
// means newline=None.
//
// CPython: Modules/_io/stringio.c:721 (init newline branch)
func (s *StringIO) applyNewline(nlSet, nlIsStr bool, nlVal string) {
	if !nlSet {
		s.readNL = "\n"
		s.hasReadNL = true
		s.readUniversal = false
		s.readTranslate = false
		s.writeNL = ""
		return
	}
	if !nlIsStr {
		s.readNL = ""
		s.hasReadNL = false
		s.readUniversal = true
		s.readTranslate = true
		s.writeNL = ""
		return
	}
	s.readNL = nlVal
	s.hasReadNL = true
	s.readUniversal = nlVal == ""
	s.readTranslate = false
	if len(nlVal) > 0 && nlVal[0] == '\r' {
		s.writeNL = nlVal
	} else {
		s.writeNL = ""
	}
}

func positional(args []objects.Object, i int) objects.Object {
	if i < len(args) {
		return args[i]
	}
	return nil
}

// strOrNone validates that v is None or a str. Returns the underlying
// Go string (empty if None or unset).
func strOrNone(v objects.Object, name string) (string, error) {
	if v == nil || objects.IsNone(v) {
		return "", nil
	}
	s, ok := v.(*objects.Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: %s must be str or None, not %s", name, v.Type().Name)
	}
	return s.Value(), nil
}

// newlineArg unpacks the `newline` argument. Returns (value, isStr,
// err): isStr=false means caller passed None (or nothing).
func newlineArg(v objects.Object) (val string, isStr bool, err error) {
	if v == nil || objects.IsNone(v) {
		return "", false, nil
	}
	s, ok := v.(*objects.Unicode)
	if !ok {
		return "", false, fmt.Errorf("TypeError: newline must be str or None, not %s", v.Type().Name)
	}
	return s.Value(), true, nil
}

func stringIORepr(_ objects.Object) (string, error) {
	return "<_io.StringIO object>", nil
}

// stringIOIter returns the StringIO itself.
//
// CPython: Modules/_io/iobase.c:732 iobase_iter
func stringIOIter(o objects.Object) (objects.Object, error) {
	s := o.(*StringIO)
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	return s, nil
}

// stringIOIterNext yields the next line. Empty line means EOF.
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

// Write appends s to the buffer at the current cursor, applying
// newline translation per the constructor's newline mode and
// zero-padding any over-seek gap. Returns the number of characters
// written (post-translation length).
//
// CPython: Modules/_io/stringio.c:189 write_str
func (s *StringIO) Write(text string) int {
	if s.readTranslate {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	if s.writeNL != "" {
		text = strings.ReplaceAll(text, "\n", s.writeNL)
	}
	r := []rune(text)
	n := len(r)
	if n == 0 {
		return 0
	}
	end := s.pos + n
	if end > len(s.buf) {
		grow := make([]rune, end)
		copy(grow, s.buf)
		s.buf = grow
	}
	if s.pos > s.stringSize {
		for i := s.stringSize; i < s.pos; i++ {
			s.buf[i] = 0
		}
	}
	copy(s.buf[s.pos:end], r)
	s.pos = end
	if s.stringSize < end {
		s.stringSize = end
	}
	return n
}

// Read returns up to size characters starting at the cursor; size<0
// reads until end of stream.
//
// CPython: Modules/_io/stringio.c:324 _io_StringIO_read_impl
func (s *StringIO) Read(size int) string {
	n := s.stringSize - s.pos
	if size < 0 || size > n {
		size = max(n, 0)
	}
	if size <= 0 {
		return ""
	}
	out := string(s.buf[s.pos : s.pos+size])
	s.pos += size
	return out
}

// readline returns one line. In translate mode the buffer already
// holds only \n terminators; in universal mode any of \n / \r / \r\n
// counts; in fixed-newline mode the configured readnl is used.
// Updates newlinesSeen so the .newlines getter can report what's been
// observed.
//
// CPython: Modules/_io/stringio.c:357 _stringio_readline
//
//	Modules/_io/textio.c:2120 _PyIO_find_line_ending
func (s *StringIO) readline(limit int) string {
	if s.pos >= s.stringSize {
		return ""
	}
	end := s.stringSize
	if limit >= 0 && s.pos+limit < end {
		end = s.pos + limit
	}
	n := s.findLineEnding(s.pos, end)
	out := string(s.buf[s.pos : s.pos+n])
	s.pos += n
	return out
}

func (s *StringIO) findLineEnding(start, end int) int {
	if s.readTranslate {
		for i := start; i < end; i++ {
			if s.buf[i] == '\n' {
				s.newlinesSeen |= 1
				return i - start + 1
			}
		}
		return end - start
	}
	if s.readUniversal {
		for i := start; i < end; i++ {
			ch := s.buf[i]
			if ch == '\n' {
				s.newlinesSeen |= 1
				return i - start + 1
			}
			if ch == '\r' {
				if i+1 < end && s.buf[i+1] == '\n' {
					s.newlinesSeen |= 4
					return i - start + 2
				}
				s.newlinesSeen |= 2
				return i - start + 1
			}
		}
		return end - start
	}
	nl := []rune(s.readNL)
	for i := start; i+len(nl) <= end; i++ {
		match := true
		for k, c := range nl {
			if s.buf[i+k] != c {
				match = false
				break
			}
		}
		if match {
			return i - start + len(nl)
		}
	}
	return end - start
}

// Seek changes the cursor position. whence=1/2 require pos==0 (matches
// CPython). whence=0 with pos<0 raises ValueError.
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
		s.pos = s.stringSize
	default:
		return 0, fmt.Errorf("ValueError: Invalid whence (%d, should be 0, 1 or 2)", whence)
	}
	return s.pos, nil
}

// Tell returns the cursor position in characters.
//
// CPython: Modules/_io/stringio.c:303 _io_StringIO_tell_impl
func (s *StringIO) Tell() int { return s.pos }

// Truncate cuts the logical length to size characters; the underlying
// rune slab is shrunk to match. The cursor is unchanged.
//
// CPython: Modules/_io/stringio.c:458 _io_StringIO_truncate_impl
func (s *StringIO) Truncate(size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("ValueError: Negative size value %d", size)
	}
	if size < s.stringSize {
		s.stringSize = size
		s.buf = s.buf[:size]
	}
	return size, nil
}

// GetValue returns the logical contents as a Go string.
//
// CPython: Modules/_io/stringio.c:284 _io_StringIO_getvalue_impl
func (s *StringIO) GetValue() string { return string(s.buf[:s.stringSize]) }

// Close marks the StringIO as closed and drops the buffer.
//
// CPython: Modules/_io/stringio.c:580 _io_StringIO_close_impl
func (s *StringIO) Close() {
	s.closed = true
	s.buf = nil
	s.stringSize = 0
}

// Closed reports whether the StringIO has been closed.
func (s *StringIO) Closed() bool { return s.closed }

// checkUsable maps the CHECK_INITIALIZED + CHECK_CLOSED preamble every
// public method runs. The Go port fuses tp_alloc + __init__ so only
// the closed flag survives.
//
// CPython: Modules/_io/stringio.c:57 CHECK_INITIALIZED / :64 CHECK_CLOSED
func (s *StringIO) checkUsable() error {
	if s.closed {
		return fmt.Errorf("ValueError: I/O operation on closed file")
	}
	return nil
}

// newlinesObj returns the value of the .newlines getter: None if no
// terminator has been seen, a single str if exactly one kind, a tuple
// of strs if several.
//
// CPython: Modules/_io/stringio.c:1023 _io_StringIO_newlines_get_impl
//
//	(delegates to IncrementalNewlineDecoder.newlines)
func (s *StringIO) newlinesObj() objects.Object {
	if !s.readTranslate && !s.readUniversal {
		return objects.None()
	}
	if s.newlinesSeen == 0 {
		return objects.None()
	}
	var seen []string
	if s.newlinesSeen&2 != 0 {
		seen = append(seen, "\r")
	}
	if s.newlinesSeen&1 != 0 {
		seen = append(seen, "\n")
	}
	if s.newlinesSeen&4 != 0 {
		seen = append(seen, "\r\n")
	}
	if len(seen) == 1 {
		return objects.NewStr(seen[0])
	}
	items := make([]objects.Object, len(seen))
	for i, v := range seen {
		items[i] = objects.NewStr(v)
	}
	return objects.NewTuple(items)
}

// getState returns the 4-tuple (initial_value, readnl, pos, dict)
// used by pickle protocol 2.
//
// CPython: Modules/_io/stringio.c:849 _io_StringIO___getstate___impl
func (s *StringIO) getState() (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	nl := objects.None()
	if s.hasReadNL {
		nl = objects.NewStr(s.readNL)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(s.GetValue()),
		nl,
		objects.NewInt(int64(s.pos)),
		objects.None(),
	}), nil
}

// setState rehydrates from a 4-tuple. Resets the buffer, replays
// __init__ with (initial_value, newline), then restores pos and merges
// the dict.
//
// CPython: Modules/_io/stringio.c:885 _io_StringIO___setstate___impl
func (s *StringIO) setState(state objects.Object) error {
	if err := s.checkUsable(); err != nil {
		return err
	}
	t, ok := state.(*objects.Tuple)
	if !ok || t.Len() < 4 {
		return fmt.Errorf("TypeError: _io.StringIO.__setstate__ argument should be 4-tuple, got %s", state.Type().Name)
	}
	initVal := ""
	if v, ok := t.Item(0).(*objects.Unicode); ok {
		initVal = v.Value()
	} else if !objects.IsNone(t.Item(0)) {
		return fmt.Errorf("TypeError: first item of state must be str or None, got %s", t.Item(0).Type().Name)
	}
	nlVal, nlIsStr, err := newlineArg(t.Item(1))
	if err != nil {
		return err
	}
	s.buf = nil
	s.stringSize = 0
	s.pos = 0
	s.newlinesSeen = 0
	s.applyNewline(true, nlIsStr, nlVal)
	if initVal != "" {
		s.buf = []rune(initVal)
		s.stringSize = len(s.buf)
	}
	posObj, ok := t.Item(2).(*objects.Int)
	if !ok {
		return fmt.Errorf("TypeError: third item of state must be an integer, got %s", t.Item(2).Type().Name)
	}
	pos, _ := posObj.Int64()
	if pos < 0 {
		return fmt.Errorf("ValueError: position value cannot be negative")
	}
	s.pos = int(pos)
	return nil
}

// stringIOGetattr exposes properties (closed, newlines, line_buffering)
// and methods as bound builtin functions.
//
// CPython: Modules/_io/stringio.c:1035 stringio_methods + :1054 stringio_getset
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
		if err := s.checkUsable(); err != nil {
			return nil, err
		}
		return s.newlinesObj(), nil
	case "line_buffering":
		if err := s.checkUsable(); err != nil {
			return nil, err
		}
		return objects.NewBool(false), nil
	case "encoding", "errors":
		// _TextIOBase exposes encoding and errors as getsets that
		// default to None. _io.StringIO inherits these in CPython;
		// gopy's StringIO has no TextIOBase parent so we serve None
		// directly. doctest._SpoofOut reads save_stdout.encoding
		// inside DocTestRunner.run.
		//
		// CPython: Modules/_io/textio.c:138 _io__TextIOBase_encoding_get_impl
		// CPython: Modules/_io/textio.c:172 _io__TextIOBase_errors_get_impl
		return objects.None(), nil
	}
	if fn := stringIOMethod(s, n.Value()); fn != nil {
		return fn, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", StringIOType.Name, n.Value())
}

// stringIOMethod maps method names to BuiltinFunctions. Split in two
// to keep each half under the cyclomatic threshold.
//
// CPython: Modules/_io/stringio.c:1035 stringio_methods
func stringIOMethod(s *StringIO, name string) objects.Object {
	if fn := stringIOIOMethod(s, name); fn != nil {
		return fn
	}
	return stringIOCapMethod(s, name)
}

// stringIOIOMethod returns the I/O-active methods.
func stringIOIOMethod(s *StringIO, name string) objects.Object {
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
	case "readlines":
		return objects.NewBuiltinFunction("readlines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOReadlinesCall(s, args)
		})
	case "writelines":
		return objects.NewBuiltinFunction("writelines", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return stringIOWritelinesCall(s, args)
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
	}
	return nil
}

// stringIOCapMethod returns the capability/protocol methods.
func stringIOCapMethod(s *StringIO, name string) objects.Object {
	switch name {
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
	case "__getstate__":
		return objects.NewBuiltinFunction("__getstate__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return s.getState()
		})
	case "__setstate__":
		return objects.NewBuiltinFunction("__setstate__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly 1 argument (%d given)", len(args))
			}
			if err := s.setState(args[0]); err != nil {
				return nil, err
			}
			return objects.None(), nil
		})
	case "__sizeof__":
		return objects.NewBuiltinFunction("__sizeof__", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return objects.NewInt(int64(len(s.buf) * 4)), nil
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
	// CPython returns the *input* length, not the post-translation length.
	n := len([]rune(u.Value()))
	s.Write(u.Value())
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

// stringIOReadlinesCall implements readlines(hint=-1).
//
// CPython: Modules/_io/iobase.c:849 _io__IOBase_readlines_impl
func stringIOReadlinesCall(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
		return nil, err
	}
	hint := -1
	if len(args) >= 1 && !objects.IsNone(args[0]) {
		v, err := intArg(args[0], "readlines")
		if err != nil {
			return nil, err
		}
		hint = v
	}
	var lines []objects.Object
	total := 0
	for {
		line := s.readline(-1)
		if line == "" {
			break
		}
		lines = append(lines, objects.NewStr(line))
		total += len([]rune(line))
		if hint > 0 && total >= hint {
			break
		}
	}
	return objects.NewList(lines), nil
}

// stringIOWritelinesCall iterates any iterable, calling write() per
// element.
//
// CPython: Modules/_io/iobase.c:863 _io__IOBase_writelines
func stringIOWritelinesCall(s *StringIO, args []objects.Object) (objects.Object, error) {
	if err := s.checkUsable(); err != nil {
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
		u, ok := item.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: string argument expected, got '%s'", item.Type().Name)
		}
		s.Write(u.Value())
	}
	return objects.None(), nil
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
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: truncate() takes at most 1 argument (%d given)", len(args))
	}
	size := s.pos
	if len(args) == 1 && !objects.IsNone(args[0]) {
		v, err := intArg(args[0], "truncate")
		if err != nil {
			return nil, err
		}
		size = v
	}
	out, err := s.Truncate(size)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(out)), nil
}

// optionalSize unpacks a single int / None argument into a Go int.
// Missing or None means -1. Mirrors the Clinic size handler.
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
