// The ByteArray type, a mutable byte string. CPython exposes the
// same byte-level operations as PyBytes plus in-place mutation
// (append, extend, insert, pop, ...) and an unhashable instance.
//
// CPython: Objects/bytearrayobject.c:2654 PyByteArray_Type

package objects

import (
	"errors"
	"fmt"
	"strings"
)

// ByteArray is the mutable byte string. Mirrors PyByteArrayObject.
//
// CPython: Include/cpython/bytearrayobject.h:13 PyByteArrayObject
type ByteArray struct {
	VarHeader
	v []byte
}

// ByteArrayType is the type singleton for bytearray.
//
// CPython: Objects/bytearrayobject.c:2654 PyByteArray_Type
var ByteArrayType = NewType("bytearray", []*Type{objectType})

func init() {
	ByteArrayType.Repr = byteArrayRepr
	ByteArrayType.Str = byteArrayRepr
	ByteArrayType.Hash = byteArrayHash
	ByteArrayType.RichCmp = byteArrayRichCmp
	ByteArrayType.TpFlags |= TpFlagMatchSelf
	ByteArrayType.Sequence = &SequenceMethods{
		Length:        byteArrayLen,
		GetItem:       byteArrayGetItem,
		SetItem:       byteArraySetItem,
		Contains:      byteArrayContains,
		Concat:        byteArrayConcat,
		Repeat:        byteArrayRepeat,
		InPlaceConcat: byteArrayIConcat,
		InPlaceRepeat: byteArrayIRepeat,
	}
	// Mapping slot covers int / slice subscript dispatch. CPython
	// routes b[k] through bytearray_subscript; without a Mapping slot
	// slice keys would fall back to the generic sliceSequence helper
	// that rewraps results as a list.
	//
	// CPython: Objects/bytearrayobject.c:478 bytearray_subscript_lock_held
	ByteArrayType.Mapping = &MappingMethods{
		Length:  byteArrayLen,
		GetItem: byteArraySubscript,
	}
	// Slot wrappers added so attribute lookup finds the dunder methods.
	//
	// CPython: Objects/typeobject.c add_operators slotdefs (sq_item,
	// sq_concat, sq_repeat, sq_inplace_concat, sq_inplace_repeat,
	// sq_length, mp_ass_subscript).
	SetTypeDescr(ByteArrayType, "__getitem__", NewMethodDescr(ByteArrayType, "__getitem__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
			}
			return byteArraySubscript(args[0], args[1])
		},
	))
	SetTypeDescr(ByteArrayType, "__len__", NewMethodDescr(ByteArrayType, "__len__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
			}
			n, err := byteArrayLen(args[0])
			if err != nil {
				return nil, err
			}
			return NewInt(int64(n)), nil
		},
	))
	SetTypeDescr(ByteArrayType, "__add__", NewMethodDescr(ByteArrayType, "__add__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __add__() takes exactly one argument (%d given)", len(args)-1)
			}
			return byteArrayConcat(args[0], args[1])
		},
	))
	SetTypeDescr(ByteArrayType, "__iadd__", NewMethodDescr(ByteArrayType, "__iadd__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __iadd__() takes exactly one argument (%d given)", len(args)-1)
			}
			return byteArrayIConcat(args[0], args[1])
		},
	))
	SetTypeDescr(ByteArrayType, "__mul__", NewMethodDescr(ByteArrayType, "__mul__",
		byteArrayMulMethod,
	))
	SetTypeDescr(ByteArrayType, "__rmul__", NewMethodDescr(ByteArrayType, "__rmul__",
		byteArrayMulMethod,
	))
	SetTypeDescr(ByteArrayType, "__imul__", NewMethodDescr(ByteArrayType, "__imul__",
		byteArrayIMulMethod,
	))
}

func byteArrayMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __mul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return NotImplemented(), nil
	}
	n, ok := idx.(*Int)
	if !ok {
		return NotImplemented(), nil
	}
	v, ok := n.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return byteArrayRepeat(args[0], int(v))
}

func byteArrayIMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __imul__() takes exactly one argument (%d given)", len(args)-1)
	}
	idx, err := NumberIndex(args[1])
	if err != nil {
		return NotImplemented(), nil
	}
	n, ok := idx.(*Int)
	if !ok {
		return NotImplemented(), nil
	}
	v, ok := n.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return byteArrayIRepeat(args[0], int(v))
}

// byteArraySubscript ports bytearray_subscript: int keys return the
// byte value as an int, slice keys return a fresh bytearray.
//
// CPython: Objects/bytearrayobject.c:478 bytearray_subscript_lock_held
func byteArraySubscript(o, key Object) (Object, error) {
	b := o.(*ByteArray)
	if sl, ok := key.(*Slice); ok {
		start, _, step, slicelen, err := sl.GetIndices(len(b.v))
		if err != nil {
			return nil, err
		}
		if slicelen <= 0 {
			return NewByteArray(nil), nil
		}
		if step == 1 {
			return NewByteArray(b.v[start : start+slicelen]), nil
		}
		out := make([]byte, slicelen)
		for i, cur := 0, start; i < slicelen; i, cur = i+1, cur+step {
			out[i] = b.v[cur]
		}
		return NewByteArray(out), nil
	}
	idx, err := indexValueAsInt(key, "bytearray")
	if err != nil {
		return nil, err
	}
	return byteArrayGetItem(o, idx)
}

