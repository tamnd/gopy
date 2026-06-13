package objects

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/cmplx"
	"strconv"
	"strings"
	"unsafe"
)

// Float is the Python float, an IEEE-754 double.
//
// CPython: Include/cpython/floatobject.h:L7 PyFloatObject
type Float struct {
	Header
	v     float64
	attrs *Dict // per-instance dict for float subclasses (CPython: tp_dictoffset)
}

// AttrDict returns the per-instance attribute dict or nil.
func (f *Float) AttrDict() *Dict { return f.attrs }

// EnsureAttrDict allocates the per-instance attribute dict on first use.
// CPython: Objects/typeobject.c subtype_setdict
func (f *Float) EnsureAttrDict() *Dict {
	if f.attrs == nil {
		f.attrs = NewDict()
		trackAttrDictHolder(f)
	}
	return f.attrs
}

// SetAttrDict rebinds the managed __dict__ for `obj.__dict__ = d`.
//
// CPython: Objects/typeobject.c:3795 subtype_setdict
func (f *Float) SetAttrDict(d *Dict) { f.attrs = d }

// FloatType is the type singleton for float. Mirrors PyFloat_Type.
// Slots are wired in init() because floatHash transitively constructs
// Ints which would otherwise close the dep cycle.
//
// CPython: Objects/floatobject.c:L2068 PyFloat_Type
var FloatType = NewType("float", []*Type{objectType})

func init() {
	FloatType.Repr = floatRepr
	// CPython: Objects/typeobject.c:1356 subtype_traverse (managed __dict__)
	FloatType.TpTraverse = attrDictHolderTraverse
	FloatType.Str = floatRepr
	FloatType.Hash = floatHash
	FloatType.RichCmp = floatRichCmp
	// CPython: Objects/floatobject.c:851 PyFloat_Type.tp_richcompare slot wrapper
	BindRichCmpDescriptors(FloatType)
	FloatType.TpFlags |= TpFlagMatchSelf
	FloatType.Number = &NumberMethods{
		Add:         floatAdd,
		Subtract:    floatSub,
		Multiply:    floatMul,
		TrueDivide:  floatTrueDiv,
		FloorDivide: floatFloorDiv,
		Remainder:   floatMod,
		Divmod:      floatDivmod,
		Negative:    floatNeg,
		Positive:    floatPos,
		Absolute:    floatAbs,
		Bool:        floatBool,
		Float:       floatPos,
		Power:       floatPower,
	}
	// float.__getformat__(typestr) is a classmethod that returns the
	// memory layout for "float"/"double". gopy targets Go which uses
	// IEEE-754; we report the host endianness. test.support's
	// requires_IEEE_754 decorator checks this at module load.
	//
	// CPython: Objects/floatobject.c:1748 float___getformat___impl
	SetTypeDescr(FloatType, "__getformat__", NewClassMethod(
		NewBuiltinFunction("__getformat__", floatGetFormat)))

	// float.__repr__ and float.__str__ slot wrappers. CPython generates
	// these via add_operators from slotdefs (TPSLOT __repr__ +
	// PyFloat_Type.tp_repr). json.encoder pins float.__repr__ as the
	// _floatstr argument default at module load; without the descriptor
	// it inherits object.__repr__ and json output prints
	// `<float object at 0x...>` instead of the IEEE-754 digit string.
	//
	// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __repr__)
	// CPython: Objects/floatobject.c:357 float_repr
	SetTypeDescr(FloatType, "__repr__", NewMethodDescr(FloatType, "__repr__", floatReprDescr))
	SetTypeDescr(FloatType, "__str__", NewMethodDescr(FloatType, "__str__", floatReprDescr))

	// float_getset (Objects/floatobject.c:1797): real returns self,
	// imag returns 0.0. CPython exposes both as get-only descriptors.
	SetTypeDescr(FloatType, "real", NewGetSetDescr("real", floatRealGetter, nil))
	SetTypeDescr(FloatType, "imag", NewGetSetDescr("imag", floatImagGetter, nil))
	SetTypeDescr(FloatType, "conjugate", NewMethodDescrConv(FloatType, "conjugate", MethNoArgs, floatConjugateMethod))
	// Install float.__hash__ as a descriptor so that subclasses that also
	// inherit from a mixin with __hash__ resolve float's tp_hash first in
	// the MRO. fixupHashAndIter uses LookupDescriptor(t, "__hash__") to
	// pick which hash to install; without this entry H.__hash__ beats
	// float.__hash__ when float comes after H in the MRO.
	//
	// CPython: Objects/typeobject.c:8230 slotdefs (TPSLOT __hash__)
	SetTypeDescr(FloatType, "__hash__", NewMethodDescr(FloatType, "__hash__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
		}
		h, err := floatHash(args[0])
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	// CPython: Objects/typeobject.c:11025 add_operators over the numeric slotdefs.
	AddNumberSlotWrappers(FloatType)
}

// floatRealGetter backs float.real: returns self.
//
// CPython: Objects/floatobject.c:1741 float_getreal
func floatRealGetter(owner Object) (Object, error) {
	f, ok := owner.(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'real' for 'float' objects doesn't apply to a '%s' object", typeNameOf(owner))
	}
	return f, nil
}

// floatImagGetter backs float.imag: always 0.0.
//
// CPython: Objects/floatobject.c:1747 float_getimag
func floatImagGetter(_ Object) (Object, error) {
	return NewFloat(0.0), nil
}

// floatConjugateMethod backs float.conjugate(): returns self.
//
// CPython: Objects/floatobject.c:1096 float_conjugate_impl
func floatConjugateMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: conjugate() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'conjugate' for 'float' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return f, nil
}

