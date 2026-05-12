package math

import (
	gomath "math"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// callF calls a math function directly with float64 args and returns float64.
func callF(t *testing.T, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error), name string, vals ...float64) float64 {
	t.Helper()
	args := make([]objects.Object, len(vals))
	for i, v := range vals {
		args[i] = objects.NewFloat(v)
	}
	res, err := fn(args, nil)
	if err != nil {
		t.Fatalf("%s(%v): %v", name, vals, err)
	}
	f, ok := res.(*objects.Float)
	if !ok {
		t.Fatalf("%s: returned %T, want *objects.Float", name, res)
	}
	return f.Float64()
}

// getConst extracts a float constant from the module dict.
func getConst(t *testing.T, mod *objects.Module, name string) float64 {
	t.Helper()
	v, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("constant %s missing: %v", name, err)
	}
	f, ok := v.(*objects.Float)
	if !ok {
		t.Fatalf("constant %s type %T, want *objects.Float", name, v)
	}
	return f.Float64()
}

func TestInittabRegistration(t *testing.T) {
	if imp.FindInitFunc("math") == nil {
		t.Fatal("math not registered in inittab")
	}
}

func TestBuildModule(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	if mod == nil {
		t.Fatal("buildModule returned nil")
	}
}

func TestConstants(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	pi := getConst(t, mod, "pi")
	if gomath.Abs(pi-gomath.Pi) > 1e-15 {
		t.Errorf("pi = %v, want %v", pi, gomath.Pi)
	}
	e := getConst(t, mod, "e")
	if gomath.Abs(e-gomath.E) > 1e-15 {
		t.Errorf("e = %v, want %v", e, gomath.E)
	}
	tau := getConst(t, mod, "tau")
	if gomath.Abs(tau-2*gomath.Pi) > 1e-15 {
		t.Errorf("tau = %v, want %v", tau, 2*gomath.Pi)
	}
	inf := getConst(t, mod, "inf")
	if !gomath.IsInf(inf, 1) {
		t.Errorf("inf = %v, want +Inf", inf)
	}
	nan := getConst(t, mod, "nan")
	if !gomath.IsNaN(nan) {
		t.Errorf("nan = %v, want NaN", nan)
	}
}

func TestSinCos(t *testing.T) {
	for _, tc := range []struct {
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
		name string
		x    float64
		want float64
	}{
		{mathSin, "sin", 0, 0},
		{mathSin, "sin", gomath.Pi / 2, 1},
		{mathCos, "cos", 0, 1},
		{mathCos, "cos", gomath.Pi, -1},
	} {
		got := callF(t, tc.fn, tc.name, tc.x)
		if gomath.Abs(got-tc.want) > 1e-10 {
			t.Errorf("%s(%v) = %v, want %v", tc.name, tc.x, got, tc.want)
		}
	}
}

func TestSqrt(t *testing.T) {
	got := callF(t, mathSqrt, "sqrt", 9)
	if gomath.Abs(got-3) > 1e-12 {
		t.Errorf("sqrt(9) = %v, want 3", got)
	}
	got = callF(t, mathSqrt, "sqrt", 2)
	if gomath.Abs(got-gomath.Sqrt2) > 1e-12 {
		t.Errorf("sqrt(2) = %v, want %v", got, gomath.Sqrt2)
	}
	// sqrt of negative should error.
	_, err := mathSqrt([]objects.Object{objects.NewFloat(-1)}, nil)
	if err == nil {
		t.Error("sqrt(-1): expected error, got nil")
	}
}

func TestLog(t *testing.T) {
	got := callF(t, mathLog, "log", gomath.E)
	if gomath.Abs(got-1) > 1e-12 {
		t.Errorf("log(e) = %v, want 1", got)
	}
	got = callF(t, mathLog10, "log10", 100)
	if gomath.Abs(got-2) > 1e-12 {
		t.Errorf("log10(100) = %v, want 2", got)
	}
	got = callF(t, mathLog2, "log2", 8)
	if gomath.Abs(got-3) > 1e-12 {
		t.Errorf("log2(8) = %v, want 3", got)
	}
}

