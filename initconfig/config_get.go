package initconfig

import "sort"

// ConfigMemberType mirrors the PyConfigMemberType enum: the storage
// class of a PyConfig field, which decides how config_get turns the raw
// member into a Python object.
//
// CPython: Python/initconfig.c:60 PyConfigMemberType
type ConfigMemberType int

const (
	ConfigMemberInt ConfigMemberType = iota
	ConfigMemberUint
	ConfigMemberBool
	ConfigMemberULong
	ConfigMemberWStr
	ConfigMemberWStrOpt
	ConfigMemberWStrList
)

// configSpec is one row of PYCONFIG_SPEC: the option name, its member
// type, the sys attribute config_get delegates to when use_sys is set
// (empty for NO_SYS / SYS_FLAG rows), and a reader that pulls the raw
// member out of a PyConfig. gopy only lists the rows whose members the
// v0.x PyConfig subset actually models; the scoped-out fields documented
// on PyConfig (tracemalloc, dump_refs, perf_profiling, ...) are absent
// here exactly as they are absent from the struct, so config_get reports
// them as unknown names until their subsystems land.
//
// CPython: Python/initconfig.c:105 PYCONFIG_SPEC
type configSpec struct {
	name    string
	typ     ConfigMemberType
	sysAttr string
	get     func(c *PyConfig) any
}

// pyconfigSpec is the gopy port of PYCONFIG_SPEC. Rows preserve the
// CPython option names, member types, and SYS_ATTR delegations.
//
// CPython: Python/initconfig.c:105 PYCONFIG_SPEC
var pyconfigSpec = []configSpec{
	// --- Public options ---
	{"argv", ConfigMemberWStrList, "argv", func(c *PyConfig) any { return c.Argv }},
	{"base_exec_prefix", ConfigMemberWStrOpt, "base_exec_prefix", func(c *PyConfig) any { return c.BaseExecPrefix }},
	{"base_executable", ConfigMemberWStrOpt, "_base_executable", func(c *PyConfig) any { return c.BaseExecutable }},
	{"base_prefix", ConfigMemberWStrOpt, "base_prefix", func(c *PyConfig) any { return c.BasePrefix }},
	{"bytes_warning", ConfigMemberUint, "", func(c *PyConfig) any { return c.BytesWarning }},
	{"exec_prefix", ConfigMemberWStrOpt, "exec_prefix", func(c *PyConfig) any { return c.ExecPrefix }},
	{"executable", ConfigMemberWStrOpt, "executable", func(c *PyConfig) any { return c.Executable }},
	{"inspect", ConfigMemberBool, "", func(c *PyConfig) any { return c.Inspect }},
	{"int_max_str_digits", ConfigMemberUint, "", func(c *PyConfig) any { return c.IntMaxStrDigits }},
	{"interactive", ConfigMemberBool, "", func(c *PyConfig) any { return c.Interactive }},
	{"module_search_paths", ConfigMemberWStrList, "path", func(c *PyConfig) any { return c.ModuleSearchPaths }},
	{"optimization_level", ConfigMemberUint, "", func(c *PyConfig) any { return c.OptimizationLevel }},
	{"parser_debug", ConfigMemberBool, "", func(c *PyConfig) any { return c.ParserDebug }},
	{"platlibdir", ConfigMemberWStr, "platlibdir", func(c *PyConfig) any { return c.Platlibdir }},
	{"prefix", ConfigMemberWStrOpt, "prefix", func(c *PyConfig) any { return c.Prefix }},
	{"pycache_prefix", ConfigMemberWStrOpt, "pycache_prefix", func(c *PyConfig) any { return c.PycachePrefix }},
	{"quiet", ConfigMemberBool, "", func(c *PyConfig) any { return c.Quiet }},
	{"stdlib_dir", ConfigMemberWStrOpt, "_stdlib_dir", func(c *PyConfig) any { return c.StdlibDir }},
	{"use_environment", ConfigMemberBool, "", func(c *PyConfig) any { return c.UseEnvironment }},
	{"verbose", ConfigMemberUint, "", func(c *PyConfig) any { return c.Verbose }},
	{"warnoptions", ConfigMemberWStrList, "warnoptions", func(c *PyConfig) any { return c.WarnOptions }},
	{"write_bytecode", ConfigMemberBool, "", func(c *PyConfig) any { return c.WriteBytecode }},
	{"xoptions", ConfigMemberWStrList, "_xoptions", func(c *PyConfig) any { return c.XOptions }},

	// --- Read-only options ---
	{"buffered_stdio", ConfigMemberBool, "", func(c *PyConfig) any { return c.BufferedStdio }},
	{"check_hash_pycs_mode", ConfigMemberWStr, "", func(c *PyConfig) any { return c.checkHashPycsMode }},
	{"code_debug_ranges", ConfigMemberBool, "", func(c *PyConfig) any { return c.CodeDebugRanges }},
	{"configure_c_stdio", ConfigMemberBool, "", func(c *PyConfig) any { return c.ConfigureCStdio }},
	{"dev_mode", ConfigMemberBool, "", func(c *PyConfig) any { return c.DevMode }},
	{"filesystem_encoding", ConfigMemberWStr, "", func(c *PyConfig) any { return c.FilesystemEncoding }},
	{"filesystem_errors", ConfigMemberWStr, "", func(c *PyConfig) any { return c.FilesystemErrors }},
	{"hash_seed", ConfigMemberULong, "", func(c *PyConfig) any { return c.HashSeed }},
	{"home", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.Home }},
	{"import_time", ConfigMemberUint, "", func(c *PyConfig) any { return c.ImportTime }},
	{"install_signal_handlers", ConfigMemberBool, "", func(c *PyConfig) any { return c.InstallSignalHandlers }},
	{"isolated", ConfigMemberBool, "", func(c *PyConfig) any { return c.Isolated }},
	{"orig_argv", ConfigMemberWStrList, "orig_argv", func(c *PyConfig) any { return c.OrigArgv }},
	{"parse_argv", ConfigMemberBool, "", func(c *PyConfig) any { return c.ParseArgv }},
	{"pathconfig_warnings", ConfigMemberBool, "", func(c *PyConfig) any { return c.PathconfigWarnings }},
	{"program_name", ConfigMemberWStr, "", func(c *PyConfig) any { return c.ProgramName }},
	{"run_command", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.RunCommand }},
	{"run_filename", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.RunFilename }},
	{"run_module", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.RunModule }},
	{"safe_path", ConfigMemberBool, "", func(c *PyConfig) any { return c.SafePath }},
	{"site_import", ConfigMemberBool, "", func(c *PyConfig) any { return c.SiteImport }},
	{"skip_source_first_line", ConfigMemberBool, "", func(c *PyConfig) any { return c.SkipSourceFirstLine }},
	{"stdio_encoding", ConfigMemberWStr, "", func(c *PyConfig) any { return c.StdioEncoding }},
	{"stdio_errors", ConfigMemberWStr, "", func(c *PyConfig) any { return c.StdioErrors }},
	{"use_frozen_modules", ConfigMemberBool, "", func(c *PyConfig) any { return c.UseFrozenModules }},
	{"use_hash_seed", ConfigMemberBool, "", func(c *PyConfig) any { return c.UseHashSeed }},
	{"user_site_directory", ConfigMemberBool, "", func(c *PyConfig) any { return c.UserSiteDirectory }},
	{"warn_default_encoding", ConfigMemberBool, "", func(c *PyConfig) any { return c.WarnDefaultEncoding }},

	// --- Init-only options ---
	{"_init_main", ConfigMemberBool, "", func(c *PyConfig) any { return c.InitMain }},
	{"_install_importlib", ConfigMemberBool, "", func(c *PyConfig) any { return c.InstallImportlib }},
	{"module_search_paths_set", ConfigMemberBool, "", func(c *PyConfig) any { return c.ModuleSearchPathsSet }},
	{"pythonpath_env", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.PythonpathEnv }},
	{"sys_path_0", ConfigMemberWStrOpt, "", func(c *PyConfig) any { return c.SysPath0 }},
}

