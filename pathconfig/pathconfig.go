// Package pathconfig is the gopy port of cpython/Modules/getpath.py
// (the resolved logic, not the script itself). It computes
// prefix / exec_prefix / stdlib_dir / module_search_paths from the
// caller's PyConfig plus the discovered executable directory.
//
// v0.7 ships the darwin and linux paths. Windows arrives later.
package pathconfig

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/initconfig"
)

// DefaultPlatlibdir is the value config.Platlibdir falls back to when
// the caller leaves it empty. Mirrors PLATLIBDIR from CPython's
// configure script: "lib" on POSIX.
//
// CPython: configure.ac PLATLIBDIR
const DefaultPlatlibdir = "lib"

// DefaultPrefix is the compiled-in prefix CPython falls back to when
// the executable-dir search fails. Mirrors PREFIX from configure.
const DefaultPrefix = "/usr/local"

// Resolve fills the path-config outputs on c. The caller is expected
// to have already populated ProgramName / OrigArgv / PythonpathEnv /
// Home / Platlibdir / Prefix / ExecPrefix slots; Resolve only writes
// fields the caller has left empty so explicit overrides survive.
//
// On any failure it returns a status error; on success c carries the
// fully-resolved Prefix, ExecPrefix, BasePrefix, BaseExecPrefix,
// StdlibDir, Executable, BaseExecutable, and ModuleSearchPaths.
//
// CPython: Modules/getpath.py (resolved logic)
// CPython: Modules/getpath.c:_PyConfig_InitPathConfig
func Resolve(c *initconfig.PyConfig) initconfig.Status {
	if c.Platlibdir == "" {
		c.Platlibdir = DefaultPlatlibdir
	}

	resolveProgramName(c)
	resolveExecutable(c)
	resolveHome(c)
	resolvePrefix(c)
	resolveStdlib(c)
	resolveModuleSearchPaths(c)

	return initconfig.StatusOk()
}

// resolveProgramName fills program_name from orig_argv[0] or falls
// back to the platform default.
//
// CPython: Modules/getpath.py:244 program_name resolution
func resolveProgramName(c *initconfig.PyConfig) {
	if c.ProgramName != "" {
		return
	}
	if len(c.OrigArgv) > 0 && c.OrigArgv[0] != "" {
		c.ProgramName = c.OrigArgv[0]
		return
	}
	c.ProgramName = defaultProgramName()
}

// resolveExecutable derives executable / base_executable from
// program_name. Resolves bare names against $PATH.
//
// CPython: Modules/getpath.py:257 executable resolution
func resolveExecutable(c *initconfig.PyConfig) {
	if c.Executable == "" {
		if strings.ContainsRune(c.ProgramName, filepath.Separator) {
			abs, err := filepath.Abs(c.ProgramName)
			if err == nil {
				c.Executable = abs
			} else {
				c.Executable = c.ProgramName
			}
		} else if found := lookPath(c.ProgramName); found != "" {
			c.Executable = found
		}
	}
	if c.BaseExecutable == "" {
		c.BaseExecutable = c.Executable
	}
}

// resolveHome lifts PYTHONHOME into c.Home when the caller has not
// already pinned a home and use_environment is on.
//
// CPython: Modules/getpath.py:328 home resolution
func resolveHome(c *initconfig.PyConfig) {
	if c.Home != "" {
		return
	}
	if c.UseEnvironment == 0 {
		return
	}
	if v := os.Getenv("PYTHONHOME"); v != "" {
		c.Home = v
	}
}

// resolvePrefix splits home (which may be "prefix:exec_prefix") into
// prefix / exec_prefix, then falls back to a search-up from the
// executable's directory looking for the stdlib landmark, and finally
// to DefaultPrefix.
//
// CPython: Modules/getpath.py:550 prefix / exec_prefix resolution
func resolvePrefix(c *initconfig.PyConfig) {
	if c.Home != "" {
		// PYTHONHOME accepts "prefix:exec_prefix" on POSIX.
		homePrefix, homeExecPrefix, hasSep := strings.Cut(c.Home, string(homeDelim()))
		if c.Prefix == "" {
			c.Prefix = homePrefix
		}
		if c.ExecPrefix == "" {
			if hasSep {
				c.ExecPrefix = homeExecPrefix
			} else {
				c.ExecPrefix = homePrefix
			}
		}
	}

	if c.Prefix == "" && c.Executable != "" {
		if found := searchUpForStdlib(filepath.Dir(c.Executable), c.Platlibdir); found != "" {
			c.Prefix = found
		}
	}
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	if c.ExecPrefix == "" {
		c.ExecPrefix = c.Prefix
	}
	if c.BasePrefix == "" {
		c.BasePrefix = c.Prefix
	}
	if c.BaseExecPrefix == "" {
		c.BaseExecPrefix = c.ExecPrefix
	}
}

// resolveStdlib computes stdlib_dir = {prefix}/{platlibdir}/python{X.Y}
// when the caller has not pinned it.
//
// CPython: Modules/getpath.py:182 STDLIB_SUBDIR composition
func resolveStdlib(c *initconfig.PyConfig) {
	if c.StdlibDir != "" {
		return
	}
	c.StdlibDir = filepath.Join(c.Prefix, c.Platlibdir, stdlibSubdir())
}

// resolveModuleSearchPaths assembles sys.path. The documented order
// (Doc/using/cmdline.rst) is: PYTHONPATH entries, then the zip stdlib
// bundle, then stdlib_dir, then the platform-stdlib (lib-dynload).
// The script-dir / "" entry is added later by pythonrun once the
// effective argv0 is known.
//
// CPython: Modules/getpath.py:744 module_search_paths assembly
func resolveModuleSearchPaths(c *initconfig.PyConfig) {
	if c.ModuleSearchPathsSet != 0 {
		return
	}

	var paths []string

	if c.PythonpathEnv != "" {
		for p := range strings.SplitSeq(c.PythonpathEnv, string(homeDelim())) {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}

	zipName := "python" + versionTag() + ".zip"
	paths = append(paths,
		filepath.Join(c.Prefix, c.Platlibdir, zipName),
		c.StdlibDir,
		filepath.Join(c.ExecPrefix, c.Platlibdir, "python"+dotVersionTag(), "lib-dynload"),
	)

	c.ModuleSearchPaths = paths
	c.ModuleSearchPathsSet = 1
}

// stdlibSubdir is the directory name immediately under platlibdir,
// e.g. "python3.14".
func stdlibSubdir() string { return "python" + dotVersionTag() }

// dotVersionTag returns "3.14".
func dotVersionTag() string {
	return itoa(build.PythonMajorVersion) + "." + itoa(build.PythonMinorVersion)
}

// versionTag returns "314".
func versionTag() string {
	return itoa(build.PythonMajorVersion) + itoa(build.PythonMinorVersion)
}

// itoa formats a small non-negative int without pulling in strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// searchUpForStdlib walks dir's ancestors looking for the
// {platlibdir}/python{X.Y}/os.py landmark. Returns the prefix
// (the directory containing platlibdir) or "" if none was found.
//
// CPython: Modules/getpath.py:210 search_up
func searchUpForStdlib(dir, platlibdir string) string {
	landmark := filepath.Join(platlibdir, stdlibSubdir(), "os.py")
	for {
		if _, err := os.Stat(filepath.Join(dir, landmark)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// lookPath searches $PATH for name. Returns "" if not found or PATH
// is unset.
func lookPath(name string) string {
	pathenv := os.Getenv("PATH")
	if pathenv == "" {
		return ""
	}
	for dir := range strings.SplitSeq(pathenv, string(homeDelim())) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}
