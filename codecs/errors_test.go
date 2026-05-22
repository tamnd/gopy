package codecs

import (
	"strings"
	"testing"
)

// TestBuiltinHandlersExist pins that strict/ignore/replace are seeded.
func TestBuiltinHandlersExist(t *testing.T) {
	for _, name := range []string{"strict", "ignore", "replace"} {
		if _, err := LookupError(name); err != nil {
			t.Errorf("handler %q not found: %v", name, err)
		}
	}
}

// TestStrictHandlerReturnsError pins that strict raises on bad bytes.
func TestStrictHandlerReturnsError(t *testing.T) {
	h, _ := LookupError("strict")
	_, _, err := h("utf-8", "invalid start byte", []byte{0xff}, 0, 1)
	if err == nil {
		t.Fatal("expected error from strict handler")
	}
	if !strings.Contains(err.Error(), "UnicodeDecodeError") {
		t.Errorf("error = %v, want UnicodeDecodeError", err)
	}
}

// TestIgnoreHandlerAdvancesPosition pins that ignore skips bad bytes.
func TestIgnoreHandlerAdvancesPosition(t *testing.T) {
	h, _ := LookupError("ignore")
	rep, pos, err := h("utf-8", "", []byte{0xff, 0xfe}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep != "" {
		t.Errorf("replacement = %q, want empty", rep)
	}
	if pos != 2 {
		t.Errorf("newpos = %d, want 2", pos)
	}
}

// TestReplaceHandlerReturnsUFFFD pins that replace substitutes U+FFFD.
func TestReplaceHandlerReturnsUFFFD(t *testing.T) {
	h, _ := LookupError("replace")
	rep, _, err := h("utf-8", "", []byte{0xff}, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep, "�") {
		t.Errorf("replacement = %q, want U+FFFD", rep)
	}
}

// TestRegisterErrorCustom pins custom handler registration and lookup.
func TestRegisterErrorCustom(t *testing.T) {
	RegisterError("myhandler", func(_, _ string, _ []byte, _ int, end int) (string, int, error) {
		return "?", end, nil
	})
	h, err := LookupError("myhandler")
	if err != nil {
		t.Fatal(err)
	}
	rep, _, _ := h("utf-8", "", nil, 0, 0)
	if rep != "?" {
		t.Errorf("custom handler returned %q, want ?", rep)
	}
}

// TestLookupErrorUnknown pins the error path for an unregistered name.
func TestLookupErrorUnknown(t *testing.T) {
	if _, err := LookupError("no_such_handler"); err == nil {
		t.Error("expected error for unknown handler")
	}
}
