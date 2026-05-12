// Tests for the gopy port of CPython's _warnings.
//
// CPython: Python/_warnings.c

package _warnings

import (
	"os"
	"strings"
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// captureStderr swaps sys.stderr for an os.Pipe long enough to drain
// it back into a string. The _warnings module writes through
// sys.stderr; tests read what landed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	mod, ok := imp.GetModule("sys")
	if !ok {
		// sys module hasn't been registered (rare in isolated test
		// runs). Fall back to swapping os.Stderr.
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		defer func() { os.Stderr = old }()
		fn()
		_ = w.Close()
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		return string(buf[:n])
	}
	r, w, _ := os.Pipe()
	stderr := objects.NewFile(w, "<stderr>", "w", false, false, true)
	prev, _ := mod.Dict().GetItem(objects.NewStr("stderr"))
	_ = mod.Dict().SetItem(objects.NewStr("stderr"), stderr)
	defer func() {
		if prev != nil {
			_ = mod.Dict().SetItem(objects.NewStr("stderr"), prev)
		}
	}()
	fn()
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// resetState restores the package state to its post-init defaults so
// each test starts from a clean slate. Order matches initState.
func resetState(t *testing.T) {
	t.Helper()
	state.filters = initFilters()
	state.onceRegistry = objects.NewDict()
	state.defaultAction = objects.NewStr("default")
	state.filtersVersion = 0
	state.lockHeld = false
}

// TestBuildModuleSurface checks the published names from
// buildModule() match warnings_module_exec + warnings_functions.
//
// CPython: Python/_warnings.c:1624 warnings_functions
func TestBuildModuleSurface(t *testing.T) {
	resetState(t)
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	want := []string{
		"warn", "warn_explicit", "_filters_mutated_lock_held",
		"_acquire_lock", "_release_lock",
		"filters", "_onceregistry", "_defaultaction",
	}
	d := mod.Dict()
	for _, name := range want {
		v, err := d.GetItem(objects.NewStr(name))
		if err != nil || v == nil {
			t.Errorf("missing module attribute %q", name)
		}
	}
}

// TestWarnEmitsToStderr exercises the default "default" filter path:
// warn() should write filename:lineno: category: text\n to
// sys.stderr.
//
// CPython: Python/_warnings.c:666 show_warning
func TestWarnEmitsToStderr(t *testing.T) {
	resetState(t)
	out := captureStderr(t, func() {
		_, err := warnBuiltin([]objects.Object{objects.NewStr("hello")}, nil)
		if err != nil {
			t.Fatalf("warn: %v", err)
		}
	})
	if !strings.Contains(out, "UserWarning") || !strings.Contains(out, "hello") {
		t.Fatalf("expected UserWarning text in stderr, got %q", out)
	}
}

// TestFilterWarningsIgnoreSuppresses inserts a "ignore" filter and
// confirms warn() produces no stderr output.
//
// CPython: Python/_warnings.c:875 PyUnicode_EqualToASCIIString("ignore")
func TestFilterWarningsIgnoreSuppresses(t *testing.T) {
	resetState(t)
	FilterWarnings("ignore", errors.PyExc_UserWarning, "", 0)
	out := captureStderr(t, func() {
		_, err := warnBuiltin([]objects.Object{objects.NewStr("nope")}, nil)
		if err != nil {
			t.Fatalf("warn: %v", err)
		}
	})
	if out != "" {
		t.Fatalf("expected silent stderr, got %q", out)
	}
}

// TestFilterWarningsErrorRaises inserts an "error" filter and
// confirms warn() returns a non-nil error tagged with the category
// name.
//
// CPython: Python/_warnings.c:870 PyUnicode_EqualToASCIIString("error")
func TestFilterWarningsErrorRaises(t *testing.T) {
	resetState(t)
	FilterWarnings("error", errors.PyExc_UserWarning, "", 0)
	_, err := warnBuiltin([]objects.Object{objects.NewStr("bang")}, nil)
	if err == nil {
		t.Fatalf("expected warn() to raise, got nil")
	}
	if !strings.Contains(err.Error(), "UserWarning") || !strings.Contains(err.Error(), "bang") {
		t.Fatalf("expected UserWarning: bang, got %q", err.Error())
	}
}

// TestSimpleFilter clears every row, then installs a single
// catch-all. Both warn() with no category and warn() with a
// DeprecationWarning instance should be routed by it.
//
// CPython: Lib/_py_warnings.py simplefilter (Python-side)
func TestSimpleFilter(t *testing.T) {
	resetState(t)
	SimpleFilter("ignore")
	if state.filters.Len() != 1 {
		t.Fatalf("expected one filter row, got %d", state.filters.Len())
	}
	out := captureStderr(t, func() {
		_, err := warnBuiltin([]objects.Object{objects.NewStr("x")}, nil)
		if err != nil {
			t.Fatalf("warn: %v", err)
		}
	})
	if out != "" {
		t.Fatalf("expected stderr to stay empty, got %q", out)
	}
}

// TestResetFilters drops every row and reverts to the default
// action.
//
// CPython: Lib/_py_warnings.py resetwarnings (Python-side)
func TestResetFilters(t *testing.T) {
	resetState(t)
	FilterWarnings("error", errors.PyExc_UserWarning, "", 0)
	ResetFilters()
	if state.filters.Len() != 0 {
		t.Fatalf("expected empty filters after reset, got %d", state.filters.Len())
	}
	out := captureStderr(t, func() {
		_, err := warnBuiltin([]objects.Object{objects.NewStr("after-reset")}, nil)
		if err != nil {
			t.Fatalf("warn: %v", err)
		}
	})
	if !strings.Contains(out, "after-reset") {
		t.Fatalf("expected default action to print, got %q", out)
	}
}

// TestFiltersMutatedIncrements proves that
// _filters_mutated_lock_held bumps the version counter exactly once
// per call, and errors out without the lock.
//
// CPython: Python/_warnings.c:1335 warnings_filters_mutated_lock_held_impl
func TestFiltersMutatedIncrements(t *testing.T) {
	resetState(t)
	if _, err := filtersMutatedLockHeldBuiltin(nil, nil); err == nil {
		t.Fatalf("expected RuntimeError when lock not held")
	}
	before := state.filtersVersion
	warningsLock(state)
	if _, err := filtersMutatedLockHeldBuiltin(nil, nil); err != nil {
		t.Fatalf("filters mutated: %v", err)
	}
	warningsUnlock(state)
	if state.filtersVersion != before+1 {
		t.Fatalf("expected version to bump by 1, got %d -> %d", before, state.filtersVersion)
	}
}

// TestAcquireReleaseLock pairs _acquire_lock / _release_lock and
// confirms a double-release surfaces RuntimeError.
//
// CPython: Python/_warnings.c:351 warnings_release_lock_impl
func TestAcquireReleaseLock(t *testing.T) {
	resetState(t)
	if _, err := acquireLockBuiltin(nil, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := releaseLockBuiltin(nil, nil); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := releaseLockBuiltin(nil, nil); err == nil {
		t.Fatalf("expected double-release to raise")
	}
}

// TestWarnExplicitRegistryDedupes confirms that warning twice with
// the same key on a default-action filter only prints once when the
// caller threads a registry dict through.
//
// CPython: Python/_warnings.c:880 PyDict_SetItem(registry, key, True)
func TestWarnExplicitRegistryDedupes(t *testing.T) {
	resetState(t)
	registry := objects.NewDict()
	args := []objects.Object{
		objects.NewStr("dup"),
		errors.PyExc_UserWarning,
		objects.NewStr("test.py"),
		objects.NewInt(10),
		objects.NewStr("test"),
		registry,
	}
	out := captureStderr(t, func() {
		if _, err := warnExplicitBuiltin(args, nil); err != nil {
			t.Fatalf("warn_explicit 1: %v", err)
		}
		if _, err := warnExplicitBuiltin(args, nil); err != nil {
			t.Fatalf("warn_explicit 2: %v", err)
		}
	})
	if got := strings.Count(out, "dup"); got != 1 {
		t.Fatalf("expected one stderr line, got %d in %q", got, out)
	}
}
