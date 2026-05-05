package initconfig

import (
	"reflect"
	"testing"
)

func TestConfigCopyValues(t *testing.T) {
	src := &PyConfig{}
	src.InitPythonConfig()
	src.Argv = []string{"gopy", "-c", "print(1)"}
	src.XOptions = []string{"dev"}
	src.WarnOptions = []string{"all"}
	src.ProgramName = "gopy"
	src.PythonpathEnv = "/from/code"
	src.Verbose = 2
	src.HashSeed = 0xdead
	src.checkHashPycsMode = "always"

	dst := &PyConfig{}
	if status := ConfigCopy(dst, src); status.IsException() {
		t.Fatalf("ConfigCopy: %+v", status)
	}

	if !reflect.DeepEqual(dst, src) {
		t.Fatalf("dst != src after ConfigCopy")
	}
}

// TestConfigCopyDecouplesSlices is the load-bearing test: ConfigRead
// mutates dst's argv / orig_argv / warn_options, and we must not
// scribble through to the caller's struct.
func TestConfigCopyDecouplesSlices(t *testing.T) {
	src := &PyConfig{}
	src.InitPythonConfig()
	src.Argv = []string{"gopy", "script.py"}
	src.XOptions = []string{"dev"}
	src.WarnOptions = []string{"already::set"}
	src.ModuleSearchPaths = []string{"/p1"}

	dst := &PyConfig{}
	if status := ConfigCopy(dst, src); status.IsException() {
		t.Fatalf("ConfigCopy: %+v", status)
	}

	dst.Argv[0] = "MUTATED"
	dst.XOptions[0] = "MUTATED"
	dst.WarnOptions[0] = "MUTATED"
	dst.ModuleSearchPaths[0] = "MUTATED"

	if src.Argv[0] != "gopy" {
		t.Errorf("src.Argv[0] = %q, want gopy (slice header shared)", src.Argv[0])
	}
	if src.XOptions[0] != "dev" {
		t.Errorf("src.XOptions[0] = %q, want dev", src.XOptions[0])
	}
	if src.WarnOptions[0] != "already::set" {
		t.Errorf("src.WarnOptions[0] = %q", src.WarnOptions[0])
	}
	if src.ModuleSearchPaths[0] != "/p1" {
		t.Errorf("src.ModuleSearchPaths[0] = %q", src.ModuleSearchPaths[0])
	}
}

func TestConfigCopyClearsDstFirst(t *testing.T) {
	src := &PyConfig{}
	src.InitPythonConfig()

	dst := &PyConfig{}
	dst.Argv = []string{"old"}
	dst.RunCommand = "stale"

	if status := ConfigCopy(dst, src); status.IsException() {
		t.Fatalf("ConfigCopy: %+v", status)
	}

	if dst.RunCommand != "" {
		t.Errorf("RunCommand = %q, want empty (Clear should drop it)", dst.RunCommand)
	}
	if !reflect.DeepEqual(dst.Argv, src.Argv) {
		t.Errorf("dst.Argv = %v, want %v", dst.Argv, src.Argv)
	}
}
