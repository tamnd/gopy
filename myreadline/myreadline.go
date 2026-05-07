// Package myreadline ports Parser/myreadline.c. CPython exposes one
// entry point, PyOS_Readline, plus a function-pointer hook
// (PyOS_ReadlineFunctionPointer) the GNU readline shared extension
// installs at import time. The default sits in PyOS_StdioReadline:
// write the prompt, fgets a line off stdin (resizing the buffer until
// the line ends in '\n' or hits EOF), and return it.
//
// gopy keeps the same shape so a real line editor (peterh/liner, a
// cgo bridge to GNU readline, libedit, anything that satisfies the
// Reader signature) can plug in via SetReader without the REPL caring.
// The default Reader is StdioReadline, which mirrors the unedited
// fallback CPython runs when the readline module isn't loaded.
//
// CPython: Parser/myreadline.c:372 PyOS_Readline (entry)
// CPython: Parser/myreadline.c:262 PyOS_StdioReadline (default)

package myreadline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Reader is the signature an alternative line reader must satisfy.
// stdin / stdout pair the streams CPython hands to
// PyOS_ReadlineFunctionPointer; prompt is the PS1 / PS2 string the
// reader should display before reading input. The returned line
// includes the trailing '\n' (or is empty at EOF, matching
// PyOS_StdioReadline). io.EOF on a clean EOF, ErrInterrupt on Ctrl-C.
//
// CPython: Parser/myreadline.c:366 PyOS_ReadlineFunctionPointer
type Reader func(stdin io.Reader, stdout io.Writer, prompt string) (string, error)

// ErrInterrupt is returned when the reader is aborted by Ctrl-C. The
// REPL distinguishes this from EOF so it can print KeyboardInterrupt
// and continue prompting.
//
// CPython: Parser/myreadline.c:117 return 1 (Interrupt) path
var ErrInterrupt = errors.New("KeyboardInterrupt")

// reader is the installed reader. nil means "use StdioReadline".
// Mirrors PyOS_ReadlineFunctionPointer.
//
// CPython: Parser/myreadline.c:366 PyOS_ReadlineFunctionPointer
var (
	mu     sync.Mutex
	reader Reader
	active bool
)

// SetReader installs r as the line reader. Pass nil to fall back to
// StdioReadline. Mirrors the readline module's startup wiring of
// PyOS_ReadlineFunctionPointer.
//
// CPython: Modules/readline.c PyInit_readline (sets the pointer)
func SetReader(r Reader) {
	mu.Lock()
	defer mu.Unlock()
	reader = r
}

// CurrentReader returns the installed reader (or nil if none). Mainly
// useful for tests that want to swap and restore.
func CurrentReader() Reader {
	mu.Lock()
	defer mu.Unlock()
	return reader
}

// Readline is the public entry. It dispatches to the installed reader
// when one is registered and otherwise falls back to StdioReadline.
// The re-entrancy guard mirrors the _PyOS_ReadlineTState check in
// PyOS_Readline.
//
// CPython: Parser/myreadline.c:372 PyOS_Readline
func Readline(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	mu.Lock()
	if active {
		mu.Unlock()
		return "", errors.New("RuntimeError: can't re-enter readline")
	}
	active = true
	r := reader
	mu.Unlock()

	defer func() {
		mu.Lock()
		active = false
		mu.Unlock()
	}()

	if r == nil {
		return StdioReadline(stdin, stdout, prompt)
	}
	return r(stdin, stdout, prompt)
}

// StdioReadline is the unedited reader: write the prompt and pull one
// line off stdin. The returned line keeps its trailing '\n' (or is
// shorter on EOF). Matches PyOS_StdioReadline's contract: a blank
// return on a clean EOF means "no more input".
//
// CPython: Parser/myreadline.c:262 PyOS_StdioReadline
func StdioReadline(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	if stdin == nil {
		return "", errors.New("RuntimeError: lost sys.stdin")
	}
	writePrompt(stdout, prompt)

	br, ok := stdin.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(stdin)
	}
	line, err := br.ReadString('\n')
	switch {
	case err == nil:
		return line, nil
	case errors.Is(err, io.EOF):
		return line, io.EOF
	default:
		return "", err
	}
}

// writePrompt emits prompt to the writer and flushes if it can. The C
// version goes via stderr (so prompts survive when stdout is
// redirected); gopy keeps the writer the caller passed in to stay
// composable with REPL-test fakes.
//
// CPython: Parser/myreadline.c:310 fprintf(stderr, "%s", prompt)
func writePrompt(w io.Writer, prompt string) {
	if w == nil || prompt == "" {
		return
	}
	fmt.Fprint(w, prompt)
	if f, ok := w.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
}
