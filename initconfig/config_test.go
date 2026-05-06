package initconfig

import (
	"reflect"
	"testing"
)

// TestInitPythonConfigDefaults pins the field shape of a fresh
// PyConfig.InitPythonConfig against the values CPython's initconfig.c
// produces in config_init_defaults + the parse_argv/configure_c_stdio
// flips at the end of PyConfig_InitPythonConfig.
//
// CPython: Python/initconfig.c:1106 PyConfig_InitPythonConfig
func TestPyConfigInitPythonConfigDefaults(t *testing.T) {
	var c PyConfig
	c.InitPythonConfig()

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ConfigInit", c.ConfigInit, ConfigInitPython},
		{"Isolated", c.Isolated, 0},
		{"UseEnvironment", c.UseEnvironment, 1},
		{"DevMode", c.DevMode, -1},
		{"InstallSignalHandlers", c.InstallSignalHandlers, 1},
		{"UseHashSeed", c.UseHashSeed, -1},
		{"ParseArgv", c.ParseArgv, 1},
		{"ConfigureCStdio", c.ConfigureCStdio, 1},
		{"SiteImport", c.SiteImport, 1},
		{"BytesWarning", c.BytesWarning, 0},
		{"Inspect", c.Inspect, 0},
		{"Interactive", c.Interactive, 0},
		{"OptimizationLevel", c.OptimizationLevel, 0},
		{"ParserDebug", c.ParserDebug, 0},
		{"WriteBytecode", c.WriteBytecode, 1},
		{"Verbose", c.Verbose, 0},
		{"Quiet", c.Quiet, 0},
		{"UserSiteDirectory", c.UserSiteDirectory, 1},
		{"BufferedStdio", c.BufferedStdio, 1},
		{"PathconfigWarnings", c.PathconfigWarnings, 1},
		{"InstallImportlib", c.InstallImportlib, 1},
		{"InitMain", c.InitMain, 1},
		{"CodeDebugRanges", c.CodeDebugRanges, 1},
		{"ImportTime", c.ImportTime, -1},
		{"WarnDefaultEncoding", c.WarnDefaultEncoding, 0},
		{"SafePath", c.SafePath, 0},
		{"IntMaxStrDigits", c.IntMaxStrDigits, -1},
		{"UseFrozenModules", c.UseFrozenModules, 0},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestInitIsolatedConfigDefaults pins the values isolated mode flips
// off relative to InitPythonConfig.
//
// CPython: Python/initconfig.c:1117 PyConfig_InitIsolatedConfig
func TestPyConfigInitIsolatedConfigDefaults(t *testing.T) {
	var c PyConfig
	c.InitIsolatedConfig()

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ConfigInit", c.ConfigInit, ConfigInitIsolated},
		{"Isolated", c.Isolated, 1},
		{"UseEnvironment", c.UseEnvironment, 0},
		{"UserSiteDirectory", c.UserSiteDirectory, 0},
		{"DevMode", c.DevMode, 0},
		{"InstallSignalHandlers", c.InstallSignalHandlers, 0},
		{"UseHashSeed", c.UseHashSeed, 0},
		{"SafePath", c.SafePath, 1},
		{"PathconfigWarnings", c.PathconfigWarnings, 0},
		{"IntMaxStrDigits", c.IntMaxStrDigits, IntMaxStrDigitsThreshold},
		{"ParseArgv", c.ParseArgv, 0},
		{"ConfigureCStdio", c.ConfigureCStdio, 0},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestInitCompatConfigKeepsTriStateUnset verifies that the legacy
// compat init leaves every "ask the caller" knob at -1.
//
// CPython: Python/initconfig.c:1006 _PyConfig_InitCompatConfig
func TestPyConfigInitCompatConfigKeepsTriStateUnset(t *testing.T) {
	var c PyConfig
	c.initCompatConfig()

	if c.ConfigInit != ConfigInitCompat {
		t.Errorf("ConfigInit = %v want %v", c.ConfigInit, ConfigInitCompat)
	}
	for name, got := range map[string]int{
		"Isolated":           c.Isolated,
		"UseEnvironment":     c.UseEnvironment,
		"DevMode":            c.DevMode,
		"UseHashSeed":        c.UseHashSeed,
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
		"ImportTime":         c.ImportTime,
		"PathconfigWarnings": c.PathconfigWarnings,
	} {
		if got != -1 {
			t.Errorf("%s = %d, want -1", name, got)
		}
	}
}

func TestPyConfigClearResetsStruct(t *testing.T) {
	var c PyConfig
	c.InitPythonConfig()
	c.RunCommand = "print(1)"
	c.Argv = []string{"gopy", "-c", "print(1)"}
	c.ProgramName = "/usr/local/bin/gopy"

	c.Clear()

	var zero PyConfig
	if !reflect.DeepEqual(c, zero) {
		t.Fatalf("Clear did not reset all fields:\n got=%+v", c)
	}
}
