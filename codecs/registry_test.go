package codecs

import (
	"testing"
)

// TestNormalizeName pins the name normalization rules.
func TestNormalizeName(t *testing.T) {
	cases := [][2]string{
		{"UTF-8", "utf_8"},
		{"utf 8", "utf_8"},
		{"ISO-8859-1", "iso_8859_1"},
		{"ascii", "ascii"},
	}
	for _, c := range cases {
		if got := NormalizeName(c[0]); got != c[1] {
			t.Errorf("NormalizeName(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// TestRegisterAndLookup pins a full register-then-lookup round trip.
func TestRegisterAndLookup(t *testing.T) {
	Register(func(name string) (*CodecInfo, error) {
		if name == "testcodec" {
			return &CodecInfo{Name: "testcodec"}, nil
		}
		return nil, nil
	})
	ci, err := Lookup("testcodec")
	if err != nil {
		t.Fatal(err)
	}
	if ci.Name != "testcodec" {
		t.Errorf("name = %q, want testcodec", ci.Name)
	}
}

// TestLookupCachesResult pins that the same CodecInfo pointer is
// returned on repeated calls.
func TestLookupCachesResult(t *testing.T) {
	Register(func(name string) (*CodecInfo, error) {
		if name == "cachedcodec" {
			return &CodecInfo{Name: "cachedcodec"}, nil
		}
		return nil, nil
	})
	a, _ := Lookup("cachedcodec")
	b, _ := Lookup("cachedcodec")
	if a != b {
		t.Error("Lookup should return the same pointer on cache hit")
	}
}

// TestLookupUnknownReturnsError pins the LookupError path.
func TestLookupUnknownReturnsError(t *testing.T) {
	if _, err := Lookup("no_such_codec_xyz"); err == nil {
		t.Error("expected error for unknown codec")
	}
}

// TestLookupNormalizesName pins that "UTF-8" and "utf_8" resolve to
// the same codec.
func TestLookupNormalizesName(t *testing.T) {
	Register(func(name string) (*CodecInfo, error) {
		if name == "utf_8" {
			return &CodecInfo{Name: "utf_8"}, nil
		}
		return nil, nil
	})
	ci, err := Lookup("UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	if ci.Name != "utf_8" {
		t.Errorf("name = %q, want utf_8", ci.Name)
	}
}
