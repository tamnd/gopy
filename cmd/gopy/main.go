// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
//
// CPython: Programs/python.c:13 main
package main

import (
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
		scriptPath := argv[st.OptInd]
		sys.SetArgv(append([]string{scriptPath}, argv[st.OptInd+1:]...))
		return runFile(scriptPath, stdout, stderr)
	}
	sys.SetArgv([]string{""})
	return runInteractive(stdout, stderr)
}

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
	var paths []string
	switch {
	case scriptPath != "":
		paths = append(paths, filepath.Dir(scriptPath))
	default:
		paths = append(paths, "")
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
	sys.SetPath(paths)
	// Wire the meta-path finder to consult the live sys.path so
	// `sys.path.insert(0, x)` from user code is honored on the next
	// import. CPython's PathFinder.find_spec reads sys.path every
	// call; gopy mirrors that via this hook.
	//
	// CPython: Lib/importlib/_bootstrap_external.py:1290 path = sys.path
	imp.SetLivePathHook(sys.LivePath)
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
	install := "import importlib, sys, _imp\n" +
		"from importlib import _bootstrap, _bootstrap_external\n" +
		"_bootstrap._install(sys, _imp)\n" +
		"_bootstrap_external._install(_bootstrap)\n" +
		// CPython registers the zipimporter path hook ahead of FileFinder
		// (C-side, _PyImportZip_Init) so a sys.path entry pointing at a zip
		// archive is claimed before the directory finder rejects it.
		// CPython: Python/pylifecycle.c init_importlib_external (zipimport)
		"try:\n" +
		" import zipimport\n" +
		" sys.path_hooks.insert(0, zipimport.zipimporter)\n" +
		"except ImportError:\n" +
		" pass\n"
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
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
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
	// Equivalent of CPython's pymain_run_module which calls
	// runpy._run_module_as_main(modName) on the Python side.
	src := fmt.Sprintf("import runpy\nrunpy._run_module_as_main(%q)\n", modName)
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
	ts := state.NewThread()
	if rc := bootstrapEncodings(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
	if rc := bootstrapSite(ts, mainGlobals, stderr); rc != 0 {
		return rc
	}
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
		src := fmt.Sprintf("import importlib.util as _u, sys as _s\n"+
			"_m = _s.modules.get(%q)\n"+
			"if _m is not None and getattr(_m, '__spec__', None) is None:\n"+
			"    _m.__spec__ = _u.spec_from_file_location(%q, %q)\n"+
			"    _m.__loader__ = _m.__spec__.loader\n"+
			"del _u, _s, _m\n", modName, modName, abs)
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
	if !strings.HasPrefix(base, "test_") || !strings.HasSuffix(base, ".py") {
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
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return "test." + strings.TrimSuffix(base, ".py")
	}
	return "__main__"
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
	_ = mainDict.SetItem(objects.NewStr("__builtins__"), builtinsDict)
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
