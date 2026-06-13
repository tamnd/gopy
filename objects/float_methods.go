// float method extensions: as_integer_ratio, is_integer, hex, fromhex.
//
// CPython: Objects/floatobject.c

package objects

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

func init() {
	// METH_NOARGS rows route the arity TypeError through _PyObject_FunctionStr
	// via methodDescrCheckArity, yielding "float.hex() takes no arguments
	// (N given)" etc.
	//
	// CPython: Objects/clinic/floatobject.c.h float_methods flags
	// CPython: Objects/floatobject.c:1492 float_as_integer_ratio_impl
	SetTypeDescr(FloatType, "as_integer_ratio", NewMethodDescrConv(FloatType, "as_integer_ratio", MethNoArgs, floatAsIntegerRatio))
	// CPython: Objects/floatobject.c:845 float_is_integer_impl
	SetTypeDescr(FloatType, "is_integer", NewMethodDescrConv(FloatType, "is_integer", MethNoArgs, floatIsInteger))
	// CPython: Objects/floatobject.c:884 float___floor___impl
	SetTypeDescr(FloatType, "__floor__", NewMethodDescrConv(FloatType, "__floor__", MethNoArgs, floatFloor))
	// CPython: Objects/floatobject.c:895 float___ceil___impl
	SetTypeDescr(FloatType, "__ceil__", NewMethodDescrConv(FloatType, "__ceil__", MethNoArgs, floatCeil))
	// CPython: Objects/floatobject.c:868 float___trunc___impl
	SetTypeDescr(FloatType, "__trunc__", NewMethodDescrConv(FloatType, "__trunc__", MethNoArgs, floatTrunc))
	// CPython: Objects/floatobject.c:1164 float_hex_impl
	SetTypeDescr(FloatType, "hex", NewMethodDescrConv(FloatType, "hex", MethNoArgs, floatHex))
	// CPython: Objects/floatobject.c:1235 float_fromhex_impl — classmethod
	SetTypeDescr(FloatType, "fromhex", NewClassMethod(NewBuiltinFunction("float.fromhex", floatFromHex)))
	// CPython: Objects/floatobject.c:1649 float_from_number_impl — classmethod
	SetTypeDescr(FloatType, "from_number", NewClassMethod(NewBuiltinFunction("float.from_number", floatFromNumber)))
	// CPython: Objects/floatobject.c:797 float_richcompare — expose binary ops as descriptors
	// float.__truediv__(self, other) so that float(n).__truediv__(d) works.
	// CPython: Objects/floatobject.c float_true_divide (slot wrapper)
	SetTypeDescr(FloatType, "__truediv__", NewMethodDescr(FloatType, "__truediv__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __truediv__ expected 2 arguments, got %d", len(args))
			}
			return floatTrueDiv(args[0], args[1])
		}))
	// CPython: Objects/floatobject.c:1561 float___getnewargs___impl
	SetTypeDescr(FloatType, "__getnewargs__", NewMethodDescrConv(FloatType, "__getnewargs__", MethNoArgs, floatGetNewArgs))
	// CPython: Objects/floatobject.c:1034 float___round___impl
	SetTypeDescr(FloatType, "__round__", NewMethodDescr(FloatType, "__round__", floatRoundMethod))
}

// floatRoundMethod implements float.__round__(ndigits=None). The
// no-argument / None form returns an int; the two-argument form returns
// a float. Both round halfway cases to even.
//
// CPython: Objects/floatobject.c:1034 float___round___impl
func floatRoundMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: __round__() takes at most 1 argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__round__' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	ndigits := None()
	if len(args) == 2 {
		ndigits = args[1]
	}
	return FloatRoundImpl(self.Float64(), ndigits)
}

