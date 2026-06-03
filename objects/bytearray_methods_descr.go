// Descriptor wiring for the bytearray-only methods: the eight
// mutation entries (append, extend, insert, pop, remove, clear,
// reverse, copy) plus the two pickle helpers (__getnewargs__,
// __bytes__) that bytes exposes too. The non-mutating bytearray
// methods (find, split, replace, ...) reuse the bytes handlers in
// bytes_methods_descr.go since those inspect args[0] to know whether
// to return *Bytes or *ByteArray.
//
// CPython: Objects/bytearrayobject.c:2503 bytearray_methods
// CPython: Objects/bytesobject.c:2665 bytes_methods (for __getnewargs__,
//          __bytes__, removeprefix, removesuffix)

package objects

import (
	"errors"
	"fmt"
)

// removePrefixMethod backs bytes.removeprefix / bytearray.removeprefix.
//
// CPython: Objects/bytesobject.c:2360 bytes_removeprefix_impl
func removePrefixMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("removeprefix", 2, len(args))
		}
		v, err := bytesLikeView(args[0], "removeprefix")
		if err != nil {
			return nil, err
		}
		prefix, err := bytesArgAsBuffer(args[1], "removeprefix")
		if err != nil {
			return nil, err
		}
		return bytesLikeOf(args[0], bytesRemovePrefix(v, prefix)), nil
	}
}

// removeSuffixMethod backs bytes.removesuffix / bytearray.removesuffix.
//
// CPython: Objects/bytesobject.c:2386 bytes_removesuffix_impl
func removeSuffixMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("removesuffix", 2, len(args))
		}
		v, err := bytesLikeView(args[0], "removesuffix")
		if err != nil {
			return nil, err
		}
		suffix, err := bytesArgAsBuffer(args[1], "removesuffix")
		if err != nil {
			return nil, err
		}
		return bytesLikeOf(args[0], bytesRemoveSuffix(v, suffix)), nil
	}
}

// bytesGetNewArgsMethod backs bytes.__getnewargs__ / bytearray.__getnewargs__.
// CPython returns a 1-tuple `(payload_as_bytes,)`, used by copy / pickle to
// reconstruct the object.
//
// CPython: Objects/bytesobject.c:1462 bytes_getnewargs
func bytesGetNewArgsMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("__getnewargs__", 1, len(args))
		}
		v, err := bytesLikeView(args[0], "__getnewargs__")
		if err != nil {
			return nil, err
		}
		return NewTuple([]Object{NewBytes(v)}), nil
	}
}

// bytesDunderBytesMethod backs bytes.__bytes__ / bytearray.__bytes__.
// For bytes itself CPython returns the receiver (same identity); for
// bytearray it returns a fresh bytes copy of the buffer.
//
// CPython: Objects/bytesobject.c:1474 bytes___bytes__
func bytesDunderBytesMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("__bytes__", 1, len(args))
		}
		switch x := args[0].(type) {
		case *Bytes:
			// PyBytes_CheckExact returns self; a bytes subclass instead
			// produces a fresh plain bytes copy via PyBytes_FromStringAndSize.
			//
			// CPython: Objects/bytesobject.c bytes___bytes___impl
			if x.Type() == BytesType {
				return x, nil
			}
			return NewBytes(x.v), nil
		case *ByteArray:
			return NewBytes(x.v), nil
		}
		return nil, fmt.Errorf("TypeError: descriptor '__bytes__' requires a 'bytes' or 'bytearray' object but received a '%s'",
			args[0].Type().Name)
	}
}

// asByteArray pulls *ByteArray out of args[0]. The mutation methods
// only make sense on bytearray, so this surface is only wired on
// ByteArrayType.
func asByteArray(self Object, name string) (*ByteArray, error) {
	b, ok := self.(*ByteArray)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'bytearray' object but received a '%s'",
			name, self.Type().Name)
	}
	return b, nil
}

// bytearrayAppendMethod backs bytearray.append(value).
//
// CPython: Objects/bytearrayobject.c:2243 bytearray_append
func bytearrayAppendMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("append", 2, len(args))
		}
		b, err := asByteArray(args[0], "append")
		if err != nil {
			return nil, err
		}
		v, err := bytearrayCoerceByte(args[1])
		if err != nil {
			return nil, err
		}
		if err := b.Append(v); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// bytearrayExtendMethod backs bytearray.extend(iterable_of_ints | bytes_like).
//
// CPython: Objects/bytearrayobject.c:2299 bytearray_extend
func bytearrayExtendMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("extend", 2, len(args))
		}
		b, err := asByteArray(args[0], "extend")
		if err != nil {
			return nil, err
		}
		arg := args[1]
		// bytearray_setslice only accepts something supporting PEP 3118.
		if buf, ok := asBytesLike(arg); ok {
			if err := b.Extend(buf); err != nil {
				return nil, err
			}
			return None(), nil
		}
		// Get an iterator; a TypeError here means the argument is not
		// iterable, which CPython reformats.
		it, err := Iter(arg)
		if err != nil {
			if isTypeError(err) {
				return nil, fmt.Errorf("TypeError: can't extend bytearray with %s", arg.Type().Name)
			}
			return nil, err
		}
		// Probe the length to presize the buffer (32 is arbitrary). This
		// also surfaces a __len__/__length_hint__ that raises something
		// other than TypeError: PyObject_LengthHint propagates it, which
		// is what makes b.extend(BadLen()) raise RuntimeError.
		//
		// CPython: Objects/bytearrayobject.c:2325 bytearray_extend (PyObject_LengthHint)
		bufSize, err := LengthHint(arg, 32)
		if err != nil {
			return nil, err
		}
		// Coerce every item into a private buffer first; self is only
		// mutated once the whole iterable has been consumed successfully,
		// so a mid-stream failure leaves the bytearray untouched.
		buf := make([]byte, 0, bufSize)
		for {
			item, err := IterNext(it)
			if err != nil {
				if errors.Is(err, ErrStopIteration) {
					break
				}
				return nil, err
			}
			v, err := bytearrayCoerceByte(item)
			if err != nil {
				if isTypeError(err) && arg.Type() == StrType() {
					return nil, fmt.Errorf("TypeError: expected iterable of integers; got: 'str'")
				}
				return nil, err
			}
			buf = append(buf, byte(v))
		}
		if err := b.Extend(buf); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// bytearrayInsertMethod backs bytearray.insert(index, value).
