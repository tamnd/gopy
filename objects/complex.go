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
		Positive:   complexPos,
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
	// CPython: Python/formatter_unicode.c:1693 _PyComplex_FormatAdvancedWriter
	// is reachable both via PyObject_Format (the Format slot above) and
	// via instance.__format__(spec). Install a method descriptor so the
	// attribute-access path hits the same renderer; without this, MRO
	// inherits objectFormatDescr which rejects every non-empty spec.
	SetTypeDescr(ComplexType, "__format__", NewMethodDescr(ComplexType, "__format__", complexFormatMethod))
	// CPython: Objects/complexobject.c:1301 complex_from_number_impl, METH_O | METH_CLASS
	SetTypeDescr(ComplexType, "from_number", NewClassMethod(
		NewBuiltinFunction("complex.from_number", complexFromNumber)))
	// CPython exposes nb_add / nb_sub / nb_mul / nb_true_divide /
	// nb_power / nb_negative / nb_positive / nb_absolute / nb_bool
	// as `__add__` / `__radd__` / ... slot wrappers via
	// add_operators. Replicate the surface that test_complex pokes.
	//
	// CPython: Objects/typeobject.c:10090 add_operators
	bindComplexBinOp("__add__", complexAdd, false)
	bindComplexBinOp("__radd__", complexAdd, true)
	bindComplexBinOp("__sub__", complexSub, false)
	bindComplexBinOp("__rsub__", complexSub, true)
	bindComplexBinOp("__mul__", complexMul, false)
	bindComplexBinOp("__rmul__", complexMul, true)
	bindComplexBinOp("__truediv__", complexTrueDiv, false)
	bindComplexBinOp("__rtruediv__", complexTrueDiv, true)
	SetTypeDescr(ComplexType, "__neg__", NewMethodDescr(ComplexType, "__neg__",
		complexUnaryMethod("__neg__", complexNeg)))
	SetTypeDescr(ComplexType, "__pos__", NewMethodDescr(ComplexType, "__pos__",
		complexUnaryMethod("__pos__", complexPos)))
	SetTypeDescr(ComplexType, "__abs__", NewMethodDescr(ComplexType, "__abs__",
		complexUnaryMethod("__abs__", complexAbs)))
	SetTypeDescr(ComplexType, "__bool__", NewMethodDescr(ComplexType, "__bool__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __bool__ expected 1 argument, got %d", len(args))
			}
			b, err := complexBool(args[0])
			if err != nil {
				return nil, err
			}
			return NewBool(b), nil
		}))
	SetTypeDescr(ComplexType, "__pow__", NewMethodDescr(ComplexType, "__pow__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("TypeError: __pow__ expected 2 or 3 arguments, got %d", len(args))
			}
			var mod Object
			if len(args) == 3 {
				mod = args[2]
			}
			return complexPower(args[0], args[1], mod)
		}))
	SetTypeDescr(ComplexType, "__rpow__", NewMethodDescr(ComplexType, "__rpow__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) < 2 || len(args) > 3 {
				return nil, fmt.Errorf("TypeError: __rpow__ expected 2 or 3 arguments, got %d", len(args))
			}
			var mod Object
			if len(args) == 3 {
				mod = args[2]
			}
			return complexPower(args[1], args[0], mod)
		}))
	for _, op := range []struct {
		name string
		cmp  CompareOp
	}{
		{"__eq__", CompareEQ},
		{"__ne__", CompareNE},
		{"__lt__", CompareLT},
		{"__le__", CompareLE},
		{"__gt__", CompareGT},
		{"__ge__", CompareGE},
	} {
		name, cmp := op.name, op.cmp
		SetTypeDescr(ComplexType, name, NewMethodDescr(ComplexType, name,
			func(args []Object, _ map[string]Object) (Object, error) {
				if len(args) != 2 {
					return nil, fmt.Errorf("TypeError: %s expected 2 arguments, got %d", name, len(args))
				}
				return complexRichCmp(args[0], args[1], cmp)
			}))
	}
}