// FloatRoundImpl is the shared float-rounding kernel behind both
// float.__round__ and the round() builtin. With None ndigits it rounds
// to the nearest integer (round-half-to-even) and returns an int; with
// an integer ndigits it returns a float rounded to that many decimal
// places using the dtoa path.
//
// CPython: Objects/floatobject.c:1034 float___round___impl
func FloatRoundImpl(x float64, ndigits Object) (Object, error) {
	if IsNone(ndigits) {
		// CPython: Objects/longobject.c:458 PyLong_FromDouble
		if math.IsInf(x, 0) {
			return nil, errors.New("OverflowError: cannot convert float infinity to integer")
		}
		if math.IsNaN(x) {
			return nil, errors.New("ValueError: cannot convert float NaN to integer")
		}
		rounded := math.Round(x)
		if math.Abs(x-rounded) == 0.5 {
			rounded = 2 * math.Round(x/2)
		}
		bi, _ := big.NewFloat(rounded).Int(nil)
		return NewIntFromBig(bi), nil
	}
	n, err := NumberIndex(ndigits)
	if err != nil {
		return nil, err
	}
	nb := n.(*Int).BigInt()
	nv, fits := intExactInt64(nb)
	if !fits {
		// Same clamp as float___round___impl's NDIGITS_MAX/_MIN: way
		// outside the meaningful precision range, so x rounds to itself
		// or to a signed zero.
		if nb.Sign() > 0 {
			return NewFloat(x), nil
		}
		return NewFloat(0.0 * x), nil
	}
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return NewFloat(x), nil
	}
	const (
		ndigitsMax = 323
		ndigitsMin = -308
	)
	if nv > ndigitsMax {
		return NewFloat(x), nil
	}
	if nv < ndigitsMin {
		return NewFloat(0.0 * x), nil
	}
	z, ok := floatDoubleRound(x, int(nv))
	if !ok {
		return nil, errors.New("OverflowError: overflow occurred during round")
	}
	return NewFloat(z), nil
}

// floatDoubleRound mirrors CPython's double_round_double: it rounds x to
// ndigits decimal places using a dtoa-based conversion so the result is
// correctly rounded rather than suffering the precision loss of a naive
// multiply-round-divide.
//
// CPython: Objects/floatobject.c:874 double_round_double
func floatDoubleRound(x float64, ndigits int) (float64, bool) {
	if ndigits < 0 {
		pow1 := math.Pow(10, float64(-ndigits))
		y := x / pow1
		z := math.Round(y)
		if math.Abs(y-z) == 0.5 {
			z = 2 * math.Round(y/2)
		}
		result := z * pow1
		if math.IsInf(result, 0) {
			return 0, false
		}
		return result, true
	}
	s := strconv.FormatFloat(x, 'f', ndigits, 64)
	z, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(z, 0) {
		return 0, false
	}
	return z, true
}

// intExactInt64 reports whether b fits in an int64 and returns the value.
func intExactInt64(b *big.Int) (int64, bool) {
	if b.IsInt64() {
		return b.Int64(), true
	}
	return 0, false
}

// floatGetNewArgs implements float.__getnewargs__. It returns a 1-tuple
// holding a plain float copy of self so a float subclass round-trips
// through pickle via __newobj__(cls, float(self)).
//
// CPython: Objects/floatobject.c:1561 float___getnewargs___impl
func floatGetNewArgs(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getnewargs__() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := asFloat(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getnewargs__' requires a 'float' object")
	}
	return NewTuple([]Object{NewFloat(f)}), nil
}