// floatReprDescr is the slot wrapper for float.__repr__ / float.__str__.
// Required for the same reason as intReprDescr: json.encoder captures
// `_floatstr=float.__repr__` as a default parameter, so the attribute
// has to resolve to the digit-emitting routine, not object.__repr__.
// FloatType.Repr already implements the runtime slot; this descriptor
// is what makes the bound-method lookup return a real callable. Once
// the add_operators slot-wrapper generation in #647 ships, this manual
// wiring goes away alongside intReprDescr.
//
// CPython: Objects/floatobject.c:357 float_repr
func floatReprDescr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: expected 1 argument, got %d", len(args))
	}
	f, ok := args[0].(*Float)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'float' object")
	}
	out, err := floatRepr(f)
	if err != nil {
		return nil, err
	}
	return NewStr(out), nil
}

// floatGetFormat backs float.__getformat__. args[0] is the type
// (classmethod binding), args[1] is "float" or "double".
//
// CPython: Objects/floatobject.c:1748 float___getformat___impl
func floatGetFormat(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getformat__() takes exactly one argument (%d given)", len(args)-1)
	}
	typestr, ok := args[1].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: __getformat__() argument must be str, not %s", args[1].Type().Name)
	}
	switch typestr.v {
	case "float", "double":
	default:
		return nil, fmt.Errorf("ValueError: __getformat__() argument 1 must be 'double' or 'float'")
	}
	// gopy runs on host architectures where Go float64 is IEEE-754.
	// Detect endianness at runtime via unsafe to stay portable.
	if hostIsLittleEndian() {
		return NewStr("IEEE, little-endian"), nil
	}
	return NewStr("IEEE, big-endian"), nil
}

func hostIsLittleEndian() bool {
	var x uint16 = 1
	b := (*[2]byte)(unsafe.Pointer(&x))
	return b[0] == 1
}

// NewFloat builds a float. The singleton cache short-circuits the
// well-known constants (0.0, -0.0, +/- 1.0, NaN, +/- Inf) so the hot
// numeric loops that bounce on those values stay allocation-free.
//
// CPython: Objects/floatobject.c:124 PyFloat_FromDouble
func NewFloat(x float64) *Float {
	if cached := cachedFloat(x); cached != nil {
		return cached
	}
	return newFloatRaw(x)
}

// Float64 returns the underlying double.
func (f *Float) Float64() float64 {
	return f.v
}

// newFloatAs builds a float tagged with t instead of FloatType.
// Used by the float subtype path so a class like `class F(float): pass`
// yields instances whose Type() is F.
//
// CPython: Objects/floatobject.c:1596 float_subtype_new
func newFloatAs(x float64, t *Type) *Float {
	o := &Float{v: x}
	o.init(t)
	return o
}

