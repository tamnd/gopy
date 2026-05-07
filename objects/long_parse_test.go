package objects

import (
	"math/big"
	"testing"
)

func intFromStr(t *testing.T, s string, base int) *Int {
	t.Helper()
	i, err := IntFromString(s, base)
	if err != nil {
		t.Fatalf("IntFromString(%q, %d): %v", s, base, err)
	}
	return i
}

func TestIntFromStringDecimal(t *testing.T) {
	cases := map[string]int64{
		"0":           0,
		"42":          42,
		"-7":          -7,
		"+10":         10,
		"  123  ":     123,
		"1_000":       1000,
		"1_000_000":   1_000_000,
		"-1_2_3":      -123,
		"99999999999": 99999999999,
	}
	for s, want := range cases {
		got := intFromStr(t, s, 10)
		v, _ := got.Int64()
		if v != want {
			t.Errorf("%q -> %d, want %d", s, v, want)
		}
	}
}

func TestIntFromStringBaseAuto(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0x10", 16},
		{"0o17", 15},
		{"0b101", 5},
		{"-0xff", -255},
		{"0X1_F", 31},
	}
	for _, tc := range tests {
		got := intFromStr(t, tc.in, 0)
		v, _ := got.Int64()
		if v != tc.want {
			t.Errorf("%q -> %d, want %d", tc.in, v, tc.want)
		}
	}
}

func TestIntFromStringExplicitBase(t *testing.T) {
	got := intFromStr(t, "0xff", 16)
	if v, _ := got.Int64(); v != 255 {
		t.Fatalf("0xff base16 -> %d", v)
	}
	got = intFromStr(t, "ff", 16)
	if v, _ := got.Int64(); v != 255 {
		t.Fatalf("ff base16 -> %d", v)
	}
}

func TestIntFromStringBigPrecision(t *testing.T) {
	huge := "123456789012345678901234567890"
	got, err := IntFromString(huge, 10)
	if err != nil {
		t.Fatalf("IntFromString huge: %v", err)
	}
	want, _ := new(big.Int).SetString(huge, 10)
	if got.BigInt().Cmp(want) != 0 {
		t.Fatalf("got %s, want %s", got.BigInt(), want)
	}
}

func TestIntFromStringRejects(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"_1",
		"1_",
		"1__2",
		"0x",
		"abc",  // letters in base 10
		"0xZZ", // bad hex digit
		"+",
		"-",
		"1 2", // internal whitespace
	}
	for _, s := range bad {
		if _, err := IntFromString(s, 10); err == nil {
			t.Errorf("%q parsed without error", s)
		}
	}
}

func TestIntFromStringBaseValidation(t *testing.T) {
	if _, err := IntFromString("1", 1); err == nil {
		t.Fatalf("base 1 should error")
	}
	if _, err := IntFromString("1", 37); err == nil {
		t.Fatalf("base 37 should error")
	}
}
