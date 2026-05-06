package pythonrun

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// writeTempScript drops src into a temp .py file and returns the
// path. The file is cleaned up automatically.
func writeTempScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.py")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunFileEmptySourceReturnsNone(t *testing.T) {
	path := writeTempScript(t, "")
	ts := state.NewThread()
	v, err := RunFile(ts, path, objects.NewDict(), nil)
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if v != objects.None() {
		t.Errorf("got %v, want None", v)
	}
}

func TestRunFileMissingFileReturnsError(t *testing.T) {
	ts := state.NewThread()
	_, err := RunFile(ts, filepath.Join(t.TempDir(), "no-such.py"), objects.NewDict(), nil)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}
}

func TestRunFileSetsFileNameDuringRun(t *testing.T) {
	path := writeTempScript(t, "pass\n")
	g := objects.NewDict()
	ts := state.NewThread()

	if _, err := RunFile(ts, path, g, nil); err != nil {
		if shouldSkipUnimplemented(err) {
			t.Skipf("parser/VM gap: %v", err)
		}
		t.Fatalf("RunFile: %v", err)
	}

	if has, _ := g.Contains(objects.NewStr("__file__")); has {
		t.Error("__file__ leaked out of RunFile (should be popped on the way out)")
	}
	if has, _ := g.Contains(objects.NewStr("__cached__")); has {
		t.Error("__cached__ leaked out of RunFile")
	}
}

func TestRunFilePreservesPreExistingFileName(t *testing.T) {
	path := writeTempScript(t, "pass\n")
	g := objects.NewDict()
	preset := objects.NewStr("preset")
	if err := g.SetItem(objects.NewStr("__file__"), preset); err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()

	if _, err := RunFile(ts, path, g, nil); err != nil {
		if shouldSkipUnimplemented(err) {
			t.Skipf("parser/VM gap: %v", err)
		}
		t.Fatalf("RunFile: %v", err)
	}
	got, err := g.GetItem(objects.NewStr("__file__"))
	if err != nil {
		t.Fatalf("GetItem(__file__): %v", err)
	}
	if got != preset {
		t.Errorf("__file__ overwritten; got %v, want preset", got)
	}
}

func TestRunSimpleFileSuccessReturnsZero(t *testing.T) {
	path := writeTempScript(t, "pass\n")
	var stdout, stderr bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	rc := RunSimpleFile(ts, path, g, &stderr)
	if rc != 0 && !shouldSkipUnimplemented(parserOrCompilerErr(t, "pass\n")) {
		t.Fatalf("rc=%d, stderr=%q", rc, stderr.String())
	}
}

func TestRunSimpleFileMissingFileReturnsOne(t *testing.T) {
	var stderr bytes.Buffer
	ts := state.NewThread()
	rc := RunSimpleFile(ts, filepath.Join(t.TempDir(), "missing.py"), objects.NewDict(), &stderr)
	if rc != 1 {
		t.Fatalf("rc = %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "missing.py") {
		t.Errorf("stderr should mention the missing path, got %q", stderr.String())
	}
}

func TestRunAnyFileDispatchesToSimpleFile(t *testing.T) {
	path := writeTempScript(t, "pass\n")
	var stdout, stderr bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	ts := state.NewThread()
	rc := RunAnyFile(ts, path, g, &stderr)
	if rc != 0 && !shouldSkipUnimplemented(parserOrCompilerErr(t, "pass\n")) {
		t.Fatalf("rc=%d, stderr=%q", rc, stderr.String())
	}
}
