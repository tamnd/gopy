package initconfig

import "strings"

// ConfigRead applies the env-vars, the CLI args, and the layered
// defaults to c. Mirrors _PyConfig_Read: pre-init bridge, argv copy,
// isolated-mode forced flips, env-var read, CLI parse, default
// fallthrough.
//
// The caller seeds c via PyConfig.InitPythonConfig (or
// InitIsolatedConfig) and stamps any explicit overrides before
// calling ConfigRead. After ConfigRead returns OK, c is in a
// consistent state ready for pyinit_core to consume.
//
// Resolution order:
//
//  1. Environment variables (only when use_environment is set).
//  2. Command-line arguments.
//  3. Layered defaults for any field still at its tri-state -1
//     sentinel.
//
// Explicit writes by the caller win over both env and CLI: env reads
// only fill empty string slots, CLI options use ++ for counters that
// merge with existing values, and default fallthrough only flips -1
// sentinels.
//
// CPython: Python/initconfig.c:3471 _PyConfig_Read
func ConfigRead(c *PyConfig) Status {
	if len(c.OrigArgv) == 0 && (len(c.Argv) != 1 || c.Argv[0] != "") {
		c.OrigArgv = append([]string(nil), c.Argv...)
	}

	if c.Isolated > 0 {
		c.SafePath = 1
		c.UseEnvironment = 0
		c.UserSiteDirectory = 0
	}

	if c.UseEnvironment != 0 {
		if status := ConfigReadEnvVars(c); status.IsException() {
			return status
		}
	}

	var cmdlineWarnopts []string
	if c.ParseArgv != 0 {
		if status := ConfigParseCmdline(c, &cmdlineWarnopts); status.IsException() {
			return status
		}
	}

	c.WarnOptions = append(c.WarnOptions, cmdlineWarnopts...)

	applyDefaults(c)

	if len(c.Argv) < 1 {
		c.Argv = []string{""}
	}

	return StatusOk()
}

// getXOption reports whether the -X option named by key is present in
// xoptions, comparing against the part before any '=' so both bare
// "-X dev" and "-X dev=1" forms match.
//
// CPython: Python/preconfig.c:582 _Py_get_xoption
func getXOption(xoptions []string, key string) bool {
	for _, opt := range xoptions {
		name, _, _ := strings.Cut(opt, "=")
		if name == key {
			return true
		}
	}
	return false
}

// applyDefaults flips every "ask the caller" sentinel that survived
// the env + CLI passes to its CPython default. Mirrors the
// "default values" section of config_read.
//
// CPython: Python/initconfig.c:2689 config_read defaults block
func applyDefaults(c *PyConfig) {
	if c.UseHashSeed < 0 {
		c.UseHashSeed = 0
		c.HashSeed = 0
	}
	// dev_mode: -X dev or PYTHONDEVMODE turns it on before the default
	// fallthrough pins the remaining sentinel to off.
	//
	// CPython: Python/preconfig.c:253 _PyPreCmdline_SetConfig dev_mode block
	if c.DevMode < 0 && (getXOption(c.XOptions, "dev") || GetEnv(c.UseEnvironment, "PYTHONDEVMODE") != "") {
		c.DevMode = 1
	}
	if c.DevMode < 0 {
		c.DevMode = 0
	}
	if c.Isolated < 0 {
		c.Isolated = 0
	}
	if c.UseEnvironment < 0 {
		c.UseEnvironment = 1
	}
	if c.SiteImport < 0 {
		c.SiteImport = 1
	}
	if c.BytesWarning < 0 {
		c.BytesWarning = 0
	}
	if c.Inspect < 0 {
		c.Inspect = 0
	}
	if c.Interactive < 0 {
		c.Interactive = 0
	}
	if c.OptimizationLevel < 0 {
		c.OptimizationLevel = 0
	}
	if c.ParserDebug < 0 {
		c.ParserDebug = 0
	}
	if c.WriteBytecode < 0 {
		c.WriteBytecode = 1
	}
	if c.Verbose < 0 {
		c.Verbose = 0
	}
	if c.Quiet < 0 {
		c.Quiet = 0
	}
	if c.UserSiteDirectory < 0 {
		c.UserSiteDirectory = 1
	}
	if c.BufferedStdio < 0 {
		c.BufferedStdio = 1
	}
	if c.PathconfigWarnings < 0 {
		c.PathconfigWarnings = 1
	}
	if c.ImportTime < 0 {
		c.ImportTime = 0
	}
}
