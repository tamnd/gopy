package initconfig

import "testing"

func TestInitCompatConfigDefaults(t *testing.T) {
	var c PyPreConfig
	c.InitCompatConfig()

	want := PyPreConfig{
		ConfigInit:              ConfigInitCompat,
		ParseArgv:               0,
		Isolated:                -1,
		UseEnvironment:          -1,
		ConfigureLocale:         1,
		CoerceCLocale:           0,
		CoerceCLocaleWarn:       0,
		LegacyWindowsFSEncoding: -1,
		UTF8Mode:                0,
		DevMode:                 -1,
		Allocator:               AllocatorNotSet,
	}
	if c != want {
		t.Fatalf("InitCompatConfig:\n got=%+v\nwant=%+v", c, want)
	}
}

func TestInitPythonConfigDefaults(t *testing.T) {
	var c PyPreConfig
	c.InitPythonConfig()

	want := PyPreConfig{
		ConfigInit:              ConfigInitPython,
		ParseArgv:               1,
		Isolated:                0,
		UseEnvironment:          1,
		ConfigureLocale:         1,
		CoerceCLocale:           -1,
		CoerceCLocaleWarn:       -1,
		LegacyWindowsFSEncoding: 0,
		UTF8Mode:                -1,
		DevMode:                 -1,
		Allocator:               AllocatorNotSet,
	}
	if c != want {
		t.Fatalf("InitPythonConfig:\n got=%+v\nwant=%+v", c, want)
	}
}

func TestInitIsolatedConfigDefaults(t *testing.T) {
	var c PyPreConfig
	c.InitIsolatedConfig()

	want := PyPreConfig{
		ConfigInit:              ConfigInitIsolated,
		ParseArgv:               0,
		Isolated:                1,
		UseEnvironment:          0,
		ConfigureLocale:         0,
		CoerceCLocale:           0,
		CoerceCLocaleWarn:       0,
		LegacyWindowsFSEncoding: 0,
		UTF8Mode:                0,
		DevMode:                 0,
		Allocator:               AllocatorNotSet,
	}
	if c != want {
		t.Fatalf("InitIsolatedConfig:\n got=%+v\nwant=%+v", c, want)
	}
}

// TestInitOverwritesPriorState confirms that calling InitPythonConfig
// on a struct that previously held isolated values produces the same
// shape as a fresh-zero call. Mirrors the CPython contract that the
// initializers fully reseed from compat defaults.
func TestInitOverwritesPriorState(t *testing.T) {
	var c PyPreConfig
	c.InitIsolatedConfig()
	c.InitPythonConfig()

	var fresh PyPreConfig
	fresh.InitPythonConfig()

	if c != fresh {
		t.Fatalf("re-init drift:\n got=%+v\nwant=%+v", c, fresh)
	}
}

func TestInitFromPreConfigCopiesFields(t *testing.T) {
	var src PyPreConfig
	src.InitIsolatedConfig()
	src.UTF8Mode = 1
	src.DevMode = 1
	src.Allocator = AllocatorMalloc

	var dst PyPreConfig
	dst.InitFromPreConfig(&src)

	if dst != src {
		t.Fatalf("InitFromPreConfig mismatch:\n got=%+v\nwant=%+v", dst, src)
	}
}
