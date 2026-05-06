package imp

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func makeInitFunc(name string) InitFunc {
	return func() (*objects.Module, error) {
		return objects.NewModule(name), nil
	}
}

// TestAppendAndFindInitFunc pins that AppendInittab / FindInitFunc
// round-trip correctly.
func TestAppendAndFindInitFunc(t *testing.T) {
	fn := makeInitFunc("_test_builtin")
	if err := AppendInittab("_test_builtin", fn); err != nil {
		t.Fatalf("AppendInittab: %v", err)
	}
	got := FindInitFunc("_test_builtin")
	if got == nil {
		t.Fatal("FindInitFunc: not found after AppendInittab")
	}
	mod, err := got()
	if err != nil {
		t.Fatalf("InitFunc: %v", err)
	}
	if mod == nil {
		t.Fatal("InitFunc returned nil module")
	}
}

// TestFindInitFuncMissing pins that a missing name returns nil.
func TestFindInitFuncMissing(t *testing.T) {
	got := FindInitFunc("no_such_builtin_xyz")
	if got != nil {
		t.Error("FindInitFunc: expected nil for unknown module")
	}
}

// TestAppendInittabNilError pins that a nil InitFunc returns an error.
func TestAppendInittabNilError(t *testing.T) {
	err := AppendInittab("_test_nil_init", nil)
	if err == nil {
		t.Fatal("expected error for nil InitFunc")
	}
}

// TestAppendInittabDedup pins that registering the same name twice
// does not duplicate the entry.
func TestAppendInittabDedup(t *testing.T) {
	before := len(InittabSnapshot())
	fn := makeInitFunc("_test_dedup_mod")
	_ = AppendInittab("_test_dedup_mod", fn)
	_ = AppendInittab("_test_dedup_mod", fn)
	after := len(InittabSnapshot())
	if after != before+1 {
		t.Errorf("after two AppendInittab: len=%d, want %d", after, before+1)
	}
}

// TestExtendInittab pins that ExtendInittab adds multiple entries.
func TestExtendInittab(t *testing.T) {
	before := len(InittabSnapshot())
	entries := []InittabEntry{
		{Name: "_test_extend_a", Init: makeInitFunc("_test_extend_a")},
		{Name: "_test_extend_b", Init: makeInitFunc("_test_extend_b")},
	}
	if err := ExtendInittab(entries); err != nil {
		t.Fatalf("ExtendInittab: %v", err)
	}
	after := len(InittabSnapshot())
	if after != before+2 {
		t.Errorf("after ExtendInittab: len=%d, want %d", after, before+2)
	}
}
