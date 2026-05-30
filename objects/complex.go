// The Complex type, a Python complex number. CPython stores the real
// and imaginary parts as two C doubles; gopy uses Go's complex128 so
// the math/cmplx routines do most of the heavy lifting.
//
// CPython: Objects/complexobject.c:1075 PyComplex_Type

package objects

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/cmplx"
	"strings"

	"github.com/tamnd/gopy/format"
)

// Complex is the Python complex number.
//
// CPython: Include/cpython/complexobject.h:7 PyComplexObject
type Complex struct {
	Header
	v complex128
}

// ComplexType is the type singleton for complex.
//
// CPython: Objects/complexobject.c:1075 PyComplex_Type
var ComplexType = NewType("complex", []*Type{objectType})

func init() {
	ComplexType.Repr = complexRepr
	ComplexType.Str = complexRepr
	ComplexType.Format = complexFormat
	ComplexType.Hash = complexHash
	ComplexType.RichCmp = complexRichCmp
	ComplexType.Number = &NumberMethods{
		Add:        complexAdd,
		Subtract:   complexSub,
		Multiply:   complexMul,
		TrueDivide: complexTrueDiv,
		Power:      complexPower,
		Negative:   complexNeg,
		Positive:   func(o Object) (Object, error) { return o, nil },
		Absolute:   complexAbs,
		Bool:       complexBool,
	}

	// complex_members (Objects/complexobject.c:1337): real/imag are
	// PyMemberDef Py_T_DOUBLE Py_READONLY slots backed by the cval
	// fields. PyMember and getset behave the same to Python attribute
	// lookup, so install GetSetDescr here for consistency with int/float.
	SetTypeDescr(ComplexType, "real", NewGetSetDescr("real", complexRealGetter, nil))
	SetTypeDescr(ComplexType, "imag", NewGetSetDescr("imag", complexImagGetter, nil))
	SetTypeDescr(ComplexType, "conjugate", NewMethodDescr(ComplexType, "conjugate", complexConjugateMethod))
	SetTypeDescr(ComplexType, "__complex__", NewMethodDescr(ComplexType, "__complex__", complexComplexMethod))
	SetTypeDescr(ComplexType, "__getnewargs__", NewMethodDescr(ComplexType, "__getnewargs__", complexGetNewArgsMethod))
	// CPython: Objects/complexobject.c:1301 complex_from_number_impl, METH_O | METH_CLASS
	SetTypeDescr(ComplexType, "from_number", NewClassMethod(
		NewBuiltinFunction("complex.from_number", complexFromNumber)))
}

// complexRealGetter backs complex.real: returns the real component as
// a Python float.
//
// CPython: Objects/complexobject.c:1338 complex_members[real]
func complexRealGetter(owner Object) (Object, error) {
	c, ok := owner.(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'real' for 'complex' objects doesn't apply to a '%s' object", typeNameOf(owner))
	}
	return NewFloat(real(c.v)), nil
}

// complexImagGetter backs complex.imag: returns the imaginary
// component as a Python float.
//
// CPython: Objects/complexobject.c:1340 complex_members[imag]
func complexImagGetter(owner Object) (Object, error) {
	c, ok := owner.(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'imag' for 'complex' objects doesn't apply to a '%s' object", typeNameOf(owner))
	}
	return NewFloat(imag(c.v)), nil
}

// complexComplexMethod backs complex.__complex__(): returns self for
// exact complex, otherwise rebuilds a fresh exact-type complex from
// the cval. Subclasses lose their identity, matching CPython.
//
// CPython: Objects/complexobject.c:918 complex___complex___impl
func complexComplexMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __complex__() takes no arguments (%d given)", len(args)-1)
	}
	c, ok := args[0].(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__complex__' for 'complex' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	if c.Type() == ComplexType {
		return c, nil
	}
	return NewComplex(real(c.v), imag(c.v)), nil
}

// complexGetNewArgsMethod backs complex.__getnewargs__(): returns
// (real, imag) so pickle/copy can reconstruct via complex(real, imag).
//
// CPython: Objects/complexobject.c:874 complex___getnewargs___impl
func complexGetNewArgsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __getnewargs__() takes no arguments (%d given)", len(args)-1)
	}
	c, ok := args[0].(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getnewargs__' for 'complex' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewTuple([]Object{NewFloat(real(c.v)), NewFloat(imag(c.v))}), nil
}

// complexFromNumber backs complex.from_number(number). Classmethod
// signature: args[0] is the receiver type, args[1] is the number to
// convert. Returns the receiver-typed complex obtained from
// PyComplex_AsCComplex (tries __complex__, then __float__).
//
// CPython: Objects/complexobject.c:1309 complex_from_number_impl
func complexFromNumber(args []Object, _ map[string]Object) (Object, error) {
	var cls *Type
	var number Object
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
	// Fast path: exact complex + exact target type.
	if c, ok := number.(*Complex); ok && c.Type() == ComplexType && (cls == nil || cls == ComplexType) {
		return c, nil
	}
	re, im, err := PyComplexAsCComplex(number)
	if err != nil {
		return nil, err
	}
	result := NewComplex(re, im)
	if cls != nil && cls != ComplexType {
		return Call(cls, NewTuple([]Object{result}), nil)
	}
	return result, nil
}

