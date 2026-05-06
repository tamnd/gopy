package imp

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// recordingExec lets a test see how many times ExecCode was called and
// which code object was last passed.
type recordingExec struct {
	calls   int
	lastTag string
}

func (r *recordingExec) ExecCode(code *objects.Code, _ *objects.Module) (objects.Object, error) {
	r.calls++
	if code != nil {
		r.lastTag = code.Name
	}
	return objects.None(), nil
}

// TestReloadModuleNilArgRejected pins that a nil module is rejected.
func TestReloadModuleNilArgRejected(t *testing.T) {
	if _, err := ReloadModule(&stubExec{}, nil); err == nil {
		t.Fatal("expected error for nil module")
	}
}

// TestReloadModuleMissingNameRejected pins that a module without
// __name__ in its dict is rejected with TypeError.
func TestReloadModuleMissingNameRejected(t *testing.T) {
	mod := objects.NewModule("temp")
	_ = mod.Dict().DelItem(objects.NewStr("__name__"))
	if _, err := ReloadModule(&stubExec{}, mod); err == nil {
		t.Fatal("expected TypeError for module with no __name__")
	}
}

// TestReloadModuleNotInSysModules pins that a module that no longer
// matches sys.modules[name] surfaces ImportError.
func TestReloadModuleNotInSysModules(t *testing.T) {
	mod := objects.NewModule("_test_reload_orphan")
	RemoveModule("_test_reload_orphan")
	if _, err := ReloadModule(&stubExec{}, mod); err == nil {
		t.Fatal("expected ImportError when module missing from sys.modules")
	}
}

// TestReloadModuleSysModulesIdentityCheck pins that a stale module
// pointer (sys.modules has a different object under the same name) is
// rejected.
func TestReloadModuleSysModulesIdentityCheck(t *testing.T) {
	stale := objects.NewModule("_test_reload_stale")
	current := objects.NewModule("_test_reload_stale")
	AddModule("_test_reload_stale", current)
	defer RemoveModule("_test_reload_stale")

	if _, err := ReloadModule(&stubExec{}, stale); err == nil {
		t.Fatal("expected ImportError when sys.modules has a different module")
	}
}

// TestReloadModuleFrozen pins that a frozen module is re-executed and
// the existing module pointer is returned.
func TestReloadModuleFrozen(t *testing.T) {
	name := "_test_reload_frozen"
	RemoveModule(name)

	code := objects.NewCode()
	code.Name = name
	RegisterFrozen(&FrozenModule{Name: name, Code: code})
	defer func() {
		RegisterFrozen(&FrozenModule{Name: name, Code: nil})
		RemoveModule(name)
	}()

	rec := &recordingExec{}
	mod, err := ImportModule(rec, name)
	if err != nil {
		t.Fatalf("initial import: %v", err)
	}
	firstCalls := rec.calls

	got, err := ReloadModule(rec, mod)
	if err != nil {
		t.Fatalf("ReloadModule: %v", err)
	}
	if got != mod {
		t.Error("ReloadModule returned a different module pointer for frozen reload")
	}
	if rec.calls != firstCalls+1 {
		t.Errorf("ExecCode calls = %d, want %d", rec.calls, firstCalls+1)
	}
	if rec.lastTag != name {
		t.Errorf("ExecCode last code = %q, want %q", rec.lastTag, name)
	}
}

// TestReloadModuleBuiltin pins that a built-in module's init is called
// again and its dict is folded back into the cached module pointer.
func TestReloadModuleBuiltin(t *testing.T) {
	name := "_test_reload_builtin"
	RemoveModule(name)
	calls := 0
	_ = AppendInittab(name, func() (*objects.Module, error) {
		calls++
		m := objects.NewModule(name)
		_ = m.Dict().SetItem(objects.NewStr("counter"), objects.NewInt(int64(calls)))
		return m, nil
	})
	defer RemoveModule(name)

	mod, err := ImportModule(&stubExec{}, name)
	if err != nil {
		t.Fatalf("initial import: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after import = %d, want 1", calls)
	}

	got, err := ReloadModule(&stubExec{}, mod)
	if err != nil {
		t.Fatalf("ReloadModule: %v", err)
	}
	if got != mod {
		t.Error("ReloadModule did not preserve the module pointer for builtin reload")
	}
	if calls != 2 {
		t.Errorf("init calls = %d, want 2 after reload", calls)
	}
	v, err := mod.Dict().GetItem(objects.NewStr("counter"))
	if err != nil {
		t.Fatalf("counter missing: %v", err)
	}
	n, _ := v.(*objects.Int).Int64()
	if n != 2 {
		t.Errorf("counter = %d, want 2", n)
	}
}

// TestReloadModuleNoSourceRejected pins that a module that is neither
// frozen nor in the inittab cannot be reloaded.
func TestReloadModuleNoSourceRejected(t *testing.T) {
	name := "_test_reload_loose"
	mod := objects.NewModule(name)
	AddModule(name, mod)
	defer RemoveModule(name)

	_, err := ReloadModule(&stubExec{}, mod)
	if err == nil {
		t.Fatal("expected error for module with no frozen or builtin source")
	}
	if !errors.Is(err, ErrModuleNotFound) {
		t.Errorf("error = %v, want wrapping ErrModuleNotFound", err)
	}
}
