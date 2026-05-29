// MemoryView is the Python memoryview type: a window over a bytes-like
// object's underlying buffer. CPython models this through the PEP 3118
// buffer protocol; gopy's port covers the 1-D contiguous case with the
// common scalar format codes (b, B, c, h, H, i, I, l, L, q, Q, n, N)
// in native byte order. Multi-dim shape and exotic formats land later.
//
// CPython: Objects/memoryobject.c:3402 PyMemoryView_Type

package objects

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// MemoryView is a 1-D contiguous view over a byte buffer.
//
// CPython: Objects/memoryobject.c:3402 PyMemoryView_Type
type MemoryView struct {
	Header

	buf      []byte
	readonly bool
	format   string
	itemsize int
}

// MemoryViewType is the type singleton for memoryview.
//
// CPython: Objects/memoryobject.c:3402 PyMemoryView_Type
var MemoryViewType = NewType("memoryview", []*Type{objectType})

func init() {
	MemoryViewType.Repr = memoryViewRepr
	MemoryViewType.Str = memoryViewRepr
	MemoryViewType.Hash = memoryViewHash
	MemoryViewType.RichCmp = memoryViewRichCmp
	MemoryViewType.Iter = memoryViewIter
	MemoryViewType.Getattro = memoryViewGetattr
	// CPython tags PyMemoryView_Type with Py_TPFLAGS_SEQUENCE so
	// match-statement sequence patterns decompose memoryview through
	// the buffer protocol.
	//
	// CPython: Objects/memoryobject.c:3597 PyMemoryView_Type tp_flags
	MemoryViewType.TpFlags |= TpFlagSequence
	MemoryViewType.Sequence = &SequenceMethods{
		Length:   memoryViewLen,
		GetItem:  memoryViewGetItem,
		Contains: memoryViewContains,
	}
	MemoryViewType.Mapping = &MappingMethods{
		Length:  memoryViewLen,
		GetItem: memoryViewGetItemKey,
	}

	SetTypeDescr(MemoryViewType, "cast", NewMethodDescr(MemoryViewType, "cast", memoryViewCastMethod))
	SetTypeDescr(MemoryViewType, "tobytes", NewMethodDescr(MemoryViewType, "tobytes", memoryViewTobytesMethod))
	SetTypeDescr(MemoryViewType, "tolist", NewMethodDescr(MemoryViewType, "tolist", memoryViewTolistMethod))
	SetTypeDescr(MemoryViewType, "hex", NewMethodDescr(MemoryViewType, "hex", memoryViewHexMethod))
	SetTypeDescr(MemoryViewType, "release", NewMethodDescr(MemoryViewType, "release", memoryViewReleaseMethod))
	SetTypeDescr(MemoryViewType, "toreadonly", NewMethodDescr(MemoryViewType, "toreadonly", memoryViewToreadonlyMethod))
}

// formatItemsize maps a PEP 3118 format code to its byte width. Returns
// 0 for unsupported formats.
//
// CPython: Objects/memoryobject.c:240 get_native_fmtchar
func formatItemsize(format string) int {
	if format == "" {
		return 0
	}
	switch format {
	case "b", "B", "c":
		return 1
	case "h", "H":
		return 2
	case "i", "I", "l", "L", "n", "N":
		return 4
	case "q", "Q":
		return 8
	case "f":
		return 4
	case "d":
		return 8
	case "?":
		return 1
	}
	return 0
}

// NewMemoryView wraps src in a memoryview. The view aliases the
// underlying byte slice; callers can read but the readonly flag
// determines whether the type can later expose mutation. Bytes get a
// read-only view; bytearray is technically writable but every view
// stays read-only for now.
//
// CPython: Objects/memoryobject.c:1041 PyMemoryView_FromObject
func NewMemoryView(src Object) (*MemoryView, error) {
	switch s := src.(type) {
	case *Bytes:
		mv := &MemoryView{buf: s.Bytes(), readonly: true, format: "B", itemsize: 1}
		mv.init(MemoryViewType)
		return mv, nil
	case *ByteArray:
		mv := &MemoryView{buf: s.Bytes(), readonly: true, format: "B", itemsize: 1}
		mv.init(MemoryViewType)
		return mv, nil
	case *MemoryView:
		mv := &MemoryView{buf: s.buf, readonly: s.readonly, format: s.format, itemsize: s.itemsize}
		mv.init(MemoryViewType)
		return mv, nil
	}
	return nil, fmt.Errorf("TypeError: memoryview: a bytes-like object is required, not '%s'", src.Type().Name)
}

