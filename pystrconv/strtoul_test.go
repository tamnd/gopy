package pystrconv_test

import (
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

func TestParseUintBase10(t *testing.T) {
	v, n, st := pystrconv.ParseUint("12345", 10)
	if st != pystrconv.IntOK || v != 12345 || n != 5 {
		t.Fatalf("got v=%d n=%d st=%v", v, n, st)
	}
}

func TestParseUintAutoBase(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"0xff", 255},
		{"0XFF", 255},
		{"0o17", 15},
		{"0b1010", 10},
		{"42", 42},
	}
	for _, c := range cases {
		v, _, st := pystrconv.ParseUint(c.in, 0)
		if st != pystrconv.IntOK || v != c.want {
			t.Errorf("%q: got v=%d st=%v want v=%d", c.in, v, st, c.want)
		}
	}
}

func TestParseUintLeadingZeroDecimal(t *testing.T) {
	// CPython: int("0", 0) == 0; "00000" parses as 0 too.
	v, _, st := pystrconv.ParseUint("00000", 0)
	if st != pystrconv.IntOK || v != 0 {
		t.Fatalf("got v=%d st=%v", v, st)
	}
}

func TestParseUintInvalidPrefix(t *testing.T) {
	_, _, st := pystrconv.ParseUint("0xZ", 0)
	if st != pystrconv.IntInvalid {
		t.Fatalf("got st=%v", st)
	}
}

func TestParseUintOverflow(t *testing.T) {
	// 2^64 in decimal
	_, _, st := pystrconv.ParseUint("18446744073709551616", 10)
	if st != pystrconv.IntOverflow {
		t.Fatalf("got st=%v", st)
	}
}

func TestParseIntSign(t *testing.T) {
	v, _, st := pystrconv.ParseInt("-42", 10)
	if st != pystrconv.IntOK || v != -42 {
		t.Fatalf("got v=%d st=%v", v, st)
	}
	v, _, st = pystrconv.ParseInt("+42", 10)
	if st != pystrconv.IntOK || v != 42 {
		t.Fatalf("got v=%d st=%v", v, st)
	}
}

func TestParseIntMinInt64(t *testing.T) {
	v, _, st := pystrconv.ParseInt("-9223372036854775808", 10)
	if st != pystrconv.IntOK || v != -1<<63 {
		t.Fatalf("got v=%d st=%v", v, st)
	}
}

func TestParseIntPositiveOverflow(t *testing.T) {
	_, _, st := pystrconv.ParseInt("9223372036854775808", 10)
	if st != pystrconv.IntOverflow {
		t.Fatalf("got st=%v", st)
	}
}

func TestParseUintLeadingWhitespace(t *testing.T) {
	v, n, st := pystrconv.ParseUint("   42rest", 10)
	if st != pystrconv.IntOK || v != 42 {
		t.Fatalf("got v=%d n=%d st=%v", v, n, st)
	}
	if n != len("   42") {
		t.Fatalf("n=%d", n)
	}
}