//
// CPython: Objects/bytearrayobject.c:2206 bytearray_insert
func bytearrayInsertMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 3 {
			return nil, arityErr("insert", 3, len(args))
		}
		b, err := asByteArray(args[0], "insert")
		if err != nil {
			return nil, err
		}
		where, err := bytesIntArg(args, 1, "insert", 0)
		if err != nil {
			return nil, err
		}
		v, err := bytearrayCoerceByte(args[2])
		if err != nil {
			return nil, err
		}
		if err := b.Insert(where, v); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// bytearrayPopMethod backs bytearray.pop(index=-1).
//
// CPython: Objects/bytearrayobject.c:2267 bytearray_pop
func bytearrayPopMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, arityRangeErr("pop", 0, 1, len(args)-1)
		}
		b, err := asByteArray(args[0], "pop")
		if err != nil {
			return nil, err
		}
		idx := -1
		if len(args) == 2 {
			idx, err = bytesIntArg(args, 1, "pop", -1)
			if err != nil {
				return nil, err
			}
		}
		v, err := b.Pop(idx)
		if err != nil {
			return nil, err
		}
		return NewInt(int64(v)), nil
	}
}

// bytearrayRemoveMethod backs bytearray.remove(value): drop the first
// occurrence of value, ValueError if absent.
//
// CPython: Objects/bytearrayobject.c:2287 bytearray_remove
func bytearrayRemoveMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("remove", 2, len(args))
		}
		b, err := asByteArray(args[0], "remove")
		if err != nil {
			return nil, err
		}
		v, err := bytearrayCoerceByte(args[1])
		if err != nil {
			return nil, err
		}
		for i, c := range b.v {
			if c == byte(v) {
				if err := b.checkResize(len(b.v) - 1); err != nil {
					return nil, err
				}
				b.v = append(b.v[:i], b.v[i+1:]...)
				b.size = int64(len(b.v))
				return None(), nil
			}
		}
		return nil, fmt.Errorf("ValueError: value not found in bytearray")
	}
}

// bytearrayClearMethod backs bytearray.clear().
//
// CPython: Objects/bytearrayobject.c:2191 bytearray_clear
func bytearrayClearMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("clear", 1, len(args))
		}
		b, err := asByteArray(args[0], "clear")
		if err != nil {
			return nil, err
		}
		if err := b.Clear(); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// bytearrayReverseMethod backs bytearray.reverse().
//
// CPython: Objects/bytearrayobject.c:2226 bytearray_reverse
func bytearrayReverseMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("reverse", 1, len(args))
		}
		b, err := asByteArray(args[0], "reverse")
		if err != nil {
			return nil, err
		}
		b.Reverse()
		return None(), nil
	}
}

// bytearrayCopyMethod backs bytearray.copy().
//
// CPython: Objects/bytearrayobject.c:2167 bytearray_copy
func bytearrayCopyMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("copy", 1, len(args))
		}
		b, err := asByteArray(args[0], "copy")
		if err != nil {
			return nil, err
		}
		return NewByteArray(b.v), nil
	}
}

// bytearrayAllocMethod backs bytearray.__alloc__(): the number of bytes
// actually allocated for the buffer.
//
// CPython: Objects/bytearrayobject.c:2493 bytearray_alloc
func bytearrayAllocMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, arityErr("__alloc__", 1, len(args))
		}
		b, err := asByteArray(args[0], "__alloc__")
		if err != nil {
			return nil, err
		}
		return NewInt(int64(b.Alloc())), nil
	}
}

// bytearrayResizeMethod backs bytearray.resize(size): grow or shrink the
// internal buffer to the requested length, zero-filling new bytes. The
// clinic signature is METH_O with a Py_ssize_t converter, so the single
// argument runs through __index__.
//
// CPython: Objects/bytearrayobject.c:1564 bytearray_resize_impl
func bytearrayResizeMethod() methodFn {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, arityErr("resize", 2, len(args))
		}
		b, err := asByteArray(args[0], "resize")
		if err != nil {
			return nil, err
		}
		iv, ierr := NumberIndex(args[1])
		if ierr != nil {
			return nil, ierr
		}
		n, fits := iv.(*Int).Int64()
		if !fits {
			return nil, errors.New("MemoryError")
		}
		if err := b.Resize(int(n)); err != nil {
			return nil, err
		}
		return None(), nil
	}
}

// bytearrayCoerceByte unpacks an object as a byte value in [0, 255].
// Used by append / insert / remove / extend, whose clinic signature
// uses the "bytesvalue" converter. The converter runs __index__ on the
// argument (via PyLong_AsLongAndOverflow), so any object implementing
// __index__ is accepted; out-of-range or non-index values raise.
//
// CPython: Objects/bytearrayobject.c:26 _getbytevalue
func bytearrayCoerceByte(obj Object) (int, error) {
	iv, err := NumberIndex(obj)
	if err != nil {
		return -1, err
	}
	n, fits := iv.(*Int).Int64()
	if !fits || n < 0 || n >= 256 {
		return -1, fmt.Errorf("ValueError: byte must be in range(0, 256)")
	}
	return int(n), nil
}
