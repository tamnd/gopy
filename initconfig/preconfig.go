// Package initconfig ports cpython/Python/preconfig.c and
// cpython/Python/initconfig.c. It owns PyPreConfig, PyConfig, and the
// env+CLI+defaults read pipeline that Py_Initialize consumes.
//
// v0.7 lands the preconfig struct and its three initializers. The
// remaining surface (env-var reader, PyConfig, _PyConfig_Read) lands
// in tasks 1622-B through 1622-D.
package initconfig

// ConfigInit selects which initializer was used to populate a
// PyPreConfig. Mirrors _PyConfigInitEnum.
//
// CPython: Include/internal/pycore_initconfig.h:149 _PyConfigInitEnum
type ConfigInit int

const (
	// ConfigInitCompat is the legacy "compat" init: every flag stays
	// at zero/-1 unless the caller writes it. Used by the old
	// Py_InitializeEx entry points.
	ConfigInitCompat ConfigInit = 1
	// ConfigInitPython is the modern Python init: parse_argv on,
	// use_environment on, locale coercion + UTF-8 mode left at -1 so
	// the locale probe decides.
	ConfigInitPython ConfigInit = 2
	// ConfigInitIsolated is the embedded init: parse_argv off,
	// use_environment off, isolated on, dev_mode off.
	ConfigInitIsolated ConfigInit = 3
)

// AllocatorName mirrors the PyMemAllocatorName enum. gopy never reads
// C-level allocator hooks because Go owns allocation; the field is
// kept on PyPreConfig for shape parity and has no runtime effect.
//
// CPython: Include/cpython/pymem.h:16 PyMemAllocatorName
type AllocatorName int

const (
	AllocatorNotSet        AllocatorName = 0
	AllocatorDefault       AllocatorName = 1
	AllocatorDebug         AllocatorName = 2
	AllocatorMalloc        AllocatorName = 3
	AllocatorMallocDebug   AllocatorName = 4
	AllocatorPymalloc      AllocatorName = 5
	AllocatorPymallocDebug AllocatorName = 6
	AllocatorMimalloc      AllocatorName = 7
	AllocatorMimallocDebug AllocatorName = 8
)

// PyPreConfig is the pre-init configuration: locale, encoding, and
// allocator decisions that need to fire before PyConfig_Read runs.
//
// Tri-state ints follow the CPython convention: -1 means "ask the
// caller / locale probe", 0 means off, 1 (or higher) means on.
//
// CPython: Include/cpython/initconfig.h:47 PyPreConfig
type PyPreConfig struct {
	ConfigInit ConfigInit

	ParseArgv         int
	Isolated          int
	UseEnvironment    int
	ConfigureLocale   int
	CoerceCLocale     int
	CoerceCLocaleWarn int

	// LegacyWindowsFSEncoding is read on Windows only. gopy keeps it
	// in the struct so callers porting CPython code don't need build
	// tags, but the value is ignored on darwin/linux.
	LegacyWindowsFSEncoding int

	UTF8Mode  int
	DevMode   int
	Allocator AllocatorName
}

// InitCompatConfig is the zero+defaults seeding shared by every
// initializer. Mirrors _PyPreConfig_InitCompatConfig: zero the struct,
// stamp ConfigInit=Compat, then bump the "ask the caller" knobs to -1.
//
// CPython: Python/preconfig.c:283 _PyPreConfig_InitCompatConfig
func (c *PyPreConfig) InitCompatConfig() {
	*c = PyPreConfig{}

	c.ConfigInit = ConfigInitCompat
	c.ParseArgv = 0
	c.Isolated = -1
	c.UseEnvironment = -1
	c.ConfigureLocale = 1

	c.UTF8Mode = 0
	c.CoerceCLocale = 0
	c.CoerceCLocaleWarn = 0

	c.DevMode = -1
	c.Allocator = AllocatorNotSet
	c.LegacyWindowsFSEncoding = -1
}

// InitPythonConfig is the modern Python entry point. Layered on top
// of compat: parse_argv on, use_environment on, isolated explicitly
// off, locale coercion + UTF-8 mode left at -1 so the locale probe
// decides at PreConfig.Read time.
//
// CPython: Python/preconfig.c:312 PyPreConfig_InitPythonConfig
func (c *PyPreConfig) InitPythonConfig() {
	c.InitCompatConfig()

	c.ConfigInit = ConfigInitPython
	c.Isolated = 0
	c.ParseArgv = 1
	c.UseEnvironment = 1
	c.CoerceCLocale = -1
	c.CoerceCLocaleWarn = -1
	c.UTF8Mode = -1
	c.LegacyWindowsFSEncoding = 0
}

// InitIsolatedConfig is the embedded entry point: locale untouched,
// isolated on, environment ignored, UTF-8 mode + dev_mode forced off.
//
// CPython: Python/preconfig.c:333 PyPreConfig_InitIsolatedConfig
func (c *PyPreConfig) InitIsolatedConfig() {
	c.InitCompatConfig()

	c.ConfigInit = ConfigInitIsolated
	c.ConfigureLocale = 0
	c.Isolated = 1
	c.UseEnvironment = 0
	c.UTF8Mode = 0
	c.DevMode = 0
	c.LegacyWindowsFSEncoding = 0
}

// InitFromPreConfig seeds c from another PyPreConfig: start from the
// Python defaults, then copy every field from src on top. Mirrors
// _PyPreConfig_InitFromPreConfig: PyPreConfig_InitPythonConfig
// followed by preconfig_copy.
//
// CPython: Python/preconfig.c:349 _PyPreConfig_InitFromPreConfig
func (c *PyPreConfig) InitFromPreConfig(src *PyPreConfig) {
	c.InitPythonConfig()
	*c = *src
}
