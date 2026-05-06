package imp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/marshal"
	"github.com/tamnd/gopy/objects"
)

// writeTempPyc writes a minimal .pyc file to a temp file and returns
// the path.
func writeTempPyc(t *testing.T, name string) string {
	t.Helper()
	code := objects.NewCode()
	code.Name = name
	code.Filename = name + ".py"

	f, err := os.CreateTemp(t.TempDir(), "*.pyc")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	if err := marshal.WritePyc(f, code, 0, 0); err != nil {
		t.Fatalf("WritePyc: %v", err)
	}
	return f.Name()
}

// TestLoadPycRoundtrip pins that LoadPyc reads a .pyc, executes its
// code object, and registers the module in sys.modules.
func TestLoadPycRoundtrip(t *testing.T) {
	modName := "_test_load_pyc"
	RemoveModule(modName)

	path := writeTempPyc(t, modName)

	mod, err := LoadPyc(&stubExec{}, path, modName)
	if err != nil {
		t.Fatalf("LoadPyc: %v", err)
	}
	if mod == nil {
		t.Fatal("LoadPyc returned nil module")
	}

	got, ok := GetModule(modName)
	if !ok {
		t.Fatal("module not in sys.modules after LoadPyc")
	}
	if got != mod {
		t.Error("sys.modules entry differs from returned module")
	}
}

// TestLoadPycMissingFile pins that a missing file returns an error.
func TestLoadPycMissingFile(t *testing.T) {
	_, err := LoadPyc(&stubExec{}, "/no/such/file.pyc", "missing")
	if err == nil {
		t.Fatal("expected error for missing .pyc file")
	}
}

// TestLoadSource pins that LoadSource compiles via the provided
// SourceCompiler and executes the code in a module.
func TestLoadSource(t *testing.T) {
	modName := "_test_load_source"
	RemoveModule(modName)

	wantCode := objects.NewCode()
	wantCode.Name = modName
	compiler := func(_, _ string) (*objects.Code, error) {
		return wantCode, nil
	}

	mod, err := LoadSource(&stubExec{}, compiler, "x = 1", "test.py", modName)
	if err != nil {
		t.Fatalf("LoadSource: %v", err)
	}
	if mod == nil {
		t.Fatal("LoadSource returned nil module")
	}
	if _, ok := GetModule(modName); !ok {
		t.Fatal("module not in sys.modules after LoadSource")
	}
}

// TestLoadSourceFile pins that LoadSourceFile reads a .py file and
// passes it to the compiler.
func TestLoadSourceFile(t *testing.T) {
	modName := "_test_load_source_file"
	RemoveModule(modName)

	dir := t.TempDir()
	pyPath := filepath.Join(dir, "test.py")
	if err := os.WriteFile(pyPath, []byte("x = 1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wantCode := objects.NewCode()
	wantCode.Name = modName
	var gotSrc string
	compiler := func(src, _ string) (*objects.Code, error) {
		gotSrc = src
		return wantCode, nil
	}

	_, err := LoadSourceFile(&stubExec{}, compiler, pyPath, modName)
	if err != nil {
		t.Fatalf("LoadSourceFile: %v", err)
	}
	if gotSrc != "x = 1" {
		t.Errorf("compiler received src=%q, want %q", gotSrc, "x = 1")
	}
}
