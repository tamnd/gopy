package initconfig

import "testing"

// TestConfigReadEnvOverriddenByCLI exercises the documented
// precedence: env values land first, then CLI options layered on top
// override them.
func TestConfigReadEnvOverriddenByCLI(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONOPTIMIZE", "1")
	t.Setenv("PYTHONVERBOSE", "1")

	c := &PyConfig{}
	c.InitPythonConfig()
	c.Argv = []string{"gopy", "-O", "-O", "-v", "-v", "script.py"}

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	if c.OptimizationLevel != 3 {
		t.Errorf("OptimizationLevel = %d (env=1, CLI -O -O adds 2 -> 3)", c.OptimizationLevel)
	}
	if c.Verbose != 3 {
		t.Errorf("Verbose = %d (env=1, CLI -v -v adds 2 -> 3)", c.Verbose)
	}
	if c.RunFilename != "script.py" {
		t.Errorf("RunFilename = %q", c.RunFilename)
	}
}

// TestConfigReadCLIOverriddenByExplicit exercises the second
// precedence rule: a value set on PyConfig before ConfigRead survives
// the env + CLI passes for the path-style fields.
func TestConfigReadCLIOverriddenByExplicit(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONPATH", "/from/env")

	c := &PyConfig{}
	c.InitPythonConfig()
	c.PythonpathEnv = "/from/code"
	c.Argv = []string{"gopy"}

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	if c.PythonpathEnv != "/from/code" {
		t.Errorf("PythonpathEnv = %q want /from/code", c.PythonpathEnv)
	}
}

func TestConfigReadOrigArgvCopied(t *testing.T) {
	clearAllPythonEnvForTest(t)

	c := &PyConfig{}
	c.InitPythonConfig()
	c.Argv = []string{"gopy", "-c", "print(1)"}

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}

	want := []string{"gopy", "-c", "print(1)"}
	if len(c.OrigArgv) != len(want) {
		t.Fatalf("OrigArgv = %v, want %v", c.OrigArgv, want)
	}
	for i := range want {
		if c.OrigArgv[i] != want[i] {
			t.Errorf("OrigArgv[%d] = %q want %q", i, c.OrigArgv[i], want[i])
		}
	}
}

// TestConfigReadIsolatedForcesFlags exercises the "if isolated, then
// safe_path / use_environment / user_site_directory get forced"
// branch.
func TestConfigReadIsolatedForcesFlags(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONPATH", "/should-not-appear")

	c := &PyConfig{}
	c.InitIsolatedConfig()
	c.Argv = []string{"gopy", "script.py"}

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	if c.SafePath != 1 {
		t.Errorf("SafePath = %d (isolated should force 1)", c.SafePath)
	}
	if c.UseEnvironment != 0 {
		t.Errorf("UseEnvironment = %d (isolated should force 0)", c.UseEnvironment)
	}
	if c.UserSiteDirectory != 0 {
		t.Errorf("UserSiteDirectory = %d (isolated should force 0)", c.UserSiteDirectory)
	}
	if c.PythonpathEnv != "" {
		t.Errorf("PythonpathEnv = %q (isolated should ignore env)", c.PythonpathEnv)
	}
}

// TestConfigReadDefaultsFlipSentinels confirms that any tri-state
// field still at -1 after env + CLI gets flipped to its CPython
// default.
func TestConfigReadDefaultsFlipSentinels(t *testing.T) {
	clearAllPythonEnvForTest(t)

	c := &PyConfig{}
	c.initCompatConfig()
	c.UseEnvironment = 0
	c.ParseArgv = 0

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	want := map[string]int{
		"DevMode":            0,
		"Isolated":           0,
		"SiteImport":         1,
		"BytesWarning":       0,
		"Inspect":            0,
		"Interactive":        0,
		"OptimizationLevel":  0,
		"ParserDebug":        0,
		"WriteBytecode":      1,
		"Verbose":            0,
		"Quiet":              0,
		"UserSiteDirectory":  1,
		"BufferedStdio":      1,
		"PathconfigWarnings": 1,
		"ImportTime":         0,
		"UseHashSeed":        0,
	}
	got := map[string]int{
		"DevMode":            c.DevMode,
		"Isolated":           c.Isolated,
		"SiteImport":         c.SiteImport,
		"BytesWarning":       c.BytesWarning,
		"Inspect":            c.Inspect,
		"Interactive":        c.Interactive,
		"OptimizationLevel":  c.OptimizationLevel,
		"ParserDebug":        c.ParserDebug,
		"WriteBytecode":      c.WriteBytecode,
		"Verbose":            c.Verbose,
		"Quiet":              c.Quiet,
		"UserSiteDirectory":  c.UserSiteDirectory,
		"BufferedStdio":      c.BufferedStdio,
		"PathconfigWarnings": c.PathconfigWarnings,
		"ImportTime":         c.ImportTime,
		"UseHashSeed":        c.UseHashSeed,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %d, want %d", name, got[name], w)
		}
	}
}

func TestConfigReadAppendsCmdlineWarnoptionsAfterExisting(t *testing.T) {
	clearAllPythonEnvForTest(t)

	c := &PyConfig{}
	c.InitPythonConfig()
	c.WarnOptions = []string{"already::set"}
	c.Argv = []string{"gopy", "-W", "ignore", "-W", "default"}

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	want := []string{"already::set", "ignore", "default"}
	if len(c.WarnOptions) != len(want) {
		t.Fatalf("WarnOptions = %v, want %v", c.WarnOptions, want)
	}
	for i := range want {
		if c.WarnOptions[i] != want[i] {
			t.Errorf("WarnOptions[%d] = %q want %q", i, c.WarnOptions[i], want[i])
		}
	}
}

// TestConfigReadEmptyArgvFilledIn confirms that an empty argv after
// CLI parsing gets a single empty string appended, mirroring the
// CPython "Ensure at least one (empty) argument is seen" branch.
func TestConfigReadEmptyArgvFilledIn(t *testing.T) {
	clearAllPythonEnvForTest(t)

	c := &PyConfig{}
	c.InitPythonConfig()
	c.ParseArgv = 0

	if status := ConfigRead(c); status.IsException() {
		t.Fatalf("ConfigRead: %+v", status)
	}
	if len(c.Argv) != 1 || c.Argv[0] != "" {
		t.Fatalf("Argv = %v, want [\"\"]", c.Argv)
	}
}
