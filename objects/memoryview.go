// MemoryView is the Python memoryview type: a read-only window over a
// bytes-like object's underlying buffer. CPython models this through
// the PEP 3118 buffer protocol and supports format codes, strides,
// suboffsets, and multi-dim shapes; the v0.10 gopy port covers the
// common case (1-D contiguous unsigned bytes), which is what user
// code wrapping bytes / bytearray actually exercises. Multi-format
// and writable views land with 1689-B once a Buffer slot exists on
// Type.
//
// Indexing returns an int (byte value). Slicing returns a new
// MemoryView aliasing the same underlying slice (so cheap; no copy).
// len, .format ("B"), .itemsize (1), .nbytes, and .readonly mirror
// the names CPython exposes.
//
// CPython: Objects/memoryobject.c:3402 PyMemoryView_Type

package objects

import (
	"fmt"
)

// MemoryView is a 1-D contiguous view over a byte buffer.
//
// CPython: Objects/memoryobject.c:3402 PyMemoryView_Type
type MemoryView struct {
	Header

	buf      []byte
	readonly bool
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
	MemoryViewType.Sequence = &SequenceMethods{
		Length:   memoryViewLen,
		GetItem:  memoryViewGetItem,
		Contains: memoryViewContains,
	}
	MemoryViewType.Mapping = &MappingMethods{
		Length:  memoryViewLen,
		GetItem: memoryViewGetItemKey,
	}
}

// NewMemoryView wraps src in a memoryview. The view aliases the
// underlying byte slice; callers can read but the readonly flag
// determines whether the type can later expose mutation. Bytes get a
// read-only view; bytearray is technically writable but v0.10 keeps
// every view read-only.
//
// CPython: Objects/memoryobject.c:1041 PyMemoryView_FromObject
func NewMemoryView(src Object) (*MemoryView, error) {
	switch s := src.(type) {
	case *Bytes:
		mv := &MemoryView{buf: s.Bytes(), readonly: true}
		mv.init(MemoryViewType)
		return mv, nil
	case *ByteArray:
		// ByteArray's underlying buffer can grow; the alias is still
		// safe for read access today because Bytes() returns the live
		// slice and our slicing path only takes new sub-slices.
		mv := &MemoryView{buf: s.Bytes(), readonly: true}
		mv.init(MemoryViewType)
		return mv, nil
	case *MemoryView:
		mv := &MemoryView{buf: s.buf, readonly: s.readonly}
		mv.init(MemoryViewType)
		return mv, nil
	}
	return nil, fmt.Errorf("TypeError: memoryview: a bytes-like object is required, not '%s'", src.Type().Name)
}

// Bytes returns the underlying byte slice. The slice is the live
// buffer, not a copy: matches CPython's PyMemoryView_GetContiguous
// for the trivial 1-D unsigned-byte case.
func (m *MemoryView) Bytes() []byte { return m.buf }

// Len reports the number of items (bytes, in the v0.10 port).
func (m *MemoryView) Len() int { return len(m.buf) }

// Tobytes returns a fresh Bytes copy of the view.
//
// CPython: Objects/memoryobject.c:2374 memoryview_tobytes_impl
func (m *MemoryView) Tobytes() *Bytes {
	out := make([]byte, len(m.buf))
	copy(out, m.buf)
	return NewBytes(out)
}

// Tolist returns the view as a list of int byte values.
//
// CPython: Objects/memoryobject.c:2467 memoryview_tolist
func (m *MemoryView) Tolist() *List {
	items := make([]Object, len(m.buf))
	for i, b := range m.buf {
		items[i] = NewInt(int64(b))
	}
	return NewList(items)
}

func memoryViewLen(o Object) (int, error) { return o.(*MemoryView).Len(), nil }

func memoryViewGetItem(o Object, i int) (Object, error) {
	m := o.(*MemoryView)
	if i < 0 {
		i += len(m.buf)
	}
	if i < 0 || i >= len(m.buf) {
		return nil, errIndexOutOfRange
	}
	return NewInt(int64(m.buf[i])), nil
}

// memoryViewGetItemKey is the mapping-shaped GetItem dispatch: it
// accepts an Int (byte index, returns int) or a Slice (returns a new
// memoryview over the sub-slice). Routes integer keys through the
// sequence path so behavior stays in one place.
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
		start, stop, step, n, err := k.GetIndices(len(m.buf))
		if err != nil {
			return nil, err
		}
		if step != 1 {
			out := make([]byte, n)
			for i := 0; i < n; i++ {
				out[i] = m.buf[start+i*step]
			}
			view := &MemoryView{buf: out, readonly: m.readonly}
			view.init(MemoryViewType)
			return view, nil
		}
		view := &MemoryView{buf: m.buf[start:stop], readonly: m.readonly}
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
func bytesViewOf(o Object) ([]byte, bool) {
	switch v := o.(type) {
	case *MemoryView:
		return v.buf, true
	case *Bytes:
		return v.Bytes(), true
	case *ByteArray:
		return v.Bytes(), true
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
		return NewStr("B"), nil
	case "itemsize":
		return NewInt(1), nil
	case "nbytes":
		return NewInt(int64(len(m.buf))), nil
	case "readonly":
		return NewBool(m.readonly), nil
	case "ndim":
		return NewInt(1), nil
	case "shape":
		return NewTuple([]Object{NewInt(int64(len(m.buf)))}), nil
	case "strides":
		return NewTuple([]Object{NewInt(1)}), nil
	case "suboffsets":
		return NewTuple(nil), nil
	case "c_contiguous", "f_contiguous", "contiguous":
		return NewBool(true), nil
	case "obj":
		return None(), nil
	}
	return nil, fmt.Errorf("AttributeError: 'memoryview' object has no attribute '%s'", n.v)
}

// memoryViewIterator yields one int per byte.
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
		if it.pos >= len(it.src.buf) {
			return nil, ErrStopIteration
		}
		v := NewInt(int64(it.src.buf[it.pos]))
		it.pos++
		return v, nil
	}
}

func memoryViewIter(o Object) (Object, error) {
	it := &memoryViewIterator{src: o.(*MemoryView)}
	it.init(memoryViewIterType)
	return it, nil
}
