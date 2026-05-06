package imp

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// stubExec is a test-only Executor that runs a code object by calling
// its name attribute as a no-op.
type stubExec struct{}

func (s *stubExec) ExecCode(_ *objects.Code, _ *objects.Module) (objects.Object, error) {
	return objects.None(), nil
}

// TestInitImportlibNotReady pins that ErrBootstrapNotReady is returned
// when the frozen _frozen_importlib code is nil (placeholder state).
func TestInitImportlibNotReady(t *testing.T) {
	fm, ok := FindFrozen("_frozen_importlib")
	if !ok {
		t.Skip("_frozen_importlib not registered")
	}
	if fm.Code != nil {
		t.Skip("_frozen_importlib already has code; skipping placeholder test")
	}

	sys := objects.NewModule("sys")
	err := InitImportlib(&stubExec{}, sys)
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Errorf("InitImportlib with nil code = %v, want ErrBootstrapNotReady", err)
	}
}

// TestInitImportlibExternalNotReady pins the same for the external module.
func TestInitImportlibExternalNotReady(t *testing.T) {
	fm, ok := FindFrozen("_frozen_importlib_external")
	if !ok {
		t.Skip("_frozen_importlib_external not registered")
	}
	if fm.Code != nil {
		t.Skip("_frozen_importlib_external already has code; skipping placeholder test")
	}

	// Seed a fake bootstrap module so the check passes.
	AddModule("_frozen_importlib", objects.NewModule("_frozen_importlib"))
	err := InitImportlibExternal(&stubExec{})
	if !errors.Is(err, ErrBootstrapNotReady) {
		t.Errorf("InitImportlibExternal with nil code = %v, want ErrBootstrapNotReady", err)
	}
}

// TestInitImportlibWithCode pins that when Code is set, InitImportlib
// executes it and registers the module in sys.modules.
func TestInitImportlibWithCode(t *testing.T) {
	code := objects.NewCode()
	code.Name = "_frozen_importlib"

	// Temporarily replace the frozen entry.
	orig, hadOrig := FindFrozen("_frozen_importlib")
	RegisterFrozen(&FrozenModule{Name: "_frozen_importlib", Code: code})
	defer func() {
		if hadOrig {
			RegisterFrozen(orig)
		}
	}()

	// Remove any pre-existing sys.modules entry.
	RemoveModule("_frozen_importlib")

	sys := objects.NewModule("sys")
	err := InitImportlib(&stubExec{}, sys)
	if err != nil {
		t.Fatalf("InitImportlib: %v", err)
	}
	if _, ok := GetModule("_frozen_importlib"); !ok {
		t.Error("_frozen_importlib not in sys.modules after InitImportlib")
	}
}

// TestInitImportlibExternalMissingBootstrap pins that calling
// InitImportlibExternal without _frozen_importlib in sys.modules
// returns a descriptive error.
func TestInitImportlibExternalMissingBootstrap(t *testing.T) {
	RemoveModule("_frozen_importlib")
	err := InitImportlibExternal(&stubExec{})
	if err == nil {
		t.Fatal("expected error when _frozen_importlib absent from sys.modules")
	}
}
