// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
//
// CPython: Programs/python.c:13 main
package main

import (
	"fmt"
	"os"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/getopt"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
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
		return runFile(argv[st.OptInd], stdout, stderr)
	}
	return runInteractive(stdout, stderr)
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
	ts := state.NewThread()
	if pythonrun.InteractiveLoop(ts, os.Stdin, stdout, stderr, g) != 0 {
		return 1
	}
	return 0
}