// PyComplexAsCComplex coerces o to a (real, imag) pair. Tries the
// __complex__ special method first via _PyObject_LookupSpecial, then
// __float__ (Number.Float), finally __index__ (Number.Index). Mirrors
// CPython's PyComplex_AsCComplex dispatch order.
//
// CPython: Objects/complexobject.c:521 PyComplex_AsCComplex
func PyComplexAsCComplex(o Object) (float64, float64, error) {
	if c, ok := o.(*Complex); ok {
		return real(c.v), imag(c.v), nil
	}
	// __complex__ slot via _PyObject_LookupSpecial.
	special, err := lookupSpecial(o, "__complex__")
	if err != nil {
		return 0, 0, err
	}
	if special != nil {
		res, callErr := CallNoArgs(special)
		if callErr != nil {
			return 0, 0, callErr
		}
		c, ok := res.(*Complex)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: __complex__ returned non-complex (type %s)", res.Type().Name)
		}
		return real(c.v), imag(c.v), nil
	}
	// Fall back to __float__: real part only, imag = 0.
	switch v := o.(type) {
	case *Float:
		return v.Float64(), 0, nil
	case *Int:
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		return f, 0, nil
	case *Bool:
		if v.v.Sign() != 0 {
			return 1.0, 0, nil
		}
		return 0, 0, nil
	}
	n := o.Type().Number
	if n != nil && n.Float != nil {
		fv, ferr := n.Float(o)
		if ferr != nil {
			return 0, 0, ferr
		}
		ff, ok := fv.(*Float)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: %s.__float__ returned non-float (type %s)", o.Type().Name, fv.Type().Name)
		}
		return ff.Float64(), 0, nil
	}
	if n != nil && n.Index != nil {
		idx, ierr := n.Index(o)
		if ierr != nil {
			return 0, 0, ierr
		}
		i, ok := idx.(*Int)
		if !ok {
			return 0, 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
		}
		f, _ := new(big.Float).SetInt(i.BigInt()).Float64()
		return f, 0, nil
	}
	return 0, 0, fmt.Errorf("TypeError: complex() argument must be a string or a number, not '%s'", o.Type().Name)
}

// complexConjugateMethod backs complex.conjugate(): negates the
// imaginary part.
//
// CPython: Objects/complexobject.c:1058 complex_conjugate_impl
func complexConjugateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: conjugate() takes no arguments (%d given)", len(args)-1)
	}
	c, ok := args[0].(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'conjugate' for 'complex' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewComplex(real(c.v), -imag(c.v)), nil
}

// NewComplex builds a complex from real and imaginary parts.
//
// CPython: Objects/complexobject.c:138 PyComplex_FromDoubles
func NewComplex(re, im float64) *Complex {
	o := &Complex{v: complex(re, im)}
	o.init(ComplexType)
	return o
}

// Complex128 returns the underlying value.
func (c *Complex) Complex128() complex128 {
	return c.v
}

// Real returns the real component.
func (c *Complex) Real() float64 { return real(c.v) }

// Imag returns the imaginary component.
func (c *Complex) Imag() float64 { return imag(c.v) }

// complexRepr formats as "(re+imj)" / "(re-imj)" / "imj" depending on
// the real and imaginary parts, matching complex_repr. CPython does
// not pass Py_DTSF_ADD_DOT_0 here, so integral parts come out without
// the trailing ".0".
//
// CPython: Objects/complexobject.c:362 complex_repr
func complexRepr(o Object) (string, error) {
	v := o.(*Complex).v
	re, im := real(v), imag(v)
	if re == 0 && !math.Signbit(re) {
		return complexFormatPart(im) + "j", nil
	}
	sign := "+"
	if math.Signbit(im) {
		sign = ""
	}
	return "(" + complexFormatPart(re) + sign + complexFormatPart(im) + "j)", nil
}

// complexFormatPart is formatFloatShort without the Py_DTSF_ADD_DOT_0
// flag: drops the trailing ".0" that complex_repr leaves out for
// integral real/imag parts.
//
// CPython: Python/pystrtod.c:1265 PyOS_double_to_string ('r' mode, no flag)
func complexFormatPart(v float64) string {
	s := formatFloatShort(v)
	if strings.HasSuffix(s, ".0") {
		return s[:len(s)-2]
	}
	return s
}