// floatAsIntegerRatio returns (numerator, denominator) such that
// numerator/denominator == self exactly, in lowest terms with positive denominator.
//
// CPython: Objects/floatobject.c:1492 float_as_integer_ratio_impl
func floatAsIntegerRatio(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: as_integer_ratio() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'as_integer_ratio' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	v := f.Float64()
	if math.IsInf(v, 0) {
		return nil, fmt.Errorf("OverflowError: cannot convert Infinity to integer ratio")
	}
	if math.IsNaN(v) {
		return nil, fmt.Errorf("ValueError: cannot convert NaN to integer ratio")
	}

	// frexp: v == floatPart * 2**exp exactly
	floatPart, exp := math.Frexp(v)

	// multiply floatPart by 2 until it is an integer (at most 53 iterations
	// for IEEE-754 double; CPython uses 300 as a safety margin)
	for i := 0; i < 300 && floatPart != math.Floor(floatPart); i++ {
		floatPart *= 2.0
		exp--
	}

	numBig := new(big.Int)
	fBig := new(big.Float).SetPrec(64).SetFloat64(floatPart)
	fBig.Int(numBig)
	numerator := NewIntFromBig(numBig)

	denomBig := new(big.Int).SetInt64(1)

	// fold in 2**exp
	shift := new(big.Int).SetInt64(1)
	if exp > 0 {
		shift.Lsh(shift, uint(exp))
		numBig.Mul(numBig, shift)
		numerator = NewIntFromBig(numBig)
	} else if exp < 0 {
		shift.Lsh(shift, uint(-exp))
		denomBig.Mul(denomBig, shift)
	}

	denominator := NewIntFromBig(denomBig)
	return NewTuple([]Object{numerator, denominator}), nil
}

// floatIsInteger returns True if the float is an integer value.
//
// CPython: Objects/floatobject.c:845 float_is_integer_impl
func floatIsInteger(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: is_integer() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'is_integer' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	v := f.Float64()
	if !math.IsInf(v, 0) && math.Floor(v) == v {
		return True(), nil
	}
	return False(), nil
}

// floatFloor returns math.floor(self) as an int.
//
// CPython: Objects/floatobject.c:884 float___floor___impl
func floatFloor(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __floor__() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__floor__' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return floatToInt(math.Floor(f.Float64()))
}

// floatCeil returns math.ceil(self) as an int.
//
// CPython: Objects/floatobject.c:895 float___ceil___impl
func floatCeil(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __ceil__() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__ceil__' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return floatToInt(math.Ceil(f.Float64()))
}

// floatTrunc returns int(self) truncated toward zero.
//
// CPython: Objects/floatobject.c:868 float___trunc___impl
func floatTrunc(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __trunc__() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__trunc__' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return floatToInt(math.Trunc(f.Float64()))
}

// floatToInt converts a finite double to Int, mirroring PyLong_FromDouble.
//
// CPython: Objects/longobject.c PyLong_FromDouble
func floatToInt(v float64) (Object, error) {
	if math.IsInf(v, 0) {
		return nil, fmt.Errorf("OverflowError: cannot convert float infinity to integer")
	}
	if math.IsNaN(v) {
		return nil, fmt.Errorf("ValueError: cannot convert float NaN to integer")
	}
	b := new(big.Float).SetPrec(64).SetFloat64(v)
	i, _ := b.Int(nil)
	return NewIntFromBig(i), nil
}

// floatHex returns a hexadecimal string representation of the float.
// Format: [±]0x<hex_int>.<hex_frac>p[±]<decimal_exp>
//
// CPython: Objects/floatobject.c:1164 float_hex_impl
func floatHex(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: hex() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'hex' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	x := f.Float64()
	// float_repr renders nan / inf / -inf for the non-finite cases.
	if math.IsNaN(x) {
		return NewStr("nan"), nil
	}
	if math.IsInf(x, 1) {
		return NewStr("inf"), nil
	}
	if math.IsInf(x, -1) {
		return NewStr("-inf"), nil
	}
	if x == 0.0 {
		if math.Signbit(x) {
			return NewStr("-0x0.0p+0"), nil
		}
		return NewStr("0x0.0p+0"), nil
	}

	// TOHEX_NBITS is DBL_MANT_DIG (53) rounded up to 4k+1, giving 53, so
	// the mantissa always carries 1 leading plus (53-1)/4 = 13 fractional
	// hex digits with trailing zeros retained. DBL_MIN_EXP is -1021.
	const dblMinExp = -1021
	const nfrac = (53 - 1) / 4

	m, e := math.Frexp(math.Abs(x))
	shift := 1
	if dblMinExp-e > 0 {
		shift = 1 - (dblMinExp - e)
	}
	m = math.Ldexp(m, shift)
	e -= shift

	const hexdigits = "0123456789abcdef"
	var sb strings.Builder
	lead := int(m)
	sb.WriteByte(hexdigits[lead])
	m -= float64(lead)
	sb.WriteByte('.')
	for range nfrac {
		m *= 16.0
		d := int(m)
		sb.WriteByte(hexdigits[d])
		m -= float64(d)
	}

	esign := byte('+')
	if e < 0 {
		esign = '-'
		e = -e
	}
	sign := ""
	if x < 0.0 {
		sign = "-"
	}
	return NewStr(fmt.Sprintf("%s0x%sp%c%d", sign, sb.String(), esign, e)), nil
}