func TestExp(t *testing.T) {
	got := callF(t, mathExp, "exp", 1)
	if gomath.Abs(got-gomath.E) > 1e-12 {
		t.Errorf("exp(1) = %v, want e", got)
	}
	got = callF(t, mathExp, "exp", 0)
	if got != 1 {
		t.Errorf("exp(0) = %v, want 1", got)
	}
}

func TestFactorial(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want int64
	}{
		{0, 1},
		{1, 1},
		{5, 120},
		{10, 3628800},
	} {
		res, err := mathFactorial([]objects.Object{objects.NewInt(tc.n)}, nil)
		if err != nil {
			t.Fatalf("factorial(%d): %v", tc.n, err)
		}
		v, ok := res.(*objects.Int)
		if !ok {
			t.Fatalf("factorial(%d): got %T", tc.n, res)
		}
		got, _ := v.Int64()
		if got != tc.want {
			t.Errorf("factorial(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
	// negative factorial should error.
	_, err := mathFactorial([]objects.Object{objects.NewInt(-1)}, nil)
	if err == nil {
		t.Error("factorial(-1): expected error, got nil")
	}
}

func TestGcd(t *testing.T) {
	for _, tc := range []struct {
		a, b int64
		want int64
	}{
		{12, 8, 4},
		{7, 3, 1},
		{0, 5, 5},
		{6, 0, 6},
	} {
		res, err := mathGcd([]objects.Object{objects.NewInt(tc.a), objects.NewInt(tc.b)}, nil)
		if err != nil {
			t.Fatalf("gcd(%d,%d): %v", tc.a, tc.b, err)
		}
		v, ok := res.(*objects.Int)
		if !ok {
			t.Fatalf("gcd(%d,%d): got %T", tc.a, tc.b, res)
		}
		got, _ := v.Int64()
		if got != tc.want {
			t.Errorf("gcd(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIsnanIsinf(t *testing.T) {
	nanArg := []objects.Object{objects.NewFloat(gomath.NaN())}
	infArg := []objects.Object{objects.NewFloat(gomath.Inf(1))}
	oneArg := []objects.Object{objects.NewFloat(1.0)}

	check := func(fn func([]objects.Object, map[string]objects.Object) (objects.Object, error), args []objects.Object, want bool, label string) {
		t.Helper()
		res, err := fn(args, nil)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		got := res == objects.NewBool(true)
		if got != want {
			t.Errorf("%s = %v, want %v", label, got, want)
		}
	}

	check(mathIsnan, nanArg, true, "isnan(nan)")
	check(mathIsnan, oneArg, false, "isnan(1.0)")
	check(mathIsinf, infArg, true, "isinf(inf)")
	check(mathIsinf, oneArg, false, "isinf(1.0)")
	check(mathIsinf, nanArg, false, "isinf(nan)")
}

func TestFloorCeil(t *testing.T) {
	for _, tc := range []struct {
		x         float64
		wantFloor int64
		wantCeil  int64
	}{
		{2.3, 2, 3},
		{-2.3, -3, -2},
		{3.0, 3, 3},
	} {
		rf, err := mathFloor([]objects.Object{objects.NewFloat(tc.x)}, nil)
		if err != nil {
			t.Fatalf("floor(%v): %v", tc.x, err)
		}
		rc, err := mathCeil([]objects.Object{objects.NewFloat(tc.x)}, nil)
		if err != nil {
			t.Fatalf("ceil(%v): %v", tc.x, err)
		}
		floorV, _ := rf.(*objects.Int).Int64()
		ceilV, _ := rc.(*objects.Int).Int64()
		if floorV != tc.wantFloor {
			t.Errorf("floor(%v) = %d, want %d", tc.x, floorV, tc.wantFloor)
		}
		if ceilV != tc.wantCeil {
			t.Errorf("ceil(%v) = %d, want %d", tc.x, ceilV, tc.wantCeil)
		}
	}
}
