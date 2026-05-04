// Command gopy is the gopy interpreter entry point. It mirrors the
// scaffolding role of CPython's Programs/python.c at this stage of the
// port. Subsequent milestones plug in the parser, compiler, and VM.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/gopy/build"
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
	)
	fs.BoolVar(&showVersion, "version", false, "print the gopy version and exit")
	fs.BoolVar(&showVersion, "V", false, "print the gopy version and exit (shorthand)")
	fs.BoolVar(&showCopyright, "copyright", false, "print the copyright notice and exit")

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
	}

	fmt.Fprintln(stdout, build.VersionString())
	fmt.Fprintln(stdout, "interactive interpreter not yet available; see https://github.com/tamnd/gopy")
	return 0
}
