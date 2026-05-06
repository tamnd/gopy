package imp

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// errExec is a test Executor that always returns the given error.
type errExec struct{ err error }

func (e *errExec) ExecCode(_ *objects.Code, _ *objects.Module) (objects.Object, error) {
	return nil, e.err
}

// TestExecCodeModuleRegisters pins that ExecCodeModule adds the module
// to sys.modules.
func TestExecCodeModuleRegisters(t *testing.T) {
	RemoveModule("_test_exec_reg")
	code := objects.NewCode()
	code.Name = "_test_exec_reg"

	mod, err := ExecCodeModule(&stubExec{}, "_test_exec_reg", code)
	if err != nil {
		t.Fatalf("ExecCodeModule: %v", err)
	}
	if mod == nil {
		t.Fatal("ExecCodeModule returned nil module")
	}

	got, ok := GetModule("_test_exec_reg")
	if !ok {
		t.Fatal("module not found in sys.modules after ExecCodeModule")
	}
	if got != mod {
		t.Error("sys.modules entry differs from returned module")
	}
}

// TestExecCodeModuleRemovesOnError pins that a failed execution
// removes the partial module from sys.modules.
func TestExecCodeModuleRemovesOnError(t *testing.T) {
	RemoveModule("_test_exec_fail")
	code := objects.NewCode()
	code.Name = "_test_exec_fail"

	execErr := errors.New("exec failed")
	_, err := ExecCodeModule(&errExec{execErr}, "_test_exec_fail", code)
	if err == nil {
		t.Fatal("expected error from ExecCodeModule")
	}
	if !errors.Is(err, execErr) {
		t.Errorf("error = %v, want wrapping %v", err, execErr)
	}
	if _, ok := GetModule("_test_exec_fail"); ok {
		t.Error("partial module left in sys.modules after exec failure")
	}
}

// TestExecCodeModuleReusesExisting pins that an already-present module
// in sys.modules is reused (not replaced).
func TestExecCodeModuleReusesExisting(t *testing.T) {
	existing := objects.NewModule("_test_exec_reuse")
	AddModule("_test_exec_reuse", existing)

	code := objects.NewCode()
	code.Name = "_test_exec_reuse"

	got, err := ExecCodeModule(&stubExec{}, "_test_exec_reuse", code)
	if err != nil {
		t.Fatalf("ExecCodeModule: %v", err)
	}
	if got != existing {
		t.Error("ExecCodeModule did not reuse the existing module")
	}
}

// TestExecCodeModuleSetsDunders pins that __loader__ and __spec__ are
// set on the module dict.
func TestExecCodeModuleSetsDunders(t *testing.T) {
	RemoveModule("_test_exec_dunders")
	code := objects.NewCode()
	code.Name = "_test_exec_dunders"

	mod, err := ExecCodeModule(&stubExec{}, "_test_exec_dunders", code)
	if err != nil {
		t.Fatalf("ExecCodeModule: %v", err)
	}

	d := mod.Dict()
	for _, key := range []string{"__loader__", "__spec__"} {
		v, err := d.GetItem(objects.NewStr(key))
		if err != nil {
			t.Errorf("GetItem(%q): %v", key, err)
			continue
		}
		if v == nil {
			t.Errorf("%q is missing from module dict", key)
		}
	}
}