// NewByteArray wraps a copy of buf in a ByteArray.
//
// CPython: Objects/bytearrayobject.c:138 PyByteArray_FromStringAndSize
func NewByteArray(buf []byte) *ByteArray {
	b := &ByteArray{v: append([]byte(nil), buf...)}
	b.init(ByteArrayType)
	b.size = int64(len(buf))
	return b
}

// Bytes returns the underlying mutable buffer. Callers may mutate
// the returned slice; doing so updates the ByteArray in place.
func (b *ByteArray) Bytes() []byte {
	return b.v
}

// Len returns the number of bytes.
//
// CPython: Objects/bytearrayobject.c:62 PyByteArray_Size
func (b *ByteArray) Len() int {
	return len(b.v)
}

// Append puts byte v at the tail. v must be in [0, 255].
//
// CPython: Objects/bytearrayobject.c:2243 bytearray_append
func (b *ByteArray) Append(v int) error {
	if v < 0 || v > 255 {
		return errors.New("ValueError: byte must be in range(0, 256)")
	}
	b.v = append(b.v, byte(v))
	b.size = int64(len(b.v))
	return nil
}

// Extend concatenates the bytes from src.
//
// CPython: Objects/bytearrayobject.c:2299 bytearray_extend
func (b *ByteArray) Extend(src []byte) {
	b.v = append(b.v, src...)
	b.size = int64(len(b.v))
}

// Insert places v at position where, shifting later bytes right.
// Negative where is clamped to 0; where past the end appends.
//
// CPython: Objects/bytearrayobject.c:2206 bytearray_insert
func (b *ByteArray) Insert(where int, v int) error {
	if v < 0 || v > 255 {
		return errors.New("ValueError: byte must be in range(0, 256)")
	}
	n := len(b.v)
	if where < 0 {
		where += n
		if where < 0 {
			where = 0
		}
	}
	if where > n {
		where = n
	}
	b.v = append(b.v, 0)
	copy(b.v[where+1:], b.v[where:])
	b.v[where] = byte(v)
	b.size = int64(len(b.v))
	return nil
}

// Pop removes and returns the byte at index i. Default index in
// CPython is -1; the gopy port leaves that decision to the caller.
//
// CPython: Objects/bytearrayobject.c:2267 bytearray_pop
func (b *ByteArray) Pop(i int) (int, error) {
	n := len(b.v)
	if n == 0 {
		return 0, errors.New("IndexError: pop from empty bytearray")
	}
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return 0, errors.New("IndexError: pop index out of range")
	}
	v := b.v[i]
	b.v = append(b.v[:i], b.v[i+1:]...)
	b.size = int64(len(b.v))
	return int(v), nil
}

// Clear truncates the buffer to zero length.
//
// CPython: Objects/bytearrayobject.c:2191 bytearray_clear
func (b *ByteArray) Clear() {
	b.v = b.v[:0]
	b.size = 0
}

// Reverse reverses bytes in place.
//
// CPython: Objects/bytearrayobject.c:2226 bytearray_reverse
func (b *ByteArray) Reverse() {
	for i, j := 0, len(b.v)-1; i < j; i, j = i+1, j-1 {
		b.v[i], b.v[j] = b.v[j], b.v[i]
	}
}

// byteArrayRepr formats as bytearray(b'...'), reusing bytesRepr for
// the inner literal so quoting and escaping match exactly.
//
// CPython: Objects/bytearrayobject.c:2566 bytearray_repr
func byteArrayRepr(o Object) (string, error) {
	inner, err := bytesRepr(NewBytes(o.(*ByteArray).v))
	if err != nil {
		return "", err
	}
	return "bytearray(" + inner + ")", nil
}

// byteArrayHash always raises TypeError. bytearray is mutable and
// therefore unhashable, matching CPython.
//
// CPython: Objects/bytearrayobject.c:1031 bytearray_hash
func byteArrayHash(o Object) (int64, error) {
	return 0, errors.New("TypeError: unhashable type: 'bytearray'")
}

