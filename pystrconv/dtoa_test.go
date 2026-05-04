package pystrconv_test

import (
	"math"
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

func TestFormatFloatRepr(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0, "0.0"},
		{1.0, "1.0"},
		{-1.0, "-1.0"},
		{1.5, "1.5"},
		{-1.5, "-1.5"},
		{0.1, "0.1"},
		{0.2, "0.2"},
		{1e-5, "1e-05"},
		{1e-4, "0.0001"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{1e20, "1e+20"},
		{1e100, "1e+100"},
		{3.141592653589793, "3.141592653589793"},
		{2.718281828459045, "2.718281828459045"},
		{1234567.89, "1234567.89"},
		{1e-300, "1e-300"},
		{1e308, "1e+308"},
		{1.7976931348623157e308, "1.7976931348623157e+308"},
		{math.Copysign(0, -1), "-0.0"},
	}
	for _, c := range cases {
		got := pystrconv.FormatFloat(c.in, 'r', 0, pystrconv.FlagAddDotZero)
		if got != c.want {
			t.Errorf("FormatFloat(%v,'r') = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatFloatSpecials(t *testing.T) {
	if got := pystrconv.FormatFloat(math.NaN(), 'r', 0, pystrconv.FlagAddDotZero); got != "nan" {
		t.Errorf("nan -> %q", got)
	}
	pos := pystrconv.FormatFloat(math.Inf(1), 'r', 0, pystrconv.FlagAddDotZero)
	if pos != "inf" {
		t.Errorf("+inf -> %q", pos)
	}
	neg := pystrconv.FormatFloat(math.Inf(-1), 'r', 0, pystrconv.FlagAddDotZero)
	if neg != "-inf" {
		t.Errorf("-inf -> %q", neg)
	}
}

func TestFormatFloatExp(t *testing.T) {
	if got := pystrconv.FormatFloat(1234.5, 'e', 2, 0); got != "1.23e+03" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(0.0, 'e', 2, 0); got != "0.00e+00" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(1.0, 'e', 6, 0); got != "1.000000e+00" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(0.1, 'e', 2, 0); got != "1.00e-01" {
		t.Errorf("got %q", got)
	}
}

func TestFormatFloatGeneral(t *testing.T) {
	if got := pystrconv.FormatFloat(1.0, 'g', 6, 0); got != "1" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(12345.6789, 'g', 6, 0); got != "12345.7" {
		t.Errorf("got %q", got)
	}
}

func TestFormatFloatFixed(t *testing.T) {
	if got := pystrconv.FormatFloat(3.14159, 'f', 2, 0); got != "3.14" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(1234.5, 'f', 0, 0); got != "1234" {
		t.Errorf("got %q", got)
	}
	if got := pystrconv.FormatFloat(1.0, 'f', 6, 0); got != "1.000000" {
		t.Errorf("got %q", got)
	}
	// Banker's rounding: 1.5 and 2.5 both round to 2 with %.0f.
	if got := pystrconv.FormatFloat(1.5, 'f', 0, 0); got != "2" {
		t.Errorf("1.5 'f' 0 -> %q", got)
	}
	if got := pystrconv.FormatFloat(2.5, 'f', 0, 0); got != "2" {
		t.Errorf("2.5 'f' 0 -> %q", got)
	}
}