// SetFloatTpNewBase wires the value-side constructor (float(value)) and
// makes it subtype-aware: when cls != FloatType the result is re-tagged.
// Mirrors SetIntTpNewBase.
//
// CPython: Objects/floatobject.c:1575 float_new_impl (subtype branch)
func SetFloatTpNewBase(fn func(args []Object, kwargs map[string]Object) (Object, error)) {
	FloatType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		ctorKwargs := kwargs
		if cls != nil && cls != FloatType {
			// CPython: Objects/floatobject.c:1575 float_new_impl
			// float.__new__'s "x" parameter is positional-only, so passing
			// x as a keyword is always a TypeError even for subtypes.
			// Other unknown kwargs belong to __init__ and must not be
			// rejected here.
			if _, hasX := kwargs["x"]; hasX {
				return nil, fmt.Errorf("TypeError: float() takes no keyword arguments")
			}
			ctorKwargs = nil
		}
		out, err := fn(args, ctorKwargs)
		if err != nil {
			return nil, err
		}
		if cls == nil || cls == FloatType {
			return out, nil
		}
		f, ok := out.(*Float)
		if !ok {
			return out, nil
		}
		return newFloatAs(f.v, cls), nil
	}
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
// hashINF is the hash sentinel for positive infinity.
// CPython: Python/pyhash.c _PyHASH_INF
const hashINF = 314159

// floatHash ports _Py_HashDouble. Integral floats share the same hash
// as the equivalent int. Inf maps to the dedicated sentinel. NaN uses
// the identity hash (CPython 3.14 change: gh-93883).
//
// CPython: Python/pyhash.c:87 _Py_HashDouble
func floatHash(o Object) (int64, error) {
	v := o.(*Float).v
	if math.IsNaN(v) {
		// CPython 3.14: NaN hashes via PyObject_GenericHash (identity hash).
		// CPython: Python/pyhash.c:87 _Py_HashDouble (NaN branch)
		return identityHash(o)
	}
	if math.IsInf(v, 1) {
		return hashINF, nil
	}
	if math.IsInf(v, -1) {
		return -hashINF, nil
	}

	// Decompose v = m * 2**e (0.5 <= |m| < 1.0)
	m, e := math.Frexp(v)
	sign := int64(1)
	if m < 0 {
		sign = -1
		m = -m
	}

	// Process 28 bits at a time, accumulating into x modulo _PyHASH_MODULUS (2^61-1)
	const (
		pyHashBits    = 61
		pyHashModulus = (int64(1) << pyHashBits) - 1
	)
	var x int64
	for m != 0 {
		x = ((x << 28) & pyHashModulus) | (x >> (pyHashBits - 28))
		m *= 268435456.0 // 2**28
		e -= 28
		y := int64(m)
		m -= float64(y)
		x += y
		if x >= pyHashModulus {
			x -= pyHashModulus
		}
	}

	// Adjust for the exponent (reduce e mod pyHashBits first)
	if e >= 0 {
		e %= pyHashBits
	} else {
		e = pyHashBits - 1 - ((-1 - e) % pyHashBits)
	}
	x = ((x << e) & pyHashModulus) | (x >> (pyHashBits - e))

	x *= sign
	if x == -1 {
		x = -2
	}
	return x, nil
}

// floatRichCmp implements float vs (float | int) ordering. The float vs
// int case avoids a naive float64(int) cast (which loses precision past
// 2^53 and overflows past 2^1024) by mirroring float_richcompare's
// sign / bit-count / mantissa-truncation algorithm.
//
// CPython: Objects/floatobject.c:382 float_richcompare
func floatRichCmp(a, b Object, op CompareOp) (Object, error) {
	af, ok := a.(*Float)
	if !ok {
		return nil, fmt.Errorf("floatRichCmp: lhs is %T", a)
	}
	i := af.v
	switch x := b.(type) {
	case *Float:
		return boolFromBool(compareFloats(i, x.v, op)), nil
	case *Bool:
		return floatVsInt(i, &x.Int, op)
	case *Int:
		return floatVsInt(i, x, op)
	}
	return notImplemented(), nil
}

