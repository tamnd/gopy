// Package v04test exercises the v0.4 gate: hashing, float parse/format,
// and the format-spec mini-language all match CPython 3.14 under
// PYTHONHASHSEED=0. Reference values were captured from
// `python3 -c "..."` on the upstream interpreter.
package v04test

import (
	"math"
	"math/big"
	"testing"

	"github.com/tamnd/gopy/format"
	"github.com/tamnd/gopy/hash"
	"github.com/tamnd/gopy/pystrconv"
)

func zeroHashSecret() {
	for i := range hash.Secret {
		hash.Secret[i] = 0
	}
}

func TestGateHashBuffer(t *testing.T) {
	zeroHashSecret()
	// CPython 3.14 under PYTHONHASHSEED=0: hash(b"hello") == -2096571579003691106.
	if got := hash.Buffer([]byte("hello")); got != -2096571579003691106 {
		t.Fatalf("hash.Buffer(\"hello\") = %d, want -2096571579003691106", got)
	}
}

func TestGateParseFloatRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"3.141592653589793", math.Float64bits(3.141592653589793)},
		{"1e-300", math.Float64bits(1e-300)},
		{"1.7976931348623157e308", math.Float64bits(1.7976931348623157e308)},
		{"0.1", math.Float64bits(0.1)},
		{"-0.0", math.Float64bits(math.Copysign(0, -1))},
	}
	for _, c := range cases {
		v, err := pystrconv.ParseFloat(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if math.Float64bits(v) != c.want {
			t.Errorf("%q: got 0x%016x want 0x%016x", c.in, math.Float64bits(v), c.want)
		}
	}
}

func TestGateFormatFloatRepr(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.1, "0.1"},
		{1e15, "1000000000000000.0"},
		{1e16, "1e+16"},
		{3.141592653589793, "3.141592653589793"},
	}
	for _, c := range cases {
		got := pystrconv.FormatFloat(c.in, 'r', 0, pystrconv.FlagAddDotZero)
		if got != c.want {
			t.Errorf("repr(%v) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestGateFormatSpec(t *testing.T) {
	// Mirrors `format(1234567, ',d')` and friends.
	spec, err := format.ParseSpec(",d")
	if err != nil {
		t.Fatal(err)
	}
	got, err := format.FormatInt(big.NewInt(1234567), spec)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1,234,567" {
		t.Errorf("got %q", got)
	}

	spec, err = format.ParseSpec(".4f")
	if err != nil {
		t.Fatal(err)
	}
	gotF, err := format.FormatFloat(math.Pi, spec)
	if err != nil {
		t.Fatal(err)
	}
	if gotF != "3.1416" {
		t.Errorf("got %q", gotF)
	}
}
