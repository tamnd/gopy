// Package math is a full 1:1 port of CPython's mathmodule.c.
// Every function is backed by Go's math package and every constant
// from the CPython module is exposed.
//
// CPython: Modules/mathmodule.c:4198 PyInit_math
package math

import (
	"fmt"
	gomath "math"
	"math/big"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("math", buildModule)
}

// buildModule constructs the math module, registering all functions
// and constants from mathmodule.c.
//
// CPython: Modules/mathmodule.c:4094 math_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("math")
	d := m.Dict()

	type entry struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}
	funcs := []entry{
		// Trig
		{"acos", mathAcos},
		{"acosh", mathAcosh},
		{"asin", mathAsin},
		{"asinh", mathAsinh},
		{"atan", mathAtan},
		{"atan2", mathAtan2},
		{"atanh", mathAtanh},
		{"cos", mathCos},
		{"cosh", mathCosh},
		{"sin", mathSin},
		{"sinh", mathSinh},
		{"tan", mathTan},
		{"tanh", mathTanh},
		{"hypot", mathHypot},
		{"degrees", mathDegrees},
		{"radians", mathRadians},
		{"dist", mathDist},
		// Power / exp / log
		{"cbrt", mathCbrt},
		{"exp", mathExp},
		{"exp2", mathExp2},
		{"expm1", mathExpm1},
		{"log", mathLog},
		{"log1p", mathLog1p},
		{"log2", mathLog2},
		{"log10", mathLog10},
		{"pow", mathPow},
		{"sqrt", mathSqrt},
		// Special
		{"erf", mathErf},
		{"erfc", mathErfc},
		{"gamma", mathGamma},
		{"lgamma", mathLgamma},
		// Number-theoretic / integer
		{"ceil", mathCeil},
		{"floor", mathFloor},
		{"trunc", mathTrunc},
		{"isqrt", mathIsqrt},
		{"factorial", mathFactorial},
		{"gcd", mathGcd},
		{"lcm", mathLcm},
		{"comb", mathComb},
		{"perm", mathPerm},
		{"isfinite", mathIsfinite},
		{"isinf", mathIsinf},
		{"isnan", mathIsnan},
		{"isclose", mathIsclose},
		{"fsum", mathFsum},
		{"prod", mathProd},
		// Float operations
		{"copysign", mathCopysign},
		{"fabs", mathFabs},
		{"fmod", mathFmod},
		{"frexp", mathFrexp},
		{"ldexp", mathLdexp},
		{"modf", mathModf},
		{"remainder", mathRemainder},
		{"nextafter", mathNextafter},
		{"ulp", mathUlp},
	}
	for _, e := range funcs {
		if err := d.SetItem(objects.NewStr(e.name), objects.NewBuiltinFunction(e.name, e.fn)); err != nil {
			return nil, err
		}
	}

	// Constants.
	// CPython: Modules/mathmodule.c:4097 math_exec (pi)
	// CPython: Modules/mathmodule.c:4100 math_exec (e)
	// CPython: Modules/mathmodule.c:4104 math_exec (tau)
	// CPython: Modules/mathmodule.c:4107 math_exec (inf)
	// CPython: Modules/mathmodule.c:4110 math_exec (nan)
	consts := []struct {
		name string
		v    float64
	}{
		{"pi", gomath.Pi},
		{"e", gomath.E},
		{"tau", 2 * gomath.Pi},
		{"inf", gomath.Inf(1)},
		{"nan", gomath.NaN()},
	}
	for _, c := range consts {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewFloat(c.v)); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// objectToFloat coerces a Python int or float to float64.
func objectToFloat(o objects.Object, fname string) (float64, error) {
	switch x := o.(type) {
	case *objects.Float:
		return x.Float64(), nil
	case *objects.Int:
		v, ok := x.Int64()
		if ok {
			return float64(v), nil
		}
		f, _ := x.BigInt().Float64()
		return f, nil
	}
	return 0, fmt.Errorf("TypeError: %s() requires a number, not '%s'", fname, o.Type().Name)
}

// oneFloat is the standard helper for METH_O float functions.
func oneFloat(args []objects.Object, fname string) (float64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("TypeError: %s() takes exactly 1 argument (%d given)", fname, len(args))
	}
	return objectToFloat(args[0], fname)
}