// complexPos backs complex.__pos__: returns self for exact complex,
// rebuilds a fresh exact-type complex for subclasses.
//
// CPython: Objects/complexobject.c:770 complex_pos
func complexPos(o Object) (Object, error) {
	c := o.(*Complex)
	if c.Type() == ComplexType {
		return c, nil
	}
	return NewComplex(real(c.v), imag(c.v)), nil
}

// complexUnaryMethod wraps a unary slot function (Negative, Positive,
// Absolute) as a method descriptor body. Validates argument count and
// receiver type.
func complexUnaryMethod(name string, fn func(Object) (Object, error)) func([]Object, map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: %s expected 1 argument, got %d", name, len(args))
		}
		return fn(args[0])
	}
}

// bindComplexBinOp installs a __name__ slot wrapper that dispatches
// through fn. When reflected, the (self, other) order is swapped so
// the underlying slot sees (other, self), matching how CPython's
// SLOT1BIN macro routes __r<name>__.
//
// CPython: Objects/typeobject.c:7950 SLOT1BIN
func bindComplexBinOp(name string, fn func(a, b Object) (Object, error), reflected bool) {
	SetTypeDescr(ComplexType, name, NewMethodDescr(ComplexType, name,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: %s expected 2 arguments, got %d", name, len(args))
			}
			a, b := args[0], args[1]
			if reflected {
				a, b = b, a
			}
			return fn(a, b)
		}))
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

