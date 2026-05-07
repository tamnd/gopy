package objects

import (
	"math"
	"testing"
)

func TestFloatFromString(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"0.0", 0},
		{"-0.0", math.Copysign(0, -1)},
		{"1.5", 1.5},
		{"  3.14  ", 3.14},
		{"+1e10", 1e10},
		{"-1.5e-3", -1.5e-3},
		{"1_000", 1000},
		{"1_000.000_1", 1000.0001},
		{"1_2_3.4_5", 123.45},
		{"inf", math.Inf(1)},
		{"-inf", math.Inf(-1)},
		{"Infinity", math.Inf(1)},
		{"NaN", math.NaN()},
	}
	for _, c := range cases {
		got, err := FloatFromString(c.in)
		if err != nil {
			t.Fatalf("FloatFromString(%q) error: %v", c.in, err)
		}
		if math.IsNaN(c.want) {
			if !math.IsNaN(got.v) {
				t.Errorf("FloatFromString(%q) = %v, want NaN", c.in, got.v)
			}
			continue
		}
		if got.v != c.want {
			t.Errorf("FloatFromString(%q) = %v, want %v", c.in, got.v, c.want)
		}
	}
}

func TestFloatFromStringRejects(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"+",
		"-",
		"abc",
		"_1",
		"1_",
		"1__2",
		"1._2",
		"1_.2",
		"1.5e_3",
		"1.5_e3",
		"_1.5",
		"1.5_",
	}
	for _, s := range bad {
		if v, err := FloatFromString(s); err == nil {
			t.Errorf("FloatFromString(%q) = %v, want error", s, v.v)
		}
	}
}
