// The Bytes type, a Python bytes object. CPython stores the byte
// payload inline through PyVarObject; the gopy port carries a Go
// []byte. Method panel (replace, decode, split, ...) is split out to
// bytes_methods_wrap.go in a follow-up task.
//
// CPython: Objects/bytesobject.c:3134 PyBytes_Type

package objects

import (
	"fmt"
	"math"
	"strings"
)

// Bytes is the immutable byte string. Mirrors PyBytesObject.
//
// CPython: Include/cpython/bytesobject.h:7 PyBytesObject
type Bytes struct {
	VarHeader
	v []byte
	// attrs holds instance attributes for bytes subclass objects. Nil for
	// plain bytes; allocated lazily by EnsureAttrDict. Mirrors the
	// tp_dictoffset a bytes subclass picks up from object.
	attrs *Dict
}

// AttrDict / EnsureAttrDict implement AttrDictHolder so bytes subclasses can
// carry instance attributes through generic_attr's HasDict path.
//
// CPython: Objects/object.c _PyObject_GetDictPtr (bytes-subclass path)
func (b *Bytes) AttrDict() *Dict { return b.attrs }

// EnsureAttrDict allocates the attrs dict on first use.
func (b *Bytes) EnsureAttrDict() *Dict {
	if b.attrs == nil {
		b.attrs = NewDict()
		trackAttrDictHolder(b)
	}
	return b.attrs
}

// NewBytesSubtype wraps a copy of buf in a Bytes object bound to a bytes
// subclass. Used by tp_new when the requested type is a proper subclass of
// bytes (bytes_subtype_new), so the instance reports the subclass type and
// can hold instance attributes.
//
// CPython: Objects/bytesobject.c:3055 bytes_subtype_new
func NewBytesSubtype(cls *Type, buf []byte) *Bytes {
	b := &Bytes{v: append([]byte(nil), buf...)}
	b.init(cls)
	b.size = int64(len(buf))
	return b
}

// BytesType is the type singleton for bytes. Mirrors PyBytes_Type.
//
// CPython: Objects/bytesobject.c:3134 PyBytes_Type
var BytesType = NewType("bytes", []*Type{objectType})

