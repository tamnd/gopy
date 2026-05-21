// Smoke tests for name / lookup. Expected outputs taken from
// CPython 3.14.5 unicodedata.

package unicodedata

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestName(t *testing.T) {
	cases := []struct {
		code rune
		want string
		ok   bool
	}{
		{0x41, "LATIN CAPITAL LETTER A", true},
		{0x61, "LATIN SMALL LETTER A", true},
		{0xE0, "LATIN SMALL LETTER A WITH GRAVE", true},
		{0xAC00, "HANGUL SYLLABLE GA", true},
		{0xAE08, "HANGUL SYLLABLE GEUM", true},
		{0x4E00, "CJK UNIFIED IDEOGRAPH-4E00", true},
		{0x20AC, "EURO SIGN", true},
		{0x1F600, "GRINNING FACE", true},
		{0x2603, "SNOWMAN", true},
		{0x10FFFF, "", false},
		{0x80, "", false},
	}
	for _, tc := range cases {
		got, ok := getUCName(tc.code, false)
		if ok != tc.ok || got != tc.want {
			t.Errorf("getUCName(%#x) = %q,%v; want %q,%v", tc.code, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNameBuiltin(t *testing.T) {
	res, err := nameBuiltin([]objects.Object{objects.NewStr("a")}, nil)
	if err != nil {
		t.Fatalf("name('a') err = %v", err)
	}
	s, ok := res.(*objects.Unicode)
	if !ok || s.Value() != "LATIN SMALL LETTER A" {
		t.Errorf("name('a') = %v, want LATIN SMALL LETTER A", res)
	}

	sentinel := objects.NewStr("nope")
	res, err = nameBuiltin([]objects.Object{objects.NewStr(string(rune(0x80))), sentinel}, nil)
	if err != nil {
		t.Fatalf("name(U+0080, sentinel) err = %v", err)
	}
	if res != sentinel {
		t.Errorf("name(U+0080, sentinel) = %v, want sentinel", res)
	}

	_, err = nameBuiltin([]objects.Object{objects.NewStr(string(rune(0x80)))}, nil)
	if err == nil {
		t.Errorf("name(U+0080) should ValueError")
	}
}

func TestLookup(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"LATIN SMALL LETTER A", "a"},
		{"HANGUL SYLLABLE GA", "가"},
		{"CJK UNIFIED IDEOGRAPH-4E00", "一"},
		{"EURO SIGN", "€"},
		{"GRINNING FACE", "\U0001F600"},
		{"LATIN SMALL LETTER A WITH GRAVE", "à"},
		{"LATIN CAPITAL LETTER GHA", "Ƣ"},
		{"KEYCAP NUMBER SIGN", "#️⃣"},
	}
	for _, tc := range cases {
		res, err := lookupBuiltin([]objects.Object{objects.NewStr(tc.name)}, nil)
		if err != nil {
			t.Errorf("lookup(%q) err = %v", tc.name, err)
			continue
		}
		got, ok := res.(*objects.Unicode)
		if !ok || got.Value() != tc.want {
			t.Errorf("lookup(%q) = %v, want %q", tc.name, res, tc.want)
		}
	}

	_, err := lookupBuiltin([]objects.Object{objects.NewStr("NOT A REAL NAME")}, nil)
	if err == nil {
		t.Errorf("lookup('NOT A REAL NAME') should KeyError")
	}
}
