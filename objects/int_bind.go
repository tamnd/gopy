// Bind int method descriptors. The methods exposed here mirror
// Objects/longobject.c long_methods, focused on the names Python code
// in the vendored stdlib actually reaches for.
//
// CPython: Objects/longobject.c:6260 long_methods

package objects

import (
	"errors"
	"fmt"
	"math/big"
)

func init() {
	IntType.Getattro = GenericGetAttr

	bind := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(IntType, name, NewMethodDescr(IntType, name, fn))
	}
	// bindNoArgs wires a METH_NOARGS row so the arg-count TypeError
	// ("int.<name>() takes no arguments (N given)") flows through
	// methodDescrCheckArity / _PyObject_FunctionStr. Slot wrappers
	// (__index__, __int__, __repr__, __str__) keep their own
	// "expected 0 arguments, got N" template and stay on plain bind.
	bindNoArgs := func(name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) {
		SetTypeDescr(IntType, name, NewMethodDescrConv(IntType, name, MethNoArgs, fn))
	}

	bindNoArgs("bit_length", intBitLengthMethod)
	bindNoArgs("bit_count", intBitCountMethod)
	bind("__index__", intIndexMethod)
	bind("__int__", intIndexMethod)
	bindNoArgs("__trunc__", intIndexMethod)
	bindNoArgs("__floor__", intIndexMethod)
	bindNoArgs("__ceil__", intIndexMethod)
	bind("__round__", intRoundMethod)
	bindNoArgs("conjugate", intIndexMethod)
	bind("to_bytes", intToBytesMethod)
	bindNoArgs("as_integer_ratio", intAsIntegerRatioMethod)
	bindNoArgs("is_integer", intIsIntegerMethod)
	bind("__repr__", intReprDescr)
	bind("__str__", intReprDescr)
	bindNoArgs("__sizeof__", intSizeofMethod)
	bindNoArgs("__getnewargs__", intGetNewArgsMethod)
	// Install int.__hash__ as a descriptor so int.__hash__ resolves to
	// long_hash rather than object.__hash__, and so fixupHashAndIter
	// (which keys off LookupDescriptor(t, "__hash__")) installs long_hash
	// as the tp_hash slot on int subclasses. Without it, hash(hexint(x))
	// fell back to the identity hash.
	//
	// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __hash__)
	bindNoArgs("__hash__", intHashMethod)

	// long_getset (Objects/longobject.c:6466): real/numerator return
	// self as int, imag returns 0, denominator returns 1.
	SetTypeDescr(IntType, "real", NewGetSetDescr("real", intRealGetter, nil))
	SetTypeDescr(IntType, "imag", NewGetSetDescr("imag", intImagGetter, nil))
	SetTypeDescr(IntType, "numerator", NewGetSetDescr("numerator", intRealGetter, nil))
	SetTypeDescr(IntType, "denominator", NewGetSetDescr("denominator", intDenominatorGetter, nil))

	SetTypeDescr(IntType, "from_bytes", NewClassMethod(
		NewBuiltinFunction("from_bytes", intFromBytesMethod),
	))
}

// intRoundMethod implements int.__round__(ndigits=None). Rounding an
// integer with no argument, None, or a non-negative ndigits returns the
// value unchanged (as a plain int). A negative ndigits rounds to the
// nearest multiple of 10**-ndigits using round-half-to-even, computed as
// self - divmod_near(self, 10**-ndigits)[1].
//
// CPython: Objects/longobject.c:6111 int___round___impl
func intRoundMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: __round__() takes at most 1 argument (%d given)", len(args)-1)
	}
	self, ok := asInt(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__round__' requires a 'int' object")
	}
	val := self.BigInt()
	if len(args) == 1 || args[1] == None() {
		return NewIntFromBig(val), nil
	}
	nd, err := NumberIndex(args[1])
	if err != nil {
		return nil, err
	}
	ndigits := nd.(*Int).BigInt()
	// ndigits >= 0: no rounding necessary, return self unchanged.
	if ndigits.Sign() >= 0 {
		return NewIntFromBig(val), nil
	}
	// b = 10 ** -ndigits.
	negNd := new(big.Int).Neg(ndigits)
	b := new(big.Int).Exp(big.NewInt(10), negNd, nil)
	_, r := divmodNear(val, b)
	// result = self - r.
	return NewIntFromBig(new(big.Int).Sub(val, r)), nil
}

// divmodNear returns (q, r) where q is the nearest integer to a/b using
// round-half-to-even and r == a - q*b. b is assumed positive here (it is
// always 10**n for n > 0), so floor division matches Python's divmod.
//
// CPython: Objects/longobject.c:6013 _PyLong_DivmodNear
func divmodNear(a, b *big.Int) (*big.Int, *big.Int) {
	q := new(big.Int)
	r := new(big.Int)
	q.DivMod(a, b, r) // Euclidean: r in [0, b) since b > 0.
	// greater_than_half = 2*r > b (b > 0); exactly_half = 2*r == b.
	twiceR := new(big.Int).Lsh(r, 1)
	cmp := twiceR.Cmp(b)
	quoIsOdd := q.Bit(0) == 1
	if cmp > 0 || (cmp == 0 && quoIsOdd) {
		q.Add(q, big.NewInt(1))
		r.Sub(r, b)
	}
	return q, r
}

