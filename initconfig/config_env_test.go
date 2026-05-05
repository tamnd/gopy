package initconfig

import "testing"

func clearAllPythonEnvForTest(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PYTHONHOME",
		"PYTHONPATH",
		"PYTHONHASHSEED",
		"PYTHONDONTWRITEBYTECODE",
		"PYTHONUNBUFFERED",
		"PYTHONUTF8",
		"PYTHONDEBUG",
		"PYTHONVERBOSE",
		"PYTHONOPTIMIZE",
		"PYTHONNOUSERSITE",
		"PYTHONDEVMODE",
		"PYTHONINSPECT",
		"PYTHONSAFEPATH",
		"PYTHONPLATLIBDIR",
	} {
		clearEnv(t, name)
	}
}

func TestConfigReadEnvVarsHonoursUseEnvironmentZero(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONDEBUG", "5")
	t.Setenv("PYTHONPATH", "/x")

	var c PyConfig
	c.InitPythonConfig()
	c.UseEnvironment = 0

	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.ParserDebug != 0 {
		t.Errorf("ParserDebug = %d, want 0 when UseEnvironment=0", c.ParserDebug)
	}
	if c.PythonpathEnv != "" {
		t.Errorf("PythonpathEnv = %q, want empty when UseEnvironment=0", c.PythonpathEnv)
	}
}

func TestConfigReadEnvVarsAppliesPythonFlags(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONDEBUG", "2")
	t.Setenv("PYTHONVERBOSE", "1")
	t.Setenv("PYTHONOPTIMIZE", "3")
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("PYTHONNOUSERSITE", "1")
	t.Setenv("PYTHONUNBUFFERED", "1")
	t.Setenv("PYTHONINSPECT", "1")
	t.Setenv("PYTHONSAFEPATH", "1")
	t.Setenv("PYTHONPATH", "/opt")
	t.Setenv("PYTHONPLATLIBDIR", "lib64")

	var c PyConfig
	c.InitPythonConfig()
	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.ParserDebug != 2 {
		t.Errorf("ParserDebug = %d, want 2", c.ParserDebug)
	}
	if c.Verbose != 1 {
		t.Errorf("Verbose = %d, want 1", c.Verbose)
	}
	if c.OptimizationLevel != 3 {
		t.Errorf("OptimizationLevel = %d, want 3", c.OptimizationLevel)
	}
	if c.WriteBytecode != 0 {
		t.Errorf("WriteBytecode = %d, want 0 (DONTWRITEBYTECODE flips it)", c.WriteBytecode)
	}
	if c.UserSiteDirectory != 0 {
		t.Errorf("UserSiteDirectory = %d, want 0", c.UserSiteDirectory)
	}
	if c.BufferedStdio != 0 {
		t.Errorf("BufferedStdio = %d, want 0", c.BufferedStdio)
	}
	if c.Inspect != 1 {
		t.Errorf("Inspect = %d, want 1", c.Inspect)
	}
	if c.SafePath != 1 {
		t.Errorf("SafePath = %d, want 1", c.SafePath)
	}
	if c.PythonpathEnv != "/opt" {
		t.Errorf("PythonpathEnv = %q", c.PythonpathEnv)
	}
	if c.Platlibdir != "lib64" {
		t.Errorf("Platlibdir = %q", c.Platlibdir)
	}
}

// TestConfigReadEnvVarsDoesNotOverwriteExplicitPythonpath verifies the
// "explicit writes survive" rule for the string fields.
func TestConfigReadEnvVarsDoesNotOverwriteExplicitPythonpath(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONPATH", "/from/env")

	var c PyConfig
	c.InitPythonConfig()
	c.PythonpathEnv = "/from/code"

	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.PythonpathEnv != "/from/code" {
		t.Fatalf("PythonpathEnv = %q, want %q", c.PythonpathEnv, "/from/code")
	}
}

func TestConfigReadEnvVarsHashSeedRandom(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONHASHSEED", "random")

	var c PyConfig
	c.InitPythonConfig()
	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.UseHashSeed != 0 || c.HashSeed != 0 {
		t.Fatalf("random seed: UseHashSeed=%d HashSeed=%d", c.UseHashSeed, c.HashSeed)
	}
}

func TestConfigReadEnvVarsHashSeedExplicit(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONHASHSEED", "12345")

	var c PyConfig
	c.InitPythonConfig()
	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.UseHashSeed != 1 || c.HashSeed != 12345 {
		t.Fatalf("explicit seed: UseHashSeed=%d HashSeed=%d", c.UseHashSeed, c.HashSeed)
	}
}

func TestConfigReadEnvVarsHashSeedRejectsBadValue(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONHASHSEED", "12345abc")

	var c PyConfig
	c.InitPythonConfig()
	status := ConfigReadEnvVars(&c)
	if !status.IsError() {
		t.Fatalf("expected error, got %+v", status)
	}
	if status.ErrMsg == "" {
		t.Fatalf("expected error message")
	}
}

func TestConfigReadEnvVarsHashSeedRejectsOutOfRange(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONHASHSEED", "4294967296")

	var c PyConfig
	c.InitPythonConfig()
	if status := ConfigReadEnvVars(&c); !status.IsError() {
		t.Fatalf("expected error for out-of-range seed, got %+v", status)
	}
}

// TestConfigReadEnvVarsLeavesUseHashSeedAloneIfPreset ensures that
// callers who set UseHashSeed before Read do not get overwritten by
// the env-derived value.
func TestConfigReadEnvVarsLeavesUseHashSeedAloneIfPreset(t *testing.T) {
	clearAllPythonEnvForTest(t)
	t.Setenv("PYTHONHASHSEED", "9999")

	var c PyConfig
	c.InitPythonConfig()
	c.UseHashSeed = 0
	c.HashSeed = 0

	if status := ConfigReadEnvVars(&c); status.IsException() {
		t.Fatalf("ConfigReadEnvVars: %+v", status)
	}
	if c.UseHashSeed != 0 || c.HashSeed != 0 {
		t.Fatalf("explicit UseHashSeed=0 should suppress env read: got UseHashSeed=%d HashSeed=%d", c.UseHashSeed, c.HashSeed)
	}
}
