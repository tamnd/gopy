package lifecycle

import (
	"errors"
	"io"
	"os"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/initconfig"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

// ErrModuleNotImplemented is returned by Main when the resolved
// config asks for `-m module`. The import-driven module entry lands
// with v0.8 (spec 1623); v0.7 only handles -c, scripts, and the
// REPL.
var ErrModuleNotImplemented = errors.New("lifecycle: -m module dispatch lands with v0.8 (spec 1623)")

// Main is the gopy equivalent of CPython's Py_Main. It parses argv
// into a PyConfig, runs the init / dispatch / finalize sequence,
// and returns the process exit code.
//
// argv follows the Py_Main contract: argv[0] is the program name and
// the option scan starts at argv[1]. MainOSArgs passes os.Args
// directly so the binary name is always present.
//
// The dispatch arms map to the pythonrun ports landed in spec 1624:
//
//   - RunCommand (-c) -> pythonrun.RunSimpleString
//   - RunFilename     -> pythonrun.RunAnyFile
//   - RunModule (-m)  -> ErrModuleNotImplemented (v0.8 / spec 1623)
//   - default         -> pythonrun.InteractiveLoop
//
// stdin/stdout/stderr are threaded as parameters so tests can
// capture output; the cmd/gopy binary passes os.Stdin/Stdout/Stderr.
//
// CPython: Modules/main.c:811 Py_Main
func Main(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg := &initconfig.PyConfig{}
	cfg.InitPythonConfig()
	if len(argv) == 0 {
		argv = []string{"gopy"}
	}
	cfg.Argv = append([]string{}, argv...)

	rt := state.NewRuntime()
	tstate, status := Initialize(rt, cfg)
	if status.IsException() {
		_, _ = io.WriteString(stderr, status.ErrMsg+"\n")
		return 1
	}

	exitcode := runPython(tstate, stdin, stdout, stderr)

	if status := Finalize(rt); status.IsException() {
		// CPython's Py_RunMain returns 120 when Py_FinalizeEx fails.
		// CPython: Modules/main.c:781
		if exitcode == 0 {
			exitcode = 120
		}
	}
	return exitcode
}

// runPython is the dispatch core: pick the right pythonrun arm based
// on the resolved config. Mirrors pymain_run_python's switch on
// run_command / run_module / run_filename.
//
// CPython: Modules/main.c:614 pymain_run_python
func runPython(tstate *state.Thread, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, ok := tstate.Interp().Config.(*initconfig.PyConfig)
	if !ok || cfg == nil {
		_, _ = io.WriteString(stderr, "lifecycle: interpreter has no config\n")
		return 1
	}

	g, err := builtins.Init(stdout)
	if err != nil {
		_, _ = io.WriteString(stderr, "builtins: "+err.Error()+"\n")
		return 1
	}

	switch {
	case cfg.RunCommand != "":
		if pythonrun.RunSimpleString(tstate, cfg.RunCommand, g, stderr) != 0 {
			return 1
		}
		return 0
	case cfg.RunModule != "":
		_, _ = io.WriteString(stderr, ErrModuleNotImplemented.Error()+"\n")
		return 1
	case cfg.RunFilename != "":
		if pythonrun.RunAnyFile(tstate, cfg.RunFilename, g, stderr) != 0 {
			return 1
		}
		return 0
	default:
		return pythonrun.InteractiveLoop(tstate, stdin, stdout, stderr, g)
	}
}

// MainOSArgs is the convenience entry that wires Main to the
// real process argv and standard streams.
func MainOSArgs() int {
	return Main(os.Args, os.Stdin, os.Stdout, os.Stderr)
}
