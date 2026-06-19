// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
//
// CPython: Programs/python.c:13 main
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/compile"
	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/getopt"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/gc"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"

	// Pull in the stdlib inittab. Each built-in module package
	// registers itself with imp.AppendInittab from its own init().
	// stdlibinit is the central blank-import surface, equivalent to
	// CPython's Modules/config.c.in.
	"github.com/tamnd/gopy/module/sys"
	_ "github.com/tamnd/gopy/stdlibinit"
)

func main() {
	os.Exit(mainWithProfile())
}

// mainWithProfile wraps run() so that GOPY_CPUPROFILE's deferred
// pprof.StopCPUProfile actually runs before the process exits.
// os.Exit on its own skips defers, which would leave the profile
// file empty.
func mainWithProfile() int {
	if path := os.Getenv("GOPY_CPUPROFILE"); path != "" {
		f, err := os.Create(path) //nolint:gosec // GOPY_CPUPROFILE is a developer-supplied profile path; opening it is the entire contract.
		if err != nil {
			fmt.Fprintln(os.Stderr, "GOPY_CPUPROFILE:", err)
			return 1
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, "GOPY_CPUPROFILE:", err)
			_ = f.Close()
			return 1
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}
	exitcode := run(os.Args[1:], os.Stdout, os.Stderr)
	// bpo-1054041: if a KeyboardInterrupt went unhandled, exit through
	// the default SIGINT handler so a calling shell sees the ^C and the
	// process reports death-by-signal rather than a plain error code.
	//
	// CPython: Modules/main.c:786 Py_RunMain (unhandled_keyboard_interrupt)
	if pyerrors.UnhandledKeyboardInterrupt() {
		exitcode = exitSigint()
	}
	return exitcode
}

// run drives _PyOS_GetOpt the same way pymain_init walks argv before
// the runtime config exists. argv is rewrapped with a leading program
// name because getopt.GetOpt starts at OptInd=1 (matching CPython's
// _PyOS_optind reset value).
//
// CPython: Modules/main.c:48 pymain_init
func run(args []string, stdout, stderr *os.File) int {
	// Stamp sys.executable / sys._base_executable from os.Executable so
	// subprocess.Popen([sys.executable, ...]) can spawn a child
	// interpreter. CPython picks this up via PyConfig.executable in
	// Py_InitializeFromConfig; until initconfig lands end-to-end the
	// pending hand-off in module/sys keeps the live module honest.
	//
	// CPython: Modules/main.c:118 pymain_init (PyConfig.executable)
	// CPython: Python/initconfig.c:1734 _PyConfig_InitPathConfig
	if exe, err := os.Executable(); err == nil {
		sys.SetExecutable(exe, exe)
	}

	argv := make([]string, 0, len(args)+1)
	argv = append(argv, "gopy")
	argv = append(argv, args...)

	st := getopt.New()
	st.Stderr = stderr

	var (
		showVersion bool
		evalSrc     string
		modName     string
		hasC, hasM  bool
		xOptions    []string
		safePath    bool
	)

opts:
	for {
		c := st.GetOpt(argv, getopt.PythonShortOpts, getopt.PythonLongOpts)
		switch c {
		case getopt.EOF:
			break opts
		case getopt.ErrorMark:
			return 2
		case 'V':
			showVersion = true
		case 'h', '?':
			fmt.Fprintln(stderr, "usage: gopy [option] ... [-c cmd | -m mod | file | -] [arg] ...")
			return 0
		case 'c':
			evalSrc = st.OptArg
			hasC = true
			break opts
		case 'm':
			modName = st.OptArg
			hasM = true
			break opts
		case 'X':
			xOptions = append(xOptions, st.OptArg)
		case 'P':
			// -P sets safe_path: the script directory / cwd / '' is not
			// prepended to sys.path[0].
			//
			// CPython: Python/initconfig.c:2098 config_parse_cmdline ('P')
			safePath = true
		case 'I':
			// -I (isolated) implies -P plus -E/-s; the safe_path effect on
			// sys.path[0] is what the shadowing tests exercise.
			//
			// CPython: Python/initconfig.c:2080 config_parse_cmdline ('I')
			safePath = true
		default:
			// Other CPython flags (-b, -B, -O, -W, ...) are accepted for
			// option-set parity. Wiring each to the runtime config lands
			// with initconfig.c in a later milestone.
		}
	}

	// dev_mode: -X dev or PYTHONDEVMODE turns on the development-mode
	// checks (currently the eager encoding/errors validation in the
	// str(bytes) decode path).
	//
	// CPython: Python/preconfig.c:253 _PyPreCmdline_SetConfig dev_mode block
	if hasXOption(xOptions, "dev") || os.Getenv("PYTHONDEVMODE") != "" {
		codecs.SetDevMode(true)
	}

	// safe_path: -P, -I, or PYTHONSAFEPATH suppresses prepending the
	// script directory / cwd / "" to sys.path[0], and is exposed as
	// sys.flags.safe_path. installPathFinder reads safePathMode to skip
	// the unsafe leading entry.
	//
	// CPython: Python/initconfig.c:1828 config_init_safe_path
	if safePath || os.Getenv("PYTHONSAFEPATH") != "" {
		safePathMode = true
		sys.SetSafePath(true)
	}

	switch {
	case showVersion:
		fmt.Fprintln(stdout, build.VersionString())
		return 0
	case hasC:
		return runSource(evalSrc, stdout, stderr)
	case hasM:
		modArgs := argv[st.OptInd:]
		return runModule(modName, modArgs, stdout, stderr)
	}

	if st.OptInd < len(argv) {
		return runPositional(argv[st.OptInd], argv[st.OptInd+1:], stdout, stderr)
	}
	sys.SetArgv([]string{""})
	return runInteractive(stdout, stderr)
}

