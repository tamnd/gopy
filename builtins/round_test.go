package builtins

import (
	"math"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestRoundIntNoNdigits(t *testing.T) {
	out, err := Round([]objects.Object{objects.NewInt(42)}, nil)
	if err != nil {
		t.Fatalf("round(42): %v", err)
	}
	got := out.(*objects.Int)
	v, _ := got.Int64()
	if v != 42 {
		t.Fatalf("round(42) = %d, want 42", v)
	}
}

func TestRoundIntNoneNdigits(t *testing.T) {
	out, err := Round([]objects.Object{objects.NewInt(42), objects.None()}, nil)
	if err != nil {
		t.Fatalf("round(42, None): %v", err)
	}
	got := out.(*objects.Int)
	v, _ := got.Int64()
	if v != 42 {
		t.Fatalf("round(42, None) = %d, want 42", v)
	}
}

func TestRoundIntPositiveNdigitsReturnsSelf(t *testing.T) {
	out, err := Round([]objects.Object{objects.NewInt(123), objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("round(123, 2): %v", err)
	}
	v, _ := out.(*objects.Int).Int64()
	if v != 123 {
		t.Fatalf("round(123, 2) = %d, want 123", v)
	}
}

func TestRoundIntNegativeNdigits(t *testing.T) {
	cases := []struct{ n, d, want int64 }{
		{1234, -2, 1200},
		{1250, -2, 1200}, // banker's rounding: 12.5 -> 12
		{1350, -2, 1400}, // 13.5 -> 14
		{-1234, -2, -1200},
		{-1250, -2, -1200},
		{15, -1, 20},
		{25, -1, 20},
		{35, -1, 40},
	}
	for _, tc := range cases {
		out, err := Round([]objects.Object{objects.NewInt(tc.n), objects.NewInt(tc.d)}, nil)
		if err != nil {
			t.Fatalf("round(%d, %d): %v", tc.n, tc.d, err)
		}
		v, _ := out.(*objects.Int).Int64()
		if v != tc.want {
			t.Fatalf("round(%d, %d) = %d, want %d", tc.n, tc.d, v, tc.want)
		}
	}
}

func TestRoundFloatNoNdigits(t *testing.T) {
	cases := []struct {
		x    float64
		want int64
	}{
		{2.5, 2}, // halfway -> even
		{3.5, 4},
		{-0.5, 0},
		{-1.5, -2},
		{1.4, 1},
		{1.6, 2},
	}
	for _, tc := range cases {
		out, err := Round([]objects.Object{objects.NewFloat(tc.x)}, nil)
		if err != nil {
			t.Fatalf("round(%v): %v", tc.x, err)
		}
		v, _ := out.(*objects.Int).Int64()
		if v != tc.want {
			t.Fatalf("round(%v) = %d, want %d", tc.x, v, tc.want)
		}
	}
}

func TestRoundFloatPositiveNdigits(t *testing.T) {
	out, err := Round([]objects.Object{objects.NewFloat(1.2345), objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	got := out.(*objects.Float).Float64()
	if math.Abs(got-1.23) > 1e-9 {
		t.Fatalf("round(1.2345, 2) = %v, want ~1.23", got)
	}
}

func TestRoundFloatNegativeNdigits(t *testing.T) {
	out, err := Round([]objects.Object{objects.NewFloat(1234.5), objects.NewInt(-2)}, nil)
	if err != nil {
		t.Fatalf("round: %v", err)
	}
	got := out.(*objects.Float).Float64()
	if math.Abs(got-1200) > 1e-9 {
		t.Fatalf("round(1234.5, -2) = %v, want 1200", got)
	}
}

func TestRoundFloatNonFinite(t *testing.T) {
	for _, x := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		out, err := Round([]objects.Object{objects.NewFloat(x), objects.NewInt(2)}, nil)
		if err != nil {
			t.Fatalf("round(%v, 2): %v", x, err)
		}
		got := out.(*objects.Float).Float64()
		if math.IsNaN(x) {
			if !math.IsNaN(got) {
				t.Fatalf("round(NaN, 2) = %v, want NaN", got)
			}
			continue
		}
		if got != x {
			t.Fatalf("round(%v, 2) = %v, want %v", x, got, x)
		}
	}
}

func TestRoundFloatNoNdigitsRejectsInf(t *testing.T) {
	if _, err := Round([]objects.Object{objects.NewFloat(math.Inf(1))}, nil); err == nil {
		t.Fatal("round(inf): want ValueError, got nil")
	}
}

func TestRoundBoolDispatchesAsInt(t *testing.T) {
	out, err := Round([]objects.Object{objects.True()}, nil)
	if err != nil {
		t.Fatalf("round(True): %v", err)
	}
	got := out.(*objects.Int)
	v, _ := got.Int64()
	if v != 1 {
		t.Fatalf("round(True) = %d, want 1", v)
	}
}

func TestRoundRejectsUnsupportedType(t *testing.T) {
	if _, err := Round([]objects.Object{objects.NewStr("x")}, nil); err == nil {
		t.Fatal("round(str): want TypeError, got nil")
	}
}

func TestRoundArgRange(t *testing.T) {
	if _, err := Round(nil, nil); err == nil {
		t.Fatal("round(): want TypeError, got nil")
	}
	too := []objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)}
	if _, err := Round(too, nil); err == nil {
		t.Fatal("round(1,2,3): want TypeError, got nil")
	}
}
