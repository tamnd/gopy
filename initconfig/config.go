package initconfig

// IntMaxStrDigitsThreshold is the lower bound CPython enforces on
// sys.set_int_max_str_digits. Mirrors _PY_LONG_DEFAULT_MAX_STR_DIGITS.
//
// CPython: Include/internal/pycore_long.h:155 _PY_LONG_DEFAULT_MAX_STR_DIGITS
const IntMaxStrDigitsThreshold = 4300

// PyConfig is the gopy v0.7 subset of the PyConfig struct. Tri-state
// ints follow CPython: -1 means "not set, ask the env / locale / CLI
// reader to decide", 0 means off, 1 (or higher) means on.
//
// Fields scoped out of v0.7 (tracemalloc, perf_profiling, faulthandler,
// dump_refs, malloc_stats, show_ref_count, check_hash_pycs_mode,
// thread_inherit_context, context_aware_warnings, enable_gil,
// tlbc_enabled, legacy_windows_stdio, use_system_logger,
// _is_python_build) land alongside the subsystems that consume them.
//
// CPython: Include/cpython/initconfig.h:134 PyConfig
type PyConfig struct {
	ConfigInit ConfigInit

	// Init flags.
	Isolated              int
	UseEnvironment        int
	DevMode               int
	InstallSignalHandlers int
	UseHashSeed           int
	HashSeed              uint64
	ParseArgv             int

	// PYTHON*-mapped flags.
	ParserDebug         int
	Verbose             int
	OptimizationLevel   int
	WriteBytecode       int
	BufferedStdio       int
	UserSiteDirectory   int
	Inspect             int
	Interactive         int
	Quiet               int
	BytesWarning        int
	ImportTime          int
	CodeDebugRanges     int
	WarnDefaultEncoding int
	SiteImport          int

	// Argv and option lists.
	Argv        []string
	OrigArgv    []string
	XOptions    []string
	WarnOptions []string

	// Filesystem + stdio encoding.
	FilesystemEncoding string
	FilesystemErrors   string
	StdioEncoding      string
	StdioErrors        string
	ConfigureCStdio    int
	PycachePrefix      string

	// Path configuration inputs.
	ProgramName   string
	PythonpathEnv string
	Home          string
	Platlibdir    string

	// Path configuration outputs.
	ModuleSearchPathsSet int
	ModuleSearchPaths    []string
	StdlibDir            string
	Executable           string
	BaseExecutable       string
	Prefix               string
	BasePrefix           string
	ExecPrefix           string
	BaseExecPrefix       string
	PathconfigWarnings   int
	SafePath             int

	// Py_Main parameters.
	SkipSourceFirstLine int
	RunCommand          string
	RunModule           string
	RunFilename         string
	SysPath0            string

	// Misc.
	IntMaxStrDigits  int
	UseFrozenModules int

	// Private fields. Mirror CPython's leading-underscore names.
	InstallImportlib int
	InitMain         int

	// checkHashPycsMode parks --check-hash-based-pycs until the
	// marshal arm consumes it in v0.8. Lower-case so it is
	// package-private; the v0.8 import port lifts it to a public
	// CheckHashPycsMode field.
	checkHashPycsMode string
}

// initCompatConfig is the shared seeding step for the three public
// initializers. Mirrors _PyConfig_InitCompatConfig: zero the struct,
// then bump the "ask the caller" knobs to -1 and pin the handful of
// fields CPython keeps non-zero by default (install_signal_handlers,
// _install_importlib, _init_main, code_debug_ranges, use_frozen_modules).
//
// CPython: Python/initconfig.c:1006 _PyConfig_InitCompatConfig
func (c *PyConfig) initCompatConfig() {
	*c = PyConfig{}

	c.ConfigInit = ConfigInitCompat
	c.ImportTime = -1
	c.Isolated = -1
	c.UseEnvironment = -1
	c.DevMode = -1
	c.InstallSignalHandlers = 1
	c.UseHashSeed = -1
	c.ModuleSearchPathsSet = 0
	c.ParseArgv = 0
	c.SiteImport = -1
	c.BytesWarning = -1
	c.WarnDefaultEncoding = 0
	c.Inspect = -1
	c.Interactive = -1
	c.OptimizationLevel = -1
	c.ParserDebug = -1
	c.WriteBytecode = -1
	c.Verbose = -1
	c.Quiet = -1
	c.UserSiteDirectory = -1
	c.ConfigureCStdio = 0
	c.BufferedStdio = -1
	c.InstallImportlib = 1
	c.PathconfigWarnings = -1
	c.InitMain = 1
	// gopy ships use_frozen_modules off by default. CPython gates this
	// on Py_DEBUG; we surface the field but keep it disabled until the
	// freeze story lands with import (v0.8).
	c.UseFrozenModules = 0
	c.SafePath = 0
	c.IntMaxStrDigits = -1
	c.CodeDebugRanges = 1
}

// initDefaults is the layered seeding shared by the Python and
// Isolated entry points: compat plus the "non-isolated default" knobs
// (site import, write bytecode, optimization level zero, etc).
//
// CPython: Python/initconfig.c:1071 config_init_defaults
func (c *PyConfig) initDefaults() {
	c.initCompatConfig()

	c.Isolated = 0
	c.UseEnvironment = 1
	c.SiteImport = 1
	c.BytesWarning = 0
	c.Inspect = 0
	c.Interactive = 0
	c.OptimizationLevel = 0
	c.ParserDebug = 0
	c.WriteBytecode = 1
	c.Verbose = 0
	c.Quiet = 0
	c.UserSiteDirectory = 1
	c.BufferedStdio = 1
	c.PathconfigWarnings = 1
}

// InitPythonConfig is the modern Python entry point: layered defaults
// plus parse_argv on and configure_c_stdio on.
//
// CPython: Python/initconfig.c:1106 PyConfig_InitPythonConfig
func (c *PyConfig) InitPythonConfig() {
	c.initDefaults()

	c.ConfigInit = ConfigInitPython
	c.ConfigureCStdio = 1
	c.ParseArgv = 1
}

// InitIsolatedConfig is the embedded entry point. Drops every
// environment- and user-driven knob: isolated on, use_environment off,
// user_site_directory off, dev_mode off, install_signal_handlers off,
// use_hash_seed pinned to zero so the seed becomes deterministic,
// safe_path on, pathconfig_warnings off, int_max_str_digits clamped
// to the threshold default.
//
// CPython: Python/initconfig.c:1117 PyConfig_InitIsolatedConfig
func (c *PyConfig) InitIsolatedConfig() {
	c.initDefaults()

	c.ConfigInit = ConfigInitIsolated
	c.Isolated = 1
	c.UseEnvironment = 0
	c.UserSiteDirectory = 0
	c.DevMode = 0
	c.InstallSignalHandlers = 0
	c.UseHashSeed = 0
	c.IntMaxStrDigits = IntMaxStrDigitsThreshold
	c.SafePath = 1
	c.PathconfigWarnings = 0
}

// Clear releases the heap-backed slices and strings on c. Mirrors
// PyConfig_Clear: in CPython this frees the wchar_t * fields and the
// PyWideStringList allocations; in Go we just reset the struct so any
// downstream references release their backing memory.
//
// CPython: Python/initconfig.c:959 PyConfig_Clear
func (c *PyConfig) Clear() {
	*c = PyConfig{}
}