// configFindSpec locates the PYCONFIG_SPEC row for name.
//
// CPython: Python/initconfig.c:4360 config_find_spec
func configFindSpec(name string) *configSpec {
	for i := range pyconfigSpec {
		if pyconfigSpec[i].name == name {
			return &pyconfigSpec[i]
		}
	}
	return nil
}

// ConfigMember is the raw value of a config option plus the metadata
// config_get needs to wrap it: its member type and, when the option is
// exposed through sys, the sys attribute name to read instead.
//
// CPython: Python/initconfig.c:4378 config_get
type ConfigMember struct {
	Value   any
	Type    ConfigMemberType
	SysAttr string
}

// ConfigGet looks up name in PYCONFIG_SPEC and returns its raw member
// from c. The bool reports whether the name is a known config option;
// an unknown name maps to the "unknown config option name" ValueError
// the caller raises.
//
// This is the gopy split of config_get: this half resolves the spec and
// reads the raw member (config_find_spec + config_get_spec_member); the
// _testcapi layer wraps the member into a Python object and handles the
// use_sys delegation, exactly as config_get does once it has the member.
//
// CPython: Python/initconfig.c:4458 PyConfig_Get
func (c *PyConfig) ConfigGet(name string) (ConfigMember, bool) {
	spec := configFindSpec(name)
	if spec == nil {
		return ConfigMember{}, false
	}
	return ConfigMember{
		Value:   spec.get(c),
		Type:    spec.typ,
		SysAttr: spec.sysAttr,
	}, true
}

// ConfigNames returns the sorted list of every known config option name.
//
// CPython: Modules/_testcapi/config.c:74 _testcapi_config_names
func ConfigNames() []string {
	names := make([]string, len(pyconfigSpec))
	for i := range pyconfigSpec {
		names[i] = pyconfigSpec[i].name
	}
	sort.Strings(names)
	return names
}
