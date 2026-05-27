// Value-coercion helpers shared by typecodes.go and the sequence/
// method handlers. Each helper mirrors the relevant PyArg_Parse
// format code or PyLong/PyFloat conversion path in CPython.

package array

import (
	"fmt"
	"math"
	"math/big"

	"github.com/tamnd/gopy/objects"
)

// indexAsInt coerces a Python integer-shaped value to a Go int via
// the __index__ slot. Used by the integer arms of mp_subscript /
// mp_ass_subscript / mp_delitem.
//
// CPython: Objects/abstract.c:1356 PyNumber_AsSsize_t
func indexAsInt(o objects.Object) (int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return 0, err
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C ssize_t")
	}
	return int(v), nil
}

// asLongRange coerces v to int64 and verifies it falls in [lo, hi].
// Mirrors PyArg_Parse's 'h'/'i'/'b' formatters used by the small
// signed-int setitems: the conversion goes through __index__ and the
// overflow check uses the typecode-specific bound text from the
// CPython error strings.
//
// CPython: Modules/arraymodule.c:243 b_setitem (and h_setitem, i_setitem)
func asLongRange(v objects.Object, lo, hi int64, typeName string) (int64, error) {
	x, err := asInt64(v, true, typeName)
	if err != nil {
		return 0, err
	}
	if x < lo {
		return 0, fmt.Errorf("OverflowError: %s is less than minimum", typeName)
	}
	if x > hi {
		return 0, fmt.Errorf("OverflowError: %s is greater than maximum", typeName)
	}
	return x, nil
}

// asUlongRange coerces v to uint64 and verifies it falls in [0, hi].
// Used by the small unsigned-int setitems ('B', 'H', 'I').
//
// CPython: Modules/arraymodule.c:419 HH_setitem (and II_setitem)
func asUlongRange(v objects.Object, hi uint64, typeName string) (uint64, error) {
	x, err := asUint64(v, typeName)
	if err != nil {
		return 0, err
	}
	if x > hi {
		return 0, fmt.Errorf("OverflowError: %s is greater than maximum", typeName)
	}
	return x, nil
}

// asInt64 coerces v through __index__ to an int64. The signed flag
// is informational, used only in error messages: the conversion is
// the same regardless because Python ints are arbitrary precision.
//
// CPython: Modules/arraymodule.c:479 l_setitem (PyLong_AsLong)
func asInt64(v objects.Object, _ bool, typeName string) (int64, error) {
	idx, err := objects.NumberIndex(v)
	if err != nil {
		return 0, fmt.Errorf("TypeError: %s: %w", "array item must be integer", err)
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	x, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to %s", typeName)
	}
	return x, nil
}

// asUint64 coerces v through __index__ to a uint64. Negative values
// raise OverflowError to match PyLong_AsUnsignedLong's behavior.
//
// CPython: Modules/arraymodule.c:498 LL_setitem (PyLong_AsUnsignedLong)
func asUint64(v objects.Object, typeName string) (uint64, error) {
	idx, err := objects.NumberIndex(v)
	if err != nil {
		return 0, fmt.Errorf("TypeError: %s: %w", "array item must be integer", err)
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
	}
	bi := i.BigInt()
	if bi.Sign() < 0 {
		return 0, fmt.Errorf("OverflowError: can't convert negative value to %s", typeName)
	}
	if !bi.IsUint64() {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to %s", typeName)
	}
	return bi.Uint64(), nil
}

// asFloat coerces v to float64 via int and float operand recognition.
// Mirrors PyFloat_AsDouble: ints promote, floats pass through, every
// other type is a TypeError.
//
// CPython: Modules/arraymodule.c:618 f_setitem (PyFloat_AsDouble)
func asFloat(v objects.Object) (float64, error) {
	switch x := v.(type) {
	case *objects.Float:
		return x.Float64(), nil
	case *objects.Int:
		if iv, ok := x.Int64(); ok {
			return float64(iv), nil
		}
		f, _ := new(big.Float).SetInt(x.BigInt()).Float64()
		return f, nil
	case *objects.Bool:
		if x == objects.True() {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("TypeError: must be real number, not %s", v.Type().Name)
}

// newIntFromUint64 builds a Python int from a uint64. NewInt only
// accepts int64; values in (math.MaxInt64, math.MaxUint64] need the
// math/big.Int constructor to avoid silent truncation.
func newIntFromUint64(x uint64) *objects.Int {
	if x <= math.MaxInt64 {
		return objects.NewInt(int64(x))
	}
	return objects.NewIntFromBig(new(big.Int).SetUint64(x))
}

// float32FromBits / float32Bits / float64FromBits / float64Bits are
// thin wrappers over math's IEEE helpers, named to read closer to the
// CPython _PyFloat_Pack4 / _PyFloat_Pack8 functions they replace.
//
// CPython: Python/pyhash.c IEEE helpers
func float32FromBits(b uint32) float32 { return math.Float32frombits(b) }
func float32Bits(f float32) uint32     { return math.Float32bits(f) }
func float64FromBits(b uint64) float64 { return math.Float64frombits(b) }
func float64Bits(f float64) uint64     { return math.Float64bits(f) }
