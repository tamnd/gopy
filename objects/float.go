package objects

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Float is the Python float, an IEEE-754 double.
//
// CPython: Include/cpython/floatobject.h:L7 PyFloatObject
type Float struct {
	Header
	v float64
}

// FloatType is the type singleton for float. Mirrors PyFloat_Type.
// Slots are wired in init() because floatHash transitively constructs
// Ints which would otherwise close the dep cycle.
//
// CPython: Objects/floatobject.c:L2068 PyFloat_Type
var FloatType = NewType("float", []*Type{objectType})

func init() {
	FloatType.Repr = floatRepr
	FloatType.Str = floatRepr
	FloatType.Hash = floatHash
	FloatType.RichCmp = floatRichCmp
	FloatType.Number = &NumberMethods{
		Add:         floatAdd,
		Subtract:    floatSub,
		Multiply:    floatMul,
		TrueDivide:  floatTrueDiv,
		FloorDivide: floatFloorDiv,
		Remainder:   floatMod,
		Divmod:      floatDivmod,
		Negative:    floatNeg,
		Positive:    func(o Object) (Object, error) { return o, nil },
		Absolute:    floatAbs,
		Bool:        floatBool,
		Float:       func(o Object) (Object, error) { return o, nil },
	}
}

// NewFloat builds a float.
//
// CPython: Objects/floatobject.c:L132 PyFloat_FromDouble
func NewFloat(x float64) *Float {
	o := &Float{v: x}
	o.init(FloatType)
	return o
}

// Float64 returns the underlying double.
func (f *Float) Float64() float64 {
	return f.v
}

func floatRepr(o Object) (string, error) {
	return formatFloatShort(o.(*Float).v), nil
}

// formatFloatShort mirrors PyOS_double_to_string('r', 0,
// Py_DTSF_ADD_DOT_0): emit the shortest decimal digit string that
// round-trips to the same double, then choose between fixed and
// exponential layout the same way CPython does. CPython switches to
// exponent form when the decimal-point position is <= -4 or > 16;
// Go's strconv.FormatFloat 'g' format flips earlier, so this routine
// re-derives the layout by going through 'e' first.
//
// CPython: Python/pystrtod.c:1265 PyOS_double_to_string
func formatFloatShort(v float64) string {
	switch {
	case math.IsNaN(v):
		return "nan"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(v, 'e', -1, 64)
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	mantissa, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := mantissa
	if intPart, frac, hasDot := strings.Cut(mantissa, "."); hasDot {
		digits = intPart + frac
	}
	decpt := exp + 1
	if decpt <= -4 || decpt > 16 {
		return formatFloatExp(neg, digits, decpt)
	}
	return formatFloatFixed(neg, digits, decpt)
}

// formatFloatExp lays the digits out as d.dddde+NN, mirroring the
// 'e' branch inside CPython's format_float_short.
//
// CPython: Python/pystrtod.c:1027 format_float_short (e branch)
func formatFloatExp(neg bool, digits string, decpt int) string {
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteByte(digits[0])
	if len(digits) > 1 {
		b.WriteByte('.')
		b.WriteString(digits[1:])
	}
	b.WriteByte('e')
	e := decpt - 1
	if e >= 0 {
		b.WriteByte('+')
	} else {
		b.WriteByte('-')
		e = -e
	}
	if e < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.Itoa(e))
	return b.String()
}

// formatFloatFixed lays the digits out as ddd.ddd, padding zeros on
// either side as needed and adding a trailing ".0" for integral
// values (the Py_DTSF_ADD_DOT_0 flag CPython's float_repr passes in).
//
// CPython: Python/pystrtod.c:1027 format_float_short (f branch)
func formatFloatFixed(neg bool, digits string, decpt int) string {
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	switch {
	case decpt <= 0:
		b.WriteString("0.")
		for i := 0; i < -decpt; i++ {
			b.WriteByte('0')
		}
		b.WriteString(digits)
	case decpt >= len(digits):
		b.WriteString(digits)
		for i := 0; i < decpt-len(digits); i++ {
			b.WriteByte('0')
		}
		b.WriteString(".0")
	default:
		b.WriteString(digits[:decpt])
		b.WriteByte('.')
		b.WriteString(digits[decpt:])
	}
	return b.String()
}