// runPositional dispatches the trailing positional argument. If it names
// a package (a directory or a ZIP archive) it is an import-path entry,
// not a source file: prepend it to sys.path and run its __main__
// submodule. CPython detects this in pymain_get_importer
// (PyImport_GetImporter runs the path hooks) and runs
// pymain_run_module(L"__main__", 0); set_argv0=0 leaves argv[0] as the
// archive path. A plain .py file falls through to pymain_run_file.
//
// CPython: Modules/main.c:127 pymain_get_importer
// CPython: Modules/main.c:691 pymain_run_python (main_importer_path branch)
func runPositional(scriptPath string, rest []string, stdout, stderr *os.File) int {
	sys.SetArgv(append([]string{scriptPath}, rest...))
	if isImporterPath(scriptPath) {
		return runImporterMain(scriptPath, stdout, stderr)
	}
	return runFile(scriptPath, stdout, stderr)
}

// safePathMode records whether -P / -I / PYTHONSAFEPATH was supplied, so
// installPathFinder omits the unsafe leading sys.path[0] entry.
//
// CPython: Python/initconfig.c:1828 config_init_safe_path
var safePathMode bool

// sysPath0Entry / sysPath0Present hold the leading sys.path entry
// (script directory, or "" for -c / -m / interactive). CPython prepends
// config->sys_path_0 to sys.path AFTER site.main() runs, so
// site.removeduppaths() never rewrites a "" entry into an absolute cwd.
// gopy mirrors that: installPathFinder records the entry here, and
// prependSysPath0 inserts it once site has run.
//
// CPython: Modules/main.c:pymain_run_python (sys.path[0] insertion)
var (
	sysPath0Entry   string
	sysPath0Present bool
)

// hasXOption reports whether the -X option named key was supplied,
// matching against the part before any '=' so "-X dev" and "-X dev=1"
// both count.
//
// CPython: Python/preconfig.c:582 _Py_get_xoption
func hasXOption(xOptions []string, key string) bool {
	for _, opt := range xOptions {
		name, _, _ := strings.Cut(opt, "=")
		if name == key {
			return true
		}
	}
	return false
}

// installPathFinder wires the PathFinder consulted by imp.ImportModule
// after the inittab miss. The path list mirrors CPython's
// _PyConfig_InitPathConfig prefix: argv[0]'s parent directory (or "."
// for -c / interactive), PYTHONPATH entries, then the vendored
// stdlib root. The full initconfig path resolution lands later; this
// is the slice that matters for unittest enablement.
//
// CPython: Python/initconfig.c:1734 _PyConfig_InitPathConfig
// CPython: Lib/importlib/_bootstrap_external.py:1196 PathFinder
func installPathFinder(scriptPath string) {
	// The leading sys.path entry (script dir, or "" for -c / -m /
	// interactive) is NOT placed in `paths` here: CPython inserts
	// config->sys_path_0 after site.main() runs, so site.removeduppaths()
	// does not rewrite a "" entry into an absolute cwd. prependSysPath0
	// adds it once site has run.
	var paths []string
	switch {
	case safePathMode:
		// safe_path drops the leading script-dir / cwd / "" entry so an
		// importable name is resolved only from PYTHONPATH and the stdlib.
		// CPython leaves config->sys_path_0 unset, which disables the
		// module-shadowing heuristic.
		//
		// CPython: Python/initconfig.c:1828 config_init_safe_path
		sysPath0Present = false
		imp.SetConfigSysPath0("", false)
	case scriptPath != "":
		// config->sys_path_0 for a script is the ABSOLUTE directory of the
		// script file (CPython resolves it), so the shadowing check and the
		// live sys.path[0] both use an absolute path.
		dir := filepath.Dir(scriptPath)
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		sysPath0Entry = dir
		sysPath0Present = true
		imp.SetConfigSysPath0(dir, true)
	default:
		sysPath0Entry = ""
		sysPath0Present = true
		imp.SetConfigSysPath0("", true)
	}
	if env := os.Getenv("PYTHONPATH"); env != "" {
		for _, p := range strings.Split(env, string(os.PathListSeparator)) {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	if root := findStdlibRoot(); root != "" {
		paths = append(paths, root)
		// Materialize the compiled-in extension modules as stub files in a
		// lib-dynload directory and add it to sys.path, the gopy analog of
		// CPython's <prefix>/lib-dynload. The real PathFinder -> FileFinder
		// discovers them by suffix and routes them through ExtensionFileLoader
		// -> _imp.create_dynamic, so module.__spec__.loader is an
		// ExtensionFileLoader exactly as for a CPython .so. The stub lives
		// outside the vendored stdlib tree so that tree stays pristine.
		//
		// CPython: Modules/getpath.py (lib-dynload on sys.path)
		dynload := filepath.Join(os.TempDir(), "gopy-lib-dynload")
		if err := imp.MaterializeExtensions(dynload); err == nil {
			paths = append(paths, dynload)
		}
		// Expose the resolved stdlib root as sys._stdlib_dir so
		// FrozenImporter._resolve_filename can compute __file__ and a
		// frozen package's __path__ against the on-disk Lib copy, letting
		// an unfrozen submodule (e.g. __phello__.spam from disk) be found
		// when its parent was loaded frozen.
		//
		// CPython: Lib/importlib/_bootstrap.py:1108 _resolve_filename
		sys.SetStdlibDir(root)
		// Pin the resolved root into the environment so any subprocess
		// this interpreter spawns through sys.executable bootstraps from
		// the same stdlib, even when it runs in an unrelated cwd (e.g.
		// subprocess.run(cwd=tmpdir)). CPython's child interpreters
		// self-locate from the executable's prefix; gopy carries it
		// explicitly via GOPY_STDLIB.
		//
		// CPython: Modules/getpath.py:550 calculate_path (prefix inherited)
		if os.Getenv("GOPY_STDLIB") == "" {
			_ = os.Setenv("GOPY_STDLIB", root)
		}
	}
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    paths,
		Compiler: gopyCompile,
	})
	// Frozen test modules (__hello__, __phello__ and friends) keep their
	// source verbatim and compile lazily through the same compiler the
	// path finder uses.
	//
	// CPython: Python/frozen.c _PyImport_FrozenModules
	imp.FrozenCompiler = gopyCompile
	sys.SetPath(paths)
	// Wire the meta-path finder to consult the live sys.path so
	// `sys.path.insert(0, x)` from user code is honored on the next
	// import. CPython's PathFinder.find_spec reads sys.path every
	// call; gopy mirrors that via this hook.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
	imp.SetLivePathHook(sys.LivePath)
}