// floatFromNumber converts a real number to float. Unlike float(), it rejects
// strings, bytes, and other non-numeric types; only objects with __float__
// or __index__ are accepted. Mirrors PyFloat_AsDouble's call surface.
//
// CPython: Objects/floatobject.c:1649 float_from_number_impl
func floatFromNumber(args []Object, _ map[string]Object) (Object, error) {
	var number Object
	var cls *Type
	switch len(args) {
	case 1:
		number = args[0]
	case 2:
		if t, ok := args[0].(*Type); ok {
			cls = t
		}
		number = args[1]
	default:
		return nil, fmt.Errorf("TypeError: from_number() takes exactly one argument (%d given)", len(args))
	}
	if number == nil {
		return nil, fmt.Errorf("TypeError: from_number() argument must not be None")
	}
	var x float64
	switch v := number.(type) {
	case *Float:
		// CPython: Objects/floatobject.c:1654 — only return self for exact float + float type
		if v.Type() == FloatType && (cls == nil || cls == FloatType) {
			return v, nil
		}
		x = v.Float64()
	case *Int:
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		x = f
	case *Bool:
		if v.v.Sign() != 0 {
			x = 1.0
		}
	default:
		n := number.Type().Number
		if n == nil {
			return nil, fmt.Errorf("TypeError: must be real number, not %s", number.Type().Name)
		}
		switch {
		case n.Float != nil:
			res, err := n.Float(number)
			if err != nil {
				return nil, err
			}
			rf, ok := res.(*Float)
			if !ok {
				return nil, fmt.Errorf("TypeError: %.50s.__float__ returned non-float (type %.50s)",
					number.Type().Name, res.Type().Name)
			}
			if rf.Type() != FloatType {
				msg := fmt.Sprintf("__float__ returned non-float (type %s). "+
					"The ability to return an instance of a strict subclass of float "+
					"is deprecated, and may be removed in a future version of Python.",
					rf.Type().Name)
				if DeprecWarnHook != nil {
					if werr := DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
			}
			x = rf.Float64()
		case n.Index != nil:
			idx, err := n.Index(number)
			if err != nil {
				return nil, err
			}
			i, ok := idx.(*Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
			}
			f, _ := new(big.Float).SetInt(i.BigInt()).Float64()
			x = f
		default:
			return nil, fmt.Errorf("TypeError: must be real number, not %s", number.Type().Name)
		}
	}
	result := NewFloat(x)
	// CPython: Objects/floatobject.c:1669 float_from_number_impl — subtype call
	if cls != nil && cls != FloatType {
		return Call(cls, NewTuple([]Object{result}), nil)
	}
	return result, nil
}

// floatFromHex creates a float from a hexadecimal string.
// Accepts optional "float" or subtype as first argument (classmethod form).
//
// CPython: Objects/floatobject.c:1235 float_fromhex_impl
func floatFromHex(args []Object, _ map[string]Object) (Object, error) {
	// classmethod: args[0] is the type, args[1] is the string
	// but we also allow args[0] as the string (if called as float.fromhex(s))
	var s string
	var cls *Type // nil means return plain float
	switch len(args) {
	case 1:
		// float.fromhex("0x1.0p+0") called as unbound
		str, ok := args[0].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: fromhex() argument must be str, not '%s'", typeNameOf(args[0]))
		}
		s = str.Value()
	case 2:
		// classmethod call: args[0] is type, args[1] is string
		if t, ok := args[0].(*Type); ok && t != FloatType {
			cls = t
		}
		str, ok := args[1].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: fromhex() argument must be str, not '%s'", typeNameOf(args[1]))
		}
		s = str.Value()
	default:
		return nil, fmt.Errorf("TypeError: fromhex() takes exactly one argument (%d given)", len(args))
	}

	// strip whitespace
	s = strings.TrimFunc(s, unicode.IsSpace)

	// CPython: Objects/floatobject.c:1302 _Py_parse_inf_or_nan
	// handle inf/nan before hex parsing
	low := strings.ToLower(s)
	{
		neg := false
		tail := low
		if strings.HasPrefix(tail, "-") {
			neg = true
			tail = tail[1:]
		} else if strings.HasPrefix(tail, "+") {
			tail = tail[1:]
		}
		if tail == "inf" || tail == "infinity" {
			f := math.Inf(1)
			if neg {
				f = math.Inf(-1)
			}
			if cls != nil {
				// CPython: Objects/floatobject.c:1454 PyObject_CallOneArg(type, result)
				return Call(cls, NewTuple([]Object{NewFloat(f)}), nil)
			}
			return NewFloat(f), nil
		}
		if tail == "nan" {
			if cls != nil {
				// CPython: Objects/floatobject.c:1454 PyObject_CallOneArg(type, result)
				return Call(cls, NewTuple([]Object{NewFloat(math.NaN())}), nil)
			}
			return NewFloat(math.NaN()), nil
		}
	}

	// Normalise to what Go's strconv.ParseFloat requires:
	//   [+-]0x<hex>.<hex>p[+-]<dec>
	// CPython accepts [+-][0x]<hex>[.<hex>][p[+-]<dec>] where the 0x
	// prefix and p exponent are both optional.
	//
	// CPython: Objects/floatobject.c:1318 [0x], :1351 [p <exponent>]
	norm, origForErr := hexFloatNormalise(s)
	v, err := strconv.ParseFloat(norm, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			// CPython: Objects/floatobject.c:1428 overflow_error
			return nil, fmt.Errorf("OverflowError: hexadecimal value too large to represent as a float")
		}
		return nil, fmt.Errorf("ValueError: invalid hexadecimal floating-point string %q", origForErr)
	}
	// CPython: Objects/floatobject.c:1454 PyObject_CallOneArg(type, result)
	if cls != nil {
		return Call(cls, NewTuple([]Object{NewFloat(v)}), nil)
	}
	return NewFloat(v), nil
}

