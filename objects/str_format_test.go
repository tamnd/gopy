package objects

import (
	"strings"
	"testing"
)

func TestStrFormatEmptySpec(t *testing.T) {
	got, err := Format(NewStr("hi"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi" {
		t.Errorf("Format empty = %q, want hi", got)
	}
}

func TestStrFormatWidthAlign(t *testing.T) {
	cases := []struct {
		spec, want string
	}{
		{"5", "hi   "},
		{"<5", "hi   "},
		{">5", "   hi"},
		{"^5", " hi  "},
		{"*^6", "**hi**"},
		{".1", "h"},
		{"5.1", "h    "},
	}
	for _, c := range cases {
		got, err := Format(NewStr("hi"), c.spec)
		if err != nil {
			t.Errorf("spec %q: err %v", c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("spec %q: got %q, want %q", c.spec, got, c.want)
		}
	}
}

func TestStrFormatTypeS(t *testing.T) {
	got, err := Format(NewStr("hi"), "s")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi" {
		t.Errorf("Format 's' = %q, want hi", got)
	}
}

func TestStrFormatRejectsSign(t *testing.T) {
	_, err := Format(NewStr("hi"), "+5")
	if err == nil {
		t.Error("sign on str spec should error")
	}
}

func TestStrFormatRejectsAlternate(t *testing.T) {
	_, err := Format(NewStr("hi"), "#5")
	if err == nil {
		t.Error("alternate on str spec should error")
	}
}

func TestStrFormatRejectsBadType(t *testing.T) {
	_, err := Format(NewStr("hi"), "d")
	if err == nil {
		t.Error("type 'd' on str spec should error")
	}
}

func TestFormatNoSlotEmptySpec(t *testing.T) {
	// An object with no Format slot but with Str/Repr should pass
	// through Str when spec is empty.
	got, err := Format(NewInt(42), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "42" {
		t.Errorf("int empty spec = %q, want 42", got)
	}
}

func TestFormatNoSlotNonEmptySpec(t *testing.T) {
	// Without a Format slot wired, a non-empty spec is a TypeError.
	// NotImplemented has no Format slot.
	_, err := Format(NotImplemented(), "x")
	if err == nil {
		t.Fatal("non-empty spec without slot should error")
	}
	if !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("err = %v, want TypeError", err)
	}
}

func TestFormatNil(t *testing.T) {
	got, err := Format(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<nil>" {
		t.Errorf("nil format = %q", got)
	}
}
