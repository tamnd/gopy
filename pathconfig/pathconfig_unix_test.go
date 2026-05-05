//go:build darwin || linux

package pathconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/pathconfig"
)

// makePrefix builds a synthetic install layout under tmp/{prefix}:
//
//	{prefix}/lib/python3.14/os.py
//	{prefix}/lib/python3.14/lib-dynload/  (directory)
//	{prefix}/lib/python314.zip            (touched as a placeholder)
//
// and returns the absolute path to {prefix}.
func makePrefix(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	stdlib := filepath.Join(root, "lib", "python3.14")
	if err := os.MkdirAll(filepath.Join(stdlib, "lib-dynload"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stdlib, "os.py"), []byte("# synthetic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "python314.zip"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveSynthesizesSysPathFromPrefix(t *testing.T) {
	t.Setenv("PYTHONHOME", "")
	t.Setenv("PYTHONPATH", "")

	root := makePrefix(t)

	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 1
	c.Home = root

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.Prefix != root {
		t.Errorf("Prefix = %q, want %q", c.Prefix, root)
	}
	if c.ExecPrefix != root {
		t.Errorf("ExecPrefix = %q, want %q", c.ExecPrefix, root)
	}
	if c.BasePrefix != root {
		t.Errorf("BasePrefix = %q, want %q", c.BasePrefix, root)
	}
	if c.BaseExecPrefix != root {
		t.Errorf("BaseExecPrefix = %q, want %q", c.BaseExecPrefix, root)
	}

	wantStdlib := filepath.Join(root, "lib", "python3.14")
	if c.StdlibDir != wantStdlib {
		t.Errorf("StdlibDir = %q, want %q", c.StdlibDir, wantStdlib)
	}

	wantPaths := []string{
		filepath.Join(root, "lib", "python314.zip"),
		filepath.Join(root, "lib", "python3.14"),
		filepath.Join(root, "lib", "python3.14", "lib-dynload"),
	}
	if got := c.ModuleSearchPaths; len(got) != len(wantPaths) {
		t.Fatalf("ModuleSearchPaths = %v, want %v", got, wantPaths)
	}
	for i, want := range wantPaths {
		if c.ModuleSearchPaths[i] != want {
			t.Errorf("ModuleSearchPaths[%d] = %q, want %q", i, c.ModuleSearchPaths[i], want)
		}
	}
	if c.ModuleSearchPathsSet != 1 {
		t.Errorf("ModuleSearchPathsSet = %d, want 1", c.ModuleSearchPathsSet)
	}
}

func TestResolvePythonpathEnvLandsFirst(t *testing.T) {
	root := makePrefix(t)

	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 1
	c.Home = root
	c.PythonpathEnv = "/from/env:/another"

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if len(c.ModuleSearchPaths) < 2 {
		t.Fatalf("ModuleSearchPaths too short: %v", c.ModuleSearchPaths)
	}
	if c.ModuleSearchPaths[0] != "/from/env" {
		t.Errorf("ModuleSearchPaths[0] = %q, want /from/env", c.ModuleSearchPaths[0])
	}
	if c.ModuleSearchPaths[1] != "/another" {
		t.Errorf("ModuleSearchPaths[1] = %q, want /another", c.ModuleSearchPaths[1])
	}
}

func TestResolveHonorsExplicitOverrides(t *testing.T) {
	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 0
	c.Prefix = "/custom/prefix"
	c.ExecPrefix = "/custom/exec"
	c.StdlibDir = "/custom/stdlib"
	c.ModuleSearchPathsSet = 1
	c.ModuleSearchPaths = []string{"/explicit"}

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.Prefix != "/custom/prefix" {
		t.Errorf("Prefix = %q", c.Prefix)
	}
	if c.ExecPrefix != "/custom/exec" {
		t.Errorf("ExecPrefix = %q", c.ExecPrefix)
	}
	if c.StdlibDir != "/custom/stdlib" {
		t.Errorf("StdlibDir = %q", c.StdlibDir)
	}
	if len(c.ModuleSearchPaths) != 1 || c.ModuleSearchPaths[0] != "/explicit" {
		t.Errorf("ModuleSearchPaths = %v, want [/explicit]", c.ModuleSearchPaths)
	}
}

func TestResolvePYTHONHOMEFromEnv(t *testing.T) {
	root := makePrefix(t)
	t.Setenv("PYTHONHOME", root)

	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 1

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.Home != root {
		t.Errorf("Home = %q, want %q", c.Home, root)
	}
	if c.Prefix != root {
		t.Errorf("Prefix = %q, want %q (from env-supplied home)", c.Prefix, root)
	}
}

func TestResolvePYTHONHOMEPrefixExecSplit(t *testing.T) {
	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 0
	c.Home = "/p:/x"

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.Prefix != "/p" {
		t.Errorf("Prefix = %q, want /p", c.Prefix)
	}
	if c.ExecPrefix != "/x" {
		t.Errorf("ExecPrefix = %q, want /x", c.ExecPrefix)
	}
}

func TestResolvePlatlibdirDefault(t *testing.T) {
	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 0
	c.Home = "/p"

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.Platlibdir != "lib" {
		t.Errorf("Platlibdir = %q, want lib", c.Platlibdir)
	}
	if c.StdlibDir != "/p/lib/python3.14" {
		t.Errorf("StdlibDir = %q", c.StdlibDir)
	}
}

func TestResolveUsesProgramNameFromOrigArgv(t *testing.T) {
	c := &initconfig.PyConfig{}
	c.InitPythonConfig()
	c.UseEnvironment = 0
	c.OrigArgv = []string{"./mygopy", "script.py"}
	c.Home = "/p"

	if status := pathconfig.Resolve(c); status.IsException() {
		t.Fatalf("Resolve: %+v", status)
	}

	if c.ProgramName != "./mygopy" {
		t.Errorf("ProgramName = %q, want ./mygopy", c.ProgramName)
	}
	abs, err := filepath.Abs("./mygopy")
	if err != nil {
		t.Fatal(err)
	}
	if c.Executable != abs {
		t.Errorf("Executable = %q, want %q", c.Executable, abs)
	}
}
