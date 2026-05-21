// Smoke tests for the unicodedata Phase 1-3 surface. The expected
// outputs are taken from CPython 3.14.5's unicodedata module under
// $HOME/cpython-314 (UNIDATA_VERSION 16.0.0).

package unicodedata

import (
	"reflect"
	"testing"
)

func TestNormalizeNFD(t *testing.T) {
	cases := []struct {
		in   string
		want []rune
	}{
		// os_helper.py:31 case
		{"@test-àòɘŁğ", []rune{0x40, 0x74, 0x65, 0x73, 0x74, 0x2d, 0x61, 0x300, 0x6f, 0x300, 0x258, 0x141, 0x67, 0x306}},
		// Single precomposed grave
		{"à", []rune{0x61, 0x300}},
		// Two combining marks both CCC=230 (ring 0x30a, tilde 0x303) keep
		// input order under canonical reordering. Verified against CPython.
		{"n̊̃", []rune{0x6e, 0x30a, 0x303}},
		// Hangul algorithmic decomposition: U+AC00 -> U+1100, U+1161.
		{"가", []rune{0x1100, 0x1161}},
		// ASCII passthrough
		{"hello", []rune{'h', 'e', 'l', 'l', 'o'}},
		// Empty string
		{"", nil},
	}
	for _, tc := range cases {
		got := nfdNFKD([]rune(tc.in), false)
		if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
			t.Errorf("nfdNFKD(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeNFKD(t *testing.T) {
	cases := []struct {
		in   string
		want []rune
	}{
		// Superscript 4 -> compatibility-decomposes to plain 4
		{"⁴", []rune{'4'}},
		// Roman numeral V -> compatibility V
		{"Ⅴ", []rune{'V'}},
	}
	for _, tc := range cases {
		got := nfdNFKD([]rune(tc.in), true)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("nfdNFKD(%q, k=true) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeNFC(t *testing.T) {
	cases := []struct {
		in   string
		want []rune
	}{
		// Decomposed grave should recompose
		{"à", []rune{0x00e0}},
		// Hangul L+V
		{"가", []rune{0xAC00}},
		// Plain ASCII passthrough
		{"abc", []rune{'a', 'b', 'c'}},
	}
	for _, tc := range cases {
		got := nfcNFKC([]rune(tc.in), false)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("nfcNFKC(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCategory(t *testing.T) {
	cases := []struct {
		r    rune
		want string
	}{
		{'a', "Ll"},
		{'A', "Lu"},
		{'1', "Nd"},
		{' ', "Zs"},
		{0x0300, "Mn"}, // combining grave
		{0x4E2D, "Lo"}, // CJK ideograph
	}
	for _, tc := range cases {
		idx := int(getRecord(tc.r).Category)
		if got := categoryNames[idx]; got != tc.want {
			t.Errorf("category(%U) = %s, want %s", tc.r, got, tc.want)
		}
	}
}

func TestEastAsianWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want string
	}{
		{'a', "Na"},   // ASCII letter
		{0x4E2D, "W"}, // CJK wide
		{0xFF21, "F"}, // fullwidth A
	}
	for _, tc := range cases {
		idx := int(getRecord(tc.r).EastAsianWidth)
		if got := eastAsianWidthNames[idx]; got != tc.want {
			t.Errorf("east_asian_width(%U) = %s, want %s", tc.r, got, tc.want)
		}
	}
}

func TestCombiningClass(t *testing.T) {
	cases := []struct {
		r    rune
		want uint8
	}{
		{'a', 0},
		{0x0300, 230}, // combining grave
		{0x05BC, 21},  // hebrew point dagesh
	}
	for _, tc := range cases {
		if got := getRecord(tc.r).Combining; got != tc.want {
			t.Errorf("combining(%U) = %d, want %d", tc.r, got, tc.want)
		}
	}
}

func TestUnidataVersion(t *testing.T) {
	if unidataVersion != "16.0.0" {
		t.Errorf("unidataVersion = %q, want %q", unidataVersion, "16.0.0")
	}
}