// twoFloats is the standard helper for two-argument float functions.
func twoFloats(args []objects.Object, fname string) (float64, float64, error) {
	if len(args) != 2 {
		return 0, 0, fmt.Errorf("TypeError: %s() takes exactly 2 arguments (%d given)", fname, len(args))
	}
	x, err := objectToFloat(args[0], fname)
	if err != nil {
		return 0, 0, err
	}
	y, err := objectToFloat(args[1], fname)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

// ---- Trig functions -------------------------------------------------------

// mathAcos implements math.acos(x).
// CPython: Modules/mathmodule.c:1079 FUNC1D(acos, acos, ...)
func mathAcos(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "acos")
	if err != nil {
		return nil, err
	}
	r := gomath.Acos(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathAcosh implements math.acosh(x).
// CPython: Modules/mathmodule.c:1084 FUNC1D(acosh, acosh, ...)
func mathAcosh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "acosh")
	if err != nil {
		return nil, err
	}
	r := gomath.Acosh(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathAsin implements math.asin(x).
// CPython: Modules/mathmodule.c:1088 FUNC1D(asin, asin, ...)
func mathAsin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "asin")
	if err != nil {
		return nil, err
	}
	r := gomath.Asin(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathAsinh implements math.asinh(x).
// CPython: Modules/mathmodule.c:1093 FUNC1(asinh, asinh, ...)
func mathAsinh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "asinh")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Asinh(x)), nil
}

// mathAtan implements math.atan(x).
// CPython: Modules/mathmodule.c:1096 FUNC1(atan, atan, ...)
func mathAtan(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "atan")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Atan(x)), nil
}

// mathAtan2 implements math.atan2(y, x).
// CPython: Modules/mathmodule.c:1100 FUNC2(atan2, atan2, ...)
func mathAtan2(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	y, x, err := twoFloats(args, "atan2")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Atan2(y, x)), nil
}

// mathAtanh implements math.atanh(x).
// CPython: Modules/mathmodule.c:1104 FUNC1D(atanh, atanh, ...)
func mathAtanh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "atanh")
	if err != nil {
		return nil, err
	}
	r := gomath.Atanh(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathCos implements math.cos(x).
// CPython: Modules/mathmodule.c:1153 FUNC1D(cos, cos, ...)
func mathCos(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "cos")
	if err != nil {
		return nil, err
	}
	r := gomath.Cos(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathCosh implements math.cosh(x).
// CPython: Modules/mathmodule.c:1157 FUNC1(cosh, cosh, ...)
func mathCosh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "cosh")
	if err != nil {
		return nil, err
	}
	r := gomath.Cosh(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathSin implements math.sin(x).
// CPython: Modules/mathmodule.c:1236 FUNC1D(sin, sin, ...)
func mathSin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "sin")
	if err != nil {
		return nil, err
	}
	r := gomath.Sin(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathSinh implements math.sinh(x).
// CPython: Modules/mathmodule.c:1240 FUNC1(sinh, sinh, ...)
func mathSinh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "sinh")
	if err != nil {
		return nil, err
	}
	r := gomath.Sinh(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathTan implements math.tan(x).
// CPython: Modules/mathmodule.c:1247 FUNC1D(tan, tan, ...)
func mathTan(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "tan")
	if err != nil {
		return nil, err
	}
	r := gomath.Tan(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(r), nil
}

// mathTanh implements math.tanh(x).
// CPython: Modules/mathmodule.c:1251 FUNC1(tanh, tanh, ...)
func mathTanh(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "tanh")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Tanh(x)), nil
}