// prependSysPath0 inserts the leading sys.path entry (config->sys_path_0)
// recorded by installPathFinder. CPython does this after site.main()
// runs, so the entry (notably "" for -c) is never absolutized by
// site.removeduppaths(). Call it once the site bootstrap has completed.
//
// CPython: Modules/main.c:pymain_run_python (sys.path[0] insertion)
func prependSysPath0() {
	if !sysPath0Present {
		return
	}
	cur := sys.LivePath()
	sys.SetPath(append([]string{sysPath0Entry}, cur...))
}

// bootstrapEncodings imports the encodings package so its
// search_function lands in the codec search path. CPython does this
// from _PyCodec_Init at the tail of interpreter startup, after the
// path config is wired and the C-side error handlers are registered.
// Without it, codecs.lookup of anything but utf-8 / ascii / latin-1
// (the Go-side registered codecs) fails, and the lexer cannot decode
// source files that carry a non-ASCII PEP 263 encoding cookie.
//
// We drive the import through pythonrun so the package's
// `codecs.register(search_function)` call at module init runs under
// a real Executor; imp.ImportModule(nil, ...) only works for inittab
// entries and would crash trying to exec Python source.
//
// CPython: Python/codecs.c:1690 _PyCodec_Init (PyImport_ImportModule "encodings")
func bootstrapEncodings(ts *state.Thread, globals *objects.Dict, stderr *os.File) int {
	// Initialize the importlib bootstrap before any Python-level import.
	// CPython freezes importlib._bootstrap / _bootstrap_external and runs
	// init_importlib well before _PyCodec_Init. gopy loads them as regular
	// .py modules on first reference; the encodings preload below pulls
	// _bootstrap_external in transitively (encodings -> codecs ->
	// importlib.util -> _bootstrap_external). Importing it here first means
	// it is fully cached before encodings runs, so its own load does not
	// re-enter the import system while the encodings package is still
	// half-initialized (which would strand `from . import aliases`).
	//
	// CPython: Python/pylifecycle.c:1041 init_importlib_external
	// Two-phase importlib install, mirroring init_importlib /
	// init_importlib_external. importlib/__init__.py self-bootstraps via
	// its `except ImportError` branch (gopy has no frozen _frozen_importlib),
	// which runs _bootstrap._setup(sys, _imp) and binds _bootstrap_external.
	// Phase 2 then calls _bootstrap_external._install(_bootstrap) directly
	// (CPython's _install_external_importers imports _frozen_importlib_external,
	// which gopy lacks), appending PathFinder to sys.meta_path and the
	// FileFinder path hook to sys.path_hooks.
	//
	// CPython: Python/pylifecycle.c:1041 init_importlib_external
	// CPython runs init_importlib exactly once, against a fresh per-process
	// interpreter. gopy reuses one process-wide sys.modules across every run()
	// invocation (the cmd/gopy tests call run() several times in a single
	// binary), so the install must be idempotent. Once the import system is
	// live, sys.modules already holds _frozen_importlib aliased to the source
	// _bootstrap module, which carries no __origname__; re-running
	// _bootstrap._install would make _setup re-scan sys.modules and trip the
	// frozen fix-up assert on it. Guard the whole install on the first run.
	//
	// CPython: Python/pylifecycle.c:1041 init_importlib_external
	install := "import sys\n" +
		"if '_frozen_importlib' not in sys.modules:\n" +
		" import importlib, _imp\n" +
		" from importlib import _bootstrap, _bootstrap_external\n" +
		" _bootstrap._install(sys, _imp)\n" +
		" _bootstrap_external._install(_bootstrap)\n" +
		// CPython's C bootstrap freezes _bootstrap / _bootstrap_external and
		// publishes them under the _frozen_importlib* names; importlib then
		// aliases those exact objects to importlib._bootstrap[_external]. gopy
		// loads them as plain .py modules, so re-publish the same objects
		// under the frozen names to keep sys.modules['_frozen_importlib'] and
		// importlib._bootstrap identical (issue #15386 / bootstrap tests).
		//
		// CPython: Lib/importlib/__init__.py:50 (_bootstrap aliasing)
		" sys.modules['_frozen_importlib'] = _bootstrap\n" +
		" sys.modules['_frozen_importlib_external'] = _bootstrap_external\n" +
		// CPython registers the zipimporter path hook ahead of FileFinder
		// (C-side, _PyImportZip_Init) so a sys.path entry pointing at a zip
		// archive is claimed before the directory finder rejects it.
		// CPython: Python/pylifecycle.c init_importlib_external (zipimport)
		" try:\n" +
		"  import zipimport\n" +
		"  sys.path_hooks.insert(0, zipimport.zipimporter)\n" +
		" except ImportError:\n" +
		"  pass\n" +
		// CPython freezes importlib._bootstrap[_external] and the importlib
		// package, so _setup gives them a __spec__ via the frozen loader
		// before any user import runs. gopy loads these as plain .py files
		// through the Go-side driver during this bootstrap, before the
		// machinery is live, so they reach sys.modules without __spec__.
		// Rebuild a SourceFileLoader spec for every still-spec-less module
		// that carries a __file__ (importlib, importlib._bootstrap,
		// _bootstrap_external, importlib.util), matching the spec PathFinder
		// would have produced. Without __spec__ on the importlib package,
		// `import importlib.util` raises AttributeError at _bootstrap.py:1325.
		//
		// CPython: Lib/importlib/_bootstrap.py:1517 _setup (spec fix-up loop)
		" for _n in list(sys.modules):\n" +
		"  _m = sys.modules[_n]\n" +
		"  if getattr(_m, '__spec__', None) is None and getattr(_m, '__file__', None):\n" +
		"   try:\n" +
		"    _sp = _bootstrap_external.spec_from_file_location(_n, _m.__file__)\n" +
		"    _bootstrap._init_module_attrs(_sp, _m, override=True)\n" +
		"   except Exception:\n" +
		"    pass\n"
	if _, err := pythonrun.RunString(ts, install, "<bootstrap>", parser.ModeFile, globals, nil); err != nil {
		fmt.Fprintln(stderr, "preload importlib:", err)
		return 1
	}
	if _, err := pythonrun.RunString(ts, "import encodings", "<bootstrap>", parser.ModeFile, globals, nil); err != nil {
		fmt.Fprintln(stderr, "preload encodings:", err)
		return 1
	}
	return 0
}