func init() {
	BytesType.Repr = bytesRepr
	BytesType.Str = bytesRepr
	BytesType.Hash = bytesHash
	BytesType.RichCmp = bytesRichCmp
	// CPython: Objects/bytesobject.c:3158 PyBytes_Type.tp_richcompare slot wrapper
	BindRichCmpDescriptors(BytesType)
	BytesType.TpFlags |= TpFlagMatchSelf
	// A bytes subclass can declare __dict__, so its instances participate
	// in reference cycles through their attributes. Subtypes inherit this
	// tp_traverse, and EnsureAttrDict gc-tracks the instance on first
	// store, so the collector can reclaim such cycles.
	//
	// CPython: Objects/typeobject.c:1356 subtype_traverse
	BytesType.TpTraverse = attrDictHolderTraverse
	// CPython: Objects/bytesobject.c bytes_doc (tp_doc)
	SetTypeDescr(BytesType, "__doc__", NewStr("bytes(iterable_of_ints) -> bytes\n"+
		"bytes(string, encoding[, errors]) -> bytes\n"+
		"bytes(bytes_or_buffer) -> immutable copy of bytes_or_buffer\n"+
		"bytes(int) -> bytes object of size given by the parameter initialized with null bytes\n"+
		"bytes() -> empty bytes object\n"+
		"\n"+
		"Construct an immutable array of bytes from:\n"+
		"  - an iterable yielding integers in range(256)\n"+
		"  - a text string encoded using the specified encoding\n"+
		"  - any object implementing the buffer API.\n"+
		"  - an integer"))
	BytesType.Sequence = &SequenceMethods{
		Length:   bytesLen,
		Concat:   bytesConcat,
		Repeat:   bytesRepeat,
		GetItem:  bytesGetItem,
		Contains: bytesContains,
	}
	// Mapping slot covers the int / slice subscript dispatch that
	// CPython routes through bytes_subscript. Without it, slice keys
	// fall back to a generic sliceSequence helper that rewraps the
	// result as a list.
	//
	// CPython: Objects/bytesobject.c:1635 bytes_subscript
	BytesType.Mapping = &MappingMethods{
		Length:  bytesLen,
		GetItem: bytesSubscript,
	}
	BytesType.Iter = bytesIter
	// PEP 461: bytes % args formatting.
	// CPython: Objects/bytesobject.c:2893 bytes_mod
	BytesType.Number = &NumberMethods{
		Remainder: bytesModulo,
	}
	// CPython: Objects/typeobject.c add_operators slotdefs tp_iter row
	AddIterSlotWrappers(BytesType)
	// Slot wrappers for sequence dunders. add_operators on the C side
	// installs these from sq_item/sq_concat/sq_repeat; gopy mirrors them
	// directly so attribute lookup finds bytes.__getitem__ etc.
	//
	// CPython: Objects/typeobject.c add_operators slotdefs (sq_item,
	// sq_concat, sq_repeat).
	SetTypeDescr(BytesType, "__getitem__", NewMethodDescr(BytesType, "__getitem__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
			}
			return bytesSubscript(args[0], args[1])
		},
	))
	SetTypeDescr(BytesType, "__len__", NewMethodDescr(BytesType, "__len__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
			}
			n, err := bytesLen(args[0])
			if err != nil {
				return nil, err
			}
			return NewInt(int64(n)), nil
		},
	))
	// bytes.__repr__ / __str__ slot wrappers. CPython exposes tp_repr via
	// add_operators -> slotdefs so a bytes subclass finds bytes_repr in the
	// MRO instead of inheriting object.__repr__'s `<addr object>` format.
	//
	// CPython: Objects/typeobject.c slotdefs (TPSLOT __repr__ + PyBytes_Type.tp_repr)
	SetTypeDescr(BytesType, "__repr__", NewMethodDescr(BytesType, "__repr__", bytesReprDescr))
	SetTypeDescr(BytesType, "__str__", NewMethodDescr(BytesType, "__str__", bytesReprDescr))
	SetTypeDescr(BytesType, "__add__", NewMethodDescr(BytesType, "__add__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __add__() takes exactly one argument (%d given)", len(args)-1)
			}
			return bytesConcat(args[0], args[1])
		},
	))
	SetTypeDescr(BytesType, "__mul__", NewMethodDescr(BytesType, "__mul__",
		bytesMulMethod,
	))
	SetTypeDescr(BytesType, "__rmul__", NewMethodDescr(BytesType, "__rmul__",
		bytesMulMethod,
	))
	SetTypeDescr(BytesType, "__mod__", NewMethodDescr(BytesType, "__mod__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __mod__() takes exactly one argument (%d given)", len(args)-1)
			}
			return bytesModulo(args[0], args[1])
		},
	))
	SetTypeDescr(BytesType, "__rmod__", NewMethodDescr(BytesType, "__rmod__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __rmod__() takes exactly one argument (%d given)", len(args)-1)
			}
			// a.__rmod__(b) computes b % a; bytes_mod requires the left
			// operand to be bytes, so a non-bytes lhs yields NotImplemented.
			return bytesModulo(args[1], args[0])
		},
	))
	// PEP 688: bf_getbuffer surfaced as __buffer__(flags) -> memoryview.
	// CPython: Objects/typeobject.c:9737 wrap_buffer (PyBytes_Type.tp_as_buffer)
	SetTypeDescr(BytesType, "__buffer__", NewMethodDescr(BytesType, "__buffer__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __buffer__() takes exactly one argument (%d given)", len(args)-1)
			}
			if _, ok := args[0].(*Bytes); !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__buffer__' requires a 'bytes' object")
			}
			if _, err := indexAsInt(args[1]); err != nil {
				return nil, err
			}
			return NewMemoryView(args[0])
		},
	))
}

func bytesMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return when other can't be coerced to an index
	}
	n, ok := idx.(*Int)
	if !ok {
		return NotImplemented(), nil
	}
	v, ok := n.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return bytesRepeat(args[0], int(v))
}