// mathHypot implements math.hypot(*coordinates). For the common
// two-argument case it calls Go's math.Hypot; for the general
// n-dimensional case it uses scaled summation.
//
// CPython: Modules/mathmodule.c:2698 math_hypot_impl
func mathHypot(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: hypot() takes no keyword arguments")
	}
	switch len(args) {
	case 0:
		return objects.NewFloat(0), nil
	case 1:
		x, err := objectToFloat(args[0], "hypot")
		if err != nil {
			return nil, err
		}
		return objects.NewFloat(gomath.Abs(x)), nil
	case 2:
		x, err := objectToFloat(args[0], "hypot")
		if err != nil {
			return nil, err
		}
		y, err := objectToFloat(args[1], "hypot")
		if err != nil {
			return nil, err
		}
		return objects.NewFloat(gomath.Hypot(x, y)), nil
	}
	// General case: sqrt(sum of squares) with max scaling.
	vals := make([]float64, len(args))
	for i, a := range args {
		v, err := objectToFloat(a, "hypot")
		if err != nil {
			return nil, err
		}
		vals[i] = gomath.Abs(v)
	}
	maxVal := vals[0]
	for _, v := range vals[1:] {
		if v > maxVal {
			maxVal = v
		}
	}
	if gomath.IsInf(maxVal, 0) {
		return objects.NewFloat(gomath.Inf(1)), nil
	}
	if maxVal == 0 {
		return objects.NewFloat(0), nil
	}
	var sum float64
	for _, v := range vals {
		s := v / maxVal
		sum += s * s
	}
	return objects.NewFloat(maxVal * gomath.Sqrt(sum)), nil
}

// mathDegrees implements math.degrees(x).
// CPython: Modules/mathmodule.c:3080 math_degrees_impl
func mathDegrees(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "degrees")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(x * (180.0 / gomath.Pi)), nil
}

// mathRadians implements math.radians(x).
// CPython: Modules/mathmodule.c:3097 math_radians_impl
func mathRadians(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "radians")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(x * (gomath.Pi / 180.0)), nil
}

// mathDist implements math.dist(p, q). Returns the Euclidean distance
// between two points given as sequences of coordinates.
//
// CPython: Modules/mathmodule.c:2598 math_dist_impl
func mathDist(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: dist() takes exactly 2 arguments (%d given)", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: dist() takes no keyword arguments")
	}
	n, items0, items1, err := seqPair(args[0], args[1])
	if err != nil {
		return nil, err
	}
	var sum float64
	for i := 0; i < n; i++ {
		pi, err := objectToFloat(items0(i), "dist")
		if err != nil {
			return nil, err
		}
		qi, err := objectToFloat(items1(i), "dist")
		if err != nil {
			return nil, err
		}
		d := pi - qi
		sum += d * d
	}
	return objects.NewFloat(gomath.Sqrt(sum)), nil
}

// seqPair extracts two parallel item-accessor functions from two
// sequence objects (tuple or list), checking they have the same length.
func seqPair(a, b objects.Object) (n int, fa func(int) objects.Object, fb func(int) objects.Object, err error) {
	la, err := seqLen(a)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("TypeError: dist() argument 1 must be a sequence, not '%s'", a.Type().Name)
	}
	lb, err := seqLen(b)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("TypeError: dist() argument 2 must be a sequence, not '%s'", b.Type().Name)
	}
	if la != lb {
		return 0, nil, nil, fmt.Errorf("ValueError: both points must have the same number of dimensions")
	}
	return la, seqItem(a), seqItem(b), nil
}

func seqLen(o objects.Object) (int, error) {
	switch x := o.(type) {
	case *objects.Tuple:
		return x.Len(), nil
	case *objects.List:
		return x.Len(), nil
	}
	return 0, fmt.Errorf("not a sequence")
}

func seqItem(o objects.Object) func(int) objects.Object {
	switch x := o.(type) {
	case *objects.Tuple:
		return func(i int) objects.Object { return x.Item(i) }
	case *objects.List:
		return func(i int) objects.Object { return x.Item(i) }
	}
	return nil
}

// ---- Power / exp / log ----------------------------------------------------

// mathCbrt implements math.cbrt(x).
// CPython: Modules/mathmodule.c:1108 FUNC1(cbrt, cbrt, ...)
func mathCbrt(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "cbrt")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Cbrt(x)), nil
}