// TryComplexSpecialMethod calls o.__complex__() if defined and returns
// the resulting *Complex. Returns (nil, nil) when no __complex__ slot
// is found, mirroring CPython's try_complex_special_method.
//
// CPython: Objects/complexobject.c:488 try_complex_special_method
func TryComplexSpecialMethod(o Object) (*Complex, error) {
	special, err := lookupSpecial(o, "__complex__")
	if err != nil {
		return nil, err
	}
	if special == nil {
		return nil, nil
	}
	res, callErr := CallNoArgs(special)
	if callErr != nil {
		return nil, callErr
	}
	c, ok := res.(*Complex)
	if !ok {
		return nil, fmt.Errorf("TypeError: __complex__ returned non-complex (type %s)", res.Type().Name)
	}
	// bpo-29894: returning a complex subclass is deprecated.
	if c.Type() != ComplexType && DeprecWarnHook != nil {
		msg := fmt.Sprintf("__complex__ returned non-complex (type %s).  "+
			"The ability to return an instance of a strict subclass of complex "+
			"is deprecated, and may be removed in a future version of Python.",
			c.Type().Name)
		if werr := DeprecWarnHook(msg); werr != nil {
			return nil, werr
		}
	}
	return c, nil
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

// PyNumberFloat ports PyNumber_Float: returns a float built from o's
// nb_float, else nb_index, else TypeError. Used by complex_new_impl's
// real/imag dispatch where __complex__ must NOT be invoked.
//
// CPython: Objects/abstract.c:1788 PyNumber_Float
func PyNumberFloat(o Object) (float64, error) {
	switch v := o.(type) {
	case *Float:
		return v.Float64(), nil
	case *Int:
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		if math.IsInf(f, 0) {
			return 0, fmt.Errorf("OverflowError: int too large to convert to float")
		}
		return f, nil
	case *Bool:
		if v.v.Sign() != 0 {
			return 1.0, nil
		}
		return 0, nil
	}
	n := o.Type().Number
	if n != nil && n.Float != nil {
		fv, ferr := n.Float(o)
		if ferr != nil {
			return 0, ferr
		}
		ff, ok := fv.(*Float)
		if !ok {
			return 0, fmt.Errorf("TypeError: %s.__float__ returned non-float (type %s)", o.Type().Name, fv.Type().Name)
		}
		return ff.Float64(), nil
	}
	if n != nil && n.Index != nil {
		idx, ierr := n.Index(o)
		if ierr != nil {
			return 0, ierr
		}
		i, ok := idx.(*Int)
		if !ok {
			return 0, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
		}
		f, _ := new(big.Float).SetInt(i.BigInt()).Float64()
		if math.IsInf(f, 0) {
			return 0, fmt.Errorf("OverflowError: int too large to convert to float")
		}
		return f, nil
	}
	return 0, fmt.Errorf("TypeError: float() argument must be a string or a real number, not '%s'", o.Type().Name)
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

// newComplexAs builds a complex tagged with t instead of ComplexType.
// Used so a class like `class C(complex): pass` yields instances whose
// Type() is C.
//
// CPython: Objects/complexobject.c:400 complex_subtype_from_c_complex
func newComplexAs(re, im float64, t *Type) *Complex {
	o := &Complex{v: complex(re, im)}
	o.init(t)
	return o
}

// SetComplexTpNewBase wires the value-side constructor (complex(real,
// imag)) and makes it subtype-aware: when cls != ComplexType the
// result is re-tagged. Mirrors SetFloatTpNewBase.
//
// CPython: Objects/complexobject.c:1094 actual_complex_new (subtype branch)
func SetComplexTpNewBase(fn func(args []Object, kwargs map[string]Object) (Object, error)) {
	ComplexType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		out, err := fn(args, kwargs)
		if err != nil {
			return nil, err
		}
		if cls == nil || cls == ComplexType {
			return out, nil
		}
		c, ok := out.(*Complex)
		if !ok {
			return out, nil
		}
		return newComplexAs(real(c.v), imag(c.v), cls), nil
	}
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
	// CPython formats the imaginary part with Py_DTSF_SIGN so a
	// leading '+' is forced for positive values; NaN renders as "nan"
	// without sign but is still preceded by the connector, giving
	// "(nan+nanj)". We mimic by always using "+" unless the imag part
	// starts with "-" (i.e. negative finite or -inf).
	//
	// CPython: Objects/complexobject.c:580 complex_repr
	imStr := complexFormatPart(im)
	sign := "+"
	if strings.HasPrefix(imStr, "-") {
		sign = ""
	}
	return "(" + complexFormatPart(re) + sign + imStr + "j)", nil
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

// complexFormatMethod is the method body bound to ComplexType's
// __format__ attribute. It validates arity / argument type and then
// delegates to the same renderer used by the Format slot.
//
// CPython: Objects/complexobject.c:887 complex___format___impl
func complexFormatMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __format__() takes exactly one argument (%d given)", len(args)-1)
	}
	if _, ok := args[0].(*Complex); !ok {
		return nil, fmt.Errorf(
			"TypeError: descriptor '__format__' requires a 'complex' object but received a '%s'",
			typeNameOf(args[0]))
	}
	spec, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __format__() argument 1 must be str, not %s", typeNameOf(args[1]))
	}
	out, err := complexFormat(args[0], spec.v)
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

