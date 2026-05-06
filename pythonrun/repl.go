package pythonrun

import (
	"bufio"
	"fmt"
	"io"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
)

// PromptPS1 is the primary prompt displayed before each line of
// input. CPython grabs sys.ps1 first; that wiring lands with
// 1651-sys-D.
//
// CPython: Python/pythonrun.c:122 sys.ps1 default
const PromptPS1 = ">>> "

// PromptPS2 is the continuation prompt. The v0.7 line-buffered REPL
// does not ask for continuation input, so this is held for the
// multi-line work in 1645.
const PromptPS2 = "... "

// InteractiveLoop is the basic REPL: read a line from stdin, run it
// against globals, print any expression value, repeat until EOF.
// Mirrors PyRun_InteractiveLoopFlags. The v0.7 port uses bufio for
// line input; readline editing comes with 1645.
//
// Each line is tried in eval mode first (so "1 + 2" prints "3");
// on a parse failure, the line is retried in single-statement mode
// so assignments, print() calls, and other statements still run.
//
// CPython: Python/pythonrun.c:128 PyRun_InteractiveLoopFlags
func InteractiveLoop(ts *state.Thread, stdin io.Reader, stdout, stderr io.Writer, globals objects.Object) int {
	r := bufio.NewReader(stdin)
	for {
		fmt.Fprint(stdout, PromptPS1)
		line, err := r.ReadString('\n')
		if err == io.EOF {
			if line == "" {
				return 0
			}
			runOneInteractive(ts, line, globals, stdout, stderr)
			return 0
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return -1
		}
		runOneInteractive(ts, line, globals, stdout, stderr)
	}
}

// runOneInteractive runs a single line of REPL input. Mirrors
// PyRun_InteractiveOneObjectEx for the line-buffered case: try
// expression mode first to capture the value print(), fall back to
// statement mode so print() calls and assignments still run.
//
// CPython: Python/pythonrun.c:288 PyRun_InteractiveOneObjectEx
func runOneInteractive(ts *state.Thread, line string, globals objects.Object, stdout, stderr io.Writer) {
	if line == "" || line == "\n" {
		return
	}
	if v, err := RunString(ts, line, "<stdin>", parser.ModeEval, globals, nil); err == nil {
		if v != nil && v != objects.None() {
			s, serr := objects.Str(v)
			if serr != nil {
				printRunError(ts, serr, stderr)
				return
			}
			fmt.Fprintln(stdout, s)
		}
		return
	}
	if _, err := RunString(ts, line, "<stdin>", parser.ModeFile, globals, nil); err != nil {
		printRunError(ts, err, stderr)
	}
}
