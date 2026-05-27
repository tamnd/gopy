// __format__ wiring for int (and bool, which inherits int's slot in
// CPython by way of tp_format). The format-spec mini-language parser
// and integer renderer live in the format package; this file just
// glues the IntType.Format slot to it, plus the float-spec coercion
// branch CPython takes for 'e'/'E'/'f'/'F'/'g'/'G'/'%' types.
//
// Findings worth noting for future readers (the things that took the
// longest to track down):
//
//  1. IntType.Format and BoolType.Format had no wiring, so any
//     non-empty spec hit objectFormatDescr's TypeError fallback. That
//     alone was enough to break json.encoder at import time, since
//     ESCAPE_DCT initializes via '\\u{0:04x}'.format(i). The format/
//     package already shipped a complete CPython-equivalent
//     mini-language parser and integer renderer; the only missing
//     piece was attaching them to the type.
//
//  2. CPython "inherits" the Format slot for bool via tp_base =
//     PyLong_Type plus inherit_slots walking the base chain. gopy's
//     type machinery does not walk the base chain for built-in Format
//     slots, so the wiring has to be explicit. Setting
//     BoolType.Format = intFormat mirrors what inherit_slots would
//     have produced.
//
//  3. The 'e'/'E'/'f'/'F'/'g'/'G'/'%' branch matches CPython's
//     format_long_internal: it promotes the int through
//     PyNumber_Float and dispatches to the float renderer. We mirror
//     that via bigIntToFloat64 + format.FormatFloat. This is how
//     '{:.2g}'.format(255) -> '2.6e+02' is supposed to come out, even
//     though the input was an int.
//
//  4. PyLong_AsDouble's overflow semantics: raise OverflowError when
//     the magnitude exceeds float64 range. big.Float.Float64()
//     returns +/-Inf for out-of-range values, so we test for Inf and
//     turn it into the same OverflowError CPython raises. The
//     big.Float Accuracy flag is *not* a reliable overflow signal: it
//     is also set Above/Below for ordinary rounding, so checking it
//     here would false-positive on every non-representable integer.
//
// CPython: Python/formatter_unicode.c:1589 _PyLong_FormatAdvancedWriter

package objects

import (
	"fmt"
	"math"
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
	case 0, 'b', 'c', 'd', 'o', 'x', 'X', 'n':
		out, err := format.FormatInt(v, parsed)
		if err != nil {
			return "", fmt.Errorf("ValueError: %w", err)
		}
		return out, nil
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
	return "", unknownPresentationType(parsed.Type, o.Type().Name)
}

// unknownPresentationType ports the unknown_presentation_type helper
// CPython uses across the four __format__ slots. The %c / \\x%x split
// keeps the rendered ValueError byte-identical to PyErr_Format's
// output when the presentation byte is outside printable ASCII.
//
// CPython: Python/formatter_unicode.c:14 unknown_presentation_type
func unknownPresentationType(presentationType byte, typeName string) error {
	if presentationType > 32 && presentationType < 128 {
		return fmt.Errorf("ValueError: Unknown format code '%c' for object of type '%s'", presentationType, typeName)
	}
	return fmt.Errorf("ValueError: Unknown format code '\\x%x' for object of type '%s'", uint(presentationType), typeName)
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
// us the same rounding semantics with one allocation, and big.Float's
// accuracy flag signals whether the result was clamped to +/-Inf.
//
// CPython: Objects/longobject.c:3038 PyLong_AsDouble
func bigIntToFloat64(v *big.Int) (float64, error) {
	f, _ := new(big.Float).SetInt(v).Float64()
	if math.IsInf(f, 0) {
		return 0, fmt.Errorf("OverflowError: int too large to convert to float")
	}
	return f, nil
}
