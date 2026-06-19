// Tests for the os module shim.

package os

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

// buildOSDict calls buildOS and returns its dict for direct testing.
func buildOSDict(t *testing.T) *objects.Dict {
	t.Helper()
	mod, err := buildOS()
	if err != nil {
		t.Fatalf("buildOS: %v", err)
	}
	return mod.Dict()
}

func callFn(t *testing.T, d *objects.Dict, name string, args []objects.Object) objects.Object {
	t.Helper()
	v, err := d.GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("os.%s: %v", name, err)
	}
	bf, ok := v.(*objects.BuiltinFunction)
	if !ok {
		t.Fatalf("os.%s is %T, want *BuiltinFunction", name, v)
	}
	result, err := bf.Fn(args, nil)
	if err != nil {
		t.Fatalf("os.%s(...): %v", name, err)
	}
	return result
}

func TestGetcwd(t *testing.T) {
	d := buildOSDict(t)
	result := callFn(t, d, "getcwd", nil)
	got, err := objects.Str(result)
	if err != nil {
		t.Fatalf("Str: %v", err)
	}
	want, _ := os.Getwd()
	if got != want {
		t.Errorf("getcwd() = %q, want %q", got, want)
	}
}

func TestListdir(t *testing.T) {
	d := buildOSDict(t)
	tmpDir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result := callFn(t, d, "listdir", []objects.Object{objects.NewStr(tmpDir)})
	lst, ok := result.(*objects.List)
	if !ok {
		t.Fatalf("listdir returned %T, want *List", result)
	}
	if lst.Len() != 2 {
		t.Errorf("listdir length = %d, want 2", lst.Len())
	}
}

func TestStat(t *testing.T) {
	d := buildOSDict(t)
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "file.txt")
	content := []byte("hello")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	result := callFn(t, d, "stat", []objects.Object{objects.NewStr(path)})
	ss, ok := result.(*objects.StructSeq)
	if !ok {
		t.Fatalf("stat returned %T, want *StructSeq", result)
	}
	// st_size is at index 6.
	sizeInt, ok := ss.Items()[6].(*objects.Int)
	if !ok {
		t.Fatalf("st_size is %T, want *Int", ss.Items()[6])
	}
	if sizeInt.BigInt().Int64() != int64(len(content)) {
		t.Errorf("st_size = %d, want %d", sizeInt.BigInt().Int64(), len(content))
	}
}

func TestMkdirRmdir(t *testing.T) {
	d := buildOSDict(t)
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "newdir")

	callFn(t, d, "mkdir", []objects.Object{objects.NewStr(newDir)})
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("mkdir did not create directory: %v", err)
	}

	callFn(t, d, "rmdir", []objects.Object{objects.NewStr(newDir)})
	if _, err := os.Stat(newDir); err == nil {
		t.Error("rmdir did not remove directory")
	}
}

func TestEnviron(t *testing.T) {
	d := buildOSDict(t)
	env, err := d.GetItem(objects.NewStr("environ"))
	if err != nil {
		t.Fatalf("environ: %v", err)
	}
	envDict, ok := env.(*objects.Dict)
	if !ok {
		t.Fatalf("environ is %T, want *Dict", env)
	}
	// On POSIX, posix.environ holds bytes keys/values (Lib/os.py decodes
	// them); the nt build keeps str. Look the key up the same way.
	keyObj := func(s string) objects.Object {
		if runtime.GOOS == "windows" {
			return objects.NewStr(s)
		}
		return objects.NewBytes([]byte(s))
	}
	pathKey := "PATH"
	if runtime.GOOS == "windows" {
		if _, err2 := envDict.GetItem(keyObj("Path")); err2 == nil {
			pathKey = "Path"
		}
	}
	v, err := envDict.GetItem(keyObj(pathKey))
	if err != nil {
		t.Fatalf("environ[%q]: %v", pathKey, err)
	}
	got, _ := objects.Str(v)
	sep := string(os.PathListSeparator)
	if !strings.Contains(got, sep) && got == "" {
		t.Errorf("PATH/Path looks wrong: %q", got)
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("GOPY_TEST_VAR", "hello")
	// Build after setenv so the function reads the live env.
	d := buildOSDict(t)
	result := callFn(t, d, "getenv", []objects.Object{objects.NewStr("GOPY_TEST_VAR")})
	got, _ := objects.Str(result)
	if got != "hello" {
		t.Errorf("getenv('GOPY_TEST_VAR') = %q, want 'hello'", got)
	}

	// Missing key with no default returns None.
	result2 := callFn(t, d, "getenv", []objects.Object{objects.NewStr("GOPY_NO_SUCH_VAR_XYZ")})
	if result2 != objects.None() {
		t.Errorf("getenv missing = %v, want None", result2)
	}
}

func TestGetpid(t *testing.T) {
	d := buildOSDict(t)
	result := callFn(t, d, "getpid", nil)
	pidInt, ok := result.(*objects.Int)
	if !ok {
		t.Fatalf("getpid returned %T, want *Int", result)
	}
	if pidInt.BigInt().Int64() != int64(os.Getpid()) {
		t.Errorf("getpid() = %d, want %d", pidInt.BigInt().Int64(), os.Getpid())
	}
}
