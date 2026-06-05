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
	"fmt"
	"math"
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
	// contiguous is false once a strided slice (step != 1) detaches the
	// view from a single run of bytes. A non-contiguous view cannot back a
	// writable buffer request, matching PyBUF_C_CONTIGUOUS in CPython.
	contiguous bool
	// exporter is the bytearray this view was opened on, if any. The
	// view holds one of the bytearray's ob_exports for its lifetime so
	// the bytearray cannot be re-sized while the view is live; release()
	// (or GC) drops it.
	//
	// CPython: Objects/memoryobject.c mbuf_release / bytearray_releasebuffer
	exporter *ByteArray
	// obj is the object this view was opened on (CPython's view->obj). It is
	// kept so memory_hash can pin the view across the underlying object's
	// own hash (gh-142664) and so a re-entrant release during that hash is
	// rejected. nil for a view that owns its bytes outright.
	//
	// CPython: Objects/memoryobject.c:3235 memory_hash (view->obj)
	obj Object
	// exports counts the buffers handed out of this view. A release() while
	// exports > 0 raises BufferError, matching memoryview_release_impl. The
	// hash path bumps it for the duration of the underlying object's hash so
	// a re-entrant release() inside that hash is turned into a BufferError.
	//
	// CPython: Objects/memoryobject.c:1131 memoryview_release_impl (get_exports)
	exports  int
	released bool
	// hash caches the result of the first successful hash(). Like CPython
	// (and weakrefs), the cached value survives release(): hashing a view
	// for the first time after release raises, but a view hashed while live
	// keeps returning the stored value.
	//
	// CPython: Objects/memoryobject.c:3213 memory_hash (self->hash)
	hashValue int64
	hashSet   bool
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
	// PyMemoryView_Type carries Py_TPFLAGS_HAVE_GC. Its tp_traverse visits the
	// managed buffer, whose own traverse reaches the object the buffer was
	// taken from, so a reference cycle running through a memoryview is
	// collectable. gopy flattens the managed buffer into the obj/exporter
	// fields, so the view traverse visits them directly.
	//
	// CPython: Objects/memoryobject.c:1164 memory_traverse / :131 mbuf_traverse
	MemoryViewType.TpTraverse = memoryViewTraverse
	MemoryViewType.Getattro = memoryViewGetattr
	// CPython tags PyMemoryView_Type with Py_TPFLAGS_SEQUENCE so
	// match-statement sequence patterns decompose memoryview through
	// the buffer protocol.
	//
	// CPython: Objects/memoryobject.c:3597 PyMemoryView_Type tp_flags
	MemoryViewType.TpFlags |= TpFlagSequence
	// memoryview wraps a Py_buffer with no __dict__ or __slots__, so the
	// default pickler cannot capture its state; CPython's
	// object_getstate_default raises "cannot pickle 'memoryview' object"
	// because tp_basicsize exceeds the accountable base size.
	//
	// CPython: Objects/typeobject.c:7363 object_getstate_default
	MemoryViewType.OpaqueCState = true
	MemoryViewType.Sequence = &SequenceMethods{
		Length:   memoryViewLen,
		GetItem:  memoryViewGetItem,
		Contains: memoryViewContains,
	}
	MemoryViewType.Mapping = &MappingMethods{
		Length:  memoryViewLen,
		GetItem: memoryViewGetItemKey,
		SetItem: memoryViewSetItemKey,
	}
	// memoryview is weak-referenceable (test_weakref) and participates in
	// cyclic GC through its exporter, matching PyMemoryView_Type.
	//
	// CPython: Objects/memoryobject.c:3582 PyMemoryView_Type tp_weaklistoffset
	MemoryViewType.HasWeakref = true

	SetTypeDescr(MemoryViewType, "cast", NewMethodDescr(MemoryViewType, "cast", memoryViewCastMethod))
	SetTypeDescr(MemoryViewType, "tobytes", NewMethodDescr(MemoryViewType, "tobytes", memoryViewTobytesMethod))
	SetTypeDescr(MemoryViewType, "tolist", NewMethodDescr(MemoryViewType, "tolist", memoryViewTolistMethod))
	SetTypeDescr(MemoryViewType, "hex", NewMethodDescr(MemoryViewType, "hex", memoryViewHexMethod))
	SetTypeDescr(MemoryViewType, "release", NewMethodDescr(MemoryViewType, "release", memoryViewReleaseMethod))
	SetTypeDescr(MemoryViewType, "toreadonly", NewMethodDescr(MemoryViewType, "toreadonly", memoryViewToreadonlyMethod))
	// Expose the tp_hash slot as memoryview.__hash__ so an explicit
	// mv.__hash__() call routes through memory_hash rather than falling back
	// to object.__hash__ (the identity hash). The use-after-free guard lives
	// in memory_hash, so the explicit call must reach it.
	//
	// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __hash__)
	SetTypeDescr(MemoryViewType, "__hash__", NewMethodDescr(MemoryViewType, "__hash__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
		}
		m, ok := args[0].(*MemoryView)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor '__hash__' requires a 'memoryview' object")
		}
		h, err := memoryViewHash(m)
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	SetTypeDescr(MemoryViewType, "count", NewMethodDescr(MemoryViewType, "count", memoryViewCountMethod))
	SetTypeDescr(MemoryViewType, "index", NewMethodDescr(MemoryViewType, "index", memoryViewIndexMethod))
	SetTypeDescr(MemoryViewType, "__enter__", NewMethodDescr(MemoryViewType, "__enter__", memoryViewEnterMethod))
	SetTypeDescr(MemoryViewType, "__exit__", NewMethodDescr(MemoryViewType, "__exit__", memoryViewExitMethod))
}