// floatVsInt is the PyLong_Check(w) branch of float_richcompare:
// it computes the exact ordering of float i vs integer w without
// rounding either operand.
//
// CPython: Objects/floatobject.c:407 float_richcompare (PyLong_Check branch)
func floatVsInt(i float64, w *Int, op CompareOp) (Object, error) {
	// Infinities and NaN compare against any finite int without
	// looking at the magnitude.
	if math.IsInf(i, 0) || math.IsNaN(i) {
		return boolFromBool(compareFloats(i, 0, op)), nil
	}
	vsign := 0
	switch {
	case i > 0:
		vsign = 1
	case i < 0:
		vsign = -1
	}
	wsign := w.Sign()
	if vsign != wsign {
		return boolFromBool(compareFloats(float64(vsign), float64(wsign), op)), nil
	}
	// Same sign: compare magnitudes precisely.
	nbits := w.v.BitLen()
	const dblMaxExp = 1024 // float64 DBL_MAX_EXP
	if nbits > dblMaxExp {
		// |w| exceeds any finite float, so the answer is determined by sign.
		return boolFromBool(compareFloats(float64(vsign), float64(wsign)*2.0, op)), nil
	}
	if nbits <= 48 {
		// w fits exactly in a float64 mantissa (52 bits, but CPython
		// uses the conservative 48 to match its 32-bit C long path).
		j, _ := w.v.Float64()
		return boolFromBool(compareFloats(i, j, op)), nil
	}
	// nbits > 48 and same sign. Compare via frexp: the exponent of i
	// is the number of bits before the radix point, so an exponent
	// mismatch decides the ordering by magnitude.
	_, exponent := math.Frexp(i)
	if exponent < nbits {
		// |w| > |i|. CPython sets j = i; i = 0; then compares
		// i op j, which is 0 op old_i. We mirror that directly.
		return boolFromBool(compareFloats(0, i, op)), nil
	}
	if exponent > nbits {
		// |i| > |w|. CPython sets j = 0 and compares i op j.
		return boolFromBool(compareFloats(i, 0, op)), nil
	}
	// Same number of bits before the radix point: split i into
	// integer and fractional parts, fold a non-zero fractional part
	// into the comparison op, then run an int/int compare.
	intpart, fracpart := math.Modf(i)
	if fracpart != 0 {
		switch op {
		case CompareEQ:
			return False(), nil
		case CompareNE:
			return True(), nil
		case CompareLT, CompareLE:
			if vsign > 0 {
				op = CompareLT
			} else {
				op = CompareLE
			}
		case CompareGT, CompareGE:
			if vsign > 0 {
				op = CompareGE
			} else {
				op = CompareGT
			}
		}
	}
	bf := new(big.Float).SetPrec(64).SetFloat64(intpart)
	vv := new(big.Int)
	bf.Int(vv)
	cmp := vv.Cmp(&w.v)
	var res bool
	switch op {
	case CompareLT:
		res = cmp < 0
	case CompareLE:
		res = cmp <= 0
	case CompareEQ:
		res = cmp == 0
	case CompareNE:
		res = cmp != 0
	case CompareGT:
		res = cmp > 0
	case CompareGE:
		res = cmp >= 0
	}
	return boolFromBool(res), nil
}

func boolFromBool(b bool) Object {
	if b {
		return True()
	}
	return False()
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
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af + bf), nil
}

func floatSub(a, b Object) (Object, error) {
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af - bf), nil
}

func floatMul(a, b Object) (Object, error) {
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	return NewFloat(af * bf), nil
}

func floatNeg(o Object) (Object, error) {
	return NewFloat(-o.(*Float).v), nil
}

// floatPos ports float_float, which backs both float.__float__ and
// nb_positive. An exact float is returned unchanged; a subclass instance
// is collapsed to a fresh plain float carrying the same value.
//
// CPython: Objects/floatobject.c:1602 float_float
func floatPos(o Object) (Object, error) {
	if o.Type() == FloatType {
		return o, nil
	}
	return NewFloat(o.(*Float).v), nil
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
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
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
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	return NewFloat(af / bf), nil
}

// floatFloorDiv returns floor(a/b) as a float, matching Python's
// `__floordiv__`. The result is still a float when both operands are
// float; the int / int case stays in intFloorDiv.
//
// CPython: Objects/floatobject.c float_floor_div
func floatFloorDiv(a, b Object) (Object, error) {
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	return NewFloat(math.Floor(af / bf)), nil
}

// floatMod implements Python `%` for floats, where the sign of the
// result matches the divisor (like math.Mod, then adjusted).
//
// CPython: Objects/floatobject.c float_rem
func floatMod(a, b Object) (Object, error) {
	af, bf, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	if bf == 0 {
		return nil, errors.New("ZeroDivisionError: division by zero")
	}
	r := math.Mod(af, bf)
	if r != 0 {
		if (r < 0) != (bf < 0) {
			r += bf
		}
	} else {
		// CPython: Objects/floatobject.c:620 ensure zero has sign of divisor
		r = math.Copysign(0.0, bf)
	}
	return NewFloat(r), nil
}

func floatBool(o Object) (bool, error) {
	return o.(*Float).v != 0, nil
}

