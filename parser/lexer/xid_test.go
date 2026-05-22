package lexer

import "testing"

// TestScanIdentifierAccepts covers the identifiers CPython accepts
// through _PyUnicode_ScanIdentifier on a vanilla 3.14 build.
func TestScanIdentifierAccepts(t *testing.T) {
	cases := []string{
		"_",
		"x",
		"x1",
		"_x_y_2",
		"αβγ",    // Greek
		"привет", // Cyrillic
		"漢字",     // CJK
		"á",     // letter + combining acute
		"µx",     // micro sign (XID_Start in Unicode)
		"x·",     // letter + middle dot (XID_Continue in Unicode)
		"℘x",     // SCRIPT CAPITAL P (Other_ID_Start)
	}
	for _, s := range cases {
		if off, bad, ok := scanIdentifier(s); !ok {
			t.Errorf("scanIdentifier(%q) = (%d,%q,false), want ok", s, off, bad)
		}
	}
}

// TestScanIdentifierRejects covers the cases CPython rejects.
func TestScanIdentifierRejects(t *testing.T) {
	cases := []struct {
		s     string
		wantR rune
	}{
		{"", 0},
		{"1abc", '1'}, // digit can't lead
		{"$x", '$'},   // ASCII non-identifier
		{"x y", ' '},  // space inside
		{"x-y", '-'},  // hyphen inside
		{" x", 0xa0},  // non-breaking space (Pattern_White_Space, not an identifier char)
	}
	for _, c := range cases {
		off, bad, ok := scanIdentifier(c.s)
		if ok {
			t.Errorf("scanIdentifier(%q) = ok, want reject", c.s)
			continue
		}
		if c.s != "" && bad != c.wantR {
			t.Errorf("scanIdentifier(%q) bad = U+%04X (%q), want U+%04X", c.s, bad, bad, c.wantR)
		}
		_ = off
	}
}
