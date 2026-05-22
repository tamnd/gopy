// Smoke tests for decimal / digit / numeric. Expected outputs taken
// from CPython 3.14.5 unicodedata under $HOME/cpython-314.

package unicodedata

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestDecimal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"9", 9, true},
		{"a", 0, false},
		{"²", 0, false}, // superscript 2: digit yes, decimal no
	}
	for _, tc := range cases {
		res, err := decimalBuiltin([]objects.Object{objects.NewStr(tc.in)}, nil)
		if tc.ok {
			if err != nil {
				t.Errorf("decimal(%q) err = %v", tc.in, err)
				continue
			}
			got, ok := res.(*objects.Int)
			if !ok {
				t.Errorf("decimal(%q) = %T, want *Int", tc.in, res)
				continue
			}
			v, _ := got.Int64()
			if v != tc.want {
				t.Errorf("decimal(%q) = %v, want %d", tc.in, res, tc.want)
			}
		} else if err == nil {
			t.Errorf("decimal(%q) = %v, want error", tc.in, res)
		}
	}
}

func TestDigit(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"5", 5, true},
		{"²", 2, true},
		{"a", 0, false},
	}
	for _, tc := range cases {
		res, err := digitBuiltin([]objects.Object{objects.NewStr(tc.in)}, nil)
		if tc.ok {
			if err != nil {
				t.Errorf("digit(%q) err = %v", tc.in, err)
				continue
			}
			got, ok := res.(*objects.Int)
			if !ok {
				t.Errorf("decimal(%q) = %T, want *Int", tc.in, res)
				continue
			}
			v, _ := got.Int64()
			if v != tc.want {
				t.Errorf("digit(%q) = %v, want %d", tc.in, res, tc.want)
			}
		} else if err == nil {
			t.Errorf("digit(%q) = %v, want error", tc.in, res)
		}
	}
}

func TestNumeric(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"0", 0.0, true},
		{"½", 0.5, true},
		{"Ⅴ", 5.0, true},    // roman numeral five
		{"༳", -0.5, true},   // tibetan digit half minus one
		{"Ⅰ", 1.0, true},    // roman numeral one
		{"Ⅿ", 1000.0, true}, // roman numeral one thousand
		{"a", 0.0, false},
	}
	for _, tc := range cases {
		res, err := numericBuiltin([]objects.Object{objects.NewStr(tc.in)}, nil)
		if tc.ok {
			if err != nil {
				t.Errorf("numeric(%q) err = %v", tc.in, err)
				continue
			}
			got, ok := res.(*objects.Float)
			if !ok || got.Float64() != tc.want {
				t.Errorf("numeric(%q) = %v, want %g", tc.in, res, tc.want)
			}
		} else if err == nil {
			t.Errorf("numeric(%q) = %v, want error", tc.in, res)
		}
	}
}

func TestNumericDefault(t *testing.T) {
	sentinel := objects.NewStr("nope")
	res, err := numericBuiltin([]objects.Object{objects.NewStr("a"), sentinel}, nil)
	if err != nil {
		t.Fatalf("numeric('a', sentinel) err = %v", err)
	}
	if res != sentinel {
		t.Errorf("numeric('a', sentinel) = %v, want sentinel %v", res, sentinel)
	}
}