// Bytes returns the underlying byte slice. The slice is the live
// buffer, not a copy.
func (m *MemoryView) Bytes() []byte { return m.buf }

// Len reports the number of items at the current itemsize.
func (m *MemoryView) Len() int {
	if m.itemsize <= 1 {
		return len(m.buf)
	}
	return len(m.buf) / m.itemsize
}

// readItem returns the i-th item of the view interpreted per format.
// i is in item units, not bytes.
func (m *MemoryView) readItem(i int) Object {
	off := i * m.itemsize
	end := off + m.itemsize
	chunk := m.buf[off:end]
	switch m.format {
	case "B":
		return NewInt(int64(chunk[0]))
	case "b":
		return NewInt(int64(int8(chunk[0])))
	case "c":
		out := make([]byte, 1)
		out[0] = chunk[0]
		return NewBytes(out)
	case "?":
		return NewBool(chunk[0] != 0)
	case "h":
		return NewInt(int64(int16(binary.NativeEndian.Uint16(chunk))))
	case "H":
		return NewInt(int64(binary.NativeEndian.Uint16(chunk)))
	case "i", "l", "n":
		return NewInt(int64(int32(binary.NativeEndian.Uint32(chunk))))
	case "I", "L", "N":
		return NewInt(int64(binary.NativeEndian.Uint32(chunk)))
	case "q":
		return NewInt(int64(binary.NativeEndian.Uint64(chunk)))
	case "Q":
		return NewInt(int64(binary.NativeEndian.Uint64(chunk)))
	}
	return NewInt(int64(chunk[0]))
}

// Tobytes returns a fresh Bytes copy of the view.
//
// CPython: Objects/memoryobject.c:2374 memoryview_tobytes_impl
func (m *MemoryView) Tobytes() *Bytes {
	out := make([]byte, len(m.buf))
	copy(out, m.buf)
	return NewBytes(out)
}

// Tolist returns the view as a list of items decoded per format.
//
// CPython: Objects/memoryobject.c:2467 memoryview_tolist
func (m *MemoryView) Tolist() *List {
	n := m.Len()
	items := make([]Object, n)
	for i := 0; i < n; i++ {
		items[i] = m.readItem(i)
	}
	return NewList(items)
}

func memoryViewLen(o Object) (int, error) { return o.(*MemoryView).Len(), nil }

func memoryViewGetItem(o Object, i int) (Object, error) {
	m := o.(*MemoryView)
	n := m.Len()
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return nil, errIndexOutOfRange
	}
	return m.readItem(i), nil
}

// memoryViewGetItemKey is the mapping-shaped GetItem dispatch: it
// accepts an Int (item index) or a Slice (returns a new memoryview
// over the sub-slice). Routes integer keys through the sequence path
// so behavior stays in one place.
//
// CPython: Objects/memoryobject.c:2059 memory_subscript
func memoryViewGetItemKey(o, key Object) (Object, error) {
	m := o.(*MemoryView)
	switch k := key.(type) {
	case *Int:
		i, ok := k.Int64()
		if !ok {
			return nil, fmt.Errorf("IndexError: cannot fit '%s' into an index", k.Type().Name)
		}
		return memoryViewGetItem(o, int(i))
	case *Slice:
		start, stop, step, n, err := k.GetIndices(m.Len())
		if err != nil {
			return nil, err
		}
		if step != 1 {
			out := make([]byte, n*m.itemsize)
			for i := 0; i < n; i++ {
				src := (start + i*step) * m.itemsize
				copy(out[i*m.itemsize:], m.buf[src:src+m.itemsize])
			}
			view := &MemoryView{buf: out, readonly: m.readonly, format: m.format, itemsize: m.itemsize}
			view.init(MemoryViewType)
			return view, nil
		}
		view := &MemoryView{
			buf:      m.buf[start*m.itemsize : stop*m.itemsize],
			readonly: m.readonly,
			format:   m.format,
			itemsize: m.itemsize,
		}
		view.init(MemoryViewType)
		return view, nil
	}
	return nil, fmt.Errorf("TypeError: memoryview: invalid slice key '%s'", key.Type().Name)
}

