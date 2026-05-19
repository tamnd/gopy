// Bind int method descriptors. The methods exposed here mirror
// Objects/longobject.c long_methods, focused on the names Python code
// in the vendored stdlib actually reaches for.
//
// CPython: Objects/longobject.c:6260 long_methods

package objects

import (
	"fmt"
	"math/big"
)

func init() {
	IntType.Getattro = GenericGetAttr

	bind := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(IntType, name, NewMethodDescr(IntType, name, fn))
	}

	bind("bit_length", intBitLengthMethod)
	bind("bit_count", intBitCountMethod)
	bind("__index__", intIndexMethod)
	bind("__int__", intIndexMethod)
	bind("__trunc__", intIndexMethod)
	bind("__floor__", intIndexMethod)
	bind("__ceil__", intIndexMethod)
	bind("conjugate", intIndexMethod)
	bind("to_bytes", intToBytesMethod)

	SetTypeDescr(IntType, "from_bytes", NewClassMethod(
		NewBuiltinFunction("from_bytes", intFromBytesMethod),
	))
}

// intBitLengthMethod ports int.bit_length(): the number of bits needed
// to represent self in binary, excluding the sign and any leading
// zeros. Zero returns zero.
//
// CPython: Objects/longobject.c:5910 int_bit_length_impl
func intBitLengthMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: bit_length() takes no arguments (%d given)", len(args)-1)
	}
	i, ok := args[0].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'bit_length' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewInt(int64(i.v.BitLen())), nil
}

// intBitCountMethod ports int.bit_count(): number of one-bits in the
// absolute value's binary representation. Also called the population
// count or Hamming weight.
//
// CPython: Objects/longobject.c:5965 int_bit_count_impl
func intBitCountMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: bit_count() takes no arguments (%d given)", len(args)-1)
	}
	i, ok := args[0].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'bit_count' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	// big.Int has no direct popcount; walk the absolute value's words.
	count := 0
	abs := i.BigInt()
	if abs.Sign() < 0 {
		abs.Neg(abs)
	}
	for _, w := range abs.Bits() {
		x := uint64(w)
		for x != 0 {
			x &= x - 1
			count++
		}
	}
	return NewInt(int64(count)), nil
}

// intToBytesMethod ports int.to_bytes(length=1, byteorder='big', *, signed=False).
//
// CPython: Objects/longobject.c:6298 int_to_bytes_impl
func intToBytesMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: to_bytes() takes at most 2 positional arguments (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'to_bytes' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}

	length := 1
	if len(args) >= 2 {
		l, ok := args[1].(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: to_bytes() length must be int, not '%s'", typeNameOf(args[1]))
		}
		n, ok := l.Int64()
		if !ok {
			return nil, fmt.Errorf("OverflowError: to_bytes() length too large")
		}
		length = int(n)
	}
	if v, ok := kwargs["length"]; ok {
		l, ok := v.(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: to_bytes() length must be int, not '%s'", typeNameOf(v))
		}
		n, ok := l.Int64()
		if !ok {
			return nil, fmt.Errorf("OverflowError: to_bytes() length too large")
		}
		length = int(n)
	}

	littleEndian := false
	if len(args) >= 3 {
		bo, err := byteorderFrom(args[2])
		if err != nil {
			return nil, err
		}
		littleEndian = bo
	}
	if v, ok := kwargs["byteorder"]; ok {
		bo, err := byteorderFrom(v)
		if err != nil {
			return nil, err
		}
		littleEndian = bo
	}

	signed, err := signedFromKwarg(kwargs)
	if err != nil {
		return nil, err
	}

	b, err := intToByteArray(self.BigInt(), length, littleEndian, signed)
	if err != nil {
		return nil, err
	}
	return NewBytes(b), nil
}

// intFromBytesMethod ports int.from_bytes(bytes, byteorder='big', *, signed=False).
// As a classmethod, args[0] is the type.
//
// CPython: Objects/longobject.c:6360 int_from_bytes_impl
func intFromBytesMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: from_bytes() takes at most 2 positional arguments (%d given)", len(args)-1)
	}
	typ, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: from_bytes() requires int type as first argument")
	}

	data, err := bytesLike(args[1])
	if err != nil {
		return nil, err
	}

	littleEndian := false
	if len(args) >= 3 {
		bo, err := byteorderFrom(args[2])
		if err != nil {
			return nil, err
		}
		littleEndian = bo
	}
	if v, ok := kwargs["byteorder"]; ok {
		bo, err := byteorderFrom(v)
		if err != nil {
			return nil, err
		}
		littleEndian = bo
	}

	signed, err := signedFromKwarg(kwargs)
	if err != nil {
		return nil, err
	}

	val := intFromByteArray(data, littleEndian, signed)
	if typ == IntType {
		return NewIntFromBig(val), nil
	}
	return newIntAs(val, typ), nil
}