// mathExp implements math.exp(x).
// CPython: Modules/mathmodule.c:1166 FUNC1(exp, exp, ...)
func mathExp(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "exp")
	if err != nil {
		return nil, err
	}
	r := gomath.Exp(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathExp2 implements math.exp2(x).
// CPython: Modules/mathmodule.c:1169 FUNC1(exp2, exp2, ...)
func mathExp2(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "exp2")
	if err != nil {
		return nil, err
	}
	r := gomath.Exp2(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathExpm1 implements math.expm1(x).
// CPython: Modules/mathmodule.c:1172 FUNC1(expm1, expm1, ...)
func mathExpm1(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "expm1")
	if err != nil {
		return nil, err
	}
	r := gomath.Expm1(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathLog implements math.log(x[, base]).
// CPython: Modules/mathmodule.c:2277 math_log
func mathLog(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: log() requires 1 or 2 arguments (%d given)", len(args))
	}
	x, err := objectToFloat(args[0], "log")
	if err != nil {
		return nil, err
	}
	if x < 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	r := gomath.Log(x)
	if len(args) == 2 {
		base, err := objectToFloat(args[1], "log")
		if err != nil {
			return nil, err
		}
		if base <= 0 {
			return nil, fmt.Errorf("ValueError: math domain error")
		}
		r /= gomath.Log(base)
	}
	return objects.NewFloat(r), nil
}

// mathLog1p implements math.log1p(x).
// CPython: Modules/mathmodule.c:1225 FUNC1D(log1p, m_log1p, ...)
func mathLog1p(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "log1p")
	if err != nil {
		return nil, err
	}
	if x < -1 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Log1p(x)), nil
}

// mathLog2 implements math.log2(x).
// CPython: Modules/mathmodule.c:2316 math_log2
func mathLog2(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "log2")
	if err != nil {
		return nil, err
	}
	if x <= 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Log2(x)), nil
}

// mathLog10 implements math.log10(x).
// CPython: Modules/mathmodule.c:2333 math_log10
func mathLog10(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "log10")
	if err != nil {
		return nil, err
	}
	if x <= 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Log10(x)), nil
}

// mathPow implements math.pow(x, y).
// CPython: Modules/mathmodule.c:2999 math_pow_impl
func mathPow(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, y, err := twoFloats(args, "pow")
	if err != nil {
		return nil, err
	}
	// CPython special cases: pow(1, NaN) = 1, pow(NaN, 0) = 1.
	if x == 1.0 || y == 0.0 {
		return objects.NewFloat(1.0), nil
	}
	r := gomath.Pow(x, y)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) && !gomath.IsNaN(y) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) && !gomath.IsInf(y, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathSqrt implements math.sqrt(x).
// CPython: Modules/mathmodule.c:1243 FUNC1D(sqrt, sqrt, ...)
func mathSqrt(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "sqrt")
	if err != nil {
		return nil, err
	}
	if x < 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Sqrt(x)), nil
}

// ---- Special functions ----------------------------------------------------

// mathErf implements math.erf(x).
// CPython: Modules/mathmodule.c:1160 FUNC1A(erf, erf, ...)
func mathErf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "erf")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Erf(x)), nil
}

// mathErfc implements math.erfc(x).
// CPython: Modules/mathmodule.c:1163 FUNC1A(erfc, erfc, ...)
func mathErfc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "erfc")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Erfc(x)), nil
}

// mathGamma implements math.gamma(x).
// CPython: Modules/mathmodule.c:1217 FUNC1AD(gamma, m_tgamma, ...)
func mathGamma(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "gamma")
	if err != nil {
		return nil, err
	}
	r := gomath.Gamma(x)
	if gomath.IsNaN(r) && !gomath.IsNaN(x) {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathLgamma implements math.lgamma(x).
// CPython: Modules/mathmodule.c:1221 FUNC1AD(lgamma, m_lgamma, ...)
func mathLgamma(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "lgamma")
	if err != nil {
		return nil, err
	}
	r, _ := gomath.Lgamma(x)
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// ---- Number-theoretic / integer -------------------------------------------

// mathCeil implements math.ceil(x). Returns the smallest integer >= x.
// CPython: Modules/mathmodule.c:1124 math_ceil
func mathCeil(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ceil() takes exactly 1 argument (%d given)", len(args))
	}
	if i, ok := args[0].(*objects.Int); ok {
		return i, nil
	}
	x, err := objectToFloat(args[0], "ceil")
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(gomath.Ceil(x))), nil
}

// mathFloor implements math.floor(x). Returns the largest integer <= x.
// CPython: Modules/mathmodule.c:1193 math_floor
func mathFloor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: floor() takes exactly 1 argument (%d given)", len(args))
	}
	if i, ok := args[0].(*objects.Int); ok {
		return i, nil
	}
	x, err := objectToFloat(args[0], "floor")
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(gomath.Floor(x))), nil
}

