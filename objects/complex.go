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
//
// CPython: Objects/complexobject.c:798 complex_abs
func complexAbs(o Object) (Object, error) {
	return NewFloat(cmplx.Abs(o.(*Complex).v)), nil
}

func complexBool(o Object) (bool, error) {
	return o.(*Complex).v != 0, nil
}
