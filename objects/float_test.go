package objects

import (
	"math"
	"testing"
)

// TestFloatRepr pins repr(float) parity with CPython 3.14. The
// expected strings come from running repr in cpython at the same
// commit gopy targets.
func TestFloatRepr(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0.0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{1.0, "1.0"},
		{-1.0, "-1.0"},
		{1.5, "1.5"},
		{0.1, "0.1"},
		{0.0001, "0.0001"},
		{0.00001, "1e-05"},
		{1.234e-5, "1.234e-05"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{1e17, "1e+17"},
		{1.5e100, "1.5e+100"},
		{-1.5e-100, "-1.5e-100"},
		{123.45, "123.45"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{math.NaN(), "nan"},
	}
	for _, c := range cases {
		got, err := floatRepr(NewFloat(c.v))
		if err != nil {
			t.Fatalf("floatRepr(%v): %v", c.v, err)
		}
		if got != c.want {
			t.Errorf("floatRepr(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}