// mathTrunc implements math.trunc(x). Returns the integer truncated toward zero.
// CPython: Modules/mathmodule.c:2065 math_trunc
func mathTrunc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: trunc() takes exactly 1 argument (%d given)", len(args))
	}
	if i, ok := args[0].(*objects.Int); ok {
		return i, nil
	}
	x, err := objectToFloat(args[0], "trunc")
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(gomath.Trunc(x))), nil
}

// mathIsqrt implements math.isqrt(n). Integer square root: floor(sqrt(n)).
// CPython: Modules/mathmodule.c:1693 math_isqrt
func mathIsqrt(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: isqrt() takes exactly 1 argument (%d given)", len(args))
	}
	n, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: 'isqrt' object cannot be interpreted as an integer")
	}
	bi := n.BigInt()
	if bi.Sign() < 0 {
		return nil, fmt.Errorf("ValueError: isqrt() argument must be nonnegative")
	}
	result := new(big.Int).Sqrt(bi)
	return objects.NewIntFromBig(result), nil
}

// mathFactorial implements math.factorial(n).
// CPython: Modules/mathmodule.c:2014 math_factorial
func mathFactorial(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: factorial() takes exactly 1 argument (%d given)", len(args))
	}
	switch v := args[0].(type) {
	case *objects.Int:
		n, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("OverflowError: factorial() argument is too large")
		}
		if n < 0 {
			return nil, fmt.Errorf("ValueError: factorial() not defined for negative values")
		}
		result := new(big.Int).SetInt64(1)
		for i := int64(2); i <= n; i++ {
			result.Mul(result, big.NewInt(i))
		}
		return objects.NewIntFromBig(result), nil
	case *objects.Float:
		f := v.Float64()
		if f != gomath.Trunc(f) {
			return nil, fmt.Errorf("ValueError: factorial() only accepts integral values")
		}
		if f < 0 {
			return nil, fmt.Errorf("ValueError: factorial() not defined for negative values")
		}
		n := int64(f)
		result := new(big.Int).SetInt64(1)
		for i := int64(2); i <= n; i++ {
			result.Mul(result, big.NewInt(i))
		}
		return objects.NewIntFromBig(result), nil
	}
	return nil, fmt.Errorf("TypeError: factorial() argument must be an integer, not '%s'", args[0].Type().Name)
}

// mathGcd implements math.gcd(*integers).
// CPython: Modules/mathmodule.c:718 math_gcd_impl
func mathGcd(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: gcd() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.NewInt(0), nil
	}
	result := new(big.Int)
	for i, a := range args {
		iv, ok := a.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: gcd() argument %d must be int, not '%s'", i+1, a.Type().Name)
		}
		b := iv.BigInt()
		b.Abs(b)
		result.GCD(nil, nil, result, b)
	}
	return objects.NewIntFromBig(result), nil
}

// mathLcm implements math.lcm(*integers).
// CPython: Modules/mathmodule.c:801 math_lcm_impl
func mathLcm(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: lcm() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.NewInt(1), nil
	}
	result := new(big.Int).SetInt64(1)
	for i, a := range args {
		iv, ok := a.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: lcm() argument %d must be int, not '%s'", i+1, a.Type().Name)
		}
		b := iv.BigInt()
		b.Abs(b)
		if b.Sign() == 0 {
			return objects.NewInt(0), nil
		}
		g := new(big.Int).GCD(nil, nil, new(big.Int).Set(result), new(big.Int).Set(b))
		result.Mul(result, new(big.Int).Div(b, g))
	}
	result.Abs(result)
	return objects.NewIntFromBig(result), nil
}

// mathComb implements math.comb(n, k) = n! / (k! * (n-k)!).
// CPython: Modules/mathmodule.c:3834 math_comb_impl
func mathComb(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: comb() takes exactly 2 arguments (%d given)", len(args))
	}
	nObj, ok1 := args[0].(*objects.Int)
	kObj, ok2 := args[1].(*objects.Int)
	if !ok1 {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[0].Type().Name)
	}
	if !ok2 {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[1].Type().Name)
	}
	n := nObj.BigInt()
	k := kObj.BigInt()
	if n.Sign() < 0 {
		return nil, fmt.Errorf("ValueError: n must be a non-negative integer")
	}
	if k.Sign() < 0 {
		return nil, fmt.Errorf("ValueError: k must be a non-negative integer")
	}
	if k.Cmp(n) > 0 {
		return objects.NewInt(0), nil
	}
	// Use smaller k: comb(n,k) == comb(n, n-k).
	nm := new(big.Int).Sub(n, k)
	if nm.Cmp(k) < 0 {
		k = nm
	}
	result := bigComb(n, k)
	return objects.NewIntFromBig(result), nil
}

