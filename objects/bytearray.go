// The ByteArray type, a mutable byte string. CPython exposes the
// same byte-level operations as PyBytes plus in-place mutation
// (append, extend, insert, pop, ...) and an unhashable instance.
//
// CPython: Objects/bytearrayobject.c:2654 PyByteArray_Type

package objects

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ByteArray is the mutable byte string. Mirrors PyByteArrayObject.
//
// CPython: Include/cpython/bytearrayobject.h:13 PyByteArrayObject
type ByteArray struct {
	VarHeader
	v []byte
	// exports counts the live buffer views over v (memoryview windows,
	// and the in-flight join that locks its separator). While positive
	// the buffer cannot change length, matching CPython's ob_exports.
	//
	// CPython: Objects/bytearrayobject.c ob_exports
	exports int
	// attrs holds instance attributes for bytearray subclass objects.
	// Nil for plain bytearray instances; allocated lazily by
	// EnsureAttrDict on first store. Mirrors CPython's tp_dictoffset on
	// bytearray subclasses (type_new_descriptors stamps __dict__ from
	// object when the subclass picks it up).
	attrs *Dict
}

// AttrDict implements AttrDictHolder so bytearray subclasses can carry
// instance attributes through generic_attr's HasDict path.
//
// CPython: Objects/object.c _PyObject_GetDictPtr (bytearray-subclass path)
func (b *ByteArray) AttrDict() *Dict { return b.attrs }

// EnsureAttrDict allocates the attrs dict on first use.
func (b *ByteArray) EnsureAttrDict() *Dict {
	if b.attrs == nil {
		b.attrs = NewDict()
		trackAttrDictHolder(b)
	}
	return b.attrs
}

// ExportInc / ExportDec adjust the live-export count. A buffer consumer
// (memoryview, bytearray.join) raises the count for the lifetime of its
// view so a concurrent resize is rejected.
//
// CPython: Objects/bytearrayobject.c:1228 bytearray_getbuffer / bytearray_releasebuffer
func (b *ByteArray) ExportInc() { b.exports++ }

// ExportDec drops one live export, never below zero.
//
// CPython: Objects/bytearrayobject.c:1247 bytearray_releasebuffer
func (b *ByteArray) ExportDec() {
	if b.exports > 0 {
		b.exports--
	}
}

// checkResize is the ob_exports guard PyByteArray_Resize applies before
// any length change: a bytearray with live exports cannot be re-sized.
// A resize that leaves the length unchanged is always allowed.
//
// CPython: Objects/bytearrayobject.c:147 PyByteArray_Resize
func (b *ByteArray) checkResize(newLen int) error {
	if b.exports > 0 && newLen != len(b.v) {
		return errors.New("BufferError: Existing exports of data: object cannot be re-sized")
	}
	return nil
}

// ByteArrayType is the type singleton for bytearray.
//
// CPython: Objects/bytearrayobject.c:2654 PyByteArray_Type
var ByteArrayType = NewType("bytearray", []*Type{objectType})

