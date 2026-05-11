// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
//
// CPython: Programs/python.c:13 main
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/getopt"
	"github.com/tamnd/gopy/imp"
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run drives _PyOS_GetOpt the same way pymain_init walks argv before
// the runtime config exists. argv is rewrapped with a leading program
// name because getopt.GetOpt starts at OptInd=1 (matching CPython's
// _PyOS_optind reset value).
//
// CPython: Modules/main.c:48 pymain_init
func run(args []string, stdout, stderr *os.File) int {
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
		default:
			// Other CPython flags (-b, -B, -O, -W, -X, ...) are accepted
			// for option-set parity. Wiring each to the runtime config
			// lands with initconfig.c in a later milestone.
		}
	}

	switch {
	case showVersion:
		fmt.Fprintln(stdout, build.VersionString())
		return 0
	case hasC:
		return runSource(evalSrc, stdout, stderr)
	case hasM:
		fmt.Fprintf(stderr, "gopy: -m %s: not implemented yet\n", modName)
		return 2
	}

	if st.OptInd < len(argv) {
		scriptPath := argv[st.OptInd]
		sys.SetArgv(append([]string{scriptPath}, argv[st.OptInd+1:]...))
		return runFile(scriptPath, stdout, stderr)
	}
	sys.SetArgv([]string{""})
	return runInteractive(stdout, stderr)
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
	}
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    paths,
		Compiler: gopyCompile,
	})
	sys.SetPath(paths)
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
// CPython: Python/pythonrun.c:1102 Py_CompileStringExFlags
func gopyCompile(src, filename string) (*objects.Code, error) {
	if src == "" || src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, filename, parser.ModeFile)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, filename, 0)
	if err != nil {
		return nil, err
	}
	return &objects.Code{
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
	}, nil
}

// runSource is the gopy -c entry. It dispatches to
// pythonrun.RunSimpleString, the port of CPython's
// PyRun_SimpleStringFlags. The globals dict comes from builtins.Init
// for now; once 1623 lands the import system, lifecycle.Main will
// build __main__ and pythonrun will read the dict from there.
//
// CPython: Modules/main.c:289 pymain_run_command
func runSource(src string, stdout, stderr *os.File) int {
	g, err := builtins.Init(stdout)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder("")
	ts := state.NewThread()
	return pythonrun.RunSimpleString(ts, src, g, stderr)
}

// runFile is the gopy <script.py> entry. Mirrors pymain_run_file in
// the file-positional branch.
//
// CPython: Modules/main.c:373 pymain_run_file
func runFile(path string, stdout, stderr *os.File) int {
	g, err := builtins.Init(stdout)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder(path)
	ts := state.NewThread()
	return pythonrun.RunAnyFile(ts, path, g, stderr)
}

// runInteractive is the gopy bare-invocation entry: print the banner
// and hand control to pythonrun.InteractiveLoop. Mirrors
// pymain_run_stdin.
//
// CPython: Modules/main.c:469 pymain_run_stdin
func runInteractive(stdout, stderr *os.File) int {
	fmt.Fprintln(stdout, build.VersionString())
	g, err := builtins.Init(stdout)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	installPathFinder("")
	ts := state.NewThread()
	if pythonrun.InteractiveLoop(ts, os.Stdin, stdout, stderr, g) != 0 {
		return 1
	}
	return 0
}