// complexFormat implements complex.__format__. An empty spec returns str(self).
// A non-empty spec runs the format_complex_internal pipeline: per-component
// format via format.FormatFloat with the imag part's sign forced to '+',
// concatenate "re+imj" (or "imj" when skip_re), wrap in parens for the
// no-type-spec str-shaped path, then apply outer width/align/fill padding.
// '0' fill and '=' align are not allowed.
//
// CPython: Python/formatter_unicode.c:1291 format_complex_internal
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
	origType := s.Type
	switch origType {
	case 0, 'e', 'E', 'f', 'F', 'g', 'G', 'n':
	default:
		return "", fmt.Errorf("ValueError: Unknown format code '%c' for object of type 'complex'", origType)
	}

	v := o.(*Complex).v
	re, im := real(v), imag(v)

	// CPython: Python/formatter_unicode.c:1372 type=='\0' branch.
	// Omitted type behaves like str(self): skip the real component when
	// it's +0, otherwise wrap the whole result in parens. We use 'r' as
	// the inner type code (mode='r' through pystrconv) and strip the
	// trailing ".0" that ADD_DOT_0 adds, since CPython's
	// format_complex_internal does not pass Py_DTSF_ADD_DOT_0.
	skipRe := false
	addParens := false
	stripDotZero := false
	innerType := origType
	switch origType {
	case 0:
		if re == 0.0 && math.Copysign(1.0, re) == 1.0 {
			skipRe = true
		} else {
			addParens = true
		}
		innerType = 'r'
		stripDotZero = true
	case 'n':
		// CPython: Python/formatter_unicode.c:1382 'n' folds to 'g'
		// for the digit-producing core; locale grouping is handled
		// in calc_number_widths. We don't have locale grouping, so
		// the fold is sufficient.
		innerType = 'g'
	}

	// Per-component spec: strip width/align/fill so inner formatting
	// emits the bare number; we'll pad once after assembly.
	//
	// CPython: Python/formatter_unicode.c:1446 tmp_format.fill_char='\0' etc.
	inner := s
	inner.Width = -1
	inner.Align = 0
	inner.Fill = -1
	inner.Zero = false
	inner.Type = innerType

	reStr, err := format.FormatFloat(re, inner)
	if err != nil {
		return "", err
	}
	if stripDotZero {
		reStr = stripTrailingDotZero(reStr)
	}

	// Imag component: force '+' so the connector is always emitted.
	// When skip_re, fall back to the user's sign so e.g. format(0+3j,'+')
	// keeps "+3j".
	//
	// CPython: Python/formatter_unicode.c:1464 tmp_format.sign='+' unless skip_re
	imInner := inner
	if !skipRe {
		imInner.Sign = '+'
	}
	imStr, err := format.FormatFloat(im, imInner)
	if err != nil {
		return "", err
	}
	if stripDotZero {
		imStr = stripTrailingDotZero(imStr)
	}

	var body string
	if skipRe {
		body = imStr + "j"
	} else {
		body = reStr + imStr + "j"
		if addParens {
			body = "(" + body + ")"
		}
	}

	return padComplexBody(body, s.Width, s.Align, s.Fill), nil
}

// stripTrailingDotZero drops the ".0" that pystrconv.FormatFloat appends
// to integer-valued floats under FlagAddDotZero. format_complex_internal
// passes neither flag, so the inner-component output for type='r' must
// shed the ".0" before assembly.
//
// CPython: Python/pystrtod.c:1265 PyOS_double_to_string (no ADD_DOT_0 path)
func stripTrailingDotZero(s string) string {
	if strings.HasSuffix(s, ".0") {
		return s[:len(s)-2]
	}
	return s
}

