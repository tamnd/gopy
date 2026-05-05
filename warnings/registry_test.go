package warnings

import (
	"bytes"
	"strings"
	"testing"
)

// TestDefaultActionDedupsPerLocation pins the once-per-(module,
// category, lineno) shape of the default action: emitting twice
// from the same location only prints once.
func TestDefaultActionDedupsPerLocation(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetWriter(&buf)

	for i := 0; i < 3; i++ {
		_ = WarnAt(UserWarning, "boom", "app.module", 42)
	}
	got := strings.Count(buf.String(), "boom")
	if got != 1 {
		t.Errorf("emitted %d times, want 1", got)
	}
}

// TestDefaultActionDifferentLineRefires pins the line-as-key part:
// the same message from a different line emits again.
func TestDefaultActionDifferentLineRefires(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetWriter(&buf)

	_ = WarnAt(UserWarning, "boom", "app.module", 1)
	_ = WarnAt(UserWarning, "boom", "app.module", 2)

	if got := strings.Count(buf.String(), "boom"); got != 2 {
		t.Errorf("emitted %d times, want 2 (different lines)", got)
	}
}

// TestAlwaysActionDoesNotDedup pins the ActionAlways branch:
// every emission prints, regardless of dedup state.
func TestAlwaysActionDoesNotDedup(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionAlways, Category: UserWarning})
	defer Reset()

	var buf bytes.Buffer
	SetWriter(&buf)
	for i := 0; i < 4; i++ {
		_ = WarnAt(UserWarning, "x", "mod", 1)
	}
	if got := strings.Count(buf.String(), "UserWarning"); got != 4 {
		t.Errorf("emitted %d times, want 4", got)
	}
}

// TestOnceActionGlobalDedup pins ActionOnce: keyed by (category,
// message), emits once across modules and lines.
func TestOnceActionGlobalDedup(t *testing.T) {
	resetState(t)
	Insert(Filter{Action: ActionOnce, Category: UserWarning})
	defer Reset()

	var buf bytes.Buffer
	SetWriter(&buf)

	_ = WarnAt(UserWarning, "boom", "modA", 1)
	_ = WarnAt(UserWarning, "boom", "modB", 99)
	if got := strings.Count(buf.String(), "boom"); got != 1 {
		t.Errorf("once dedup let %d emissions through, want 1", got)
	}
}

// TestFilterMutationPurgesRegistry pins the filters_mutated hook:
// after a filter change, prior dedup state must not silence a
// fresh emission.
func TestFilterMutationPurgesRegistry(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetWriter(&buf)

	_ = WarnAt(UserWarning, "boom", "mod", 1)
	if got := strings.Count(buf.String(), "boom"); got != 1 {
		t.Fatalf("first emit count = %d, want 1", got)
	}
	buf.Reset()

	Insert(Filter{Action: ActionAlways, Category: UserWarning})
	defer Reset()

	_ = WarnAt(UserWarning, "boom", "mod", 1)
	if got := strings.Count(buf.String(), "boom"); got != 1 {
		t.Errorf("after Insert + emit, count = %d, want 1", got)
	}
}
