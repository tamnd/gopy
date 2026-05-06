package imp

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// TestImportModuleSysModulesCache pins that a module already in
// sys.modules is returned immediately without re-executing.
func TestImportModuleSysModulesCache(t *testing.T) {
	mod := objects.NewModule("_test_imp_cached")
	AddModule("_test_imp_cached", mod)
	defer RemoveModule("_test_imp_cached")

	got, err := ImportModule(&stubExec{}, "_test_imp_cached")
	if err != nil {
		t.Fatalf("ImportModule: %v", err)
	}
	if got != mod {
		t.Error("ImportModule did not return the cached module")
	}
}

// TestImportModuleFrozen pins that a frozen module with a non-nil Code
// is executed and registered in sys.modules.
func TestImportModuleFrozen(t *testing.T) {
	name := "_test_imp_frozen"
	RemoveModule(name)

	code := objects.NewCode()
	code.Name = name
	RegisterFrozen(&FrozenModule{Name: name, Code: code})
	defer func() {
		RegisterFrozen(&FrozenModule{Name: name, Code: nil})
		RemoveModule(name)
	}()

	got, err := ImportModule(&stubExec{}, name)
	if err != nil {
		t.Fatalf("ImportModule frozen: %v", err)
	}
	if got == nil {
		t.Fatal("ImportModule frozen returned nil")
	}
	if _, ok := GetModule(name); !ok {
		t.Error("frozen module not in sys.modules")
	}
}

// TestImportModuleBuiltin pins that a built-in module in the inittab
// is initialized and registered.
func TestImportModuleBuiltin(t *testing.T) {
	name := "_test_imp_builtin"
	RemoveModule(name)
	_ = AppendInittab(name, func() (*objects.Module, error) {
		return objects.NewModule(name), nil
	})
	defer RemoveModule(name)

	got, err := ImportModule(&stubExec{}, name)
	if err != nil {
		t.Fatalf("ImportModule builtin: %v", err)
	}
	if got == nil {
		t.Fatal("ImportModule builtin returned nil")
	}
	if _, ok := GetModule(name); !ok {
		t.Error("builtin module not in sys.modules")
	}
}

// TestImportModuleNotFound pins that an unknown module surfaces
// ErrModuleNotFound.
func TestImportModuleNotFound(t *testing.T) {
	_, err := ImportModule(&stubExec{}, "_no_such_module_xyz_abc")
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
	if !errors.Is(err, ErrModuleNotFound) {
		t.Errorf("error = %v, want wrapping ErrModuleNotFound", err)
	}
}

// TestImportModuleLevelRelative pins resolveAbsName for relative imports.
func TestImportModuleLevelRelative(t *testing.T) {
	cases := []struct {
		name    string
		pkgname string
		level   int
		want    string
	}{
		{"b", "a", 1, "a.b"},
		{"c", "a.b", 1, "a.b.c"},
		{"", "a.b", 1, "a.b"},
		{"c", "a.b.d", 2, "a.b.c"},
	}
	for _, tc := range cases {
		got, err := resolveAbsName(tc.name, tc.pkgname, tc.level)
		if err != nil {
			t.Errorf("resolveAbsName(%q,%q,%d): %v", tc.name, tc.pkgname, tc.level, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveAbsName(%q,%q,%d) = %q, want %q", tc.name, tc.pkgname, tc.level, got, tc.want)
		}
	}
}

// TestImportModuleLevelRelativeTooHigh pins that going beyond the root
// package returns an error.
func TestImportModuleLevelRelativeTooHigh(t *testing.T) {
	_, err := resolveAbsName("x", "a", 3)
	if err == nil {
		t.Fatal("expected error for level > package depth")
	}
}

// TestImportModuleLevelRelativeNoPackage pins that a relative import
// with no pkgname returns an error.
func TestImportModuleLevelRelativeNoPackage(t *testing.T) {
	_, err := resolveAbsName("x", "", 1)
	if err == nil {
		t.Fatal("expected error for relative import with no pkgname")
	}
}
