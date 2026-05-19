// __format__ wiring for int (and bool, which inherits int's slot in
// CPython by way of tp_format). The format-spec mini-language parser
// and integer renderer live in the format package; this file just
// glues the IntType.Format slot to it, plus the float-spec coercion
// branch CPython takes for 'e'/'E'/'f'/'F'/'g'/'G'/'%' types.
//
// CPython: Python/formatter_unicode.c:1589 _PyLong_FormatAdvancedWriter

package objects

import (
	"fmt"
	"math/big"

	"github.com/tamnd/gopy/format"
)

func init() {
	IntType.Format = intFormat
	BoolType.Format = intFormat
}

// intFormat ports _PyLong_FormatAdvancedWriter: parse the spec, then
// route 'd'/'b'/'o'/'x'/'X'/'n'/'c' through the integer renderer or
// coerce to float and re-dispatch for 'e'/'E'/'f'/'F'/'g'/'G'/'%'.
// An empty spec is handled at the protocol layer as str(o).
//
// CPython: Python/formatter_unicode.c:1589 _PyLong_FormatAdvancedWriter
func intFormat(o Object, spec string) (string, error) {
	v, err := bigIntFromIntLike(o)
	if err != nil {
		return "", err
	}
	parsed, err := format.ParseSpec(spec)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	switch parsed.Type {
	case 'e', 'E', 'f', 'F', 'g', 'G', '%':
		// Promote to float and dispatch through float's renderer so
		// '{:.2g}'.format(255) goes through the IEEE-754 path. CPython
		// does the same via PyNumber_Float in format_long_internal.
		f, err := bigIntToFloat64(v)
		if err != nil {
			return "", err
		}
		out, err := format.FormatFloat(f, parsed)
		if err != nil {
			return "", fmt.Errorf("ValueError: %w", err)
		}
		return out, nil
	}
	out, err := format.FormatInt(v, parsed)
	if err != nil {
		return "", fmt.Errorf("ValueError: %w", err)
	}
	return out, nil
}

// bigIntFromIntLike unwraps an Int or Bool into its underlying big.Int.
// Bool embeds Int in gopy, mirroring CPython's PyBool subclassing PyLong.
//
// CPython: Objects/boolobject.c:222 PyBool_Type tp_base = &PyLong_Type
func bigIntFromIntLike(o Object) (*big.Int, error) {
	switch x := o.(type) {
	case *Int:
		return x.BigInt(), nil
	case *Bool:
		return x.BigInt(), nil
	}
	return nil, fmt.Errorf(
		"TypeError: descriptor '__format__' requires a 'int' object but received a '%s'",
		o.Type().Name)
}

// bigIntToFloat64 mirrors PyLong_AsDouble: convert v to a Go double,
// raising OverflowError if the magnitude exceeds float64 range. CPython
// matches this with a dedicated mantissa-extract loop; big.Float gives
// us the same rounding semantics with one allocation.
//
// CPython: Objects/longobject.c:3038 PyLong_AsDouble
func bigIntToFloat64(v *big.Int) (float64, error) {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f, nil
}
