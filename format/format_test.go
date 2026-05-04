package format_test

import (
	"math/big"
	"testing"

	"github.com/tamnd/gopy/format"
)

func parse(t *testing.T, s string) format.Spec {
	t.Helper()
	spec, err := format.ParseSpec(s)
	if err != nil {
		t.Fatalf("ParseSpec(%q): %v", s, err)
	}
	return spec
}

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in   string
		want format.Spec
	}{
		{"", format.Spec{Width: -1, Precision: -1}},
		{"d", format.Spec{Width: -1, Precision: -1, Type: 'd'}},
		{"5d", format.Spec{Width: 5, Precision: -1, Type: 'd'}},
		{"05d", format.Spec{Width: 5, Precision: -1, Type: 'd', Zero: true, Fill: '0', Align: '='}},
		{",d", format.Spec{Width: -1, Precision: -1, Type: 'd', Thousands: ','}},
		{".2f", format.Spec{Width: -1, Precision: 2, Type: 'f'}},
		{"+.2f", format.Spec{Width: -1, Precision: 2, Type: 'f', Sign: '+'}},
		{"#x", format.Spec{Width: -1, Precision: -1, Type: 'x', Alt: true}},
		{"*<6", format.Spec{Width: 6, Precision: -1, Fill: '*', Align: '<'}},
		{"^10.2f", format.Spec{Width: 10, Precision: 2, Type: 'f', Align: '^'}},
	}
	for _, c := range cases {
		got, err := format.ParseSpec(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestFormatString(t *testing.T) {
	cases := []struct {
		in   string
		spec string
		want string
	}{
		{"hi", "<6", "hi    "},
		{"hi", ">6", "    hi"},
		{"hi", "^6", "  hi  "},
		{"hi", "*<6", "hi****"},
		{"hi", "*>6", "****hi"},
		{"hello", ".1", "h"},
	}
	for _, c := range cases {
		got, err := format.FormatString(c.in, parse(t, c.spec))
		if err != nil {
			t.Errorf("%q %q: %v", c.in, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q %q: got %q want %q", c.in, c.spec, got, c.want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	cases := []struct {
		v    int64
		spec string
		want string
	}{
		{42, "d", "42"},
		{42, "5d", "   42"},
		{42, "05d", "00042"},
		{-42, "05d", "-0042"},
		{255, "x", "ff"},
		{255, "#x", "0xff"},
		{255, "#X", "0XFF"},
		{255, "#08x", "0x0000ff"},
		{1234567, ",d", "1,234,567"},
		{1234567, "_d", "1_234_567"},
		{255, "_b", "1111_1111"},
		{65, "c", "A"},
	}
	for _, c := range cases {
		v := big.NewInt(c.v)
		got, err := format.FormatInt(v, parse(t, c.spec))
		if err != nil {
			t.Errorf("%d %q: %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("%d %q: got %q want %q", c.v, c.spec, got, c.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		v    float64
		spec string
		want string
	}{
		{1.5, "f", "1.500000"},
		{1.5, ".2f", "1.50"},
		{1.5, "10.2f", "      1.50"},
		{1.5, "010.2f", "0000001.50"},
		{-1.5, "+.2f", "-1.50"},
		{1.5, "+.2f", "+1.50"},
		{1234.5678, ",.2f", "1,234.57"},
		{1.5, "e", "1.500000e+00"},
		{0.0, "g", "0"},
	}
	for _, c := range cases {
		got, err := format.FormatFloat(c.v, parse(t, c.spec))
		if err != nil {
			t.Errorf("%v %q: %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("%v %q: got %q want %q", c.v, c.spec, got, c.want)
		}
	}
}