func memoryViewContains(o, v Object) (bool, error) {
	m := o.(*MemoryView)
	x, ok := v.(*Int)
	if !ok {
		return false, fmt.Errorf("TypeError: memoryview: a byte integer is required")
	}
	n, ok := x.Int64()
	if !ok || n < 0 || n > 255 {
		return false, fmt.Errorf("ValueError: byte must be in range(0, 256)")
	}
	for _, c := range m.buf {
		if int64(c) == n {
			return true, nil
		}
	}
	return false, nil
}

func memoryViewRepr(o Object) (string, error) {
	m := o.(*MemoryView)
	state := "released"
	if m.buf != nil {
		state = "ok"
	}
	return fmt.Sprintf("<memory at %p; %s; %d bytes>", m, state, len(m.buf)), nil
}

func memoryViewHash(o Object) (int64, error) {
	m := o.(*MemoryView)
	if !m.readonly {
		return 0, fmt.Errorf("ValueError: cannot hash writable memoryview object")
	}
	return HashBytes(m.buf), nil
}

func memoryViewRichCmp(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	bufA, ok := bytesViewOf(a)
	if !ok {
		return NotImplemented(), nil
	}
	bufB, ok := bytesViewOf(b)
	if !ok {
		return NotImplemented(), nil
	}
	eq := len(bufA) == len(bufB)
	if eq {
		for i := range bufA {
			if bufA[i] != bufB[i] {
				eq = false
				break
			}
		}
	}
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// bytesViewOf returns the underlying byte slice for any bytes-like
// object that memoryview can compare against. Supports MemoryView,
// Bytes, ByteArray; returns false otherwise.
func bytesViewOf(o Object) ([]byte, bool) { return AsBytesLike(o) }

// ByteBufferHook extends AsBytesLike to buffer-protocol types outside
// the objects package (e.g. array.array). Set at module init time.
var ByteBufferHook func(Object) ([]byte, bool)

// AsBytesLike unwraps any bytes-like object (Bytes, ByteArray,
// MemoryView, or a type registered via ByteBufferHook) to the
// underlying byte slice. It is the gopy equivalent of CPython's
// PyObject_GetBuffer for the common contiguous read path.
//
// CPython: Objects/abstract.c:341 PyObject_GetBuffer (PyBUF_SIMPLE)
func AsBytesLike(o Object) ([]byte, bool) {
	switch v := o.(type) {
	case *MemoryView:
		return v.buf, true
	case *Bytes:
		return v.Bytes(), true
	case *ByteArray:
		return v.Bytes(), true
	}
	if ByteBufferHook != nil {
		return ByteBufferHook(o)
	}
	return nil, false
}

func memoryViewGetattr(o Object, name Object) (Object, error) {
	m := o.(*MemoryView)
	n, ok := name.(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: attribute name must be string")
	}
	switch n.v {
	case "format":
		f := m.format
		if f == "" {
			f = "B"
		}
		return NewStr(f), nil
	case "itemsize":
		sz := m.itemsize
		if sz <= 0 {
			sz = 1
		}
		return NewInt(int64(sz)), nil
	case "nbytes":
		return NewInt(int64(len(m.buf))), nil
	case "readonly":
		return NewBool(m.readonly), nil
	case "ndim":
		return NewInt(1), nil
	case "shape":
		return NewTuple([]Object{NewInt(int64(m.Len()))}), nil
	case "strides":
		sz := m.itemsize
		if sz <= 0 {
			sz = 1
		}
		return NewTuple([]Object{NewInt(int64(sz))}), nil
	case "suboffsets":
		return NewTuple(nil), nil
	case "c_contiguous", "f_contiguous", "contiguous":
		return NewBool(true), nil
	case "obj":
		return None(), nil
	}
	return GenericGetAttr(o, name)
}

// memoryViewIterator yields one item per call decoded per format.
//
// CPython: Objects/memoryobject.c:2867 memoryiter_next
type memoryViewIterator struct {
	Header
	src *MemoryView
	pos int
}

var memoryViewIterType = NewType("memory_iterator", []*Type{objectType})

func init() {
	memoryViewIterType.Iter = func(o Object) (Object, error) { return o, nil }
	memoryViewIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*memoryViewIterator)
		if it.pos >= it.src.Len() {
			return nil, ErrStopIteration
		}
		v := it.src.readItem(it.pos)
		it.pos++
		return v, nil
	}
	AddIterSlotWrappers(memoryViewIterType)
}

