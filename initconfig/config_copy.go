package initconfig

// ConfigCopy clones src into dst. Used by pyinit_core to take a
// private copy of the caller's config before reading env-vars and
// CLI args, so subsequent ConfigRead writes do not leak back through
// shared slice headers.
//
// Mirrors CPython's spec-table walk: every int and string field gets
// a value copy, every []string gets a fresh backing array.
//
// CPython: Python/initconfig.c:1232 _PyConfig_Copy
func ConfigCopy(dst, src *PyConfig) Status {
	dst.Clear()

	dst.ConfigInit = src.ConfigInit

	dst.Isolated = src.Isolated
	dst.UseEnvironment = src.UseEnvironment
	dst.DevMode = src.DevMode
	dst.InstallSignalHandlers = src.InstallSignalHandlers
	dst.UseHashSeed = src.UseHashSeed
	dst.HashSeed = src.HashSeed
	dst.ParseArgv = src.ParseArgv

	dst.ParserDebug = src.ParserDebug
	dst.Verbose = src.Verbose
	dst.OptimizationLevel = src.OptimizationLevel
	dst.WriteBytecode = src.WriteBytecode
	dst.BufferedStdio = src.BufferedStdio
	dst.UserSiteDirectory = src.UserSiteDirectory
	dst.Inspect = src.Inspect
	dst.Interactive = src.Interactive
	dst.Quiet = src.Quiet
	dst.BytesWarning = src.BytesWarning
	dst.ImportTime = src.ImportTime
	dst.CodeDebugRanges = src.CodeDebugRanges
	dst.WarnDefaultEncoding = src.WarnDefaultEncoding
	dst.SiteImport = src.SiteImport

	dst.Argv = copyStrings(src.Argv)
	dst.OrigArgv = copyStrings(src.OrigArgv)
	dst.XOptions = copyStrings(src.XOptions)
	dst.WarnOptions = copyStrings(src.WarnOptions)

	dst.FilesystemEncoding = src.FilesystemEncoding
	dst.FilesystemErrors = src.FilesystemErrors
	dst.StdioEncoding = src.StdioEncoding
	dst.StdioErrors = src.StdioErrors
	dst.ConfigureCStdio = src.ConfigureCStdio
	dst.PycachePrefix = src.PycachePrefix

	dst.ProgramName = src.ProgramName
	dst.PythonpathEnv = src.PythonpathEnv
	dst.Home = src.Home
	dst.Platlibdir = src.Platlibdir

	dst.ModuleSearchPathsSet = src.ModuleSearchPathsSet
	dst.ModuleSearchPaths = copyStrings(src.ModuleSearchPaths)
	dst.StdlibDir = src.StdlibDir
	dst.Executable = src.Executable
	dst.BaseExecutable = src.BaseExecutable
	dst.Prefix = src.Prefix
	dst.BasePrefix = src.BasePrefix
	dst.ExecPrefix = src.ExecPrefix
	dst.BaseExecPrefix = src.BaseExecPrefix
	dst.PathconfigWarnings = src.PathconfigWarnings
	dst.SafePath = src.SafePath

	dst.SkipSourceFirstLine = src.SkipSourceFirstLine
	dst.RunCommand = src.RunCommand
	dst.RunModule = src.RunModule
	dst.RunFilename = src.RunFilename
	dst.SysPath0 = src.SysPath0

	dst.IntMaxStrDigits = src.IntMaxStrDigits
	dst.UseFrozenModules = src.UseFrozenModules

	dst.InstallImportlib = src.InstallImportlib
	dst.InitMain = src.InitMain

	dst.checkHashPycsMode = src.checkHashPycsMode

	return StatusOk()
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}
