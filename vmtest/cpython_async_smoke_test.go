// Side-by-side smoke for the async-shaped opcodes. The v0.6 panel
// covers expression-mode programs; this file covers multi-line
// programs that emit GET_AWAITABLE / GET_ANEXT in the inner
// function body. gopy can't fully drive `await coro()` end-to-end
// (the __await__ chain has gaps), so the parity check here is on
// the compile-and-instantiate shape: an `async def` produces a
// coroutine of the same type CPython produces, and `async def +
// yield` produces an async generator. The dispatch-arm execution
// parity for the underlying GET_AWAITABLE / GET_ANEXT opcodes is
// pinned separately by vm/eval_async_test.go, which runs hand-
// crafted bytecode through EvalCode.
//
// CPython: Python/bytecodes.c GET_AWAITABLE, GET_ANEXT.
//          Lib/test/test_coroutines.py exercises the same shapes.

package vmtest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// asyncSmokeCase is one async-shape entry. src is a multi-line
// Python program that assigns its observable result to the global
// _result. CPython runs the program via python3 -c followed by
// print(_result); gopy runs it via parse(ModeFile) -> compile ->
// EvalCode against a fresh globals dict, then pulls _result out.
type asyncSmokeCase struct {
	name string
	src  string
}

func asyncSmokePanel() []asyncSmokeCase {
	// The fixtures use obj.__class__.__name__ instead of
	// type(obj).__name__ so the program runs against an empty
	// globals dict; the parser-level shape is what's under test
	// and the bare-globals harness keeps the gate small.
	return []asyncSmokeCase{
		{
			name: "coroutine_type_name",
			src: `async def f():
    return 42
_result = f().__class__.__name__
`,
		},
		{
			name: "async_generator_type_name",
			src: `async def g():
    yield 1
    yield 2
_result = g().__class__.__name__
`,
		},
		{
			name: "await_compiles_in_body",
			src: `async def inner():
    return 1
async def outer():
    return await inner()
_result = outer().__class__.__name__
`,
		},
		{
			name: "async_comprehension_compiles_in_body",
			src: `async def src():
    yield 1
    yield 2
async def collect():
    return [x async for x in src()]
_result = collect().__class__.__name__
`,
		},
	}
}

// runProgram pipes src through parser(ModeFile) -> compile ->
// EvalCode against a fresh globals dict and returns the globals so
// the caller can pull individual names out. Mirrors gate_test.go's
// runSource but exposes globals instead of the frame-result value.
func runProgram(t *testing.T, src string) (*objects.Dict, error) {
	t.Helper()
	mod, err := parser.ParseString(src, "<async-smoke>", parser.ModeFile)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, "<async-smoke>", 0)
	if err != nil {
		return nil, err
	}
	co := liftCode(cco)
	ts := state.NewThread()
	g := objects.NewDict()
	if _, err := vm.EvalCode(ts, co, g, nil); err != nil {
		return nil, err
	}
	return g, nil
}

// cpythonRunProgram runs the multi-line source through python3 -c,
// appending a `print(_result)` line so the global comes back on
// stdout. Same timeout shape as cpythonRender.
func cpythonRunProgram(src string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", src+"\nprint(_result)\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", errors.New("python3: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

func TestCPythonAsyncSmokePanel(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; async smoke needs CPython for the parity comparison")
	}
	for _, tc := range asyncSmokePanel() {
		t.Run(tc.name, func(t *testing.T) {
			want, err := cpythonRunProgram(tc.src)
			if err != nil {
				t.Fatalf("python3 run of %q: %v", tc.name, err)
			}

			g, err := runProgram(t, tc.src)
			if shouldSkipUnimplemented(err) {
				t.Skipf("gopy gap: %v", err)
			}
			if err != nil {
				t.Fatalf("gopy run of %q: %v", tc.name, err)
			}
			rv, gerr := g.GetItem(objects.NewStr("_result"))
			if gerr != nil {
				t.Fatalf("globals[_result]: %v", gerr)
			}
			if rv == nil {
				t.Fatalf("_result not set in globals")
			}
			got, gerr := objects.Str(rv)
			if gerr != nil {
				t.Fatalf("Str(_result): %v", gerr)
			}
			if got != want {
				t.Errorf("%s: gopy=%q, cpython=%q", tc.name, got, want)
			}
		})
	}
}