// byteorderFrom validates the `byteorder` argument. Returns true for
// "little", false for "big".
//
// CPython: Objects/longobject.c:6308 byteorder matching
func byteorderFrom(o Object) (bool, error) {
	s, ok := o.(*Unicode)
	if !ok {
		return false, fmt.Errorf("TypeError: to_bytes() byteorder must be str, not '%s'", typeNameOf(o))
	}
	switch s.Value() {
	case "little":
		return true, nil
	case "big":
		return false, nil
	}
	return false, fmt.Errorf("ValueError: byteorder must be either 'little' or 'big'")
}

// signedFromKwarg reads the keyword-only `signed` argument.
func signedFromKwarg(kwargs map[string]Object) (bool, error) {
	v, ok := kwargs["signed"]
	if !ok {
		return false, nil
	}
	b, ok := v.(*Bool)
	if ok {
		i, _ := b.Int64()
		return i != 0, nil
	}
	if i, ok := v.(*Int); ok {
		// CPython accepts any int-compatible.
		val, _ := i.Int64()
		return val != 0, nil
	}
	return false, fmt.Errorf("TypeError: signed must be bool, not '%s'", typeNameOf(v))
}

// bytesLike extracts the underlying byte slice from a bytes/bytearray
// object, mirroring CPython's PyObject_Bytes coercion path.
//
// CPython: Objects/longobject.c:6380 PyObject_Bytes
func bytesLike(o Object) ([]byte, error) {
	switch v := o.(type) {
	case *Bytes:
		return v.Bytes(), nil
	case *ByteArray:
		return v.Bytes(), nil
	}
	return nil, fmt.Errorf("TypeError: cannot convert '%s' object to bytes", typeNameOf(o))
}

// intToByteArray ports _PyLong_AsByteArray: pack v as `length` bytes in
// the chosen byte order using two's complement for signed negatives.
//
// CPython: Objects/longobject.c:1031 _PyLong_AsByteArray
func intToByteArray(v *big.Int, length int, littleEndian, signed bool) ([]byte, error) {
	if length < 0 {
		return nil, fmt.Errorf("ValueError: length argument must be non-negative")
	}
	if v.Sign() < 0 && !signed {
		return nil, fmt.Errorf("OverflowError: can't convert negative int to unsigned")
	}

	var u *big.Int
	if v.Sign() < 0 {
		if length == 0 {
			return nil, fmt.Errorf("OverflowError: int too big to convert")
		}
		bound := new(big.Int).Lsh(big.NewInt(1), uint(length*8))
		u = new(big.Int).Add(v, bound)
		if u.Sign() < 0 {
			return nil, fmt.Errorf("OverflowError: int too big to convert")
		}
		threshold := new(big.Int).Lsh(big.NewInt(1), uint(length*8-1))
		if u.Cmp(threshold) < 0 {
			return nil, fmt.Errorf("OverflowError: int too big to convert")
		}
	} else {
		u = v
		maxBits := length * 8
		if signed {
			maxBits = length*8 - 1
		}
		if maxBits < 0 || u.BitLen() > maxBits {
			return nil, fmt.Errorf("OverflowError: int too big to convert")
		}
	}

	result := make([]byte, length)
	raw := u.Bytes()
	if len(raw) > length {
		return nil, fmt.Errorf("OverflowError: int too big to convert")
	}
	copy(result[length-len(raw):], raw)

	if littleEndian {
		for i, j := 0, length-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}
	return result, nil
}

// intFromByteArray ports _PyLong_FromByteArray: decode `data` as an
// integer in the chosen byte order, with two's complement when signed.
//
// CPython: Objects/longobject.c:923 _PyLong_FromByteArray
func intFromByteArray(data []byte, littleEndian, signed bool) *big.Int {
	if len(data) == 0 {
		return big.NewInt(0)
	}
	var be []byte
	if littleEndian {
		be = make([]byte, len(data))
		for i, b := range data {
			be[len(data)-1-i] = b
		}
	} else {
		be = data
	}
	u := new(big.Int).SetBytes(be)
	if signed && be[0]&0x80 != 0 {
		bound := new(big.Int).Lsh(big.NewInt(1), uint(len(data)*8))
		return new(big.Int).Sub(u, bound)
	}
	return u
}

// intIndexMethod ports int.__index__ / int.__int__ / int.__trunc__ /
// int.conjugate (all return self for plain ints; subclasses get a
// fresh int object).
//
// CPython: Objects/longobject.c:5862 int___index___impl, int___int___impl,
//
//	int___trunc___impl, int_conjugate
func intIndexMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: this method takes no arguments (%d given)", len(args)-1)
	}
	i, ok := args[0].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'this' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	if i.Type() == IntType {
		return i, nil
	}
	// For subclasses, return a fresh int with the same value so callers
	// that compare `type(x) is int` still get a plain int.
	return NewIntFromBig(i.BigInt()), nil
}
