package pystrconv_test

import (
	"testing"

	"github.com/tamnd/gopy/pystrconv"
)

func TestCompareInsensitive(t *testing.T) {
	cases := []struct {
		a, b string
		sign int
	}{
		{"abc", "ABC", 0},
		{"abc", "abd", -1},
		{"abd", "abc", 1},
		{"abc", "abcd", -1},
		{"abcd", "abc", 1},
		{"", "", 0},
		{"", "x", -1},
		{"Hello", "hello", 0},
	}
	for _, c := range cases {
		got := pystrconv.CompareInsensitive(c.a, c.b)
		if !sameSign(got, c.sign) {
			t.Errorf("CompareInsensitive(%q,%q): got %d want sign %d", c.a, c.b, got, c.sign)
		}
	}
}

func TestCompareInsensitiveN(t *testing.T) {
	if pystrconv.CompareInsensitiveN("abcdef", "ABCXYZ", 3) != 0 {
		t.Fatal("first 3 bytes match case-insensitively")
	}
	if pystrconv.CompareInsensitiveN("abc", "abd", 0) != 0 {
		t.Fatal("size 0 must return 0")
	}
	if pystrconv.CompareInsensitiveN("abc", "abd", 3) >= 0 {
		t.Fatal("c < d at index 2")
	}
}

func sameSign(got, want int) bool {
	if want == 0 {
		return got == 0
	}
	if want < 0 {
		return got < 0
	}
	return got > 0
}