func init() {
	ByteArrayType.Repr = byteArrayRepr
	// CPython: Objects/typeobject.c:1356 subtype_traverse (managed __dict__)
	ByteArrayType.TpTraverse = attrDictHolderTraverse
	ByteArrayType.Str = byteArrayRepr
	ByteArrayType.Hash = byteArrayHash
	ByteArrayType.RichCmp = byteArrayRichCmp
	ByteArrayType.TpFlags |= TpFlagMatchSelf
	// CPython: Objects/bytearrayobject.c bytearray_doc (tp_doc)
	SetTypeDescr(ByteArrayType, "__doc__", NewStr("bytearray(iterable_of_ints) -> bytearray\n"+
		"bytearray(string, encoding[, errors]) -> bytearray\n"+
		"bytearray(bytes_or_buffer) -> mutable copy of bytes_or_buffer\n"+
		"bytearray(int) -> bytes array of size given by the parameter initialized with null bytes\n"+
		"bytearray() -> empty bytes array\n"+
		"\n"+
		"Construct a mutable bytearray object from:\n"+
		"  - an iterable yielding integers in range(256)\n"+
		"  - a text string encoded using the specified encoding\n"+
		"  - a bytes or a buffer object\n"+
		"  - any object implementing the buffer API.\n"+
		"  - an integer"))
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
		SetItem: byteArrayAssSubscript,
		DelItem: byteArrayMappingDel,
	}
	// nb_remainder backs bytearray % args (PEP 461 printf-style format).
	//
	// CPython: Objects/bytearrayobject.c:2825 bytearray_as_number
	ByteArrayType.Number = &NumberMethods{
		Remainder: byteArrayModulo,
	}
	SetTypeDescr(ByteArrayType, "__mod__", NewMethodDescr(ByteArrayType, "__mod__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __mod__() takes exactly one argument (%d given)", len(args)-1)
			}
			return byteArrayModulo(args[0], args[1])
		},
	))
	SetTypeDescr(ByteArrayType, "__rmod__", NewMethodDescr(ByteArrayType, "__rmod__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __rmod__() takes exactly one argument (%d given)", len(args)-1)
			}
			// a.__rmod__(b) computes b % a; bytearray_mod requires the
			// left operand to be a bytearray, so a str/other lhs yields
			// NotImplemented.
			return byteArrayModulo(args[1], args[0])
		},
	))
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
	SetTypeDescr(ByteArrayType, "__setitem__", NewMethodDescr(ByteArrayType, "__setitem__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("TypeError: __setitem__() takes exactly two arguments (%d given)", len(args)-1)
			}
			if err := byteArrayAssSubscript(args[0], args[1], args[2]); err != nil {
				return nil, err
			}
			return None(), nil
		},
	))
	SetTypeDescr(ByteArrayType, "__delitem__", NewMethodDescr(ByteArrayType, "__delitem__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __delitem__() takes exactly one argument (%d given)", len(args)-1)
			}
			if err := byteArrayMappingDel(args[0], args[1]); err != nil {
				return nil, err
			}
			return None(), nil
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
	// __repr__ / __str__ descriptors so a bytearray subclass instance
	// reprs through bytearray_repr (ClassName(b'...')) instead of falling
	// back to object.__repr__. CPython's add_operators wraps tp_repr/tp_str
	// into the type dict for every type that defines them.
	//
	// CPython: Objects/typeobject.c add_operators slotdefs (tp_repr, tp_str)
	byteArrayReprDescr := func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: __repr__() takes no arguments (%d given)", len(args)-1)
		}
		s, err := byteArrayRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}
	SetTypeDescr(ByteArrayType, "__repr__", NewMethodDescr(ByteArrayType, "__repr__", byteArrayReprDescr))
	SetTypeDescr(ByteArrayType, "__str__", NewMethodDescr(ByteArrayType, "__str__", byteArrayReprDescr))
	// CPython: Objects/bytearrayobject.c:2754 bytearray.__hash__ = None
	SetTypeDescr(ByteArrayType, "__hash__", None())
	// __reduce__ / __reduce_ex__ carry the byte content (and instance
	// state) through pickle and copy. Without these, object's default
	// reducer rebuilds the subclass via object.__new__ (losing the bytes
	// and producing a plain *Instance). bytearray's own reducer returns
	// (cls, args, state) where args reconstruct the buffer.
	//
	// CPython: Objects/bytearrayobject.c:1880 _common_reduce
	SetTypeDescr(ByteArrayType, "__reduce__", NewMethodDescrConv(ByteArrayType, "__reduce__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
			}
			return byteArrayCommonReduce(args[0], 2)
		}))
	SetTypeDescr(ByteArrayType, "__reduce_ex__", NewMethodDescrConv(ByteArrayType, "__reduce_ex__", MethO,
		func(args []Object, _ map[string]Object) (Object, error) {
			proto := 2
			if len(args) >= 2 {
				p, ok := args[1].(*Int)
				if !ok {
					return nil, fmt.Errorf("TypeError: an integer is required")
				}
				v, _ := p.Int64()
				proto = int(v)
			}
			return byteArrayCommonReduce(args[0], proto)
		}))
	// PEP 688: bf_getbuffer surfaced as __buffer__(flags) -> memoryview.
	// CPython: Objects/typeobject.c:9737 wrap_buffer (PyByteArray_Type.tp_as_buffer)
	SetTypeDescr(ByteArrayType, "__buffer__", NewMethodDescr(ByteArrayType, "__buffer__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __buffer__() takes exactly one argument (%d given)", len(args)-1)
			}
			if _, ok := args[0].(*ByteArray); !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__buffer__' requires a 'bytearray' object")
			}
			if _, err := indexAsInt(args[1]); err != nil {
				return nil, err
			}
			return NewMemoryView(args[0])
		},
	))
	// TpNew allocates a bare *ByteArray bound to the requested class so
	// `class S(bytearray): pass; S(b'..')` returns an S instance that can
	// also hold instance attributes. Population happens in __init__
	// (wired in builtins/ctor.go via bindByteArrayCtor), matching
	// CPython's split of bytearray_alloc (allocate) and
	// bytearray___init___impl (populate).
	//
	// CPython: Objects/bytearrayobject.c:2674 PyByteArray_Type (tp_new = bytearray_new)
	ByteArrayType.TpNew = func(cls *Type, _ []Object, _ map[string]Object) (Object, error) {
		b := &ByteArray{}
		b.init(cls)
		return b, nil
	}
}