func memoryViewIter(o Object) (Object, error) {
	it := &memoryViewIterator{src: o.(*MemoryView)}
	it.init(memoryViewIterType)
	return it, nil
}

// memoryViewCastMethod backs memoryview.cast(format[, shape]).
// gopy supports the 1-D scalar case: the underlying buffer is
// reinterpreted at a new item width, shape is accepted but only a
// 1-tuple matching the implied count is allowed.
//
// CPython: Objects/memoryobject.c:1572 memoryview_cast_impl
func memoryViewCastMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: cast() takes 1 or 2 arguments (%d given)", len(args)-1)
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'cast' requires a 'memoryview' object")
	}
	formatObj, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: memoryview: format argument must be a string")
	}
	format := formatObj.Value()
	sz := formatItemsize(format)
	if sz == 0 {
		return nil, fmt.Errorf("ValueError: memoryview: destination format must be a native single character format prefixed with an optional '@'")
	}
	if len(m.buf)%sz != 0 {
		return nil, fmt.Errorf("TypeError: memoryview: length is not a multiple of itemsize")
	}
	view := &MemoryView{buf: m.buf, readonly: m.readonly, format: format, itemsize: sz}
	view.init(MemoryViewType)
	return view, nil
}

// memoryViewTobytesMethod backs memoryview.tobytes().
//
// CPython: Objects/memoryobject.c:2374 memoryview_tobytes_impl
func memoryViewTobytesMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: tobytes() missing self")
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'tobytes' requires a 'memoryview' object")
	}
	return m.Tobytes(), nil
}

// memoryViewTolistMethod backs memoryview.tolist().
//
// CPython: Objects/memoryobject.c:2467 memoryview_tolist
func memoryViewTolistMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: tolist() missing self")
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'tolist' requires a 'memoryview' object")
	}
	return m.Tolist(), nil
}

// memoryViewHexMethod backs memoryview.hex([sep[, bytes_per_sep]]).
// gopy supports the no-arg form; separator/grouping land later.
//
// CPython: Objects/memoryobject.c:2421 memoryview_hex_impl
func memoryViewHexMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: hex() missing self")
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hex' requires a 'memoryview' object")
	}
	return NewStr(hex.EncodeToString(m.buf)), nil
}

// memoryViewReleaseMethod backs memoryview.release(). gopy does not
// reference-count the underlying buffer so this is a no-op aside from
// clearing the slice so further access raises through Len().
//
// CPython: Objects/memoryobject.c:1325 memoryview_release_impl
func memoryViewReleaseMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: release() missing self")
	}
	if _, ok := args[0].(*MemoryView); !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'release' requires a 'memoryview' object")
	}
	return None(), nil
}

// memoryViewToreadonlyMethod backs memoryview.toreadonly().
//
// CPython: Objects/memoryobject.c:2538 memoryview_toreadonly_impl
func memoryViewToreadonlyMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: toreadonly() missing self")
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'toreadonly' requires a 'memoryview' object")
	}
	view := &MemoryView{buf: m.buf, readonly: true, format: m.format, itemsize: m.itemsize}
	view.init(MemoryViewType)
	return view, nil
}