// padComplexBody applies outer width/align/fill padding. Default
// alignment is '>' (numeric) and default fill is ASCII space.
//
// CPython: Python/formatter_unicode.c:1478 calc_padding + fill_padding
func padComplexBody(body string, width int, align byte, fill rune) string {
	if width <= 0 || width <= len(body) {
		return body
	}
	if align == 0 {
		align = '>'
	}
	if fill < 0 {
		fill = ' '
	}
	missing := width - len(body)
	switch align {
	case '<':
		return body + strings.Repeat(string(fill), missing)
	case '^':
		left := missing / 2
		right := missing - left
		return strings.Repeat(string(fill), left) + body + strings.Repeat(string(fill), right)
	default:
		return strings.Repeat(string(fill), missing) + body
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
// CPython: Objects/complexobject.c:800 complex_richcompare
func complexRichCmp(a, b Object, op CompareOp) (Object, error) {
	av, ok := a.(*Complex)
	if !ok {
		return notImplemented(), nil
	}
	if op != CompareEQ && op != CompareNE {
		return notImplemented(), nil
	}
	// CPython: Objects/complexobject.c:813 long-case avoids float
	// coercion by routing through float-vs-int when imag is zero. This
	// is the only path that gives precise results for ints beyond the
	// 2**53 lossless float window.
	switch bv := b.(type) {
	case *Int:
		if imag(av.v) != 0 {
			return NewBool(op == CompareNE), nil
		}
		eq := floatEqualsBigInt(real(av.v), bv.BigInt())
		if op == CompareEQ {
			return NewBool(eq), nil
		}
		return NewBool(!eq), nil
	case *Bool:
		if imag(av.v) != 0 {
			return NewBool(op == CompareNE), nil
		}
		eq := floatEqualsBigInt(real(av.v), bv.BigInt())
		if op == CompareEQ {
			return NewBool(eq), nil
		}
		return NewBool(!eq), nil
	}
	bv, ok := asComplex(b)
	if !ok {
		return notImplemented(), nil
	}
	if op == CompareEQ {
		return NewBool(av.v == bv), nil
	}
	return NewBool(av.v != bv), nil
}

// floatEqualsBigInt reports whether x has no fractional part AND its
// integer value equals n. Inf and NaN never equal any finite int.
//
// CPython: Objects/floatobject.c:382 float_richcompare (long branch)
func floatEqualsBigInt(x float64, n *big.Int) bool {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return false
	}
	intpart, frac := math.Modf(x)
	if frac != 0 {
		return false
	}
	xi, acc := new(big.Float).SetFloat64(intpart).Int(nil)
	if acc != big.Exact {
		return false
	}
	return xi.Cmp(n) == 0
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

// realToDouble extracts the underlying float for an int / float / bool
// operand. Returns ok=false for anything else (including complex), so
// the binop wrappers can route through the mixed real-plus-complex
// helpers instead of widening through complex128 arithmetic. An int
// that doesn't fit a double raises OverflowError, matching
// _Py_convert_int_to_double via PyLong_AsDouble.
//
// CPython: Objects/complexobject.c:645 real_to_double
// CPython: Objects/longobject.c:3320 PyLong_AsDouble
func realToDouble(o Object) (float64, bool, error) {
	switch x := o.(type) {
	case *Float:
		return x.v, true, nil
	case *Bool:
		return intToFloat(&x.Int), true, nil
	case *Int:
		f := intToFloat(x)
		if math.IsInf(f, 0) {
			return 0, true, fmt.Errorf("OverflowError: int too large to convert to float")
		}
		return f, true, nil
	}
	return 0, false, nil
}

// boxInfOrZero returns copysign(isinf(x) ? 1 : 0, x). Used by _Py_c_prod
// and _Py_c_quot to recover infinities that computed as nan+nanj.
//
// CPython: Objects/complexobject.c:103 inline pattern in _Py_c_prod
func boxInfOrZero(x float64) float64 {
	if math.IsInf(x, 0) {
		return math.Copysign(1.0, x)
	}
	return math.Copysign(0.0, x)
}

// cSum adds component-wise so float64 -0 contributions stay -0 (mixed
// real-plus-complex callers use crSum / rcSum to avoid the cross terms
// that complex128 multiplication would introduce).
//
// CPython: Objects/complexobject.c:32 _Py_c_sum
func cSum(a, b complex128) complex128 {
	return complex(real(a)+real(b), imag(a)+imag(b))
}

// CPython: Objects/complexobject.c:41 _Py_cr_sum
func crSum(a complex128, b float64) complex128 {
	return complex(real(a)+b, imag(a))
}

// CPython: Objects/complexobject.c:49 _Py_rc_sum
func rcSum(a float64, b complex128) complex128 {
	return crSum(b, a)
}

// CPython: Objects/complexobject.c:55 _Py_c_diff
func cDiff(a, b complex128) complex128 {
	return complex(real(a)-real(b), imag(a)-imag(b))
}

// CPython: Objects/complexobject.c:64 _Py_cr_diff
func crDiff(a complex128, b float64) complex128 {
	return complex(real(a)-b, imag(a))
}

// CPython: Objects/complexobject.c:72 _Py_rc_diff
func rcDiff(a float64, b complex128) complex128 {
	return complex(a-real(b), -imag(b))
}

// CPython: Objects/complexobject.c:81 _Py_c_neg
func cNeg(a complex128) complex128 {
	return complex(-real(a), -imag(a))
}

// cProd multiplies and recovers infinities that fell through to NaN+NaNj,
// matching C11 Annex G.5.1 _Cmultd. Without this, inf*complex products
// surface as NaN whenever an intermediate term involves NaN.
//
// CPython: Objects/complexobject.c:90 _Py_c_prod
func cProd(z, w complex128) complex128 {
	a, b := real(z), imag(z)
	c, d := real(w), imag(w)
	ac, bd, ad, bc := a*c, b*d, a*d, b*c
	rr, ri := ac-bd, ad+bc
	if !(math.IsNaN(rr) && math.IsNaN(ri)) {
		return complex(rr, ri)
	}
	recalc := false
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		a = boxInfOrZero(a)
		b = boxInfOrZero(b)
		if math.IsNaN(c) {
			c = math.Copysign(0, c)
		}
		if math.IsNaN(d) {
			d = math.Copysign(0, d)
		}
		recalc = true
	}
	if math.IsInf(c, 0) || math.IsInf(d, 0) {
		c = boxInfOrZero(c)
		d = boxInfOrZero(d)
		if math.IsNaN(a) {
			a = math.Copysign(0, a)
		}
		if math.IsNaN(b) {
			b = math.Copysign(0, b)
		}
		recalc = true
	}
	if !recalc && (math.IsInf(ac, 0) || math.IsInf(bd, 0) || math.IsInf(ad, 0) || math.IsInf(bc, 0)) {
		if math.IsNaN(a) {
			a = math.Copysign(0, a)
		}
		if math.IsNaN(b) {
			b = math.Copysign(0, b)
		}
		if math.IsNaN(c) {
			c = math.Copysign(0, c)
		}
		if math.IsNaN(d) {
			d = math.Copysign(0, d)
		}
		recalc = true
	}
	if recalc {
		inf := math.Inf(1)
		rr = inf * (a*c - b*d)
		ri = inf * (a*d + b*c)
	}
	return complex(rr, ri)
}

// CPython: Objects/complexobject.c:151 _Py_cr_prod
func crProd(a complex128, b float64) complex128 {
	return complex(real(a)*b, imag(a)*b)
}

// CPython: Objects/complexobject.c:160 _Py_rc_prod
func rcProd(a float64, b complex128) complex128 {
	return crProd(b, a)
}

// errComplexEDOM signals "division by zero" from the cQuot/crQuot/rcQuot
// helpers. The binop wrapper translates it to ZeroDivisionError.
var errComplexEDOM = errors.New("EDOM")

// cQuot divides via Smith's CACM Algorithm 116 (the same case-split
// CPython uses to dodge spurious overflow/underflow) and recovers
// infinities and zeros that fell through to NaN+NaNj, per C11 G.5.2.
//
// CPython: Objects/complexobject.c:170 _Py_c_quot
func cQuot(a, b complex128) (complex128, error) {
	absBReal := math.Abs(real(b))
	absBImag := math.Abs(imag(b))
	var rr, ri float64
	var domErr error
	switch {
	case absBReal >= absBImag:
		if absBReal == 0.0 {
			domErr = errComplexEDOM
			rr, ri = 0, 0
		} else {
			ratio := imag(b) / real(b)
			denom := real(b) + imag(b)*ratio
			rr = (real(a) + imag(a)*ratio) / denom
			ri = (imag(a) - real(a)*ratio) / denom
		}
	case absBImag >= absBReal:
		ratio := real(b) / imag(b)
		denom := real(b)*ratio + imag(b)
		rr = (real(a)*ratio + imag(a)) / denom
		ri = (imag(a)*ratio - real(a)) / denom
	default:
		rr, ri = math.NaN(), math.NaN()
	}
	if math.IsNaN(rr) && math.IsNaN(ri) {
		aFinite := !math.IsInf(real(a), 0) && !math.IsNaN(real(a)) &&
			!math.IsInf(imag(a), 0) && !math.IsNaN(imag(a))
		bFinite := !math.IsInf(real(b), 0) && !math.IsNaN(real(b)) &&
			!math.IsInf(imag(b), 0) && !math.IsNaN(imag(b))
		switch {
		case (math.IsInf(real(a), 0) || math.IsInf(imag(a), 0)) && bFinite:
			x := boxInfOrZero(real(a))
			y := boxInfOrZero(imag(a))
			rr = math.Inf(1) * (x*real(b) + y*imag(b))
			ri = math.Inf(1) * (y*real(b) - x*imag(b))
		case (math.IsInf(absBReal, 0) || math.IsInf(absBImag, 0)) && aFinite:
			x := boxInfOrZero(real(b))
			y := boxInfOrZero(imag(b))
			rr = 0.0 * (real(a)*x + imag(a)*y)
			ri = 0.0 * (imag(a)*x - real(a)*y)
		}
	}
	return complex(rr, ri), domErr
}

// CPython: Objects/complexobject.c:249 _Py_cr_quot
func crQuot(a complex128, b float64) (complex128, error) {
	if b == 0 {
		return 0, errComplexEDOM
	}
	return complex(real(a)/b, imag(a)/b), nil
}

// CPython: Objects/complexobject.c:265 _Py_rc_quot
func rcQuot(a float64, b complex128) (complex128, error) {
	absBReal := math.Abs(real(b))
	absBImag := math.Abs(imag(b))
	var rr, ri float64
	var domErr error
	switch {
	case absBReal >= absBImag:
		if absBReal == 0.0 {
			domErr = errComplexEDOM
			rr, ri = 0, 0
		} else {
			ratio := imag(b) / real(b)
			denom := real(b) + imag(b)*ratio
			rr = a / denom
			ri = (-a * ratio) / denom
		}
	case absBImag >= absBReal:
		ratio := real(b) / imag(b)
		denom := real(b)*ratio + imag(b)
		rr = (a * ratio) / denom
		ri = (-a) / denom
	default:
		rr, ri = math.NaN(), math.NaN()
	}
	if math.IsNaN(rr) && math.IsNaN(ri) && !math.IsInf(a, 0) && !math.IsNaN(a) &&
		(math.IsInf(absBReal, 0) || math.IsInf(absBImag, 0)) {
		x := boxInfOrZero(real(b))
		y := boxInfOrZero(imag(b))
		rr = 0.0 * (a * x)
		ri = 0.0 * (-a * y)
	}
	return complex(rr, ri), domErr
}

// complexAdd dispatches to cSum / crSum / rcSum depending on which side
// is a complex. The mixed (real+complex) path goes through crSum/rcSum
// so we don't pick up spurious -0 components from a complex128 widen.
//
// CPython: Objects/complexobject.c:680 COMPLEX_BINOP (add)
func complexAdd(v, w Object) (Object, error) {
	r, ok, err := complexBinop(v, w, cSum, crSum, rcSum)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewComplex(real(r), imag(r)), nil
}

// CPython: Objects/complexobject.c:680 COMPLEX_BINOP (sub)
func complexSub(v, w Object) (Object, error) {
	r, ok, err := complexBinop(v, w, cDiff, crDiff, rcDiff)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewComplex(real(r), imag(r)), nil
}

// CPython: Objects/complexobject.c:680 COMPLEX_BINOP (mul)
func complexMul(v, w Object) (Object, error) {
	r, ok, err := complexBinop(v, w, cProd, crProd, rcProd)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewComplex(real(r), imag(r)), nil
}

// complexTrueDiv dispatches to cQuot / crQuot / rcQuot. A zero divisor
// raises ZeroDivisionError rather than producing inf+infj.
//
// CPython: Objects/complexobject.c:680 COMPLEX_BINOP (div)
func complexTrueDiv(v, w Object) (Object, error) {
	r, ok, err := complexBinopErr(v, w, cQuot, crQuot, rcQuot)
	if err != nil {
		if errors.Is(err, errComplexEDOM) {
			return nil, errors.New("ZeroDivisionError: division by zero")
		}
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewComplex(real(r), imag(r)), nil
}

// complexBinop implements the COMPLEX_BINOP macro: routes (v, w) to the
// matching (cc, cr, rc) helper based on which side is a complex and
// which is a coercible real (int/float/bool). Returns ok=false when
// neither side is a complex or the non-complex side isn't a real number.
//
// CPython: Objects/complexobject.c:680 COMPLEX_BINOP
func complexBinop(v, w Object,
	cc func(complex128, complex128) complex128,
	cr func(complex128, float64) complex128,
	rc func(float64, complex128) complex128,
) (complex128, bool, error) {
	if bw, ok := w.(*Complex); ok {
		if vc, ok := v.(*Complex); ok {
			return cc(vc.v, bw.v), true, nil
		}
		vr, ok, err := realToDouble(v)
		if err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, nil
		}
		return rc(vr, bw.v), true, nil
	}
	vc, ok := v.(*Complex)
	if !ok {
		return 0, false, nil
	}
	br, ok, err := realToDouble(w)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	return cr(vc.v, br), true, nil
}

// complexBinopErr is complexBinop for the division helpers, which can
// return errComplexEDOM.
func complexBinopErr(v, w Object,
	cc func(complex128, complex128) (complex128, error),
	cr func(complex128, float64) (complex128, error),
	rc func(float64, complex128) (complex128, error),
) (complex128, bool, error) {
	if bw, ok := w.(*Complex); ok {
		if vc, ok := v.(*Complex); ok {
			r, err := cc(vc.v, bw.v)
			return r, err == nil, err
		}
		vr, ok, err := realToDouble(v)
		if err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, nil
		}
		r, err := rc(vr, bw.v)
		return r, err == nil, err
	}
	vc, ok := v.(*Complex)
	if !ok {
		return 0, false, nil
	}
	br, ok, err := realToDouble(w)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}
	r, err := cr(vc.v, br)
	return r, err == nil, err
}

