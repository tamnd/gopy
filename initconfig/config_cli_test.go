package initconfig

import "testing"

func parseArgs(t *testing.T, args []string) (*PyConfig, []string, Status) {
	t.Helper()
	c := &PyConfig{}
	c.InitPythonConfig()
	c.Argv = args
	var warnopts []string
	status := ConfigParseCmdline(c, &warnopts)
	return c, warnopts, status
}

func TestConfigParseCmdlineDashC(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-c", "print(1)"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.RunCommand != "print(1)\n" {
		t.Errorf("RunCommand = %q want %q", c.RunCommand, "print(1)\n")
	}
}

func TestConfigParseCmdlineDashM(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-m", "json.tool"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.RunModule != "json.tool" {
		t.Errorf("RunModule = %q", c.RunModule)
	}
}

func TestConfigParseCmdlineFilename(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.RunFilename != "script.py" {
		t.Errorf("RunFilename = %q", c.RunFilename)
	}
}

func TestConfigParseCmdlineFlagPanel(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-B", "-O", "-O", "-q", "-q", "-s", "-S", "-u", "-v", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.WriteBytecode != 0 {
		t.Errorf("WriteBytecode = %d (-B)", c.WriteBytecode)
	}
	if c.OptimizationLevel != 2 {
		t.Errorf("OptimizationLevel = %d (-O -O)", c.OptimizationLevel)
	}
	if c.Quiet != 2 {
		t.Errorf("Quiet = %d (-q -q)", c.Quiet)
	}
	if c.UserSiteDirectory != 0 {
		t.Errorf("UserSiteDirectory = %d (-s)", c.UserSiteDirectory)
	}
	if c.SiteImport != 0 {
		t.Errorf("SiteImport = %d (-S)", c.SiteImport)
	}
	if c.BufferedStdio != 0 {
		t.Errorf("BufferedStdio = %d (-u)", c.BufferedStdio)
	}
	if c.Verbose != 1 {
		t.Errorf("Verbose = %d (-v)", c.Verbose)
	}
	if c.RunFilename != "script.py" {
		t.Errorf("RunFilename = %q", c.RunFilename)
	}
}

func TestConfigParseCmdlineDashI(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-i"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.Inspect != 1 || c.Interactive != 1 {
		t.Errorf("Inspect=%d Interactive=%d", c.Inspect, c.Interactive)
	}
}

func TestConfigParseCmdlineXOption(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-X", "dev", "-X", "tracemalloc=5", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	want := []string{"dev", "tracemalloc=5"}
	if len(c.XOptions) != len(want) {
		t.Fatalf("XOptions len = %d, want %d (%v)", len(c.XOptions), len(want), c.XOptions)
	}
	for i := range want {
		if c.XOptions[i] != want[i] {
			t.Errorf("XOptions[%d] = %q want %q", i, c.XOptions[i], want[i])
		}
	}
}

func TestConfigParseCmdlineWarnOptions(t *testing.T) {
	_, warnopts, status := parseArgs(t, []string{"gopy", "-W", "all", "-W", "ignore::DeprecationWarning", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	want := []string{"all", "ignore::DeprecationWarning"}
	if len(warnopts) != len(want) {
		t.Fatalf("warnopts = %v, want %v", warnopts, want)
	}
	for i := range want {
		if warnopts[i] != want[i] {
			t.Errorf("warnopts[%d] = %q want %q", i, warnopts[i], want[i])
		}
	}
}

func TestConfigParseCmdlineSafePath(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-P", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.SafePath != 1 {
		t.Errorf("SafePath = %d", c.SafePath)
	}
}

func TestConfigParseCmdlineHashSeedReset(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "-R", "script.py"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.UseHashSeed != 0 {
		t.Errorf("UseHashSeed = %d", c.UseHashSeed)
	}
}

func TestConfigParseCmdlineCheckHashPycs(t *testing.T) {
	cases := []struct {
		mode string
		ok   bool
	}{
		{"always", true},
		{"never", true},
		{"default", true},
		{"sometimes", false},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			c, _, status := parseArgs(t, []string{"gopy", "--check-hash-based-pycs", tc.mode, "script.py"})
			if tc.ok {
				if status.IsException() {
					t.Fatalf("expected ok, got %+v", status)
				}
				if c.checkHashPycsMode != tc.mode {
					t.Errorf("checkHashPycsMode = %q", c.checkHashPycsMode)
				}
			} else if !status.IsExit() || status.ExitCode != 2 {
				t.Errorf("expected exit 2, got %+v", status)
			}
		})
	}
}

func TestConfigParseCmdlineUnknownOption(t *testing.T) {
	_, _, status := parseArgs(t, []string{"gopy", "-Z"})
	if !status.IsExit() || status.ExitCode != 2 {
		t.Errorf("expected exit 2, got %+v", status)
	}
}

func TestConfigParseCmdlineDoubleDashStops(t *testing.T) {
	c, _, status := parseArgs(t, []string{"gopy", "--", "-c", "stillafilename"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	// After --, the next arg becomes the filename.
	if c.RunFilename != "-c" {
		t.Errorf("RunFilename = %q", c.RunFilename)
	}
	if c.RunCommand != "" {
		t.Errorf("RunCommand should not be set, got %q", c.RunCommand)
	}
}

func TestConfigParseCmdlineDashCAttachedArg(t *testing.T) {
	// CPython accepts -cprint(1) (no space between -c and the arg)
	// because the short opt has a colon. Same for -m.
	c, _, status := parseArgs(t, []string{"gopy", "-cprint(1)"})
	if status.IsException() {
		t.Fatalf("status: %+v", status)
	}
	if c.RunCommand != "print(1)\n" {
		t.Errorf("RunCommand = %q", c.RunCommand)
	}
}

func TestConfigParseCmdlineHelpExits(t *testing.T) {
	_, _, status := parseArgs(t, []string{"gopy", "-h"})
	if !status.IsExit() || status.ExitCode != 0 {
		t.Errorf("expected exit 0, got %+v", status)
	}
}

func TestConfigParseCmdlineVersionExits(t *testing.T) {
	_, _, status := parseArgs(t, []string{"gopy", "-V"})
	if !status.IsExit() || status.ExitCode != 0 {
		t.Errorf("expected exit 0, got %+v", status)
	}
}
