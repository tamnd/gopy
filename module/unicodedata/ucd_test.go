// Smoke tests for the UCD type + ucd_3_2_0 instance. Expected
// outputs were captured against CPython 3.14.5:
//
//	>>> import unicodedata
//	>>> unicodedata.ucd_3_2_0.unidata_version
//	'3.2.0'
//	>>> unicodedata.ucd_3_2_0.lookup('LATIN SMALL LETTER GHA')
//	Traceback (most recent call last):
//	  ...
//	KeyError: "undefined character name 'LATIN SMALL LETTER GHA'"
//	>>> unicodedata.ucd_3_2_0.category(chr(0x10000))
//	'Cn'
//	>>> unicodedata.ucd_3_2_0.normalize('NFD', '⾆8')[0]
//	# Five 3.2 rollbacks: 0x2F868 -> 0x2136A, etc.

package unicodedata

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestUCD320UnidataVersion(t *testing.T) {
	if ucd320.name != "3.2.0" {
		t.Fatalf("ucd320.name = %q, want 3.2.0", ucd320.name)
	}
}

func TestUCD320LookupRejectsAlias(t *testing.T) {
	// LATIN CAPITAL LETTER GHA (alias added post-3.2) must raise
	// KeyError when looked up via the 3.2 snapshot.
	_, err := lookupImpl(ucd320, []objects.Object{objects.NewStr("LATIN CAPITAL LETTER GHA")}, nil)
	if err == nil {
		t.Errorf("ucd_3_2_0.lookup('LATIN CAPITAL LETTER GHA') succeeded, want KeyError")
	}
}

func TestUCD320LookupRejectsNamedSeq(t *testing.T) {
	// KEYCAP NUMBER SIGN is a named sequence introduced post-3.2.
	_, err := lookupImpl(ucd320, []objects.Object{objects.NewStr("KEYCAP NUMBER SIGN")}, nil)
	if err == nil {
		t.Errorf("ucd_3_2_0.lookup('KEYCAP NUMBER SIGN') succeeded, want KeyError")
	}
}

func TestUCD320LookupBasic(t *testing.T) {
	res, err := lookupImpl(ucd320, []objects.Object{objects.NewStr("LATIN SMALL LETTER A")}, nil)
	if err != nil {
		t.Fatalf("ucd_3_2_0.lookup('LATIN SMALL LETTER A') err = %v", err)
	}
	got, ok := res.(*objects.Unicode)
	if !ok || got.Value() != "a" {
		t.Errorf("ucd_3_2_0.lookup('LATIN SMALL LETTER A') = %v, want 'a'", res)
	}
}

func TestUCD320NameUnassigned(t *testing.T) {
	// U+10000 (LINEAR B SYLLABLE B008 A) was unassigned in 3.2.0.
	// CPython 3.14: unicodedata.ucd_3_2_0.name(chr(0x10000)) raises.
	_, err := nameImpl(ucd320, []objects.Object{objects.NewStr(string(rune(0x10000)))}, nil)
	if err == nil {
		t.Errorf("ucd_3_2_0.name(U+10000) succeeded, want ValueError")
	}
}

func TestUCD320CategoryUnassigned(t *testing.T) {
	// 3.2.0 had nothing assigned at U+10000; category drops back to "Cn".
	res, err := categoryImpl(ucd320, []objects.Object{objects.NewStr(string(rune(0x10000)))}, nil)
	if err != nil {
		t.Fatalf("ucd_3_2_0.category(U+10000) err = %v", err)
	}
	got, _ := res.(*objects.Unicode)
	if got == nil || got.Value() != "Cn" {
		t.Errorf("ucd_3_2_0.category(U+10000) = %v, want 'Cn'", res)
	}
}

func TestUCD320CategoryAssigned(t *testing.T) {
	// Plain ASCII is unaffected by the rollback.
	res, err := categoryImpl(ucd320, []objects.Object{objects.NewStr("a")}, nil)
	if err != nil {
		t.Fatalf("ucd_3_2_0.category('a') err = %v", err)
	}
	got, _ := res.(*objects.Unicode)
	if got == nil || got.Value() != "Ll" {
		t.Errorf("ucd_3_2_0.category('a') = %v, want 'Ll'", res)
	}
}

func TestUCD320NormalizationRollback(t *testing.T) {
	// 3.2 rolled back: U+2F868 (CJK COMPATIBILITY IDEOGRAPH-2F868)
	// decomposes to U+2136A in 3.2 instead of the modern target.
	res, err := normalizeImpl(ucd320, []objects.Object{objects.NewStr("NFD"), objects.NewStr(string(rune(0x2F868)))}, nil)
	if err != nil {
		t.Fatalf("ucd_3_2_0.normalize('NFD', U+2F868) err = %v", err)
	}
	got, _ := res.(*objects.Unicode)
	if got == nil {
		t.Fatalf("got = %v", res)
	}
	rs := []rune(got.Value())
	if len(rs) != 1 || rs[0] != 0x2136A {
		t.Errorf("ucd_3_2_0.normalize('NFD', U+2F868) = %v (runes %x), want [0x2136A]", got.Value(), rs)
	}
}

func TestUCD320NormalizationModernPath(t *testing.T) {
	// Module-level normalize falls through to the modern table, where
	// U+2F868 decomposes to U+36FC.
	res, err := normalizeImpl(nil, []objects.Object{objects.NewStr("NFD"), objects.NewStr(string(rune(0x2F868)))}, nil)
	if err != nil {
		t.Fatalf("normalize('NFD', U+2F868) err = %v", err)
	}
	got, _ := res.(*objects.Unicode)
	if got == nil {
		t.Fatalf("got = %v", res)
	}
	rs := []rune(got.Value())
	if len(rs) != 1 || rs[0] != 0x36FC {
		t.Errorf("normalize('NFD', U+2F868) = %v (runes %x), want [0x36FC]", got.Value(), rs)
	}
}