// initView finalizes a freshly built memoryview: it stamps the type and hands
// the view to the cyclic collector. PyMemoryView_FromObject ends with
// PyObject_GC_Track, so a view that participates in a reference cycle (for
// example mv.obj is a list holding mv) can be reclaimed.
//
// CPython: Objects/memoryobject.c:1041 PyMemoryView_FromObject (PyObject_GC_Track)
func (m *MemoryView) initView() {
	m.init(MemoryViewType)
	if h := GCTrackHook; h != nil {
		h(m)
	}
}

// memoryViewTraverse visits the object the view was opened on and its
// bytearray exporter so a cycle through either is reachable by the collector.
// CPython reaches these through the managed buffer (memory_traverse visits
// self->mbuf, mbuf_traverse visits self->master.obj); gopy stores them on the
// view directly.
//
// CPython: Objects/memoryobject.c:1164 memory_traverse / :131 mbuf_traverse
func memoryViewTraverse(o Object, visit Visitor) error {
	m := o.(*MemoryView)
	if m.obj != nil {
		if err := visit(m.obj); err != nil {
			return err
		}
	}
	if m.exporter != nil {
		if err := visit(m.exporter); err != nil {
			return err
		}
	}
	return nil
}

// errReleased is the ValueError every accessor raises once the view has been
// released. CPython gates each entry point with CHECK_RELEASED, which sets
// this exact message.
//
// CPython: Objects/memoryobject.c:184 CHECK_RELEASED
func (m *MemoryView) checkReleased() error {
	if m.released {
		return fmt.Errorf("ValueError: operation forbidden on released memoryview object")
	}
	return nil
}

