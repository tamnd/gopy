package pythonrun

import (
	"errors"
	"fmt"
	"io"

	"github.com/tamnd/gopy/myreadline"
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

// PromptPS2 is the continuation prompt. CPython displays this when
// the parser asks for more input (unclosed bracket, triple-quote,
// indented block); gopy still feeds one logical line at a time, so
// the constant is wired through myreadline for the editor backend
// that wants to render it but the loop itself does not yet ask for
// continuation lines.
const PromptPS2 = "... "

// InteractiveLoop is the basic REPL: read a line from stdin, run it
// against globals, print any expression value, repeat until EOF.
// Mirrors PyRun_InteractiveLoopFlags. Each input line goes through
// myreadline.Readline so an installed line editor (peterh/liner, GNU
// readline via cgo, ...) sees the same prompt and signal handling as
// CPython hands to PyOS_ReadlineFunctionPointer.
//
// Each line is tried in eval mode first (so "1 + 2" prints "3");
// on a parse failure, the line is retried in single-statement mode
// so assignments, print() calls, and other statements still run.
//
// CPython: Python/pythonrun.c:128 PyRun_InteractiveLoopFlags
// CPython: Parser/myreadline.c:372 PyOS_Readline (line source)
func InteractiveLoop(ts *state.Thread, stdin io.Reader, stdout, stderr io.Writer, globals objects.Object) int {
	for {
		line, err := myreadline.Readline(stdin, stdout, PromptPS1)
		if errors.Is(err, myreadline.ErrInterrupt) {
			fmt.Fprintln(stderr, "KeyboardInterrupt")
			continue
		}
		if errors.Is(err, io.EOF) {
			if line != "" {
				runOneInteractive(ts, line, globals, stdout, stderr)
			}
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
