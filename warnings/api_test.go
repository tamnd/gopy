package warnings

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// TestFilterwarningsRejectsInvalidAction pins the action whitelist:
// unknown action strings come back as an error, not a panic, so the
// caller can surface a Python-level ValueError later.
func TestFilterwarningsRejectsInvalidAction(t *testing.T) {
	resetState(t)
	err := Filterwarnings(Action("nope"), nil, UserWarning, nil, 0, false)
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

// TestFilterwarningsRejectsNegativeLineno pins the lineno guard.
func TestFilterwarningsRejectsNegativeLineno(t *testing.T) {
	resetState(t)
	if err := Filterwarnings(ActionIgnore, nil, UserWarning, nil, -1, false); err == nil {
		t.Fatal("expected error for negative lineno")
	}
}

// TestFilterwarningsPrependsByDefault pins simplefilter ordering:
// the most recently inserted rule wins dispatch.
func TestFilterwarningsPrependsByDefault(t *testing.T) {
	resetState(t)
	defer Reset()

	if err := Filterwarnings(ActionIgnore, nil, UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := Filterwarnings(ActionError, nil, UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}
	err := WarnAt(UserWarning, "x", "mod", 1)
	var w *ErrWarning
	if !errors.As(err, &w) {
		t.Fatalf("expected ErrWarning, got %v", err)
	}
}

// TestFilterwarningsAppendGoesLast pins append=true: an earlier
// matching rule still wins because append puts the new filter at
// the tail of the list.
func TestFilterwarningsAppendGoesLast(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if err := Filterwarnings(ActionIgnore, nil, UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := Filterwarnings(ActionError, nil, UserWarning, nil, 0, true); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	SetWriter(&buf)
	if err := WarnAt(UserWarning, "x", "mod", 1); err != nil {
		t.Fatalf("expected ignore to win, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected silent, got %q", buf.String())
	}
}

// TestFilterwarningsDedupOnPrepend pins the _add_filter dedup
// branch: re-installing the same rule moves it to the front rather
// than producing two equal entries.
func TestFilterwarningsDedupOnPrepend(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	rule := func() error {
		return Filterwarnings(ActionIgnore, nil, UserWarning, nil, 0, false)
	}
	if err := rule(); err != nil {
		t.Fatal(err)
	}
	if err := rule(); err != nil {
		t.Fatal(err)
	}
	if got := len(Filters()); got != 1 {
		t.Errorf("filters len = %d, want 1", got)
	}
}

// TestFilterwarningsAppendSkipsDuplicate pins append=true dedup:
// re-appending the same shape is a no-op.
func TestFilterwarningsAppendSkipsDuplicate(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if err := Filterwarnings(ActionIgnore, nil, UserWarning, nil, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := Filterwarnings(ActionIgnore, nil, UserWarning, nil, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := len(Filters()); got != 1 {
		t.Errorf("filters len = %d, want 1", got)
	}
}

// TestFilterwarningsMessageRegexEquality pins the regex-pattern
// equality used by dedup: two filters with the same source pattern
// count as duplicates even though their *regexp.Regexp pointers
// differ.
func TestFilterwarningsMessageRegexEquality(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if err := Filterwarnings(ActionIgnore, regexp.MustCompile("^ssl"), UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := Filterwarnings(ActionIgnore, regexp.MustCompile("^ssl"), UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := len(Filters()); got != 1 {
		t.Errorf("filters len = %d, want 1", got)
	}
}

// TestSimplefilterInstallsCategoryOnly pins the simplefilter shape:
// no message regex, no module regex, only category narrows.
func TestSimplefilterInstallsCategoryOnly(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if err := Simplefilter(ActionError, UserWarning, 0, false); err != nil {
		t.Fatal(err)
	}
	rules := Filters()
	if len(rules) != 1 {
		t.Fatalf("filters len = %d, want 1", len(rules))
	}
	if rules[0].Message != nil || rules[0].Module != nil {
		t.Errorf("simplefilter should leave message/module nil, got %+v", rules[0])
	}
}

// TestResetwarningsClearsAll pins the resetwarnings contract:
// after the call, the seeded defaults are gone too.
func TestResetwarningsClearsAll(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if got := len(Filters()); got != 0 {
		t.Errorf("filters len = %d, want 0", got)
	}

	var buf bytes.Buffer
	SetWriter(&buf)
	if err := WarnAt(DeprecationWarning, "old api", "lib.helpers", 12); err != nil {
		t.Fatalf("Warn returned err: %v", err)
	}
	if !strings.Contains(buf.String(), "DeprecationWarning") {
		t.Errorf("after Resetwarnings, expected default-action emit, got %q", buf.String())
	}
}

// TestResetwarningsPurgesRegistry pins the filters_mutated hook:
// dedup state seeded by an earlier action does not survive a clear.
func TestResetwarningsPurgesRegistry(t *testing.T) {
	resetState(t)
	defer Reset()

	var buf bytes.Buffer
	SetWriter(&buf)
	_ = WarnAt(UserWarning, "boom", "mod", 1)
	if got := strings.Count(buf.String(), "boom"); got != 1 {
		t.Fatalf("first emit count = %d, want 1", got)
	}

	Resetwarnings()
	buf.Reset()
	_ = WarnAt(UserWarning, "boom", "mod", 1)
	if got := strings.Count(buf.String(), "boom"); got != 1 {
		t.Errorf("after Resetwarnings, second emit count = %d, want 1", got)
	}
}

// TestFilterwarningsAllAlias pins the "all" alias: it is accepted
// at validation and prints every emission like ActionAlways.
func TestFilterwarningsAllAlias(t *testing.T) {
	resetState(t)
	defer Reset()

	Resetwarnings()
	if err := Filterwarnings(Action("all"), nil, UserWarning, nil, 0, false); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	SetWriter(&buf)
	for range 3 {
		_ = WarnAt(UserWarning, "x", "mod", 1)
	}
	if got := strings.Count(buf.String(), "UserWarning"); got != 3 {
		t.Errorf("emitted %d times, want 3", got)
	}
}