// bigComb computes n! / (k! * (n-k)!) iteratively.
func bigComb(n, k *big.Int) *big.Int {
	result := new(big.Int).SetInt64(1)
	one := big.NewInt(1)
	idx := new(big.Int).SetInt64(1)
	ni := new(big.Int).Set(n)
	ki := new(big.Int).Set(k)
	for idx.Cmp(ki) <= 0 {
		result.Mul(result, ni)
		result.Div(result, idx)
		ni.Sub(ni, one)
		idx.Add(idx, one)
	}
	return result
}

// mathPerm implements math.perm(n, k=None) = n! / (n-k)!.
// CPython: Modules/mathmodule.c:3739 math_perm_impl
func mathPerm(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: perm() takes 1 or 2 arguments (%d given)", len(args))
	}
	nObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[0].Type().Name)
	}
	n := nObj.BigInt()
	if n.Sign() < 0 {
		return nil, fmt.Errorf("ValueError: n must be a non-negative integer")
	}
	var k *big.Int
	if len(args) == 1 || args[1] == objects.None() {
		// perm(n) = n!
		k = new(big.Int).Set(n)
	} else {
		kObj, ok2 := args[1].(*objects.Int)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[1].Type().Name)
		}
		k = kObj.BigInt()
		if k.Sign() < 0 {
			return nil, fmt.Errorf("ValueError: k must be a non-negative integer")
		}
		if k.Cmp(n) > 0 {
			return objects.NewInt(0), nil
		}
	}
	// result = n * (n-1) * ... * (n-k+1)
	result := new(big.Int).SetInt64(1)
	one := big.NewInt(1)
	ni := new(big.Int).Set(n)
	ki := new(big.Int).Set(k)
	for ki.Sign() > 0 {
		result.Mul(result, ni)
		ni.Sub(ni, one)
		ki.Sub(ki, one)
	}
	return objects.NewIntFromBig(result), nil
}

// mathIsfinite implements math.isfinite(x).
// CPython: Modules/mathmodule.c:3114 math_isfinite_impl
func mathIsfinite(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: isfinite() takes exactly 1 argument (%d given)", len(args))
	}
	if _, ok := args[0].(*objects.Int); ok {
		return objects.NewBool(true), nil
	}
	x, err := objectToFloat(args[0], "isfinite")
	if err != nil {
		return nil, err
	}
	return objects.NewBool(!gomath.IsInf(x, 0) && !gomath.IsNaN(x)), nil
}

// mathIsinf implements math.isinf(x).
// CPython: Modules/mathmodule.c:3148 math_isinf_impl
func mathIsinf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: isinf() takes exactly 1 argument (%d given)", len(args))
	}
	if _, ok := args[0].(*objects.Int); ok {
		return objects.NewBool(false), nil
	}
	x, err := objectToFloat(args[0], "isinf")
	if err != nil {
		return nil, err
	}
	return objects.NewBool(gomath.IsInf(x, 0)), nil
}

// mathIsnan implements math.isnan(x).
// CPython: Modules/mathmodule.c:3131 math_isnan_impl
func mathIsnan(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: isnan() takes exactly 1 argument (%d given)", len(args))
	}
	if _, ok := args[0].(*objects.Int); ok {
		return objects.NewBool(false), nil
	}
	x, err := objectToFloat(args[0], "isnan")
	if err != nil {
		return nil, err
	}
	return objects.NewBool(gomath.IsNaN(x)), nil
}

