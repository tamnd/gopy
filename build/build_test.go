package build

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestPlatform(t *testing.T) {
	got := Platform()
	want := runtime.GOOS
	switch runtime.GOOS {
	case "windows":
		want = "win32"
	case "wasip1":
		want = "wasi"
	}
	if got != want {
		t.Fatalf("Platform() = %q, want %q", got, want)
	}
}

func TestCompiler(t *testing.T) {
	if !strings.HasPrefix(Compiler(), "go") {
		t.Fatalf("Compiler() = %q, want prefix %q", Compiler(), "go")
	}
}

func TestVersionString(t *testing.T) {
	got := VersionString()
	for _, want := range []string{"gopy ", Version, PythonCompatVersion, runtime.Version()} {
		if !strings.Contains(got, want) {
			t.Errorf("VersionString() = %q, missing %q", got, want)
		}
	}
}

func TestCopyrightHasGopy(t *testing.T) {
	if !strings.Contains(Copyright, "gopy Authors") {
		t.Fatalf("Copyright must reference the gopy Authors")
	}
	if !strings.Contains(Copyright, "Python Software Foundation") {
		t.Fatalf("Copyright must credit the Python Software Foundation")
	}
}