// intGetNewArgsMethod implements int.__getnewargs__. It returns a
// 1-tuple holding a plain int copy of self's value so an int subclass
// reconstructs through __newobj__(cls, int(self)) under pickle.
//
// CPython: Objects/longobject.c:6178 long___getnewargs___impl
func intGetNewArgsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getnewargs__() takes no arguments (%d given)", len(args)-1)
	}
	i, ok := asInt(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getnewargs__' requires a 'int' object")
	}
	return NewTuple([]Object{NewIntFromBig(i.BigInt())}), nil
}

// intRealGetter backs int.real and int.numerator: returns self when
// type(self) is int, else a fresh int with the same value so that
// `type(x.real) is int` holds for subclasses too (matching long_long).
//
// CPython: Objects/longobject.c:6195 long_long
func intRealGetter(owner Object) (Object, error) {
	i, ok := asInt(owner)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'real' for 'int' objects doesn't apply to a '%s' object", typeNameOf(owner))
	}
	if owner.Type() == IntType {
		return owner, nil
	}
	return NewIntFromBig(i.BigInt()), nil
}

// intImagGetter backs int.imag: always 0.
//
// CPython: Objects/longobject.c:6210 long_get0
func intImagGetter(_ Object) (Object, error) {
	return NewInt(0), nil
}

// intDenominatorGetter backs int.denominator: always 1.
//
// CPython: Objects/longobject.c:6216 long_get1
func intDenominatorGetter(_ Object) (Object, error) {
	return NewInt(1), nil
}

// intReprDescr backs int.__dict__["__repr__"] and int.__dict__["__str__"].
// CPython generates these via add_operators from slotdefs (TPSLOT __repr__
// + PyLong_Type.tp_repr). Without the descriptor, Python code that pulls
// int.__repr__ as a value, like json.encoder._make_iterencode's
// `_intstr=int.__repr__` default, would inherit object.__repr__ and emit
// `<int object at 0x...>` instead of the decimal digit string.
//
// Why this is needed even though IntType.Repr already exists in Go:
// IntType.Repr is the runtime slot the interpreter uses for builtins like
// repr(x) or implicit str(x); it does not show up in the type's __dict__.
// Python code that uses bound-method lookup such as
// `int.__repr__(123)` or captures the attribute as a default parameter
// (json.encoder's `_intstr=int.__repr__`) needs an actual descriptor on
// the type. CPython's add_operators emits this descriptor automatically
// from slotdefs; gopy does not run that loop yet (task #647), so the
// two slots have to be installed manually for the types that
// pyperformance and the stdlib reach for. Same pattern as
// float.__repr__ / __str__ in objects/float.go.
//
// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __repr__)
// CPython: Objects/longobject.c:1762 long_repr
func intReprDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	v, err := bigIntFromIntLike(args[0])
	if err != nil {
		return nil, err
	}
	if err := checkIntToStrLimit(v.BitLen()); err != nil {
		return nil, err
	}
	s := v.String()
	if IntMaxStrDigitsHook != nil {
		if limit := IntMaxStrDigitsHook(); limit > 0 {
			n := len(s)
			if n > 0 && s[0] == '-' {
				n--
			}
			if int32(n) > limit {
				return nil, fmt.Errorf("ValueError: Exceeds the limit (%d digits) for integer string conversion; use sys.set_int_max_str_digits() to increase the limit", limit)
			}
		}
	}
	return NewStr(s), nil
}

// intSizeofMethod ports int.__sizeof__: tp_basicsize + tp_itemsize *
// max(ndigits, 1) where ndigits is the count of 30-bit limbs needed to
// hold abs(self). CPython always allocates space for at least one
// digit even when the value is zero.
//
// CPython: Objects/longobject.c:6176 int___sizeof___impl
// intHashMethod ports int.__hash__: hashes self through long_hash and
// returns the result as a plain int. Registering it as a descriptor keeps
// int.__hash__ from resolving to object.__hash__ and lets fixupHashAndIter
// install long_hash as the tp_hash slot on int subclasses.
//
// CPython: Objects/longobject.c:3287 long_hash
func intHashMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __hash__() takes no arguments (%d given)", len(args)-1)
	}
	h, err := intHash(args[0])
	if err != nil {
		return nil, err
	}
	return NewInt(h), nil
}

func intSizeofMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __sizeof__() takes no arguments (%d given)", len(args)-1)
	}
	i, ok := args[0].(*Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__sizeof__' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	const shift = 30
	bits := i.v.BitLen()
	ndigits := (bits + shift - 1) / shift
	if ndigits < 1 {
		ndigits = 1
	}
	bs := typeBasicSize(i.Type())
	is := typeItemSize(i.Type())
	return NewInt(int64(bs + is*ndigits)), nil
}