// byteArrayRichCmp compares against bytes and bytearray operands by
// byte value. Mixed compares with anything else fall through to
// NotImplemented.
//
// CPython: Objects/bytearrayobject.c:1058 bytearray_richcompare
func byteArrayRichCmp(a, b Object, op CompareOp) (Object, error) {
	ab, ok := a.(*ByteArray)
	if !ok {
		return notImplemented(), nil
	}
	var rhs []byte
	switch x := b.(type) {
	case *ByteArray:
		rhs = x.v
	case *Bytes:
		rhs = x.v
	default:
		return notImplemented(), nil
	}
	c := bytesCompare(ab.v, rhs)
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

func byteArrayLen(o Object) (int, error) {
	return o.(*ByteArray).Len(), nil
}

func byteArrayGetItem(o Object, i int) (Object, error) {
	b := o.(*ByteArray)
	if i < 0 {
		i += len(b.v)
	}
	if i < 0 || i >= len(b.v) {
		return nil, errIndexOutOfRange
	}
	return NewInt(int64(b.v[i])), nil
}

// byteArraySetItem assigns a single byte. The new value must be an
// int in [0, 255]; CPython also accepts a one-byte buffer there but
// that path isn't ported yet.
//
// CPython: Objects/bytearrayobject.c:529 bytearray_ass_subscript
func byteArraySetItem(o Object, i int, v Object) error {
	b := o.(*ByteArray)
	if i < 0 {
		i += len(b.v)
	}
	if i < 0 || i >= len(b.v) {
		return errIndexOutOfRange
	}
	iv, ok := v.(*Int)
	if !ok {
		return fmt.Errorf("TypeError: an integer is required")
	}
	n, fits := iv.Int64()
	if !fits || n < 0 || n > 255 {
		return errors.New("ValueError: byte must be in range(0, 256)")
	}
	b.v[i] = byte(n)
	return nil
}

// bytesLikeBuf returns the byte view of a bytes-like operand for
// sq_concat / sq_inplace_concat. Mirrors PyObject_GetBuffer(PyBUF_SIMPLE)
// gating: only bytes and bytearray pass; everything else raises the
// CPython TypeError that bytearray_iconcat_lock_held would format.
//
// CPython: Objects/bytearrayobject.c:354 bytearray_iconcat_lock_held
// (PyObject_GetBuffer call)
func bytesLikeBuf(o Object) ([]byte, bool) {
	switch v := o.(type) {
	case *Bytes:
		return v.Bytes(), true
	case *ByteArray:
		return v.v, true
	}
	return nil, false
}

// byteArrayConcat ports PyByteArray_Concat: returns a fresh bytearray
// holding a ++ b. Both arguments must satisfy the buffer protocol; in
// this slice we accept bytes/bytearray.
//
// CPython: Objects/bytearrayobject.c:303 PyByteArray_Concat
func byteArrayConcat(a, b Object) (Object, error) {
	va, ok := bytesLikeBuf(a)
	if !ok {
		return nil, fmt.Errorf("TypeError: can't concat %s to %s", b.Type().Name, a.Type().Name)
	}
	vb, ok := bytesLikeBuf(b)
	if !ok {
		return nil, fmt.Errorf("TypeError: can't concat %s to %s", b.Type().Name, a.Type().Name)
	}
	out := make([]byte, 0, len(va)+len(vb))
	out = append(out, va...)
	out = append(out, vb...)
	return NewByteArray(out), nil
}

// byteArrayIConcat ports bytearray_iconcat: appends other's bytes to
// self in place and returns self.
//
// CPython: Objects/bytearrayobject.c:348 bytearray_iconcat_lock_held
func byteArrayIConcat(a, b Object) (Object, error) {
	self, ok := a.(*ByteArray)
	if !ok {
		return nil, fmt.Errorf("TypeError: bytearray.__iadd__ requires bytearray, got %s", a.Type().Name)
	}
	vb, ok := bytesLikeBuf(b)
	if !ok {
		return nil, fmt.Errorf("TypeError: can't concat %s to %s", b.Type().Name, a.Type().Name)
	}
	self.v = append(self.v, vb...)
	self.size = int64(len(self.v))
	return self, nil
}

// byteArrayRepeat ports bytearray_repeat: returns a fresh bytearray
// holding self repeated n times. Negative n returns empty.
//
// CPython: Objects/bytearrayobject.c:387 bytearray_repeat_lock_held
func byteArrayRepeat(o Object, n int) (Object, error) {
	self := o.(*ByteArray)
	if n < 0 {
		n = 0
	}
	size := len(self.v) * n
	out := make([]byte, size)
	for i := 0; i < n; i++ {
		copy(out[i*len(self.v):], self.v)
	}
	return NewByteArray(out), nil
}

// byteArrayIRepeat ports bytearray_irepeat: grows self in place to
// hold n copies of its current bytes. n==1 is a no-op.
//
// CPython: Objects/bytearrayobject.c:419 bytearray_irepeat_lock_held
func byteArrayIRepeat(o Object, n int) (Object, error) {
	self := o.(*ByteArray)
	if n < 0 {
		n = 0
	}
	if n == 1 {
		return self, nil
	}
	mysize := len(self.v)
	size := mysize * n
	out := make([]byte, size)
	for i := 0; i < n; i++ {
		copy(out[i*mysize:], self.v)
	}
	self.v = out
	self.size = int64(len(self.v))
	return self, nil
}

func byteArrayContains(o, v Object) (bool, error) {
	b := o.(*ByteArray)
	switch x := v.(type) {
	case *Int:
		n, ok := x.Int64()
		if !ok || n < 0 || n > 255 {
			return false, errors.New("ValueError: byte must be in range(0, 256)")
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
	case *ByteArray:
		if len(x.v) == 0 {
			return true, nil
		}
		return strings.Contains(string(b.v), string(x.v)), nil
	}
	return false, errors.New("TypeError: a bytes-like object is required")
}