// floatPower implements `pow(a, b)` and `pow(a, b, mod)` for floats.
// Ports CPython's float_pow special-case ordering exactly so that
// IEEE-754 edge cases (NaN, Inf) return the C99 F.9.4.4 mandated values.
//
// CPython: Objects/floatobject.c:697 float_pow
func floatPower(a, b, mod Object) (Object, error) {
	if mod != nil && mod != None() {
		return nil, errors.New("TypeError: pow() 3rd argument not allowed unless all arguments are integers")
	}
	iv, iw, ok, err := floatPair(a, b)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notImplemented(), nil
	}
	// v**0 == 1, even 0**0
	if iw == 0 {
		return NewFloat(1.0), nil
	}
	// nan**w == nan
	if math.IsNaN(iv) {
		return NewFloat(iv), nil
	}
	// v**nan == nan, except 1**nan == 1
	if math.IsNaN(iw) {
		if iv == 1.0 {
			return NewFloat(1.0), nil
		}
		return NewFloat(iw), nil
	}
	// v**±inf — depends on |v| vs 1
	if math.IsInf(iw, 0) {
		absv := math.Abs(iv)
		if absv == 1.0 {
			return NewFloat(1.0), nil
		}
		if (iw > 0) == (absv > 1.0) {
			return NewFloat(math.Abs(iw)), nil // inf
		}
		return NewFloat(0.0), nil
	}
	// ±inf**w
	if math.IsInf(iv, 0) {
		iwIsOdd := isOddInteger(iw)
		if iw > 0 {
			if iwIsOdd {
				return NewFloat(iv), nil
			}
			return NewFloat(math.Abs(iv)), nil
		}
		if iwIsOdd {
			return NewFloat(math.Copysign(0.0, iv)), nil
		}
		return NewFloat(0.0), nil
	}
	// 0**w
	if iv == 0.0 {
		if iw < 0 {
			return nil, errors.New("ZeroDivisionError: zero to a negative power")
		}
		if isOddInteger(iw) {
			return NewFloat(iv), nil
		}
		return NewFloat(0.0), nil
	}
	// negative base with fractional exponent
	negateResult := false
	if iv < 0.0 {
		if iw != math.Floor(iw) {
			// CPython: Objects/floatobject.c:765 — delegates to complex.__pow__
			base := complex(iv, 0)
			exp := complex(iw, 0)
			result := cmplx.Pow(base, exp)
			return NewComplex(real(result), imag(result)), nil
		}
		iv = -iv
		negateResult = isOddInteger(iw)
	}
	if iv == 1.0 {
		if negateResult {
			return NewFloat(-1.0), nil
		}
		return NewFloat(1.0), nil
	}
	ix := math.Pow(iv, iw)
	if math.IsInf(ix, 0) && !math.IsInf(iv, 0) && !math.IsInf(iw, 0) {
		return nil, errors.New("OverflowError: math range error")
	}
	if negateResult {
		ix = -ix
	}
	return NewFloat(ix), nil
}

// isOddInteger reports whether x is a finite odd integer.
// CPython: Objects/floatobject.c DOUBLE_IS_ODD_INTEGER macro.
func isOddInteger(x float64) bool {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return false
	}
	t := math.Trunc(x)
	return t == x && math.Mod(t, 2) != 0
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

// floatPair coerces two operands to float64 for mixed-numeric float
// arithmetic. Returns ok=false when either side is not a numeric type
// (callers return NotImplemented). When both sides are numeric but a
// huge int overflows float64, err carries the OverflowError so callers
// can propagate it instead of silently producing inf.
//
// CPython: Objects/floatobject.c float_add (PyFloat_AsDouble path)
func floatPair(a, b Object) (af, bf float64, ok bool, err error) {
	af, aOk, aErr := asFloatChecked(a)
	if aErr != nil {
		return 0, 0, true, aErr
	}
	bf, bOk, bErr := asFloatChecked(b)
	if bErr != nil {
		return 0, 0, true, bErr
	}
	if !aOk || !bOk {
		return 0, 0, false, nil
	}
	return af, bf, true, nil
}

func asFloatChecked(o Object) (float64, bool, error) {
	switch x := o.(type) {
	case *Float:
		return x.v, true, nil
	case *Bool:
		return intToFloat(&x.Int), true, nil
	case *Int:
		if v, ok := x.Int64(); ok {
			return float64(v), true, nil
		}
		f, err := bigIntToFloat64(&x.v)
		if err != nil {
			return 0, true, err
		}
		return f, true, nil
	}
	return 0, false, nil
}

func asFloat(o Object) (float64, bool) {
	f, ok, _ := asFloatChecked(o)
	return f, ok
}
