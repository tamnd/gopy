package string

import "testing"

func TestCharByNameKnown(t *testing.T) {
	cases := map[string]rune{
		"NULL":                   0x0000,
		"SPACE":                  0x0020,
		"LATIN SMALL LETTER A":   0x0061,
		"GREEK SMALL LETTER PI":  0x03c0,
		"SNOWMAN":                0x2603,
		"GRINNING FACE":          0x1f600,
		"FACE WITH TEARS OF JOY": 0x1f602,
	}
	for name, want := range cases {
		got, err := CharByName(name)
		if err != nil {
			t.Errorf("CharByName(%q) err = %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("CharByName(%q) = %U, want %U", name, got, want)
		}
	}
}

func TestCharByNameUnknown(t *testing.T) {
	if _, err := CharByName("DEFINITELY NOT A REAL NAME"); err == nil {
		t.Error("CharByName(unknown) err = nil, want error")
	}
	if _, err := CharByName(""); err == nil {
		t.Error("CharByName(\"\") err = nil, want error")
	}
}

func TestDecodeNamedEscape(t *testing.T) {
	r, err := ParseString([]byte(`"\N{GRINNING FACE}"`))
	if err != nil {
		t.Fatalf("ParseString err = %v", err)
	}
	want := "\U0001f600"
	if r.Text != want {
		t.Errorf("Text = %q, want %q", r.Text, want)
	}
}

func TestDecodeNamedEscapeUnknown(t *testing.T) {
	if _, err := ParseString([]byte(`"\N{NOT A NAME}"`)); err == nil {
		t.Error("ParseString err = nil, want error for unknown name")
	}
}

func TestDecodeNamedEscapeMalformed(t *testing.T) {
	if _, err := ParseString([]byte(`"\Nfoo"`)); err == nil {
		t.Error("ParseString err = nil, want error for missing brace")
	}
}