// cPow ports _Py_c_pow. Returns errComplexEDOM when raising zero to a
// non-positive or non-real power.
//
// CPython: Objects/complexobject.c:310 _Py_c_pow
func cPow(a, b complex128) (complex128, error) {
	if real(b) == 0.0 && imag(b) == 0.0 {
		return complex(1, 0), nil
	}
	if real(a) == 0.0 && imag(a) == 0.0 {
		if imag(b) != 0.0 || real(b) < 0 {
			return complex(0, 0), errComplexEDOM
		}
		return complex(0, 0), nil
	}
	vabs := math.Hypot(real(a), imag(a))
	length := math.Pow(vabs, real(b))
	at := math.Atan2(imag(a), real(a))
	phase := at * real(b)
	if imag(b) != 0.0 {
		length *= math.Exp(-at * imag(b))
		phase += imag(b) * math.Log(vabs)
	}
	return complex(length*math.Cos(phase), length*math.Sin(phase)), nil
}

// complexPower implements `pow(a, b)`. The third modulus argument is
// rejected with TypeError, matching CPython.
//
// CPython: Objects/complexobject.c:724 complex_pow
func complexPower(a, b, mod Object) (Object, error) {
	if mod != nil && mod != None() {
		return nil, errors.New("ValueError: complex modulo")
	}
	av, bv, ok := complexPair(a, b)
	if !ok {
		return notImplemented(), nil
	}
	r, err := cPow(av, bv)
	if err != nil {
		if errors.Is(err, errComplexEDOM) {
			return nil, errors.New("ZeroDivisionError: zero to a negative or complex power")
		}
		return nil, err
	}
	if math.IsInf(real(r), 0) || math.IsInf(imag(r), 0) {
		return nil, errors.New("OverflowError: complex exponentiation")
	}
	return NewComplex(real(r), imag(r)), nil
}

// complexNeg negates component-wise so a -0 imaginary part flips to +0
// (matching CPython, which negates each field directly rather than
// going through complex128 negation).
//
// CPython: Objects/complexobject.c:760 complex_neg
func complexNeg(o Object) (Object, error) {
	v := cNeg(o.(*Complex).v)
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