// CheckBufferReleased raises the released-buffer ValueError when o is a
// memoryview that has been released, and nil otherwise (including for
// non-memoryview objects). Buffer consumers (bytes(), bytearray()) call it to
// reproduce PyObject_GetBuffer's behavior on a released view.
//
// CPython: Objects/memoryobject.c:184 CHECK_RELEASED
func CheckBufferReleased(o Object) error {
	if m, ok := o.(*MemoryView); ok {
		return m.checkReleased()
	}
	return nil
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
	case "e":
		return 2
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
// underlying byte slice; the readonly flag follows the exporter: bytes is
// read-only, bytearray and array.array are writable. A buffer-protocol
// type outside this package (array.array) is resolved through BufferHook.
//
// CPython: Objects/memoryobject.c:1041 PyMemoryView_FromObject
func NewMemoryView(src Object) (*MemoryView, error) {
	switch s := src.(type) {
	case *Bytes:
		mv := &MemoryView{buf: s.Bytes(), readonly: true, format: "B", itemsize: 1, contiguous: true, obj: s}
		mv.initView()
		return mv, nil
	case *ByteArray:
		mv := &MemoryView{buf: s.Bytes(), readonly: false, format: "B", itemsize: 1, contiguous: true, exporter: s, obj: s}
		s.ExportInc()
		mv.initView()
		return mv, nil
	case *MemoryView:
		mv := &MemoryView{buf: s.buf, readonly: s.readonly, format: s.format, itemsize: s.itemsize, contiguous: s.contiguous, obj: s.obj}
		mv.initView()
		return mv, nil
	}
	if BufferHook != nil {
		if bi, ok := BufferHook(src); ok {
			itemsize := bi.Itemsize
			if itemsize == 0 {
				itemsize = 1
			}
			format := bi.Format
			if format == "" {
				format = "B"
			}
			mv := &MemoryView{buf: bi.Buf, readonly: bi.Readonly, format: format, itemsize: itemsize, contiguous: true, obj: src}
			mv.initView()
			return mv, nil
		}
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
	case "e":
		return NewFloat(halfToDouble(binary.NativeEndian.Uint16(chunk)))
	case "f":
		return NewFloat(float64(math.Float32frombits(binary.NativeEndian.Uint32(chunk))))
	case "d":
		return NewFloat(math.Float64frombits(binary.NativeEndian.Uint64(chunk)))
	}
	return NewInt(int64(chunk[0]))
}

// halfToDouble decodes an IEEE 754 half-precision (16-bit) pattern to a double.
// bits is the logical 16-bit value already read in native byte order.
//
// CPython: Objects/floatobject.c:2379 PyFloat_Unpack2
func halfToDouble(bits uint16) float64 {
	sign := (bits >> 15) & 1
	e := int((bits >> 10) & 0x1f)
	f := uint(bits & 0x3ff)
	if e == 0x1f {
		if f == 0 {
			if sign != 0 {
				return math.Inf(-1)
			}
			return math.Inf(1)
		}
		v := uint64(0x7ff0000000000000)
		if sign != 0 {
			v = 0xfff0000000000000
		}
		v += uint64(f) << 42 // add NaN's type & payload
		return math.Float64frombits(v)
	}
	x := float64(f) / 1024.0
	if e == 0 {
		e = -14
	} else {
		x += 1.0
		e -= 15
	}
	x = math.Ldexp(x, e)
	if sign != 0 {
		x = -x
	}
	return x
}

// halfFromDouble encodes x as an IEEE 754 half-precision (16-bit) pattern with
// round-to-even, reporting overflow when a finite value is too large to fit.
//
// CPython: Objects/floatobject.c:1993 PyFloat_Pack2
func halfFromDouble(x float64) (bits uint16, overflow bool) {
	var sign uint16
	var e int
	switch {
	case x == 0.0:
		if math.Signbit(x) {
			sign = 1
		}
		e = 0
		bits = 0
	case math.IsInf(x, 0):
		if x < 0.0 {
			sign = 1
		}
		e = 0x1f
		bits = 0
	case math.IsNaN(x):
		if math.Signbit(x) {
			sign = 1
		}
		e = 0x1f
		v := math.Float64bits(x)
		v &= 0xffc0000000000
		bits = uint16(v >> 42) // NaN's type and payload
		if bits == 0 {
			bits |= 1 << 9 // set qNaN if no payload
		}
	default:
		if x < 0.0 {
			sign = 1
			x = -x
		}
		f, exp := math.Frexp(x)
		f *= 2.0
		e = exp - 1
		switch {
		case e >= 16:
			return 0, true
		case e < -25:
			f = 0.0
			e = 0
		case e < -14:
			f = math.Ldexp(f, 14+e)
			e = 0
		default:
			e += 15
			f -= 1.0
		}
		f *= 1024.0 // 2**10
		bits = uint16(f)
		if (f-float64(bits) > 0.5) || ((f-float64(bits) == 0.5) && (bits%2 == 1)) {
			bits++
			if bits == 1024 {
				bits = 0
				e++
				if e == 31 {
					return 0, true
				}
			}
		}
	}
	bits |= uint16(e<<10) | (sign << 15)
	return bits, false
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
	for i := range n {
		items[i] = m.readItem(i)
	}
	return NewList(items)
}

func memoryViewLen(o Object) (int, error) {
	m := o.(*MemoryView)
	if err := m.checkReleased(); err != nil {
		return 0, err
	}
	return m.Len(), nil
}

func memoryViewGetItem(o Object, i int) (Object, error) {
	m := o.(*MemoryView)
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
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
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	// An index-like key (int or anything with __index__) addresses a single
	// item. Resolving __index__ may run arbitrary code that releases the view
	// (gh-92888), so memory_item re-checks released; memoryViewGetItem does the
	// same on the resolved integer.
	//
	// CPython: Objects/memoryobject.c:2612 memory_subscript (_PyIndex_Check)
	if IndexCheck(key) {
		idx, err := NumberIndex(key)
		if err != nil {
			return nil, err
		}
		i, ok := idx.(*Int).Int64()
		if !ok {
			return nil, fmt.Errorf("IndexError: cannot fit '%s' into an index", key.Type().Name)
		}
		return memoryViewGetItem(o, int(i))
	}
	switch k := key.(type) {
	case *Slice:
		start, stop, step, n, err := k.GetIndices(m.Len())
		if err != nil {
			return nil, err
		}
		if step != 1 {
			out := make([]byte, n*m.itemsize)
			for i := range n {
				src := (start + i*step) * m.itemsize
				copy(out[i*m.itemsize:], m.buf[src:src+m.itemsize])
			}
			view := &MemoryView{buf: out, readonly: m.readonly, format: m.format, itemsize: m.itemsize, contiguous: false}
			view.initView()
			return view, nil
		}
		view := &MemoryView{
			buf:        m.buf[start*m.itemsize : stop*m.itemsize],
			readonly:   m.readonly,
			contiguous: m.contiguous,
			format:     m.format,
			itemsize:   m.itemsize,
		}
		view.initView()
		return view, nil
	case *Tuple:
		// A multi-index tuple addresses a single item; a multi-slice tuple
		// is multi-dimensional slicing. gopy is 1-D, so resolve the indices
		// (re-checking release per gh-92888) and read that item.
		//
		// CPython: Objects/memoryobject.c:2627 memory_subscript (is_multiindex)
		if multiIndexTuple(k) {
			off, err := resolveMultiIndex(m, k)
			if err != nil {
				return nil, err
			}
			return memoryViewGetItem(o, off)
		}
		if multiSliceTuple(k) {
			return nil, fmt.Errorf("NotImplementedError: multi-dimensional slicing is not implemented")
		}
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

// memoryViewRepr renders the view. Both the live and released forms still
// work (the released form is what _check_released relies on).
//
// CPython: Objects/memoryobject.c:2383 memory_repr
func memoryViewRepr(o Object) (string, error) {
	m := o.(*MemoryView)
	if m.released {
		return fmt.Sprintf("<released memory at %p>", m), nil
	}
	return fmt.Sprintf("<memory at %p>", m), nil
}

// memoryViewHash hashes a read-only byte-format view, caching the result.
// The cached value survives release; hashing a view for the first time after
// release raises, exactly like weakrefs.
//
// CPython: Objects/memoryobject.c:3213 memory_hash
func memoryViewHash(o Object) (int64, error) {
	m := o.(*MemoryView)
	if m.hashSet {
		return m.hashValue, nil
	}
	if err := m.checkReleased(); err != nil {
		return 0, err
	}
	if !m.readonly {
		return 0, fmt.Errorf("ValueError: cannot hash writable memoryview object")
	}
	// Hashing is restricted to the byte formats 'B', 'b' and 'c'; a view cast
	// to a wider format cannot be hashed.
	switch m.format {
	case "B", "b", "c", "":
	default:
		return 0, fmt.Errorf("ValueError: memoryview: hashing is restricted to formats 'B', 'b' or 'c'")
	}
	if m.obj != nil {
		// Hash the exporter so its own __hash__ runs (an unhashable exporter
		// such as bytearray raises TypeError here). Bumping exports first
		// pins the view: a re-entrant __hash__ that calls release() sees a
		// non-zero export count and raises BufferError instead of freeing the
		// buffer mid-hash (gh-142664). The result is discarded; the cached
		// hash comes from the buffer bytes below.
		//
		// CPython: Objects/memoryobject.c:3235 memory_hash
		m.exports++
		_, err := Hash(m.obj)
		m.exports--
		if err != nil {
			return 0, err
		}
	}
	m.hashValue = HashBytes(m.buf)
	m.hashSet = true
	return m.hashValue, nil
}

// asComparableBuffer wraps o in a temporary view so a richcompare can decode
// its items. ok is false when o does not export a buffer (a str, for example),
// which CPython surfaces as NotImplemented from memory_richcompare.
//
// CPython: Objects/memoryobject.c:3106 memory_richcompare (PyObject_GetBuffer)
func asComparableBuffer(o Object) (*MemoryView, bool) {
	mv, err := NewMemoryView(o)
	if err != nil {
		return nil, false
	}
	return mv, true
}

// memoryViewRichCmp compares two views (or a view and a bytes-like object)
// for equality. A released view on either side compares equal only to itself,
// and views differing in item count are unequal.
//
// CPython: Objects/memoryobject.c:3106 memory_richcompare
func memoryViewRichCmp(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	ma := a.(*MemoryView)
	// A released view (on either side) only equals itself by identity.
	if ma.released {
		return boolFromEqual(a == b, op), nil
	}
	if mb, ok := b.(*MemoryView); ok && mb.released {
		return boolFromEqual(a == b, op), nil
	}
	// The right operand is compared through its own buffer: wrap it in a
	// temporary view so its format decodes items the same way the left view
	// does. A non-buffer object (str) yields NotImplemented.
	mb, ok := asComparableBuffer(b)
	if !ok {
		return NotImplemented(), nil
	}
	// equiv_shape: differing item counts compare unequal without decoding.
	//
	// CPython: Objects/memoryobject.c:307 equiv_shape
	eq := ma.Len() == mb.Len()
	if eq {
		// Items are compared by decoded value, never memcmp, so '?' views
		// b'\2' and b'\4' both decode to True and compare equal.
		//
		// CPython: Objects/memoryobject.c:3106 memory_richcompare (unpack_cmp)
		for i := 0; i < ma.Len(); i++ {
			itemEq, cerr := RichCmpBool(ma.readItem(i), mb.readItem(i), CompareEQ)
			if cerr != nil {
				return nil, cerr
			}
			if !itemEq {
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

// boolFromEqual maps an equality result to True/False for the EQ/NE operator.
func boolFromEqual(equal bool, op CompareOp) Object {
	if op == CompareNE {
		return NewBool(!equal)
	}
	return NewBool(equal)
}

// ByteBufferHook extends AsBytesLike to buffer-protocol types outside
// the objects package (e.g. array.array). Set at module init time.
var ByteBufferHook func(Object) ([]byte, bool)

// BufferInfo carries the result of a buffer-protocol request on an object
// implemented outside the objects package, mirroring the fields of a
// Py_buffer the memoryview machinery cares about.
//
// CPython: Include/pybuffer.h Py_buffer
type BufferInfo struct {
	Buf      []byte
	Readonly bool
	Format   string
	Itemsize int
}

// BufferHook resolves a buffer-protocol object outside the objects package
// (array.array) into its live buffer info. Set at module init time.
var BufferHook func(Object) (BufferInfo, bool)

// AsWritableBuffer unwraps o to a live, writable, contiguous byte slice,
// or reports false. It is the gopy equivalent of PyObject_GetBuffer with
// PyBUF_WRITABLE: read-only exporters (bytes), non-contiguous views, and
// non-buffer objects are rejected.
//
// CPython: Objects/abstract.c:341 PyObject_GetBuffer (PyBUF_WRITABLE)
func AsWritableBuffer(o Object) ([]byte, bool) {
	switch v := o.(type) {
	case *ByteArray:
		return v.Bytes(), true
	case *MemoryView:
		if v.readonly || !v.contiguous {
			return nil, false
		}
		return v.buf, true
	case *Bytes:
		return nil, false
	}
	if BufferHook != nil {
		if bi, ok := BufferHook(o); ok && !bi.Readonly {
			return bi.Buf, true
		}
	}
	return nil, false
}

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
	// The buffer-derived getset members all raise once released; the methods
	// (release, __exit__, etc.) and dunders fall through to GenericGetAttr.
	//
	// CPython: Objects/memoryobject.c memory_*_get (CHECK_RELEASED)
	switch n.v {
	case "format", "itemsize", "nbytes", "readonly", "ndim", "shape", "strides", "suboffsets", "c_contiguous", "f_contiguous", "contiguous", "obj":
		if err := m.checkReleased(); err != nil {
			return nil, err
		}
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
	memoryViewIterType.Iter = SelfIter
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
	if err := m.checkReleased(); err != nil {
		return nil, err
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
	view := &MemoryView{buf: m.buf, readonly: m.readonly, format: format, itemsize: sz, contiguous: m.contiguous}
	view.initView()
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
	if err := m.checkReleased(); err != nil {
		return nil, err
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
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	return m.Tolist(), nil
}

// memoryViewHexMethod backs memoryview.hex([sep[, bytes_per_sep]]). The
// separator byte and grouping width follow bytes.hex; sep is validated
// through PyObject_Length, so a re-entrant sep.__len__ that releases the
// view or resizes the exporter is observed and turned into a BufferError
// by bumping the exporter's export count for the duration of the call.
//
// CPython: Objects/memoryobject.c:2345 memoryview_hex_impl
func memoryViewHexMethod(args []Object, kwargs map[string]Object) (Object, error) {
	args, err := bindKwargs("hex", args, kwargs, "sep", "bytes_per_sep")
	if err != nil {
		return nil, err
	}
	if len(args) < 1 || len(args) > 3 {
		return nil, arityRangeErr("hex", 0, 2, len(args)-1)
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hex' requires a 'memoryview' object")
	}
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	var sep byte
	hasSep := false
	bytesPerSep := 1
	if len(args) >= 2 && args[1] != nil {
		// Prevent the view (and its exporter) from being freed while
		// _Py_strhex_with_sep computes len(sep): a mutation inside that
		// __len__ raises BufferError.
		//
		// CPython: Objects/memoryobject.c:2356 memoryview_hex_impl (self->exports++)
		unlock := m.lockExporter()
		s, perr := hexSepByte(args[1])
		unlock()
		if perr != nil {
			return nil, perr
		}
		sep, hasSep = s, true
	}
	if len(args) == 3 && args[2] != nil && args[2] != None() {
		bytesPerSep, err = bytesIntArg(args, 2, "hex", 1)
		if err != nil {
			return nil, err
		}
	}
	return NewStr(hexEncode(m.buf, sep, hasSep, bytesPerSep)), nil
}

// lockExporter bumps the export count of the bytearray this view was opened
// on (if any) so a callback running mid-operation cannot resize or clear it,
// returning the matching unlock. It is the gopy stand-in for the self->exports++
// CPython uses to pin a view across a re-entrant separator length probe.
//
// CPython: Objects/memoryobject.c:2356 memoryview_hex_impl (self->exports++)
func (m *MemoryView) lockExporter() func() {
	if m.exporter != nil {
		m.exporter.ExportInc()
		return m.exporter.ExportDec
	}
	return func() {}
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
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'release' requires a 'memoryview' object")
	}
	// A view with live exported buffers cannot be released; CPython raises
	// BufferError rather than freeing memory another consumer still holds
	// (this is also what turns a re-entrant release() during hash() into a
	// BufferError, gh-142664).
	//
	// CPython: Objects/memoryobject.c:1138 memoryview_release_impl
	if m.exports > 0 {
		plural := "s"
		if m.exports == 1 {
			plural = ""
		}
		return nil, fmt.Errorf("BufferError: memoryview has %d exported buffer%s", m.exports, plural)
	}
	// Drop the export held on the source bytearray (idempotent: a
	// second release() is a no-op in CPython too).
	//
	// CPython: Objects/memoryobject.c:1325 memoryview_release_impl
	if !m.released {
		m.released = true
		if m.exporter != nil {
			m.exporter.ExportDec()
		}
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
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	view := &MemoryView{buf: m.buf, readonly: true, format: m.format, itemsize: m.itemsize, contiguous: m.contiguous, obj: m.obj}
	view.initView()
	return view, nil
}

// typeErrorInt / valueErrorInt are the two pack_single failure messages.
// fix_error_int maps a conversion error: a TypeError becomes type_error_int,
// an OverflowError/ValueError becomes value_error_int.
//
// CPython: Objects/memoryobject.c:1643 type_error_int / :1651 value_error_int
func typeErrorInt(format string) error {
	return fmt.Errorf("TypeError: memoryview: invalid type for format '%s'", format)
}

func valueErrorInt(format string) error {
	return fmt.Errorf("ValueError: memoryview: invalid value for format '%s'", format)
}

// packInt writes the already-indexed value idx into ptr for one of the
// fixed-width integer formats, raising value_error_int when the value falls
// outside the format's representable range. The signed formats reject !ok
// (a value that does not fit int64); 'Q' reaches for the big.Int so it can
// accept the full unsigned 64-bit range.
//
// CPython: Objects/memoryobject.c:1894 pack_single (integer formats)
// intFormatRange describes the inclusive int64 range and byte width of one
// fixed-width integer struct format. The low N bytes of v's two's-complement
// representation are exactly the value to store, so a single width switch
// handles both signed and unsigned formats once the range check passes.
type intFormatRange struct {
	min, max int64
	width    int
}

var intFormatRanges = map[string]intFormatRange{
	"B": {0, 255, 1},
	"b": {-128, 127, 1},
	"h": {-32768, 32767, 2},
	"H": {0, 65535, 2},
	"i": {-2147483648, 2147483647, 4},
	"l": {-2147483648, 2147483647, 4},
	"n": {-2147483648, 2147483647, 4},
	"I": {0, 4294967295, 4},
	"L": {0, 4294967295, 4},
	"N": {0, 4294967295, 4},
	"q": {math.MinInt64, math.MaxInt64, 8},
}

func packInt(ptr []byte, format string, idx *Int) error {
	// Q accepts the full unsigned 64-bit range; Int64 reports !ok for values
	// above math.MaxInt64, so reach for the big.Int directly.
	if format == "Q" {
		big := idx.BigInt()
		if big.Sign() < 0 || !big.IsUint64() {
			return valueErrorInt(format)
		}
		binary.NativeEndian.PutUint64(ptr, big.Uint64())
		return nil
	}
	// Every other format fits in int64; a value that does not even fit there
	// is always out of range.
	v, ok := idx.Int64()
	if !ok {
		return valueErrorInt(format)
	}
	r := intFormatRanges[format]
	if v < r.min || v > r.max {
		return valueErrorInt(format)
	}
	switch r.width {
	case 1:
		ptr[0] = byte(v)
	case 2:
		binary.NativeEndian.PutUint16(ptr, uint16(v))
	case 4:
		binary.NativeEndian.PutUint32(ptr, uint32(v))
	case 8:
		binary.NativeEndian.PutUint64(ptr, uint64(v))
	}
	return nil
}

// packFloat writes the double d into ptr for one of the floating formats:
// 'e' (IEEE half via PyFloat_Pack2, which overflows to value_error_int),
// 'f' (single) and 'd' (double).
//
// CPython: Objects/memoryobject.c:1894 pack_single (float formats)
func packFloat(ptr []byte, format string, d float64) error {
	switch format {
	case "e":
		bits, overflow := halfFromDouble(d)
		if overflow {
			return valueErrorInt(format)
		}
		binary.NativeEndian.PutUint16(ptr, bits)
	case "f":
		binary.NativeEndian.PutUint32(ptr, math.Float32bits(float32(d)))
	case "d":
		binary.NativeEndian.PutUint64(ptr, math.Float64bits(d))
	}
	return nil
}

// packSingle converts item to bytes per format and writes it into m.buf at
// byte offset off. Integer formats go through NumberIndex (_PyNumber_Index),
// floats through PyNumberFloat (PyFloat_AsDouble), '?' through IsTruthy, and
// 'c' takes a single-byte bytes object. Out-of-range integers raise the
// value_error_int message; a non-convertible item raises type_error_int.
//
// CPython: Objects/memoryobject.c:1880 pack_single
func packSingle(m *MemoryView, off int, item Object) error {
	format := m.format
	if format == "" {
		format = "B"
	}
	ptr := m.buf[off : off+m.itemsize]
	switch format {
	case "b", "B", "h", "H", "i", "I", "l", "L", "q", "Q", "n", "N":
		idx, err := NumberIndex(item)
		if err != nil {
			return typeErrorInt(format)
		}
		// _PyNumber_Index can run a __index__ that releases the view
		// (gh-92888); pack_single re-checks before writing the slot.
		//
		// CPython: Objects/memoryobject.c:1905 pack_single (CHECK_RELEASED_INT_AGAIN)
		if err := m.checkReleased(); err != nil {
			return err
		}
		return packInt(ptr, format, idx.(*Int))
	case "e", "f", "d":
		d, err := PyNumberFloat(item)
		if err != nil {
			return typeErrorInt(format)
		}
		// PyFloat_AsDouble can run a __float__ that releases the view
		// (gh-92888); pack_single re-checks before writing the slot.
		//
		// CPython: Objects/memoryobject.c:1905 pack_single (CHECK_RELEASED_INT_AGAIN)
		if err := m.checkReleased(); err != nil {
			return err
		}
		return packFloat(ptr, format, d)
	case "?":
		t, err := IsTruthy(item)
		if err != nil {
			return err
		}
		if err := m.checkReleased(); err != nil {
			return err
		}
		if t {
			ptr[0] = 1
		} else {
			ptr[0] = 0
		}
		return nil
	case "c":
		b, ok := item.(*Bytes)
		if !ok {
			return typeErrorInt(format)
		}
		if len(b.Bytes()) != 1 {
			return valueErrorInt(format)
		}
		ptr[0] = b.Bytes()[0]
		return nil
	}
	return fmt.Errorf("NotImplementedError: memoryview: format %s not supported", format)
}

// memoryViewSetItemKey backs memoryview.__setitem__. It packs an integer key
// into a single item slot, or copies a step-1 slice of equal structure from an
// rvalue exporter. Strided slices and multi-dimensional keys raise the same
// NotImplementedError CPython reports.
//
// CPython: Objects/memoryobject.c:2650 memory_ass_sub
func memoryViewSetItemKey(o, key, value Object) error {
	m := o.(*MemoryView)
	if err := m.checkReleased(); err != nil {
		return err
	}
	if m.readonly {
		return fmt.Errorf("TypeError: cannot modify read-only memory")
	}
	if value == nil {
		return fmt.Errorf("TypeError: cannot delete memory")
	}
	// An index-like key (int or anything with __index__) writes a single
	// item. Resolving __index__ may release the view (gh-92888), so re-check
	// before computing the slot.
	//
	// CPython: Objects/memoryobject.c:2685 memory_ass_sub (_PyIndex_Check)
	if IndexCheck(key) {
		return setItemIndex(m, key, value)
	}
	switch k := key.(type) {
	case *Slice:
		return setItemSlice(m, k, value)
	case *Tuple:
		// A tuple of indices addresses a single element (is_multiindex); a
		// tuple of slices is a multi-dimensional slice (is_multislice). gopy
		// is 1-D, so anything else is an invalid key.
		//
		// CPython: Objects/memoryobject.c:2727 memory_ass_sub (tuple branch)
		if multiIndexTuple(k) {
			// ptr_from_tuple resolves each element through __index__ before
			// indexing. gopy models only ndim == 1, so resolve the elements
			// (running any side effects, e.g. a __index__ that releases the
			// view per gh-92888), re-check released, then apply the 1-D rules.
			//
			// CPython: Objects/memoryobject.c:2431 ptr_from_tuple
			off, err := resolveMultiIndex(m, k)
			if err != nil {
				return err
			}
			return packSingle(m, off*m.itemsize, value)
		}
		if multiSliceTuple(k) {
			return fmt.Errorf("NotImplementedError: memoryview slice assignments are currently restricted to ndim = 1")
		}
		return fmt.Errorf("TypeError: memoryview: invalid slice key")
	}
	return fmt.Errorf("TypeError: memoryview: invalid slice key")
}

// setItemIndex writes value into the single slot addressed by an index-like
// key. NumberIndex runs the key's __index__, which can release the view
// (gh-92888), so the view is re-checked before the slot offset is computed.
//
// CPython: Objects/memoryobject.c:2685 memory_ass_sub (_PyIndex_Check branch)
func setItemIndex(m *MemoryView, key, value Object) error {
	idx, err := NumberIndex(key)
	if err != nil {
		return err
	}
	if err := m.checkReleased(); err != nil {
		return err
	}
	i, ok := idx.(*Int).Int64()
	if !ok {
		return fmt.Errorf("IndexError: cannot fit '%s' into an index", key.Type().Name)
	}
	n := m.Len()
	off := int(i)
	if off < 0 {
		off += n
	}
	if off < 0 || off >= n {
		return fmt.Errorf("IndexError: index out of bounds on dimension 1")
	}
	return packSingle(m, off*m.itemsize, value)
}

// setItemSlice copies a step-1 slice assignment from a bytes-like rvalue of
// identical structure (same itemsize, format and item count). init_slice
// resolves the slice bounds through __index__, which can release the view
// (gh-92888), so the view is re-checked before the buffer is touched. The
// copy goes through a temporary so an overlapping source and destination
// (m[0:3] = m[2:5]) behaves like memmove.
//
// CPython: Objects/memoryobject.c:2698 memory_ass_sub (slice branch)
func setItemSlice(m *MemoryView, k *Slice, value Object) error {
	start, stop, step, n, err := k.GetIndices(m.Len())
	if err != nil {
		return err
	}
	// CPython: Objects/memoryobject.c:407 copy_single (CHECK_RELEASED_INT_AGAIN)
	if err := m.checkReleased(); err != nil {
		return err
	}
	if step != 1 {
		return fmt.Errorf("NotImplementedError: memoryview slice assignments are currently restricted to ndim = 1")
	}
	// CPython: Objects/memoryobject.c:328 equiv_structure
	src, ok := AsBytesLike(value)
	if !ok {
		return fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", value.Type().Name)
	}
	srcItemsize := m.itemsize
	if sv, ok := value.(*MemoryView); ok {
		srcItemsize = sv.itemsize
		if sv.format != m.format {
			return errMemoryViewStructure
		}
	}
	if srcItemsize != m.itemsize || len(src) != n*m.itemsize {
		return errMemoryViewStructure
	}
	// CPython: Objects/memoryobject.c:340 copy_base (memmove path)
	tmp := make([]byte, len(src))
	copy(tmp, src)
	copy(m.buf[start*m.itemsize:stop*m.itemsize], tmp)
	return nil
}

// resolveMultiIndex resolves a multi-index tuple to a single 1-D item offset.
// Each element is run through __index__ first (so side effects such as a
// __index__ that releases the view are observed), then the view is re-checked
// for release before the dimension count is validated. gopy carries only
// ndim == 1, so a tuple of any size other than one is rejected exactly as
// ptr_from_tuple rejects an over-long tuple.
//
// CPython: Objects/memoryobject.c:2431 ptr_from_tuple
func resolveMultiIndex(m *MemoryView, t *Tuple) (int, error) {
	indices := make([]int64, t.Len())
	for i := 0; i < t.Len(); i++ {
		idx, err := NumberIndex(t.Item(i))
		if err != nil {
			return 0, err
		}
		v, ok := idx.(*Int).Int64()
		if !ok {
			return 0, fmt.Errorf("IndexError: cannot fit '%s' into an index", t.Item(i).Type().Name)
		}
		indices[i] = v
	}
	if err := m.checkReleased(); err != nil {
		return 0, err
	}
	if t.Len() != 1 {
		return 0, fmt.Errorf("TypeError: cannot index 1-dimension view with %d-element tuple", t.Len())
	}
	n := m.Len()
	off := int(indices[0])
	if off < 0 {
		off += n
	}
	if off < 0 || off >= n {
		return 0, fmt.Errorf("IndexError: index out of bounds on dimension 1")
	}
	return off, nil
}

// multiIndexTuple reports whether every element of t is index-like, matching
// is_multiindex (an all-integer tuple addresses a single element).
//
// CPython: Objects/memoryobject.c:2564 is_multiindex
func multiIndexTuple(t *Tuple) bool {
	for i := 0; i < t.Len(); i++ {
		if !IndexCheck(t.Item(i)) {
			return false
		}
	}
	return true
}

// multiSliceTuple reports whether t is a non-empty tuple of slices, matching
// is_multislice (a multi-dimensional slice).
//
// CPython: Objects/memoryobject.c:2545 is_multislice
func multiSliceTuple(t *Tuple) bool {
	if t.Len() == 0 {
		return false
	}
	for i := 0; i < t.Len(); i++ {
		if _, ok := t.Item(i).(*Slice); !ok {
			return false
		}
	}
	return true
}

// errMemoryViewStructure is raised when a slice assignment's lvalue and rvalue
// do not share itemsize, format and item count.
//
// CPython: Objects/memoryobject.c:331 equiv_structure
var errMemoryViewStructure = fmt.Errorf("ValueError: memoryview assignment: lvalue and rvalue have different structures")

// memoryViewCountMethod backs memoryview.count(value): the number of items
// equal to value, compared through PyObject_RichCompareBool(Py_EQ).
//
// CPython: Objects/memoryobject.c:2793 memoryview_count_impl
func memoryViewCountMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: count() takes exactly one argument (%d given)", len(args)-1)
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'count' requires a 'memoryview' object")
	}
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	count := 0
	n := m.Len()
	for i := range n {
		eq, err := RichCmpBool(m.readItem(i), args[1], CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			count++
		}
	}
	return NewInt(int64(count)), nil
}

// memoryViewIndexMethod backs memoryview.index(value[, start[, stop]]):
// the index of the first item equal to value within [start, stop), raising
// ValueError when absent.
//
// CPython: Objects/memoryobject.c:2847 memoryview_index_impl
func memoryViewIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 4 {
		return nil, fmt.Errorf("TypeError: index() takes at most 3 arguments (%d given)", len(args)-1)
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'index' requires a 'memoryview' object")
	}
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	n := m.Len()
	start := 0
	stop := n
	if len(args) >= 3 {
		s, err := sliceIndexArg(args[2])
		if err != nil {
			return nil, err
		}
		start = s
	}
	if len(args) == 4 {
		s, err := sliceIndexArg(args[3])
		if err != nil {
			return nil, err
		}
		stop = s
	}
	if start < 0 {
		if start += n; start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		if stop += n; stop < 0 {
			stop = 0
		}
	}
	if stop > n {
		stop = n
	}
	if start > stop {
		start = stop
	}
	for i := start; i < stop; i++ {
		eq, err := RichCmpBool(m.readItem(i), args[1], CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			return NewInt(int64(i)), nil
		}
	}
	return nil, fmt.Errorf("ValueError: memoryview.index(x): x not found")
}

// sliceIndexArg resolves a start/stop argument to an int through __index__,
// matching the slice_index converter the index() clinic signature uses.
//
// CPython: Objects/memoryobject.c:2837 memoryview.index (slice_index)
func sliceIndexArg(o Object) (int, error) {
	idx, err := NumberIndex(o)
	if err != nil {
		return 0, err
	}
	v, ok := idx.(*Int).Int64()
	if !ok {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(v), nil
}

// memoryViewEnterMethod backs memoryview.__enter__: it returns the view so a
// with-statement binds it, after confirming it is not already released.
//
// CPython: Objects/memoryobject.c:1356 memory_enter
func memoryViewEnterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __enter__() missing self")
	}
	m, ok := args[0].(*MemoryView)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__enter__' requires a 'memoryview' object")
	}
	if err := m.checkReleased(); err != nil {
		return nil, err
	}
	return m, nil
}

// memoryViewExitMethod backs memoryview.__exit__: it releases the view and
// returns None so exceptions propagate.
//
// CPython: Objects/memoryobject.c:1364 memory_exit
func memoryViewExitMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __exit__() missing self")
	}
	if _, err := memoryViewReleaseMethod(args[:1], nil); err != nil {
		return nil, err
	}
	return None(), nil
}