// bytesModulo implements bytes % args (PEP 461).
//
// CPython: Objects/bytesobject.c:2724 bytes_mod
func bytesModulo(a, b Object) (Object, error) {
	bv, ok := a.(*Bytes)
	if !ok {
		return notImplemented(), nil
	}
	return bytesFormat(bv.v, b, false)
}

// bytesSubscript ports bytes_subscript: integer keys return the byte
// value as an int, slice keys return a fresh bytes object. The slice
// path honors step and skips a copy when the slice covers the whole
// buffer at step 1.
//
// CPython: Objects/bytesobject.c:1635 bytes_subscript
func bytesSubscript(o, key Object) (Object, error) {
	b := o.(*Bytes)
	if sl, ok := key.(*Slice); ok {
		start, _, step, slicelen, err := sl.GetIndices(len(b.v))
		if err != nil {
			return nil, err
		}
		if slicelen <= 0 {
			return emptyBytes, nil
		}
		if step == 1 {
			return NewBytes(b.v[start : start+slicelen]), nil
		}
		out := make([]byte, slicelen)
		for i, cur := 0, start; i < slicelen; i, cur = i+1, cur+step {
			out[i] = b.v[cur]
		}
		return NewBytes(out), nil
	}
	idx, err := indexValueAsInt(key, "byte")
	if err != nil {
		return nil, err
	}
	// bytes_subscript adjusts a negative subscript against the length before
	// indexing; the sq_item slot (bytesGetItem) does not.
	//
	// CPython: Objects/bytesobject.c:1635 bytes_subscript
	if idx < 0 {
		idx += len(b.v)
	}
	return bytesGetItem(o, idx)
}

// bytesIterator yields the byte values of a bytes object as ints.
//
// CPython: Objects/bytesobject.c:3196 PyBytesIter_Type
type bytesIterator struct {
	Header
	src *Bytes
	pos int
}

var bytesIterType = NewType("bytes_iterator", []*Type{objectType})

func init() {
	bytesIterType.Iter = SelfIter
	bytesIterType.IterNext = bytesIterNext
	AddIterSlotWrappers(bytesIterType)
	// CPython: Objects/bytesobject.c:2858 bytes_iter_reduce
	SetTypeDescr(bytesIterType, "__reduce__", NewMethodDescr(bytesIterType, "__reduce__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*bytesIterator)
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.src == nil {
				return NewTuple([]Object{iterFn, NewTuple([]Object{NewTuple(nil)})}), nil
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{it.src}), NewInt(int64(it.pos))}), nil
		},
	))
	// CPython: Objects/bytesobject.c:2875 bytes_iter_setstate
	SetTypeDescr(bytesIterType, "__setstate__", NewMethodDescr(bytesIterType, "__setstate__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*bytesIterator)
			idx, ok := args[1].(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__ requires int argument")
			}
			n := int64(0)
			if it.src != nil {
				n = int64(len(it.src.v))
			}
			v, _ := idx.Int64()
			if v < 0 {
				v = 0
			} else if v > n {
				v = n
			}
			it.pos = int(v)
			return None(), nil
		},
	))
}

func bytesIter(o Object) (Object, error) {
	b := o.(*Bytes)
	it := &bytesIterator{src: b, pos: 0}
	it.init(bytesIterType)
	return it, nil
}

func bytesIterNext(o Object) (Object, error) {
	it := o.(*bytesIterator)
	if it.src == nil || it.pos >= len(it.src.v) {
		it.src = nil
		return nil, ErrStopIteration
	}
	v := it.src.v[it.pos]
	it.pos++
	return NewInt(int64(v)), nil
}

// bytesConcat ports bytes_concat.
//
// CPython: Objects/bytesobject.c:1147 bytes_concat
func bytesConcat(a, b Object) (Object, error) {
	// bytes_concat works on the buffer protocol for both operands, so any
	// bytes-like right operand (bytearray, memoryview, ...) concatenates;
	// the result is always a plain bytes object.
	av, aok := AsBytesLike(a)
	bv, bok := AsBytesLike(b)
	if !aok || !bok {
		return nil, fmt.Errorf("TypeError: can't concat %s to %s", b.Type().Name, a.Type().Name)
	}
	out := make([]byte, 0, len(av)+len(bv))
	out = append(out, av...)
	out = append(out, bv...)
	return NewBytes(out), nil
}