// bootstrapSite imports the site module, which runs site.main() at import
// time (the no_site flag is clear) to install the interpreter builtins
// exit / quit / help / copyright / credits / license via setquit /
// setcopyright / sethelper. CPython drives this from init_import_site
// during Py_Initialize after the import system is online; without it
// sys.flags.no_site reads 0 (claiming site loaded) while the builtins it
// installs are missing, so code.InteractiveConsole(local_exit=True) and
// other site-dependent paths diverge.
//
// CPython: Python/pylifecycle.c:1255 init_import_site (PyImport_ImportModule "site")
func bootstrapSite(ts *state.Thread, globals *objects.Dict, stderr *os.File) int {
	if _, err := pythonrun.RunString(ts, "import site", "<bootstrap>", parser.ModeFile, globals, nil); err != nil {
		fmt.Fprintln(stderr, "preload site:", err)
		return 1
	}
	return 0
}

// findStdlibRoot locates the vendored gopy stdlib tree. CPython's
// equivalent is Modules/getpath.py's prefix discovery; the gopy port
// (pathconfig/) targets the CPython install layout, not the gopy
// repo layout, so this entry uses a smaller resolver:
//
//  1. $GOPY_STDLIB if set and points at a directory.
//  2. Walk up from the executable until a stdlib/unittest/ entry
//     shows up; the gopy binary lives next to its source tree
//     during development and inside the install root in releases.
//  3. Walk up from the cwd looking for the same marker. This is the
//     common case for `go run ./cmd/gopy` and for tests that
//     execute under the repo.
//
// Returns the empty string when no candidate exists; the caller
// silently drops it from sys.path.
//
// CPython: Modules/getpath.py:550 calculate_path
func findStdlibRoot() string {
	if env := os.Getenv("GOPY_STDLIB"); env != "" {
		if isDir(env) {
			return env
		}
	}
	if exe, err := os.Executable(); err == nil {
		if root := walkUpForStdlib(filepath.Dir(exe)); root != "" {
			return root
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if root := walkUpForStdlib(cwd); root != "" {
			return root
		}
	}
	return ""
}

// walkUpForStdlib walks parent directories looking for a `stdlib`
// folder that contains the unittest marker file. Returns the
// `stdlib` path on hit, or "" if the search reaches the filesystem
// root with nothing.
func walkUpForStdlib(start string) string {
	dir := start
	for {
		candidate := filepath.Join(dir, "stdlib")
		if isDir(candidate) && isFile(filepath.Join(candidate, "unittest", "__init__.py")) {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isDir(p string) bool {
	info, err := os.Stat(p) //nolint:gosec // p is os.Executable/os.Getwd/$GOPY_STDLIB derived.
	return err == nil && info.IsDir()
}

func isFile(p string) bool {
	info, err := os.Stat(p) //nolint:gosec // p is os.Executable/os.Getwd/$GOPY_STDLIB or the argv script path the user asked us to run.
	return err == nil && info.Mode().IsRegular()
}

// isImporterPath reports whether p is an import-path entry rather than a
// source file: a directory (which the FileFinder path hook always claims)
// or a ZIP archive (which the zipimporter path hook claims). CPython makes
// the same determination by running PyImport_GetImporter over sys.path_hooks
// in pymain_get_importer; a regular .py file yields None and is run as a
// plain script instead.
//
// CPython: Modules/main.c:127 pymain_get_importer
// CPython: Lib/zipimport.py zipimporter.__init__ (ZIP end-of-central-directory probe)
func isImporterPath(p string) bool {
	if isDir(p) {
		return true
	}
	if !isFile(p) {
		return false
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return false
	}
	_ = zr.Close()
	return true
}

// gopyCompile is the SourceCompiler injected into PathFinder. It is
// the parser + compiler chain that pythonrun.RunString runs.
//
// The bytes-input path runs PEP 263 cookie detection and BOM
// stripping in lexer.FromBytes so imported source files with a
// `# coding: ...` declaration decode through the right codec.
// Mirrors CPython's compile(bytes_source, ...) routing through
// _Py_SourceAsString and _PyTokenizer_FromString.
//
// CPython: Python/pythonrun.c:1102 Py_CompileStringExFlags
func gopyCompile(src []byte, filename string) (*objects.Code, error) {
	if len(src) == 0 || src[len(src)-1] != '\n' {
		src = append(src, '\n')
	}
	// CPython freezes importlib._bootstrap[_external], so the code objects of
	// the import machinery carry the synthetic co_filename
	// "<frozen importlib._bootstrap>" rather than a source path. gopy loads
	// them from source; stamp the same frozen name so tracebacks that pass
	// through the machinery read identically (test_import_bug) and
	// remove_importlib_frames can recognize them.
	//
	// CPython: Python/pylifecycle.c:1041 init_importlib (frozen modules)
	filename = frozenImportlibName(filename)
	mod, err := parser.ParseBytes(src, filename, parser.ModeFile)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, filename, 0)
	if err != nil {
		return nil, err
	}
	out := &objects.Code{
		Version:         objects.AllocCodeVersion(),
		Argcount:        cco.Argcount,
		PosonlyArgcount: cco.PosOnlyArgCount,
		KwonlyArgcount:  cco.KwOnlyArgCount,
		Stacksize:       cco.Stacksize,
		Flags:           int(cco.Flags),
		Code:            cco.Code,
		Consts:          cco.Consts,
		Names:           cco.Names,
		Varnames:        cco.VarNames,
		Freevars:        cco.FreeVars,
		Cellvars:        cco.CellVars,
		Filename:        cco.Filename,
		Name:            cco.Name,
		Qualname:        cco.Qualname,
		Firstlineno:     cco.Firstlineno,
		Linetable:       cco.Linetable,
		ExceptionTable:  cco.ExceptionTable,
	}
	out.Init(objects.CodeType)
	out.SyncNameObjs()
	out.SyncConstObjs()
	return out, nil
}

// frozenImportlibName maps the source paths of the two importlib bootstrap
// modules to the synthetic co_filename CPython gives their frozen code
// objects. Any other path is returned unchanged.
//
// CPython: Python/import.c:3501 remove_importlib_frames (frozen names)
func frozenImportlibName(filename string) string {
	switch {
	case strings.HasSuffix(filename, "importlib/_bootstrap_external.py"):
		return "<frozen importlib._bootstrap_external>"
	case strings.HasSuffix(filename, "importlib/_bootstrap.py"):
		return "<frozen importlib._bootstrap>"
	}
	return filename
}

// runSource is the gopy -c entry. It dispatches to
// pythonrun.RunSimpleString, the port of CPython's
// PyRun_SimpleStringFlags.
//
// CPython: Modules/main.c:289 pymain_run_command
func runSource(src string, stdout, stderr *os.File) int {
	g, err := bootstrapBuiltins(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder("")
	mainGlobals := newMainGlobals(g, "__main__")
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	prependSysPath0()
	rc := pythonrun.RunSimpleString(ts, src, mainGlobals, stderr)
	gc.RunShutdownFinalizers()
	pythonrun.FlushStdFiles()
	return rc
}

// runModule is the gopy -m mod entry. Mirrors pymain_run_module: set
// sys.argv to (mod_name, *args) as a placeholder, then hand off to
// runpy._run_module_as_main which resolves mod_name, sets argv[0] to
// the module's file path, and executes the module in the __main__
// namespace.
//
// CPython: Modules/main.c:294 pymain_run_module
func runModule(modName string, modArgs []string, stdout, stderr *os.File) int {
	g, err := bootstrapBuiltins(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder("")
	sys.SetArgv(append([]string{modName}, modArgs...))
	mainGlobals := newMainGlobals(g, "__main__")
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	prependSysPath0()
	// Equivalent of CPython's pymain_run_module which calls
	// runpy._run_module_as_main(modName) on the Python side.
	src := fmt.Sprintf("import runpy\nrunpy._run_module_as_main(%q)\n", modName)
	rc := pythonrun.RunSimpleString(ts, src, mainGlobals, stderr)
	gc.RunShutdownFinalizers()
	pythonrun.FlushStdFiles()
	return rc
}

// runImporterMain is the gopy <dir-or-zip> entry. When the positional
// argument is an import-path entry (directory or ZIP archive), CPython
// prepends it to sys.path and runs its __main__ submodule via
// pymain_run_module(L"__main__", 0). The set_argv0=0 argument tells
// runpy._run_module_as_main NOT to rewrite argv[0]: it stays the archive
// path, which the dispatch already placed there. The module loads through
// the path hook that claims the entry (FileFinder for a directory,
// zipimporter for a ZIP), so its code object carries the loader's
// co_filename (e.g. "app.zip/__main__.py") and tracebacks resolve source
// through the loader's get_source rather than reading the raw archive.
//
// CPython: Modules/main.c:691 pymain_run_python (main_importer_path branch)
// CPython: Modules/main.c:326 pymain_run_module (set_argv0=0)
func runImporterMain(importerPath string, stdout, stderr *os.File) int {
	g, err := bootstrapBuiltins(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	// sys.path[0] is the import-path entry itself (the dir/zip), not its
	// parent directory. CPython sets path0 = main_importer_path directly,
	// even under -P / -I (the safe_path branch is skipped once an importer
	// claims the entry).
	//
	// CPython: Modules/main.c:647 path0 = Py_NewRef(main_importer_path)
	installPathFinder("")
	abs, absErr := filepath.Abs(importerPath)
	if absErr != nil {
		abs = importerPath
	}
	sysPath0Entry = abs
	sysPath0Present = true
	imp.SetConfigSysPath0(abs, true)
	mainGlobals := newMainGlobals(g, "__main__")
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	prependSysPath0()
	// pymain_run_module(L"__main__", 0): run __main__ from the import-path
	// entry without altering argv[0].
	src := "import runpy\nrunpy._run_module_as_main(\"__main__\", False)\n"
	rc := pythonrun.RunSimpleString(ts, src, mainGlobals, stderr)
	gc.RunShutdownFinalizers()
	pythonrun.FlushStdFiles()
	return rc
}

// runFile is the gopy <script.py> entry. Mirrors pymain_run_file in
// the file-positional branch.
//
// For test_*.py files that import unittest but do not call
// unittest.main() themselves, a runner suffix is appended so the tests
// actually execute. CPython's regrtest does the equivalent by calling
// unittest.main(module=module) after importing the test module; gopy
// cannot import-then-run cleanly without a full regrtest port, so the
// suffix approach achieves the same result when running a single file.
//
// CPython: Modules/main.c:373 pymain_run_file
// CPython: Lib/test/libregrtest/runtest.py unittest.main call
func runFile(path string, stdout, stderr *os.File) int {
	g, err := bootstrapBuiltins(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder(path)
	modName := mainModuleName(path)
	mainGlobals := newMainGlobals(g, modName)
	// Anchor relative imports inside a vendored test package. CPython's
	// import machinery stamps __package__ when it loads the module; a
	// synthesized main module would otherwise have no anchor and any
	// `from . import x` inside it would raise "no known parent package".
	//
	// CPython: Lib/importlib/_bootstrap.py:1350 _calc___package__
	if pkg := mainPackageName(path, modName); pkg != "" {
		_ = mainGlobals.SetItem(objects.NewStr("__package__"), objects.NewStr(pkg))
		// A package's __init__ also carries __path__ pointing at its dir,
		// so submodule imports (`from .data import x`) find sibling files.
		if filepath.Base(path) == "__init__.py" {
			if abs, absErr := filepath.Abs(filepath.Dir(path)); absErr == nil {
				_ = mainGlobals.SetItem(objects.NewStr("__path__"),
					objects.NewList([]objects.Object{objects.NewStr(abs)}))
			}
		}
	}
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	prependSysPath0()
	// A vendored test runs under "test.<name>"; regrtest imports it as a
	// normal module, so its __spec__ is a real ModuleSpec. Build the same
	// file-location spec here so code that resolves the module by name and
	// reads __spec__ (pyclbr, runpy, inspect) matches CPython. The plain
	// "__main__" run keeps __spec__ None, like `python script.py`. The
	// snippet runs through pythonrun so importlib.util loads under a real
	// Executor, the same way bootstrapEncodings drives the encodings import.
	//
	// CPython: Lib/test/libregrtest/runtest.py (imports test.<name>)
	if modName != "__main__" {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		// regrtest imports the test under "test.<name>" the normal way, so
		// the import machinery runs setattr(parent_package, child, module):
		// the `test` package ends up with a `test_import` attribute. gopy
		// pre-injects the gate module into sys.modules without that parent
		// binding, so a test like data/circular_imports/.../child.py that
		// evaluates `test.test_import.<...>` as an expression would fail its
		// first hop getattr(test, 'test_import'). Import the parent package
		// and bind the leaf to mirror what _find_and_load does.
		//
		// CPython: Lib/importlib/_bootstrap.py:1350 setattr(parent_module, child, module)
		src := fmt.Sprintf("import importlib, importlib.util as _u, sys as _s\n"+
			"_m = _s.modules.get(%q)\n"+
			"if _m is not None and getattr(_m, '__spec__', None) is None:\n"+
			"    _m.__spec__ = _u.spec_from_file_location(%q, %q)\n"+
			"    _m.__loader__ = _m.__spec__.loader\n"+
			"_parent, _, _child = %q.rpartition('.')\n"+
			"if _m is not None and _parent:\n"+
			"    setattr(importlib.import_module(_parent), _child, _m)\n"+
			"del importlib, _u, _s, _m, _parent, _child\n", modName, modName, abs, modName)
		if _, err := pythonrun.RunString(ts, src, "<spec>", parser.ModeFile, mainGlobals, nil); err != nil {
			fmt.Fprintln(stderr, "attach main spec:", err)
		}
	}
	var rc int
	if suffix, ok := unittestRunnerSuffix(path); ok {
		src, readErr := os.ReadFile(path) //nolint:gosec // reading a caller-supplied test file path is the entire contract
		if readErr != nil {
			fmt.Fprintln(stderr, readErr)
			return 1
		}
		// Set __file__ so test methods can open the script, and compile
		// the rewritten source under the real path (not "<string>") so
		// the code object's co_filename matches the file: frame repr and
		// traceback source-line lookup both key off it.
		_ = mainGlobals.SetItem(objects.NewStr("__file__"), objects.NewStr(path))
		combined := string(src) + suffix
		rc = pythonrun.RunSimpleStringWithName(ts, combined, path, mainGlobals, stderr)
	} else {
		rc = pythonrun.RunAnyFile(ts, path, mainGlobals, stderr)
	}
	gc.RunShutdownFinalizers()
	pythonrun.FlushStdFiles()
	return rc
}

// unittestRunnerSuffix returns a suffix that calls unittest.main() when
// the file is a test_*.py that uses unittest but does not invoke the
// runner itself. Mirrors what CPython's regrtest does when it imports a
// test module and calls unittest.main(module=module).
//
// CPython: Lib/test/libregrtest/runtest.py unittest.main
func unittestRunnerSuffix(path string) (string, bool) {
	base := filepath.Base(path)
	// A package test is laid out as test_xxx/__init__.py; accept it too so
	// the runner fires even though its basename is not test_*.py. The
	// module runs under "test.test_xxx" (not "__main__"), so its own
	// `if __name__ == '__main__'` guard never triggers the suite.
	isPkgInit := base == "__init__.py" && strings.HasPrefix(filepath.Base(filepath.Dir(path)), "test_")
	if !isPkgInit && (!strings.HasPrefix(base, "test_") || !strings.HasSuffix(base, ".py")) {
		return "", false
	}
	src, err := os.ReadFile(path) //nolint:gosec // reading a caller-supplied test file path is the entire contract
	if err != nil {
		return "", false
	}
	if !bytes.Contains(src, []byte("unittest")) {
		return "", false
	}
	// A bare top-level unittest.main() call (one not guarded by
	// `if __name__ == '__main__'`) already drives the suite, so a second
	// runner would double-run it. Guarded calls no longer fire because
	// the module runs under "test.<name>" rather than "__main__", so the
	// appended runner takes over for those.
	if bytes.Contains(src, []byte("unittest.main")) && !bytes.Contains(src, []byte("__main__")) {
		return "", false
	}
	// Drive the suite against the running module by name. Under
	// mainModuleName a vendored test runs as "test.<name>", so resolve
	// the module via sys.modules[__name__] rather than __main__.
	return "\nimport sys as _gpsys, unittest as _gput\n_gput.main(module=_gpsys.modules[__name__], verbosity=2)\n", true
}

// mainModuleName returns the dotted module name a script runs under.
// Vendored CPython tests live in the `test` package and bake their
// package-qualified name into doctest and error-message expectations
// (for example "test.test_extcall.f()"), so a test_*.py file runs under
// "test.<name>" to match what regrtest produces. Everything else runs
// as the conventional "__main__".
//
// CPython: Lib/test/libregrtest/runtest.py (imports test.<name>)
func mainModuleName(path string) string {
	base := filepath.Base(path)
	// A package laid out as test_xxx/__init__.py runs under the dotted
	// name "test.test_xxx": regrtest imports the directory as a package, so
	// the __init__ body sees __name__ == "test.test_xxx" and relative
	// imports inside it resolve against that anchor.
	if base == "__init__.py" {
		parent := filepath.Base(filepath.Dir(path))
		if strings.HasPrefix(parent, "test_") {
			return "test." + parent
		}
		return "__main__"
	}
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		stem := strings.TrimSuffix(base, ".py")
		// A module that lives inside a vendored test package (the parent
		// directory is a test_xxx/ with an __init__.py) runs under the
		// package-qualified name "test.test_xxx.test_yyy", not the bare
		// "test.test_yyy". Otherwise it would shadow the package object in
		// sys.modules, and a sibling import like
		// `from test.test_doctest.decorator_mod import ...` would find the
		// module instead of the package and raise "not a package".
		//
		// CPython: Lib/test/libregrtest/runtest.py (imports test.<pkg>.<mod>)
		parent := filepath.Base(filepath.Dir(path))
		if strings.HasPrefix(parent, "test_") && isFile(filepath.Join(filepath.Dir(path), "__init__.py")) {
			return "test." + parent + "." + stem
		}
		return "test." + stem
	}
	return "__main__"
}

// mainPackageName returns the __package__ anchor for the main module at
// path. A package __init__ anchors at its own dotted name; a plain module
// anchors at its parent package. Relative imports inside the file resolve
// against this value.
//
// CPython: Lib/importlib/_bootstrap.py:1350 _calc___package__
func mainPackageName(path, modName string) string {
	if modName == "__main__" {
		return ""
	}
	if filepath.Base(path) == "__init__.py" {
		return modName
	}
	if dot := strings.LastIndex(modName, "."); dot >= 0 {
		return modName[:dot]
	}
	return ""
}

// runInteractive is the gopy bare-invocation entry: print the banner
// and hand control to pythonrun.InteractiveLoop. Mirrors
// pymain_run_stdin.
//
// CPython: Modules/main.c:469 pymain_run_stdin
func runInteractive(stdout, stderr *os.File) int {
	fmt.Fprintln(stdout, build.VersionString())
	g, err := bootstrapBuiltins(stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder("")
	mainGlobals := newMainGlobals(g, "__main__")
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	prependSysPath0()
	rc := pythonrun.InteractiveLoop(ts, os.Stdin, stdout, stderr, mainGlobals)
	pythonrun.FlushStdFiles()
	if rc != 0 {
		return 1
	}
	return 0
}

// bootstrapBuiltins initializes the builtins dict and registers it as
// the builtins module so `import builtins` and frame.Builtins both
// resolve to the same dict. sys is force-imported here so sys.stdout
// / sys.stderr are in the module cache before any user code runs;
// CPython does the equivalent in _PySys_Create during Py_Initialize.
//
// CPython: Python/sysmodule.c:3818 _PySys_InitMain
func bootstrapBuiltins(stdout, stderr *os.File) (*objects.Dict, error) {
	g, err := builtins.Init(stdout)
	if err != nil {
		return nil, err
	}
	// builtins.Init already stamps the canonical builtins module into
	// sys.modules and records it as the m_self of every module-level
	// builtin. Re-wrapping the dict here would register a second module
	// object sharing the same dict, which makes `len.__self__ is
	// builtins` false. So we leave Init's registration in place.
	// Plumb the CLI's stdout/stderr into sys before sys is built so
	// callers that redirect via *os.File pipes (tests, the harness) see
	// print() output land in the redirected target.
	//
	// CPython: Python/sysmodule.c:3795 sys_init_streams
	sys.SetStdio(stdout, stderr)
	if _, err := imp.ImportModule(nil, "sys"); err != nil {
		return nil, fmt.Errorf("preload sys: %w", err)
	}
	return g, nil
}

// newMainGlobals builds the dict the script runs against. It is
// separate from the builtins dict so __name__ on globals reads as
// "__main__" (not "builtins"), matching CPython's pymain_run_command
// which executes -c against PyImport_AddModule("__main__").__dict__.
//
// CPython: Modules/main.c:289 pymain_run_command (PyImport_AddModule)
// CPython: Python/pylifecycle.c init_interp_main (sets __main__)
func newMainGlobals(builtinsDict *objects.Dict, name string) *objects.Dict {
	mainDict := objects.NewDict()
	_ = mainDict.SetItem(objects.NewStr("__name__"), objects.NewStr(name))
	// CPython binds __main__.__builtins__ to the builtins *module* object;
	// every other module receives the builtins dict instead. The frame
	// builder unwraps the module back to its dict for LOAD_GLOBAL, so the
	// only observable difference is that `del __builtins__.__import__`
	// reaches a module attribute, after which the import machinery raises
	// ImportError (test_import.test_delete_builtins_import).
	//
	// CPython: Python/pylifecycle.c init_interp_main (binds __main__.__builtins__)
	var builtinsBinding objects.Object = builtinsDict
	if bm, ok := imp.GetModule("builtins"); ok {
		builtinsBinding = bm
	}
	_ = mainDict.SetItem(objects.NewStr("__builtins__"), builtinsBinding)
	// add_main_module gives __main__ a __loader__ of BuiltinImporter when the
	// dict has none yet. imp.is_builtin("__main__") is False, but CPython picks
	// BuiltinImporter as the most appropriate initial loader so that every
	// module in sys.modules answers hasattr(m, '__loader__')
	// (test_importlib test_everyone_has___loader__).
	//
	// CPython: Python/pylifecycle.c add_main_module (sets __loader__)
	if has, _ := mainDict.Contains(objects.NewStr("__loader__")); !has {
		_ = mainDict.SetItem(objects.NewStr("__loader__"), imp.BuiltinImporterLoader())
	}
	// CPython always binds __main__.__spec__: None for `-c`/script runs,
	// a real ModuleSpec under `-m`. runFile overwrites this with a
	// file-location spec for vendored "test.<name>" runs.
	//
	// CPython: Python/pylifecycle.c init_interp_main (sets __main__.__spec__)
	_ = mainDict.SetItem(objects.NewStr("__spec__"), objects.None())
	mod := objects.NewModuleWithDict(name, mainDict)
	if _, ok := imp.GetModule(name); !ok {
		imp.AddModule(name, mod)
	}
	// When a vendored test runs under "test.<name>" the runner harness
	// and any code that does __import__('__main__') still expect a
	// __main__ entry, so alias it to the same module object. doctest's
	// _normalize_module(None) instead resolves sys.modules[__name__],
	// which the registration above already covers.
	if name != "__main__" {
		if _, ok := imp.GetModule("__main__"); !ok {
			imp.AddModule("__main__", mod)
		}
	}
	return mainDict
}
