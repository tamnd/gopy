// Port of builtin_input_impl. The C version does a lot of work (sys
// audit hook, encoding handshake with sys.stdout / sys.stdin, tty
// detection so it can route through GNU readline) that gopy does not
// yet have a place for. This implementation keeps the public shape:
// optional prompt argument written to defaultOut, line read from
// defaultIn with the trailing newline stripped, EOFError on a clean
// EOF. The readline / encoding paths land with the io module port.
//
// CPython: Python/bltinmodule.c:2327 builtin_input_impl

package builtins

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tamnd/gopy/objects"
)

// Input returns the input(prompt='') closure. defaultIn is the
// reader the line gets pulled from (CPython uses sys.stdin);
// defaultOut is the writer the prompt gets flushed to (CPython uses
// sys.stdout).
//
// CPython: Python/bltinmodule.c:2327 builtin_input_impl
func Input(defaultIn io.Reader, defaultOut io.Writer) func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: input() takes no keyword arguments")
		}
		if len(args) > 1 {
			return nil, fmt.Errorf("TypeError: input expected at most 1 argument, got %d", len(args))
		}
		if len(args) == 1 && !objects.IsNone(args[0]) {
			s, err := objects.Str(args[0])
			if err != nil {
				return nil, err
			}
			if defaultOut != nil {
				if _, err := io.WriteString(defaultOut, s); err != nil {
					return nil, err
				}
				if f, ok := defaultOut.(interface{ Flush() error }); ok {
					_ = f.Flush()
				}
			}
		}
		if defaultIn == nil {
			return nil, fmt.Errorf("RuntimeError: lost sys.stdin")
		}
		line, err := readLine(defaultIn)
		if errors.Is(err, io.EOF) && line == "" {
			return nil, fmt.Errorf("EOFError: EOF when reading a line")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		return objects.NewStr(line), nil
	}
}

// readLine reads up to and including the next newline, the same way
// PyOS_StdioReadline drains stdin one line at a time. Buffered readers
// are reused across calls so a sequence of input() calls does not
// drop bytes on the floor.
//
// CPython: Parser/myreadline.c PyOS_StdioReadline
func readLine(r io.Reader) (string, error) {
	br, ok := bufferedReaders[r]
	if !ok {
		br = bufio.NewReader(r)
		bufferedReaders[r] = br
	}
	return br.ReadString('\n')
}

// bufferedReaders caches the bufio.Reader wrapping each io.Reader.
// CPython has one stdin per process; gopy lets the test harness
// supply its own reader, and each one needs its own buffer or the
// next call would lose anything that bufio prefetched.
var bufferedReaders = map[io.Reader]*bufio.Reader{}