// bytesRepeat ports bytes_repeat. b * n returns the concatenation of n
// copies; non-positive n returns the empty-bytes singleton.
//
// CPython: Objects/bytesobject.c:1184 bytes_repeat
func bytesRepeat(o Object, n int) (Object, error) {
	b := o.(*Bytes)
	// n == 1 returns the original only for an exact bytes object; a
	// subclass instance must yield a fresh plain bytes copy so identity
	// is not preserved across the multiply.
	//
	// CPython: Objects/bytesobject.c:1184 bytes_repeat
	if n == 1 && b.Type() == BytesType {
		return b, nil
	}
	if n <= 0 || len(b.v) == 0 {
		return emptyBytes, nil
	}
	// Guard against the multiplication overflowing before the allocation
	// is attempted, matching the size-argument-may-be-negative check.
	//
	// CPython: Objects/bytesobject.c:1184 bytes_repeat
	if n > 0 && len(b.v) > (math.MaxInt-1)/n {
		return nil, fmt.Errorf("OverflowError: repeated bytes are too long")
	}
	out := make([]byte, 0, len(b.v)*n)
	for i := 0; i < n; i++ {
		out = append(out, b.v...)
	}
	return NewBytes(out), nil
}

// emptyBytes is the singleton returned by bytes() and any other path
// that produces a zero-length bytes object. CPython caches this value
// at static-init time as nullbytes.
//
// CPython: Objects/bytesobject.c:38 nullstring
var emptyBytes = func() *Bytes {
	b := &Bytes{v: []byte{}}
	b.init(BytesType)
	return b
}()

// NewBytes wraps a copy of buf in a Bytes object. An empty buf
// returns the cached singleton.
//
// CPython: Objects/bytesobject.c:120 PyBytes_FromStringAndSize
func NewBytes(buf []byte) *Bytes {
	if len(buf) == 0 {
		return emptyBytes
	}
	b := &Bytes{v: append([]byte(nil), buf...)}
	b.init(BytesType)
	b.size = int64(len(buf))
	return b
}

// NewBytesFromString wraps the bytes of s. CPython exposes this as
// PyBytes_FromString; the input is a NUL-terminated C string there
// but the gopy port takes a Go string and uses its full length.
//
// CPython: Objects/bytesobject.c:159 PyBytes_FromString
func NewBytesFromString(s string) *Bytes {
	if s == "" {
		return emptyBytes
	}
	return NewBytes([]byte(s))
}

// EmptyBytes returns the empty bytes singleton (Python b"").
//
// CPython: Objects/bytesobject.c:38 nullstring
func EmptyBytes() *Bytes {
	return emptyBytes
}

// Bytes returns the underlying byte payload. The slice is shared with
// the object; callers must not mutate it.
func (b *Bytes) Bytes() []byte {
	return b.v
}

// Len returns the number of bytes.
//
// CPython: Objects/bytesobject.c:1228 PyBytes_Size
func (b *Bytes) Len() int {
	return len(b.v)
}

// String returns the payload as a Go string. Equivalent to the Python
// expression bytes_obj.decode('latin-1').
func (b *Bytes) String() string {
	return string(b.v)
}

// bytesRepr formats the payload as b'...', preferring single quotes
// the way CPython's bytes_repr does and escaping non-printables.
//
// CPython: Objects/bytesobject.c:1418 PyBytes_Repr
// bytesReprDescr backs bytes.__dict__["__repr__"] and ["__str__"]. It
// requires a bytes (or subclass) receiver and produces the b'...' form, so
// a bytes subclass renders its payload rather than object.__repr__'s
// `<addr object>`.
func bytesReprDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	if _, ok := args[0].(*Bytes); !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'bytes' object but received a '%s'", args[0].Type().Name)
	}
	out, err := bytesRepr(args[0])
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