// intAsIntegerRatioMethod ports int.as_integer_ratio(): for an int v
// returns (v, 1). Mirrors float.as_integer_ratio's API so callers can
// duck-type over Real-protocol numbers.
//
// CPython: Objects/longobject.c:6263 int_as_integer_ratio_impl
func intAsIntegerRatioMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: as_integer_ratio() takes no arguments (%d given)", len(args)-1)
	}
	i, ok := asInt(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'as_integer_ratio' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	// Subclasses (including bool) return a fresh plain int as the
	// numerator, matching long_long's behavior.
	num := args[0]
	if args[0].Type() != IntType {
		num = NewIntFromBig(i.BigInt())
	}
	return NewTuple([]Object{num, NewInt(1)}), nil
}

// intIsIntegerMethod ports int.is_integer(): always True. Exists for
// duck-type compatibility with float.is_integer.
//
// CPython: Objects/longobject.c:6415 int_is_integer_impl
func intIsIntegerMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: is_integer() takes no arguments (%d given)", len(args)-1)
	}
	if _, ok := asInt(args[0]); !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'is_integer' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return True(), nil
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
	if typ == BoolType {
		return NewBool(val.Sign() != 0), nil
	}
	// Subclass path: invoke type(long_obj) so user-defined __new__ runs.
	// CPython: Objects/longobject.c:6402 PyObject_CallOneArg((PyObject *)type, long_obj)
	return Call(typ, NewTuple([]Object{NewIntFromBig(val)}), nil)
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

// bytesLike extracts the underlying byte slice from a bytes/bytearray/iterable
// object. Mirrors PyObject_Bytes followed by PyBytes_FromObject: first call
// __bytes__ when present (which must return bytes), otherwise reject str and
// int explicitly and fall back to iterating an iterable of 0..255 ints.
//
// CPython: Objects/object.c:870 PyObject_Bytes
// CPython: Objects/bytesobject.c:2818 PyBytes_FromObject
// CPython: Objects/longobject.c:6380 int_from_bytes_impl
func bytesLike(o Object) ([]byte, error) {
	switch v := o.(type) {
	case *Bytes:
		return v.Bytes(), nil
	case *ByteArray:
		return v.Bytes(), nil
	case *MemoryView:
		// PyBytes_FromObject acquires a buffer, so a released view raises
		// ValueError before any copy happens.
		//
		// CPython: Objects/bytesobject.c:2818 PyBytes_FromObject
		if err := v.checkReleased(); err != nil {
			return nil, err
		}
		return v.buf, nil
	}
	if descr, _ := LookupDescriptor(o.Type(), "__bytes__"); descr != nil {
		fn, err := bindDescriptor(descr, o)
		if err != nil {
			return nil, err
		}
		out, err := Call(fn, NewTuple(nil), nil)
		if err != nil {
			return nil, err
		}
		b, ok := out.(*Bytes)
		if !ok {
			return nil, fmt.Errorf("TypeError: __bytes__ returned non-bytes (type %s)", typeNameOf(out))
		}
		return b.Bytes(), nil
	}
	switch o.(type) {
	case *Unicode, *Int, *Bool, *Float, *Complex:
		return nil, fmt.Errorf("TypeError: cannot convert '%s' object to bytes", typeNameOf(o))
	}
	iter, err := Iter(o)
	if err != nil {
		return nil, fmt.Errorf("TypeError: cannot convert '%s' object to bytes", typeNameOf(o))
	}
	var out []byte
	for {
		item, iterErr := IterNext(iter)
		if iterErr != nil {
			if !errors.Is(iterErr, ErrStopIteration) {
				return nil, iterErr
			}
			break
		}
		iv, ok := item.(*Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: bytes must be in range(0, 256)")
		}
		n, ok2 := iv.Int64()
		if !ok2 || n < 0 || n > 255 {
			return nil, fmt.Errorf("ValueError: bytes must be in range(0, 256)")
		}
		out = append(out, byte(n))
	}
	return out, nil
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
		// CPython: Objects/longobject.c:1031 _PyLong_AsByteArray treats the
		// zero case as trivially fitting in any non-negative length, so
		// (0).to_bytes(0, signed=True) returns b''. Without the explicit
		// guard, length=0 with signed=True would compute maxBits = -1 and
		// reject a perfectly valid call.
		if u.Sign() != 0 {
			maxBits := length * 8
			if signed {
				maxBits = length*8 - 1
			}
			if maxBits < 0 || u.BitLen() > maxBits {
				return nil, fmt.Errorf("OverflowError: int too big to convert")
			}
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
	var i *Int
	switch v := args[0].(type) {
	case *Int:
		i = v
	case *Bool:
		i = &v.Int
	default:
		return nil, fmt.Errorf("TypeError: descriptor 'this' for 'int' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	if i.Type() == IntType {
		return i, nil
	}
	// For subclasses, return a fresh int with the same value so callers
	// that compare `type(x) is int` still get a plain int.
	return NewIntFromBig(i.BigInt()), nil
}
