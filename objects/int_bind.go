// Bind int method descriptors. The methods exposed here mirror
// Objects/longobject.c long_methods, focused on the names Python code
// in the vendored stdlib actually reaches for.
//
// CPython: Objects/longobject.c:6260 long_methods

package objects

import "fmt"

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