func bytesRepr(o Object) (string, error) {
	v := o.(*Bytes).v
	quote := byte('\'')
	if strings.IndexByte(string(v), '\'') >= 0 && strings.IndexByte(string(v), '"') < 0 {
		quote = '"'
	}
	var b strings.Builder
	b.Grow(len(v) + 3)
	b.WriteByte('b')
	b.WriteByte(quote)
	for _, c := range v {
		switch {
		case c == quote || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\t':
			b.WriteString("\\t")
		case c == '\n':
			b.WriteString("\\n")
		case c == '\r':
			b.WriteString("\\r")
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, "\\x%02x", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String(), nil
}

// bytesHash routes through the runtime hash dispatcher so two equal
// bytes objects always hash to the same value across a process run.
//
// CPython: Objects/bytesobject.c:1308 bytes_hash
func bytesHash(o Object) (int64, error) {
	return HashBytes(o.(*Bytes).v), nil
}

// bytesRichCmp implements ==, !=, and the four ordering operators
// using lexicographic byte order. Mixed-type compares return
// NotImplemented so the abstract layer can ask the other operand.
//
// CPython: Objects/bytesobject.c:1340 bytes_richcompare
func bytesRichCmp(a, b Object, op CompareOp) (Object, error) {
	ab, ok := a.(*Bytes)
	if !ok {
		return notImplemented(), nil
	}
	bb, ok := b.(*Bytes)
	if !ok {
		return notImplemented(), nil
	}
	c := bytesCompare(ab.v, bb.v)
	var res bool
	switch op {
	case CompareLT:
		res = c < 0
	case CompareLE:
		res = c <= 0
	case CompareEQ:
		res = c == 0
	case CompareNE:
		res = c != 0
	case CompareGT:
		res = c > 0
	case CompareGE:
		res = c >= 0
	}
	return NewBool(res), nil
}

// bytesCompare returns negative, zero, or positive matching strcmp
// over the two byte payloads.
//
// CPython: Objects/bytesobject.c:1340 bytes_richcompare (memcmp branch)
func bytesCompare(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func bytesLen(o Object) (int, error) {
	return o.(*Bytes).Len(), nil
}

// bytesGetItem is the sq_item slot. It bounds-checks but does NOT adjust a
// negative index: PySequence_GetItem performs that adjustment via sq_length
// before reaching the slot, so doing it again here would let, say, index -2
// on a length-1 object alias to index 0 instead of raising IndexError.
//
// CPython: Objects/bytesobject.c:1577 bytes_item
func bytesGetItem(o Object, i int) (Object, error) {
	b := o.(*Bytes)
	if i < 0 || i >= len(b.v) {
		return nil, errIndexOutOfRange
	}
	return NewInt(int64(b.v[i])), nil
}

// bytesContains implements `x in b`. The right operand can be either
// an int (single byte value) or another bytes object (substring).
//
// CPython: Objects/bytesobject.c:2900 bytes_contains
func bytesContains(o, v Object) (bool, error) {
	return bytesLikeContains(o.(*Bytes).v, v)
}

// bytesLikeContains ports _Py_bytes_contains, the shared sq_contains body
// for bytes and bytearray. The argument is first coerced through __index__
// (a byte membership test); a non-index argument falls through to a
// buffer/substring search. CPython uses PyNumber_AsSsize_t(arg, ValueError),
// so an overflowing int raises ValueError rather than wrapping.
//
// CPython: Objects/bytes_methods.c:23 _Py_bytes_contains
func bytesLikeContains(buf []byte, arg Object) (bool, error) {
	if IndexCheck(arg) {
		iv, err := NumberIndex(arg)
		if err != nil {
			if !isTypeError(err) {
				return false, err
			}
			// __index__ raised TypeError: fall through to the buffer path.
		} else {
			n, fits := iv.(*Int).Int64()
			if !fits || n < 0 || n >= 256 {
				return false, fmt.Errorf("ValueError: byte must be in range(0, 256)")
			}
			for _, c := range buf {
				if int64(c) == n {
					return true, nil
				}
			}
			return false, nil
		}
	}
	if sub, ok := asBytesLike(arg); ok {
		if len(sub) == 0 {
			return true, nil
		}
		return strings.Contains(string(buf), string(sub)), nil
	}
	return false, fmt.Errorf("TypeError: a bytes-like object is required")
}