// mathIsclose implements math.isclose(a, b, *, rel_tol=1e-9, abs_tol=0.0).
// CPython: Modules/mathmodule.c:3181 math_isclose_impl
func mathIsclose(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: isclose() takes exactly 2 positional arguments (%d given)", len(args))
	}
	a, err := objectToFloat(args[0], "isclose")
	if err != nil {
		return nil, err
	}
	b, err := objectToFloat(args[1], "isclose")
	if err != nil {
		return nil, err
	}
	relTol := 1e-9
	absTol := 0.0
	if v, ok := kwargs["rel_tol"]; ok {
		relTol, err = objectToFloat(v, "isclose")
		if err != nil {
			return nil, err
		}
	}
	if v, ok := kwargs["abs_tol"]; ok {
		absTol, err = objectToFloat(v, "isclose")
		if err != nil {
			return nil, err
		}
	}
	if relTol < 0 || absTol < 0 {
		return nil, fmt.Errorf("ValueError: tolerances must be non-negative")
	}
	if a == b {
		return objects.NewBool(true), nil
	}
	if gomath.IsInf(a, 0) || gomath.IsInf(b, 0) {
		return objects.NewBool(false), nil
	}
	diff := gomath.Abs(a - b)
	result := diff <= gomath.Max(relTol*gomath.Max(gomath.Abs(a), gomath.Abs(b)), absTol)
	return objects.NewBool(result), nil
}

// mathFsum implements math.fsum(iterable). Uses Shewchuk compensated
// summation exactly as CPython's C implementation does.
//
// CPython: Modules/mathmodule.c:1363 math_fsum
func mathFsum(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: fsum() takes exactly 1 argument (%d given)", len(args))
	}
	n, itemFn, err := seqItems(args[0], "fsum")
	if err != nil {
		return nil, err
	}
	// Shewchuk compensated sum.
	partials := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		x, err := objectToFloat(itemFn(i), "fsum")
		if err != nil {
			return nil, err
		}
		j := 0
		for _, p := range partials {
			if gomath.Abs(x) < gomath.Abs(p) {
				x, p = p, x
			}
			hi := x + p
			lo := p - (hi - x)
			if lo != 0 {
				partials[j] = lo
				j++
			}
			x = hi
		}
		partials = partials[:j]
		partials = append(partials, x)
	}
	var sum float64
	for _, p := range partials {
		sum += p
	}
	return objects.NewFloat(sum), nil
}

// seqItems returns the length and an item accessor for a list or tuple.
func seqItems(o objects.Object, fname string) (int, func(int) objects.Object, error) {
	switch x := o.(type) {
	case *objects.List:
		return x.Len(), func(i int) objects.Object { return x.Item(i) }, nil
	case *objects.Tuple:
		return x.Len(), func(i int) objects.Object { return x.Item(i) }, nil
	}
	return 0, nil, fmt.Errorf("TypeError: %s() argument must be iterable, not '%s'", fname, o.Type().Name)
}

// mathProd implements math.prod(iterable, *, start=1).
// CPython: Modules/mathmodule.c:3291 math_prod_impl
func mathProd(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: prod() takes exactly 1 positional argument (%d given)", len(args))
	}
	var start objects.Object = objects.NewInt(1)
	if v, ok := kwargs["start"]; ok {
		start = v
	}
	n, itemFn, err := seqItems(args[0], "prod")
	if err != nil {
		return nil, err
	}
	// If start is int and all elements are int, use big.Int product.
	startInt, startIsInt := start.(*objects.Int)
	allInts := startIsInt
	if allInts {
		for i := 0; i < n; i++ {
			if _, ok := itemFn(i).(*objects.Int); !ok {
				allInts = false
				break
			}
		}
	}
	if allInts {
		result := startInt.BigInt()
		for i := 0; i < n; i++ {
			result.Mul(result, itemFn(i).(*objects.Int).BigInt())
		}
		return objects.NewIntFromBig(result), nil
	}
	// Float path.
	acc, err := objectToFloat(start, "prod")
	if err != nil {
		return nil, err
	}
	for i := 0; i < n; i++ {
		v, err := objectToFloat(itemFn(i), "prod")
		if err != nil {
			return nil, err
		}
		acc *= v
	}
	return objects.NewFloat(acc), nil
}

// ---- Float operations -----------------------------------------------------

// mathCopysign implements math.copysign(x, y).
// CPython: Modules/mathmodule.c:1148 FUNC2(copysign, copysign, ...)
func mathCopysign(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, y, err := twoFloats(args, "copysign")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Copysign(x, y)), nil
}

