package imp

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestFindFrozenBootstrapNames pins that the bootstrap entries from
// frozen_bootstrap.go are all registered.
func TestFindFrozenBootstrapNames(t *testing.T) {
	names := []string{
		"_frozen_importlib",
		"_frozen_importlib_external",
		"importlib",
		"importlib._bootstrap",
		"importlib._bootstrap_external",
	}
	for _, name := range names {
		m, ok := FindFrozen(name)
		if !ok {
			t.Errorf("FindFrozen(%q) = false, want true", name)
			continue
		}
		if m.Name != name {
			t.Errorf("FindFrozen(%q).Name = %q", name, m.Name)
		}
	}
}

// TestIsFrozen pins that known frozen names return true.
func TestIsFrozen(t *testing.T) {
	if !IsFrozen("_frozen_importlib") {
		t.Error("IsFrozen(_frozen_importlib) = false, want true")
	}
	if IsFrozen("not_a_frozen_module_xyz") {
		t.Error("IsFrozen(unknown) = true, want false")
	}
}

// TestRegisterFrozenRoundtrip pins that RegisterFrozen stores and
// FindFrozen retrieves the same entry.
func TestRegisterFrozenRoundtrip(t *testing.T) {
	code := objects.NewCode()
	code.Name = "testmod"
	m := &FrozenModule{
		Name:      "_test_frozen_roundtrip",
		Code:      code,
		IsPackage: false,
	}
	RegisterFrozen(m)

	got, ok := FindFrozen("_test_frozen_roundtrip")
	if !ok {
		t.Fatal("FindFrozen: not found after RegisterFrozen")
	}
	if got.Code != code {
		t.Error("Code pointer mismatch")
	}
	if got.IsPackage {
		t.Error("IsPackage mismatch")
	}
}

// TestImportlibIsPackage pins that the importlib frozen entry is
// marked as a package.
func TestImportlibIsPackage(t *testing.T) {
	m, ok := FindFrozen("importlib")
	if !ok {
		t.Fatal("importlib not found")
	}
	if !m.IsPackage {
		t.Error("importlib.IsPackage = false, want true")
	}
}

// TestBootstrapCodeNil pins that the bootstrap entries have nil Code
// fields (placeholder state, filled by bootstrap.go later).
func TestBootstrapCodeNil(t *testing.T) {
	m, _ := FindFrozen("_frozen_importlib")
	if m.Code != nil {
		t.Error("_frozen_importlib.Code is non-nil, expected nil placeholder")
	}
}

// TestFrozenList pins that FrozenList returns all registered modules.
func TestFrozenList(t *testing.T) {
	list := FrozenList()
	if len(list) < 5 {
		t.Errorf("FrozenList len = %d, want >= 5", len(list))
	}
}