// floatHash maps a float to the same hash as the equivalent int when
// the float is integral, matching CPython's `hash` invariant. The
// full algorithm uses the modulus 2^61-1; v0.2 ships a simplified
// variant good enough for the gate. Real floats land in v0.4.
//
// CPython: Python/pyhash.c:L83 _Py_HashDouble
func floatHash(o Object) (int64, error) {
	v := o.(*Float).v
	if math.IsNaN(v) {
		return 0, nil
	}
	if v == math.Trunc(v) && !math.IsInf(v, 0) {
		return intHash(NewInt(int64(v)))
	}
	const modulus int64 = (1 << 61) - 1
	bits := math.Float64bits(v)
	h := int64(bits) % modulus
	if h == -1 {
		h = -2
	}
	return h, nil
}

func floatRichCmp(a, b Object, op CompareOp) (Object, error) {
	af, ok := a.(*Float)
	if !ok {
		return nil, fmt.Errorf("floatRichCmp: lhs is %T", a)
	}
	var bv float64
	switch x := b.(type) {
	case *Float:
		bv = x.v
	case *Int:
		bv = intToFloat(x)
	default:
		return notImplemented(), nil
	}
	res := compareFloats(af.v, bv, op)
	if res {
		return True(), nil
	}
	return False(), nil
}

func compareFloats(a, b float64, op CompareOp) bool {
	switch op {
	case CompareLT:
		return a < b
	case CompareLE:
		return a <= b
	case CompareEQ:
		return a == b
	case CompareNE:
		return a != b
	case CompareGT:
		return a > b
	case CompareGE:
		return a >= b
	}
	return false
}

func floatAdd(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af + bf), nil
}

func floatSub(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af - bf), nil
}

func floatMul(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af * bf), nil
}

func floatNeg(o Object) (Object, error) {
	return NewFloat(-o.(*Float).v), nil
}

// floatAbs ports float_abs.
//
// CPython: Objects/floatobject.c float_abs
func floatAbs(o Object) (Object, error) {
	return NewFloat(math.Abs(o.(*Float).v)), nil
}

// floatDivmod ports float_divmod, returning (floor(a/b), a - b*floor(a/b)).
//
// CPython: Objects/floatobject.c float_divmod
func floatDivmod(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: float divmod()")
	}
	q := math.Floor(af / bf)
	r := math.Mod(af, bf)
	if r != 0 && (r < 0) != (bf < 0) {
		r += bf
	}
	return NewTuple([]Object{NewFloat(q), NewFloat(r)}), nil
}

// floatTrueDiv mirrors CPython's float_true_divide; division by zero
// raises ZeroDivisionError rather than producing inf.
//
// CPython: Objects/floatobject.c float_true_divide
func floatTrueDiv(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: float division by zero")
	}
	return NewFloat(af / bf), nil
}

// floatFloorDiv returns floor(a/b) as a float, matching Python's
// `__floordiv__`. The result is still a float when both operands are
// float; the int / int case stays in intFloorDiv.
//
// CPython: Objects/floatobject.c float_floor_div
func floatFloorDiv(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: float floor division by zero")
	}
	return NewFloat(math.Floor(af / bf)), nil
}

// floatMod implements Python `%` for floats, where the sign of the
// result matches the divisor (like math.Mod, then adjusted).
//
// CPython: Objects/floatobject.c float_rem
func floatMod(a, b Object) (Object, error) {
	af, bf, ok := floatPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: float modulo")
	}
	r := math.Mod(af, bf)
	if r != 0 && (r < 0) != (bf < 0) {
		r += bf
	}
	return NewFloat(r), nil
}

func floatBool(o Object) (bool, error) {
	return o.(*Float).v != 0, nil
}

// intToFloat promotes an Int operand to float64. Loses precision for
// values outside float64's exact range, matching CPython's
// PyFloat_AsDouble behavior on a long that does not fit.
//
// CPython: Objects/floatobject.c:L255 PyFloat_FromString (analog)
func intToFloat(i *Int) float64 {
	if v, ok := i.Int64(); ok {
		return float64(v)
	}
	f, _ := i.v.Float64()
	return f
}

func floatPair(a, b Object) (af, bf float64, ok bool) {
	af, aOk := asFloat(a)
	bf, bOk := asFloat(b)
	if !aOk || !bOk {
		return 0, 0, false
	}
	return af, bf, true
}

func asFloat(o Object) (float64, bool) {
	switch x := o.(type) {
	case *Float:
		return x.v, true
	case *Int:
		return intToFloat(x), true
	}
	return 0, false
}
