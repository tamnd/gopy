package warnings

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// resetState restores the default filter list and writer between
// tests; the warnings package keeps process-wide state.
func resetState(t *testing.T) {
	t.Helper()
	Reset()
	SetWriter(nil)
}

// TestDeprecationIgnoredOutsideMain pins the seeded default rule:
// a DeprecationWarning from a non-__main__ module is dropped silently.
func TestDeprecationIgnoredOutsideMain(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetWriter(&buf)

	if err := WarnAt(DeprecationWarning, "old api", "lib.helpers", 12); err != nil {
		t.Fatalf("Warn returned err: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected silent ignore, got %q", buf.String())
	}
}

// TestDeprecationFromMainPrints pins the matching default rule for
// __main__: prints to the writer in the show_warning shape.
func TestDeprecationFromMainPrints(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetWriter(&buf)

	if err := WarnAt(DeprecationWarning, "old api", "__main__", 7); err != nil {
		t.Fatalf("Warn returned err: %v", err)
	}
	want := "__main__:7: DeprecationWarning: old api\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

// TestErrorActionRaises pins the error filter: returns ErrWarning
// instead of printing.
func TestErrorActionRaises(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionError, Category: UserWarning})
	defer Reset()

	err := WarnAt(UserWarning, "boom", "mod", 1)
	if err == nil {
		t.Fatal("expected ErrWarning")
	}
	var w *ErrWarning
	if !errors.As(err, &w) {
		t.Fatalf("err = %T, want *ErrWarning", err)
	}
	if w.Category != UserWarning || w.Message != "boom" {
		t.Errorf("ErrWarning = %+v", w)
	}
}

// TestSubclassMatch pins the issubclass walk: a filter on Warning
// catches DeprecationWarning emissions.
func TestSubclassMatch(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionError, Category: WarningCategory})
	defer Reset()

	err := WarnAt(DeprecationWarning, "x", "mod", 1)
	if err == nil {
		t.Fatal("expected ErrWarning from base-category filter")
	}
}

// TestMessageRegex pins the message regex match: a filter targeting
// "ssl" lets unrelated messages through unchanged.
func TestMessageRegex(t *testing.T) {
	resetState(t)
	Insert(Filter{
		Action:   ActionIgnore,
		Category: UserWarning,
		Message:  regexp.MustCompile("^ssl"),
	})
	defer Reset()

	var buf bytes.Buffer
	SetWriter(&buf)
	_ = WarnAt(UserWarning, "ssl: cert expired", "mod", 1)
	_ = WarnAt(UserWarning, "io error", "mod", 1)
	if strings.Contains(buf.String(), "ssl") {
		t.Errorf("ssl message leaked through: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "io error") {
		t.Errorf("non-matching message dropped: %q", buf.String())
	}
}

// TestInsertOrderingMostRecentWins pins the simplefilter contract:
// Insert prepends so the latest rule decides first.
func TestInsertOrderingMostRecentWins(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionIgnore, Category: UserWarning})
	Insert(Filter{Action: ActionError, Category: UserWarning})
	defer Reset()

	if err := WarnAt(UserWarning, "x", "mod", 1); err == nil {
		t.Errorf("expected error from most-recent rule, got nil")
	}
}

// TestResetRestoresDefaults pins Reset: after mutation, defaults
// govern dispatch again.
func TestResetRestoresDefaults(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionError, Category: WarningCategory})
	Reset()

	var buf bytes.Buffer
	SetWriter(&buf)
	if err := WarnAt(UserWarning, "x", "mod", 1); err != nil {
		t.Fatalf("after Reset, expected no error, got %v", err)
	}
	if !strings.Contains(buf.String(), "UserWarning") {
		t.Errorf("default-action emit missing: %q", buf.String())
	}
}
