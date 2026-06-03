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
	// CPython: Objects/floatobject.c:1492 float_as_integer_ratio_impl
	SetTypeDescr(FloatType, "as_integer_ratio", NewMethodDescr(FloatType, "as_integer_ratio", floatAsIntegerRatio))
	// CPython: Objects/floatobject.c:845 float_is_integer_impl
	SetTypeDescr(FloatType, "is_integer", NewMethodDescr(FloatType, "is_integer", floatIsInteger))
	// CPython: Objects/floatobject.c:884 float___floor___impl
	SetTypeDescr(FloatType, "__floor__", NewMethodDescr(FloatType, "__floor__", floatFloor))
	// CPython: Objects/floatobject.c:895 float___ceil___impl
	SetTypeDescr(FloatType, "__ceil__", NewMethodDescr(FloatType, "__ceil__", floatCeil))
	// CPython: Objects/floatobject.c:868 float___trunc___impl
	SetTypeDescr(FloatType, "__trunc__", NewMethodDescr(FloatType, "__trunc__", floatTrunc))
	// CPython: Objects/floatobject.c:1164 float_hex_impl
	SetTypeDescr(FloatType, "hex", NewMethodDescr(FloatType, "hex", floatHex))
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
	SetTypeDescr(FloatType, "__getnewargs__", NewMethodDescr(FloatType, "__getnewargs__", floatGetNewArgs))
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
	v := f.Float64()
	if math.IsNaN(v) {
		return NewStr("nan"), nil
	}
	if math.IsInf(v, 1) {
		return NewStr("inf"), nil
	}
	if math.IsInf(v, -1) {
		return NewStr("-inf"), nil
	}
	// Go's strconv.FormatFloat with 'x' produces the hex-float form.
	// e.g. 0.1 → "0x1.999999999999ap-04" but CPython gives "0x1.999999999999ap-4"
	s := strconv.FormatFloat(v, 'x', -1, 64)
	// Normalise exponent: strip leading zeros in p+04 → p+4
	if idx := strings.IndexByte(s, 'p'); idx >= 0 {
		prefix := s[:idx+1]
		expStr := s[idx+1:]
		sign := ""
		if expStr[0] == '+' || expStr[0] == '-' {
			sign = string(expStr[0])
			expStr = expStr[1:]
		}
		// strip leading zeros
		expStr = strings.TrimLeft(expStr, "0")
		if expStr == "" {
			expStr = "0"
		}
		s = prefix + sign + expStr
	}
	return NewStr(s), nil
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
