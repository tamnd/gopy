package initconfig

import (
	"os"
	"testing"
)

func TestGetEnvHonoursUseEnv(t *testing.T) {
	t.Setenv("GOPY_TEST_VAR", "yes")

	if got := GetEnv(0, "GOPY_TEST_VAR"); got != "" {
		t.Fatalf("useEnv=0 should ignore env, got %q", got)
	}
	if got := GetEnv(1, "GOPY_TEST_VAR"); got != "yes" {
		t.Fatalf("useEnv=1: got %q want %q", got, "yes")
	}
}

func TestGetEnvEmptyTreatedAsUnset(t *testing.T) {
	t.Setenv("GOPY_TEST_EMPTY", "")
	if got := GetEnv(1, "GOPY_TEST_EMPTY"); got != "" {
		t.Fatalf("empty env value should report as unset, got %q", got)
	}
}

func TestStrToInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"0", 0, true},
		{"1", 1, true},
		{"-7", -7, true},
		{"42", 42, true},
		{"  3", 0, false},
		{"3 ", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"3a", 0, false},
	}
	for _, tc := range cases {
		got, ok := StrToInt(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("StrToInt(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestGetEnvFlag(t *testing.T) {
	cases := []struct {
		name    string
		envVal  string
		setEnv  bool
		startAt int
		want    int
	}{
		{"unset", "", false, 0, 0},
		{"empty", "", true, 0, 0},
		{"plain integer", "3", true, 0, 3},
		{"bigger than current", "5", true, 2, 5},
		{"smaller than current keeps current", "1", true, 4, 4},
		{"negative becomes 1", "-2", true, 0, 1},
		{"text becomes 1", "text", true, 0, 1},
		{"text smaller than current keeps current", "text", true, 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnv {
				t.Setenv("GOPY_FLAG_VAR", tc.envVal)
			} else {
				clearEnv(t, "GOPY_FLAG_VAR")
			}
			flag := tc.startAt
			GetEnvFlag(1, &flag, "GOPY_FLAG_VAR")
			if flag != tc.want {
				t.Fatalf("got %d want %d", flag, tc.want)
			}
		})
	}
}

func TestGetEnvFlagSkipsWhenUseEnvOff(t *testing.T) {
	t.Setenv("GOPY_FLAG_VAR", "9")
	flag := 0
	GetEnvFlag(0, &flag, "GOPY_FLAG_VAR")
	if flag != 0 {
		t.Fatalf("useEnv=0 should not touch flag, got %d", flag)
	}
}

func TestReadEnvVarsAllUnset(t *testing.T) {
	for _, name := range pythonEnvNames() {
		clearEnv(t, name)
	}
	v := ReadEnvVars(1)
	want := EnvVars{
		DontWriteBytecode: -1,
		Unbuffered:        -1,
		UTF8Mode:          -1,
		Debug:             -1,
		Verbose:           -1,
		Optimize:          -1,
		NoUserSite:        -1,
		DevMode:           -1,
	}
	if v != want {
		t.Fatalf("got %+v want %+v", v, want)
	}
}

func TestReadEnvVarsPopulated(t *testing.T) {
	for _, name := range pythonEnvNames() {
		clearEnv(t, name)
	}
	t.Setenv("PYTHONHOME", "/opt/python")
	t.Setenv("PYTHONPATH", "/x:/y")
	t.Setenv("PYTHONHASHSEED", "random")
	t.Setenv("PYTHONDEBUG", "2")
	t.Setenv("PYTHONUTF8", "1")
	t.Setenv("PYTHONDEVMODE", "x")

	v := ReadEnvVars(1)
	if v.Home != "/opt/python" {
		t.Errorf("Home = %q", v.Home)
	}
	if v.Path != "/x:/y" {
		t.Errorf("Path = %q", v.Path)
	}
	if v.HashSeed != "random" {
		t.Errorf("HashSeed = %q", v.HashSeed)
	}
	if v.Debug != 2 {
		t.Errorf("Debug = %d", v.Debug)
	}
	if v.UTF8Mode != 1 {
		t.Errorf("UTF8Mode = %d", v.UTF8Mode)
	}
	if v.DevMode != 1 {
		t.Errorf("DevMode = %d (text values should clamp to 1)", v.DevMode)
	}
	if v.Verbose != -1 || v.Optimize != -1 {
		t.Errorf("unset flags should stay at -1: Verbose=%d Optimize=%d", v.Verbose, v.Optimize)
	}
}

func TestReadEnvVarsRespectsUseEnvZero(t *testing.T) {
	t.Setenv("PYTHONHOME", "/opt/python")
	t.Setenv("PYTHONDEBUG", "5")

	v := ReadEnvVars(0)
	if v.Home != "" {
		t.Errorf("Home should be empty when useEnv=0, got %q", v.Home)
	}
	if v.Debug != -1 {
		t.Errorf("Debug should stay at -1 when useEnv=0, got %d", v.Debug)
	}
}

func pythonEnvNames() []string {
	return []string{
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
	}
}

// clearEnv unsets name for the lifetime of the test. We snapshot the
// previous value via t.Setenv so the cleanup hook restores it, then
// call os.Unsetenv so the lookup sees an absent slot rather than an
// empty string. ReadEnvVars treats "" as unset, but TestGetEnvFlag
// distinguishes the two for the "unset vs empty" pair.
func clearEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}