// complexFormat implements complex.__format__. An empty spec returns str(self).
// A spec with fill/align/width and no type specifier formats the complex repr
// and pads it. '0' fill and '=' align are not allowed.
//
// CPython: Python/formatter_unicode.c:1691 _PyComplex_FormatAdvancedWriter
func complexFormat(o Object, spec string) (string, error) {
	if spec == "" {
		return complexRepr(o)
	}
	s, err := format.ParseSpec(spec)
	if err != nil {
		return "", fmt.Errorf("ValueError: invalid format spec for complex")
	}
	if s.Fill == '0' {
		return "", fmt.Errorf("ValueError: Zero padding is not allowed in complex format specifier")
	}
	if s.Align == '=' {
		return "", fmt.Errorf("ValueError: '=' alignment flag is not allowed in complex format specifier")
	}
	switch s.Type {
	case 0:
		body, reprErr := complexRepr(o)
		if reprErr != nil {
			return "", reprErr
		}
		s.Type = 's'
		s.Precision = -1
		return format.FormatString(body, s)
	case 'e', 'E', 'f', 'F', 'g', 'G', 'n':
		return "", fmt.Errorf("TypeError: unsupported format string passed to complex.__format__")
	default:
		return "", fmt.Errorf("ValueError: Unknown format code '%c' for object of type 'complex'", s.Type)
	}
}

// complexHash xors the real and imag hashes after multiplying the
// imaginary part by a fixed multiplier, matching CPython.
//
// CPython: Objects/complexobject.c:467 complex_hash
func complexHash(o Object) (int64, error) {
	v := o.(*Complex).v
	rh, err := floatHash(NewFloat(real(v)))
	if err != nil {
		return 0, err
	}
	ih, err := floatHash(NewFloat(imag(v)))
	if err != nil {
		return 0, err
	}
	const mult = 1000003
	h := rh ^ (ih * mult)
	if h == -1 {
		h = -2
	}
	return h, nil
}

// complexRichCmp implements complex equality. Ordering operators
// raise TypeError to match CPython.
//
// CPython: Objects/complexobject.c:642 complex_richcompare
func complexRichCmp(a, b Object, op CompareOp) (Object, error) {
	av, ok := asComplex(a)
	if !ok {
		return notImplemented(), nil
	}
	bv, ok := asComplex(b)
	if !ok {
		return notImplemented(), nil
	}
	switch op {
	case CompareEQ:
		return NewBool(av == bv), nil
	case CompareNE:
		return NewBool(av != bv), nil
	}
	// CPython: Objects/complexobject.c:853 complex_richcompare — ordering
	// ops return NotImplemented, letting RichCmp raise the generic TypeError.
	return notImplemented(), nil
}

// asComplex coerces an int / float / complex / bool operand to complex128.
//
// CPython: Objects/complexobject.c:281 to_complex
func asComplex(o Object) (complex128, bool) {
	switch x := o.(type) {
	case *Complex:
		return x.v, true
	case *Float:
		return complex(x.v, 0), true
	case *Bool:
		return complex(intToFloat(&x.Int), 0), true
	case *Int:
		return complex(intToFloat(x), 0), true
	}
	return 0, false
}

func complexPair(a, b Object) (av, bv complex128, ok bool) {
	av, aok := asComplex(a)
	bv, bok := asComplex(b)
	if !aok || !bok {
		return 0, 0, false
	}
	return av, bv, true
}

func complexAdd(a, b Object) (Object, error) {
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	v := av + bv
	return NewComplex(real(v), imag(v)), nil
}

func complexSub(a, b Object) (Object, error) {
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	v := av - bv
	return NewComplex(real(v), imag(v)), nil
}

func complexMul(a, b Object) (Object, error) {
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	v := av * bv
	return NewComplex(real(v), imag(v)), nil
}

// complexTrueDiv mirrors complex_div; division by zero raises
// ZeroDivisionError rather than producing inf+inf*j.
//
// CPython: Objects/complexobject.c:843 complex_div
func complexTrueDiv(a, b Object) (Object, error) {
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	if bv == 0 {
		return nil, errors.New("ZeroDivisionError: complex division by zero")
	}
	v := av / bv
	return NewComplex(real(v), imag(v)), nil
}

// complexPower implements `pow(a, b)`. The third modulus argument is
// rejected with TypeError, matching CPython.
//
// CPython: Objects/complexobject.c:884 complex_pow
func complexPower(a, b, mod Object) (Object, error) {
	if mod != nil && mod != None() {
		return nil, errors.New("TypeError: complex modulo")
	}
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	v := cmplx.Pow(av, bv)
	return NewComplex(real(v), imag(v)), nil
}

func complexNeg(o Object) (Object, error) {
	v := -o.(*Complex).v
	return NewComplex(real(v), imag(v)), nil
}

// complexAbs returns the magnitude as a float, matching CPython.
// _Py_c_abs flags hypot()'s non-finite result via errno=ERANGE, which
// complex_abs maps to OverflowError. We do the equivalent IsInf check.
//
// CPython: Objects/complexobject.c:368 _Py_c_abs
// CPython: Objects/complexobject.c:798 complex_abs
func complexAbs(o Object) (Object, error) {
	r := cmplx.Abs(o.(*Complex).v)
	if math.IsInf(r, 0) {
		return nil, fmt.Errorf("OverflowError: absolute value too large")
	}
	return NewFloat(r), nil
}

func complexBool(o Object) (bool, error) {
	return o.(*Complex).v != 0, nil
}