func byteArrayMulMethod(args []Object, _ map[string]Object) (Object, error) {
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
	return byteArrayRepeat(args[0], int(v))
}

func byteArrayIMulMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __imul__() takes exactly one argument (%d given)", len(args)-1)
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
	// bytearray_subscript adjusts a negative subscript against the length
	// before indexing; the sq_item slot (byteArrayGetItem) does not.
	//
	// CPython: Objects/bytearrayobject.c:478 bytearray_subscript_lock_held
	if idx < 0 {
		idx += len(b.v)
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
	if err := b.checkResize(len(b.v) + 1); err != nil {
		return err
	}
	b.v = append(b.v, byte(v))
	b.size = int64(len(b.v))
	return nil
}

// Extend concatenates the bytes from src.
//
// CPython: Objects/bytearrayobject.c:2299 bytearray_extend
func (b *ByteArray) Extend(src []byte) error {
	if err := b.checkResize(len(b.v) + len(src)); err != nil {
		return err
	}
	b.v = append(b.v, src...)
	b.size = int64(len(b.v))
	return nil
}

// Insert places v at position where, shifting later bytes right.
// Negative where is clamped to 0; where past the end appends.
//
// CPython: Objects/bytearrayobject.c:2206 bytearray_insert
func (b *ByteArray) Insert(where int, v int) error {
	if v < 0 || v > 255 {
		return errors.New("ValueError: byte must be in range(0, 256)")
	}
	if err := b.checkResize(len(b.v) + 1); err != nil {
		return err
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
	if err := b.checkResize(n - 1); err != nil {
		return 0, err
	}
	v := b.v[i]
	b.v = append(b.v[:i], b.v[i+1:]...)
	b.size = int64(len(b.v))
	return int(v), nil
}

// Clear truncates the buffer to zero length.
//
// CPython: Objects/bytearrayobject.c:2191 bytearray_clear
func (b *ByteArray) Clear() error {
	if err := b.checkResize(0); err != nil {
		return err
	}
	b.v = b.v[:0]
	b.size = 0
	return nil
}

// Alloc returns the number of bytes actually allocated for the buffer,
// mirroring ob_alloc. CPython always keeps room for a trailing null byte,
// so ob_alloc is strictly greater than the logical length; gopy stores no
// terminator, so we surface the Go slice capacity but never report less
// than len+1 to preserve that invariant.
//
// CPython: Objects/bytearrayobject.c:2493 bytearray_alloc
func (b *ByteArray) Alloc() int {
	if c := cap(b.v); c > len(b.v) {
		return c
	}
	return len(b.v) + 1
}

// Resize changes the logical length of the buffer to n, zero-filling any
// bytes added beyond the old length and rejecting the change when there
// are live buffer exports. A negative size is a ValueError; a size that
// cannot be allocated is a MemoryError.
//
// CPython: Objects/bytearrayobject.c:1564 bytearray_resize_impl + :208 bytearray_resize_lock_held
func (b *ByteArray) Resize(n int) error {
	if n < 0 {
		return fmt.Errorf("ValueError: Can only resize to positive sizes, got %d", n)
	}
	old := len(b.v)
	if n == old {
		return nil
	}
	if err := b.checkResize(n); err != nil {
		return err
	}
	// size + 1 overflowing a signed int matches CPython's alloc > PY_SSIZE_T_MAX
	// guard: the buffer cannot be allocated.
	if n+1 <= 0 {
		return errors.New("MemoryError")
	}
	if n < old {
		b.v = b.v[:n]
	} else {
		b.v = append(b.v, make([]byte, n-old)...)
	}
	b.size = int64(n)
	return nil
}

// SetContents replaces the whole buffer with a copy of buf. Used by
// bytearray.__init__ to populate an instance that tp_new allocated
// empty (the new/init split that keeps subclass type and __dict__).
//
// CPython: Objects/bytearrayobject.c:888 bytearray___init___impl (PyByteArray_Resize + memcpy)
func (b *ByteArray) SetContents(buf []byte) error {
	if err := b.checkResize(len(buf)); err != nil {
		return err
	}
	b.v = append(b.v[:0], buf...)
	b.size = int64(len(b.v))
	return nil
}

// Reverse reverses bytes in place.
//
// CPython: Objects/bytearrayobject.c:2226 bytearray_reverse
func (b *ByteArray) Reverse() {
	for i, j := 0, len(b.v)-1; i < j; i, j = i+1, j-1 {
		b.v[i], b.v[j] = b.v[j], b.v[i]
	}
}

// byteArrayRepr formats as ClassName(b'...'). bytearray has its own
// repr (distinct from bytes_repr): it always escapes the single quote
// and backslash regardless of which quote character wraps the literal,
// and the prefix is the instance's type name so a subclass reprs as
// ClassName(b'...').
//
// CPython: Objects/bytearrayobject.c:1092 bytearray_repr_lock_held
func byteArrayRepr(o Object) (string, error) {
	self := o.(*ByteArray)
	data := self.v
	// Figure out which quote to use; single is preferred.
	quote := byte('\'')
	for _, c := range data {
		if c == '"' {
			quote = '\''
			break
		} else if c == '\'' {
			quote = '"'
		}
	}
	var sb strings.Builder
	sb.WriteString(o.Type().Name)
	sb.WriteString("(b")
	sb.WriteByte(quote)
	for _, c := range data {
		switch {
		case c == '\'' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\t':
			sb.WriteString(`\t`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == 0:
			sb.WriteString(`\x00`)
		case c < ' ' || c >= 0x7f:
			sb.WriteString(`\x`)
			sb.WriteByte(hexAlphabet[(c&0xf0)>>4])
			sb.WriteByte(hexAlphabet[c&0xf])
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte(quote)
	sb.WriteByte(')')
	return sb.String(), nil
}

// byteArrayCommonReduce ports _common_reduce: it returns a
// (type, args, state) tuple that pickle / copy replay by calling
// type(*args) and then applying state. The args carry the byte buffer
// so a subclass round-trips its contents and its type.
//
// CPython: Objects/bytearrayobject.c:1880 _common_reduce
func byteArrayCommonReduce(o Object, proto int) (Object, error) {
	self, ok := o.(*ByteArray)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor requires a 'bytearray' object but received a '%s'", o.Type().Name)
	}
	state, err := objectGetState(o, false)
	if err != nil {
		return nil, err
	}
	cls := o.Type()
	if len(self.v) == 0 {
		return NewTuple([]Object{cls, NewTuple(nil), state}), nil
	}
	if proto < 3 {
		// Latin-1 string reduction for backward compatibility: each byte
		// maps to the matching code point.
		runes := make([]rune, len(self.v))
		for i, b := range self.v {
			runes[i] = rune(b)
		}
		argTup := NewTuple([]Object{NewStr(string(runes)), NewStr("latin-1")})
		return NewTuple([]Object{cls, argTup, state}), nil
	}
	argTup := NewTuple([]Object{NewBytes(append([]byte(nil), self.v...))})
	return NewTuple([]Object{cls, argTup, state}), nil
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

// byteArrayGetItem is the sq_item slot. Like bytes_item it bounds-checks
// only; PySequence_GetItem adjusts a negative index via sq_length before
// reaching the slot, so adjusting here too would mask an out-of-range
// negative index.
//
// CPython: Objects/bytearrayobject.c:430 bytearray_getitem_lock_held
func byteArrayGetItem(o Object, i int) (Object, error) {
	b := o.(*ByteArray)
	if i < 0 || i >= len(b.v) {
		return nil, errIndexOutOfRange
	}
	return NewInt(int64(b.v[i])), nil
}

// byteArraySetItem assigns or (when v==nil) deletes a single byte
// through the sequence protocol (sq_ass_item). The value is coerced via
// _getbytevalue, so any object implementing __index__ is accepted.
//
// CPython: Objects/bytearrayobject.c:685 bytearray_setitem_lock_held
func byteArraySetItem(o Object, i int, v Object) error {
	b := o.(*ByteArray)
	// GH-91153: coerce the value (which may run __index__ and mutate b)
	// before the size check.
	ival := -1
	if v != nil {
		var err error
		ival, err = bytearrayCoerceByte(v)
		if err != nil {
			return err
		}
	}
	if i < 0 {
		i += len(b.v)
	}
	if i < 0 || i >= len(b.v) {
		return errors.New("IndexError: bytearray index out of range")
	}
	if v == nil {
		return byteArraySetSliceLinear(b, i, i+1, nil)
	}
	b.v[i] = byte(ival)
	return nil
}

// byteArraySetSliceLinear replaces self[lo:hi] (a step==1 region) with
// src, growing or shrinking the buffer as needed. src==nil deletes the
// region. Mirrors the memmove-based resize logic.
//
// CPython: Objects/bytearrayobject.c:549 bytearray_setslice_linear
func byteArraySetSliceLinear(self *ByteArray, lo, hi int, src []byte) error {
	avail := hi - lo
	growth := len(src) - avail
	if growth < 0 {
		if err := self.checkResize(len(self.v) + growth); err != nil {
			return err
		}
		// Shift the tail left to close the gap, then truncate.
		copy(self.v[lo+len(src):], self.v[hi:])
		self.v = self.v[:len(self.v)+growth]
	} else if growth > 0 {
		if len(self.v) > math.MaxInt-growth {
			return fmt.Errorf("MemoryError")
		}
		if err := self.checkResize(len(self.v) + growth); err != nil {
			return err
		}
		// Grow the buffer, then shift the tail right to open the gap.
		self.v = append(self.v, make([]byte, growth)...)
		copy(self.v[lo+len(src):], self.v[hi:len(self.v)-growth])
	}
	if len(src) > 0 {
		copy(self.v[lo:], src)
	}
	self.size = int64(len(self.v))
	return nil
}

// byteArrayAssSubscript implements bytearray.__setitem__ /
// __delitem__: integer keys assign or delete a single byte, slice keys
// assign or delete a region. values==nil signals deletion.
//
// CPython: Objects/bytearrayobject.c:726 bytearray_ass_subscript_lock_held
func byteArrayAssSubscript(o, index, values Object) error {
	self := o.(*ByteArray)
	var start, stop, step, slicelen int

	if IndexCheck(index) {
		i, err := indexValueAsInt(index, "bytearray")
		if err != nil {
			return err
		}
		var ival int
		// GH-91153: coerce the value (which may run __index__ and mutate
		// self) before the bounds check.
		if values != nil {
			ival, err = bytearrayCoerceByte(values)
			if err != nil {
				return err
			}
		}
		if i < 0 {
			i += len(self.v)
		}
		if i < 0 || i >= len(self.v) {
			return errors.New("IndexError: bytearray index out of range")
		}
		if values == nil {
			start, stop, step, slicelen = i, i+1, 1, 1
		} else {
			self.v[i] = byte(ival)
			return nil
		}
	} else if sl, ok := index.(*Slice); ok {
		var err error
		start, stop, step, slicelen, err = sl.GetIndices(len(self.v))
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("TypeError: bytearray indices must be integers or slices, not %s", index.Type().Name)
	}

	var src []byte
	if values == nil {
		src = nil
	} else if ba, ok := values.(*ByteArray); ok && ba != self {
		src = ba.v
	} else {
		// values is self, a non-bytearray buffer, or an iterable. Reject
		// numbers and strings; otherwise build a bytearray copy (so the
		// b[:] = b aliasing case also gets a snapshot) and recurse.
		if IndexCheck(values) || values.Type() == StrType() {
			return errors.New("TypeError: can assign only bytes, buffers, or iterables of ints in range(0, 256)")
		}
		buf, err := byteArrayBytesFromObject(values)
		if err != nil {
			return err
		}
		return byteArrayAssSubscript(o, index, NewByteArray(buf))
	}

	// Make sure b[5:2] = ... inserts before 5, not before 2.
	if (step < 0 && start < stop) || (step > 0 && start > stop) {
		stop = start
	}

	if step == 1 {
		return byteArraySetSliceLinear(self, start, stop, src)
	}
	if len(src) == 0 {
		// Delete an extended slice.
		if err := self.checkResize(len(self.v) - slicelen); err != nil {
			return err
		}
		if slicelen == 0 {
			return nil
		}
		if step < 0 {
			stop = start + 1
			start = stop + step*(slicelen-1) - 1
			step = -step
		}
		buf := self.v
		size := len(buf)
		cur, i := start, 0
		for ; i < slicelen; cur, i = cur+step, i+1 {
			lim := step - 1
			// The C source tests cur+step >= size; cur is always a valid
			// index (< size) and step can be sys.maxsize, so write it as
			// step >= size-cur to dodge signed overflow.
			if step >= size-cur {
				lim = size - cur - 1
			}
			copy(buf[cur-i:], buf[cur+1:cur+1+lim])
		}
		// tail = start + slicelen*step. A huge step makes this exceed size
		// (no tail to shift); guard the multiply against overflow.
		if slicelen == 0 || step <= (size-1-start)/slicelen {
			tail := start + slicelen*step
			if tail < size {
				copy(buf[tail-slicelen:], buf[tail:])
			}
		}
		self.v = self.v[:len(self.v)-slicelen]
		self.size = int64(len(self.v))
		return nil
	}
	// Assign an extended slice: lengths must match exactly.
	if len(src) != slicelen {
		return fmt.Errorf("ValueError: attempt to assign bytes of size %d to extended slice of size %d", len(src), slicelen)
	}
	cur, i := start, 0
	for ; i < slicelen; cur, i = cur+step, i+1 {
		self.v[cur] = src[i]
	}
	return nil
}

// byteArrayBytesFromObject builds a byte buffer from a bytes-like
// object or an iterable of ints in range(256). Used by slice
// assignment when the right-hand side is not already a bytearray.
//
// CPython: Objects/bytearrayobject.c PyByteArray_FromObject
func byteArrayBytesFromObject(o Object) ([]byte, error) {
	if buf, ok := asBytesLike(o); ok {
		return append([]byte(nil), buf...), nil
	}
	items, err := IterToSlice(o)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, len(items))
	for i, it := range items {
		v, err := bytearrayCoerceByte(it)
		if err != nil {
			return nil, err
		}
		buf[i] = byte(v)
	}
	return buf, nil
}

// byteArrayMappingDel implements bytearray.__delitem__ by routing
// through the assignment path with a nil value.
//
// CPython: Objects/bytearrayobject.c:886 bytearray_ass_subscript
func byteArrayMappingDel(o, key Object) error {
	return byteArrayAssSubscript(o, key, nil)
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
	if err := self.checkResize(len(self.v) + len(vb)); err != nil {
		return nil, err
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
	// Reject sizes that would overflow Py_ssize_t before allocating.
	//
	// CPython: Objects/bytearrayobject.c:393 bytearray_repeat_lock_held
	if n > 0 && len(self.v) > math.MaxInt/n {
		return nil, fmt.Errorf("MemoryError")
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
	// Reject sizes that would overflow Py_ssize_t before resizing.
	//
	// CPython: Objects/bytearrayobject.c:419 bytearray_irepeat_lock_held
	if n > 0 && mysize > math.MaxInt/n {
		return nil, fmt.Errorf("MemoryError")
	}
	size := mysize * n
	if err := self.checkResize(size); err != nil {
		return nil, err
	}
	out := make([]byte, size)
	for i := 0; i < n; i++ {
		copy(out[i*mysize:], self.v)
	}
	self.v = out
	self.size = int64(len(self.v))
	return self, nil
}

func byteArrayContains(o, v Object) (bool, error) {
	// gh-142560: lock the buffer while the needle is coerced so a
	// re-entrant resize (v.__index__ mutating o) raises BufferError.
	ba := o.(*ByteArray)
	unlock := lockSearchSelf(ba)
	defer unlock()
	return bytesLikeContains(ba.v, v)
}