// hexFloatNormalise converts a CPython-accepted hex-float string to the
// stricter form that Go's strconv.ParseFloat requires:
//   - adds "0x" prefix when absent
//   - inserts "0" before a leading "." in the coefficient
//   - appends "p0" when no exponent is present
//
// CPython: Objects/floatobject.c:1318 [0x] and :1351 [p <exponent>]
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexFloatNormalise(s string) (norm, orig string) {
	orig = s
	sign := ""
	rest := s
	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		sign = string(rest[0])
		rest = rest[1:]
	}

	// add "0x" prefix if absent
	lower := strings.ToLower(rest)
	if !strings.HasPrefix(lower, "0x") {
		rest = "0x" + rest
		lower = "0x" + lower
	}

	// coefficient starts after "0x"; insert "0" before leading "." only
	// when at least one hex digit follows the dot. CPython requires at
	// least one hex digit in the entire coefficient (ndigits > 0).
	// CPython: Objects/floatobject.c:1342 if (ndigits == 0) goto parse_error
	coeff := rest[2:]
	if strings.HasPrefix(coeff, ".") {
		// Only insert the '0' when there's an actual hex digit after '.'
		after := coeff[1:]
		if after != "" && isHexDigit(after[0]) {
			rest = rest[:2] + "0" + coeff
			lower = strings.ToLower(rest)
		}
	}

	// append "p0" if no exponent present
	if !strings.ContainsRune(lower, 'p') {
		rest += "p0"
	}

	return sign + rest, orig
}
