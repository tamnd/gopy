// The Bytes type, a Python bytes object. CPython stores the byte
// payload inline through PyVarObject; the gopy port carries a Go
// []byte. Method panel (replace, decode, split, ...) is split out to
// bytes_methods_wrap.go in a follow-up task.
//
// CPython: Objects/bytesobject.c:3134 PyBytes_Type

package objects

import (
	"fmt"
	"strings"
)

// Bytes is the immutable byte string. Mirrors PyBytesObject.
//
// CPython: Include/cpython/bytesobject.h:7 PyBytesObject
type Bytes struct {
	VarHeader
	v []byte
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
	BytesType.TpFlags |= TpFlagMatchSelf
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
// CPython: Objects/bytesobject.c:2893 bytes_mod
func bytesModulo(a, b Object) (Object, error) {
	bv, ok := a.(*Bytes)
	if !ok {
		return notImplemented(), nil
	}
	// Decode the format bytes as latin-1. For bytes formatting only ASCII
	// format specs are meaningful, so latin-1 round-trips safely.
	fmtStr := NewStr(string(bv.v))
	// Delegate to the str % implementation.
	result, err := unicodeModulo(fmtStr, b)
	if err != nil {
		return nil, err
	}
	u, ok2 := result.(*Unicode)
	if !ok2 {
		return result, nil
	}
	// Encode the result as latin-1 bytes.
	out := make([]byte, len(u.v))
	for i, r := range u.v {
		if r > 0xff {
			return nil, fmt.Errorf("ValueError: %%b requires a bytes-like object, or an object that implements __bytes__, not 'str'")
		}
		out[i] = byte(r)
	}
	return NewBytes(out), nil
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
	ba, ok := a.(*Bytes)
	if !ok {
		return nil, fmt.Errorf("TypeError: can't concat %s to bytes", a.Type().Name)
	}
	bb, ok := b.(*Bytes)
	if !ok {
		return nil, fmt.Errorf("TypeError: can't concat %s to bytes", b.Type().Name)
	}
	out := make([]byte, 0, len(ba.v)+len(bb.v))
	out = append(out, ba.v...)
	out = append(out, bb.v...)
	return NewBytes(out), nil
}

// bytesRepeat ports bytes_repeat. b * n returns the concatenation of n
// copies; non-positive n returns the empty-bytes singleton.
//
// CPython: Objects/bytesobject.c:1184 bytes_repeat
func bytesRepeat(o Object, n int) (Object, error) {
	b := o.(*Bytes)
	if n <= 0 || len(b.v) == 0 {
		return emptyBytes, nil
	}
	if n == 1 {
		return b, nil
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

func bytesGetItem(o Object, i int) (Object, error) {
	b := o.(*Bytes)
	if i < 0 {
		i += len(b.v)
	}
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
	b := o.(*Bytes)
	switch x := v.(type) {
	case *Int:
		n, ok := x.Int64()
		if !ok || n < 0 || n > 255 {
			return false, fmt.Errorf("ValueError: byte must be in range(0, 256)")
		}
		for _, c := range b.v {
			if int64(c) == n {
				return true, nil
			}
		}
		return false, nil
	case *Bytes:
		if len(x.v) == 0 {
			return true, nil
		}
		return strings.Contains(string(b.v), string(x.v)), nil
	}
	return false, fmt.Errorf("TypeError: a bytes-like object is required")
}