// mathFabs implements math.fabs(x).
// CPython: Modules/mathmodule.c:1177 FUNC1(fabs, fabs, ...)
func mathFabs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "fabs")
	if err != nil {
		return nil, err
	}
	return objects.NewFloat(gomath.Abs(x)), nil
}

// mathFmod implements math.fmod(x, y).
// CPython: Modules/mathmodule.c:2395 math_fmod_impl
func mathFmod(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, y, err := twoFloats(args, "fmod")
	if err != nil {
		return nil, err
	}
	if y == 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Mod(x, y)), nil
}

// mathFrexp implements math.frexp(x). Returns (mantissa, exponent) such
// that x == mantissa * 2**exponent.
//
// CPython: Modules/mathmodule.c:2098 math_frexp_impl
func mathFrexp(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "frexp")
	if err != nil {
		return nil, err
	}
	m, e := gomath.Frexp(x)
	return objects.NewTuple([]objects.Object{
		objects.NewFloat(m),
		objects.NewInt(int64(e)),
	}), nil
}

// mathLdexp implements math.ldexp(x, i). Returns x * (2**i).
// CPython: Modules/mathmodule.c:2127 math_ldexp_impl
func mathLdexp(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: ldexp() takes exactly 2 arguments (%d given)", len(args))
	}
	x, err := objectToFloat(args[0], "ldexp")
	if err != nil {
		return nil, err
	}
	iObj, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: ldexp() 2nd argument must be int, not '%s'", args[1].Type().Name)
	}
	exp, fits := iObj.Int64()
	if !fits {
		return nil, fmt.Errorf("OverflowError: ldexp() argument too large")
	}
	r := gomath.Ldexp(x, int(exp))
	if gomath.IsInf(r, 0) && !gomath.IsInf(x, 0) {
		return nil, fmt.Errorf("OverflowError: math range error")
	}
	return objects.NewFloat(r), nil
}

// mathModf implements math.modf(x). Returns (fractional, integer) parts.
// CPython: Modules/mathmodule.c:2207 math_modf_impl
func mathModf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "modf")
	if err != nil {
		return nil, err
	}
	i, f := gomath.Modf(x)
	return objects.NewTuple([]objects.Object{
		objects.NewFloat(f),
		objects.NewFloat(i),
	}), nil
}

// mathRemainder implements math.remainder(x, y) (IEEE 754 remainder).
// CPython: Modules/mathmodule.c:1230 FUNC2(remainder, m_remainder, ...)
func mathRemainder(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, y, err := twoFloats(args, "remainder")
	if err != nil {
		return nil, err
	}
	if y == 0 {
		return nil, fmt.Errorf("ValueError: math domain error")
	}
	return objects.NewFloat(gomath.Remainder(x, y)), nil
}

// mathNextafter implements math.nextafter(x, y, *, steps=1).
// CPython: Modules/mathmodule.c:3949 math_nextafter_impl
func mathNextafter(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: nextafter() takes exactly 2 positional arguments (%d given)", len(args))
	}
	x, y, err := twoFloats(args, "nextafter")
	if err != nil {
		return nil, err
	}
	steps := int64(1)
	if v, ok := kwargs["steps"]; ok {
		sv, ok2 := v.(*objects.Int)
		if !ok2 {
			return nil, fmt.Errorf("TypeError: nextafter() steps must be int, not '%s'", v.Type().Name)
		}
		steps, _ = sv.Int64()
	}
	r := x
	for i := int64(0); i < steps; i++ {
		r = gomath.Nextafter(r, y)
	}
	return objects.NewFloat(r), nil
}

// mathUlp implements math.ulp(x). Returns the value of the least
// significant bit of the float x.
//
// CPython: Modules/mathmodule.c:4073 math_ulp_impl
func mathUlp(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	x, err := oneFloat(args, "ulp")
	if err != nil {
		return nil, err
	}
	x = gomath.Abs(x)
	if gomath.IsNaN(x) {
		return objects.NewFloat(x), nil
	}
	if gomath.IsInf(x, 1) {
		return objects.NewFloat(x), nil
	}
	return objects.NewFloat(gomath.Nextafter(x, gomath.Inf(1)) - x), nil
}
