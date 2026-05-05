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
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
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
	}

	fmt.Fprintln(stdout, build.VersionString())
	fmt.Fprintln(stdout, "interactive interpreter not yet available; see https://github.com/tamnd/gopy")
	return 0
}

// runSource pipes src through parser -> compile -> vm.EvalCode and
// reports failures on stderr. The v0.6 -c flag is the smoke harness:
// the parser/VM panel is incomplete, so most non-trivial programs will
// surface ErrNotImplemented or a parser bail; report and exit 1.
func runSource(src string, stdout, stderr *os.File) int {
	if src == "" || src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, "<string>", parser.ModeFile)
	if err != nil {
		fmt.Fprintln(stderr, "parse:", err)
		return 1
	}
	cco, err := compile.Compile(mod, "<string>", 0)
	if err != nil {
		fmt.Fprintln(stderr, "compile:", err)
		return 1
	}
	co := liftCode(cco)
	ts := state.NewThread()
	g, err := builtins.Init(stdout)
	if err != nil {
		fmt.Fprintln(stderr, "builtins:", err)
		return 1
	}
	v, err := vm.EvalCode(ts, co, g, nil)
	if err != nil {
		fmt.Fprintln(stderr, "eval:", err)
		return 1
	}
	if v != nil && v != objects.None() {
		s, serr := objects.Str(v)
		if serr != nil {
			fmt.Fprintln(stderr, "str:", serr)
			return 1
		}
		fmt.Fprintln(stdout, s)
	}
	return 0
}

// liftCode adapts compile.Code into objects.Code (same shape, two
// type names today). Collapses once spec 1687 retires compile.Code.
func liftCode(c *compile.Code) *objects.Code {
	return &objects.Code{
		Code:           c.Code,
		Consts:         c.Consts,
		Names:          c.Names,
		Varnames:       c.VarNames,
		Freevars:       c.FreeVars,
		Cellvars:       c.CellVars,
		Stacksize:      c.Stacksize,
		ExceptionTable: c.ExceptionTable,
	}
}
