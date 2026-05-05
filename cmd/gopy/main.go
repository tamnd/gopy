// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
//
// CPython: Programs/python.c:13 main
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/gopy/build"
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("gopy", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		showVersion   bool
		showCopyright bool
		evalSrc       string
	)
	fs.BoolVar(&showVersion, "version", false, "print the gopy version and exit")
	fs.BoolVar(&showVersion, "V", false, "print the gopy version and exit (shorthand)")
	fs.BoolVar(&showCopyright, "copyright", false, "print the copyright notice and exit")
	fs.StringVar(&evalSrc, "c", "", "program passed in as string (terminates option list)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: gopy [options]\n\noptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch {
	case showVersion:
		fmt.Fprintln(stdout, build.VersionString())
		return 0
	case showCopyright:
		fmt.Fprint(stdout, build.Copyright)
		return 0
	case evalSrc != "":
		return runSource(evalSrc, stdout, stderr)
	case fs.NArg() > 0:
		return runFile(fs.Arg(0), stdout, stderr)
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
