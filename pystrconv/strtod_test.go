package pystrconv_test

import (
	"errors"
	"math"
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

func TestParseFloatBasic(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"1.5", 1.5},
		{"-1.5", -1.5},
		{"1e10", 1e10},
		{"3.141592653589793", 3.141592653589793},
		{"1_000.5", 1000.5},
		{"inf", math.Inf(1)},
		{"-Infinity", math.Inf(-1)},
		{"+INF", math.Inf(1)},
	}
	for _, c := range cases {
		got, err := pystrconv.ParseFloat(c.in)
		if err != nil {
			t.Errorf("%q: unexpected err %v", c.in, err)
			continue
		}
		if math.Float64bits(got) != math.Float64bits(c.want) {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestParseFloatNaN(t *testing.T) {
	got, err := pystrconv.ParseFloat("nan")
	if err != nil || !math.IsNaN(got) {
		t.Fatalf("got %v err=%v", got, err)
	}
}

func TestParseFloatInvalid(t *testing.T) {
	for _, in := range []string{"", "abc", "1.5.5", "_1.5", "1.5_", "1__5", "nan(1)"} {
		if _, err := pystrconv.ParseFloat(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestParseFloatOverflow(t *testing.T) {
	_, err := pystrconv.ParseFloat("1e500")
	if !errors.Is(err, pystrconv.ErrFloatOverflow) {
		t.Fatalf("got err=%v", err)
	}
	// Underflow should silently return 0, not error.
	got, err := pystrconv.ParseFloat("1e-400")
	if err != nil || got != 0 {
		t.Fatalf("got %v err=%v", got, err)
	}
}
