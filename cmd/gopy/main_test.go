package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	stdout, cleanup := captureFile(t)
	defer cleanup()

	rc := run([]string{"--version"}, stdout, os.Stderr)
	if rc != 0 {
		t.Fatalf("run --version returned %d, want 0", rc)
	}

	out := readFile(t, stdout)
	if !strings.HasPrefix(out, "gopy ") {
		t.Fatalf("version output %q must start with %q", out, "gopy ")
	}
}

func TestRunDefault(t *testing.T) {
	stdout, cleanup := captureFile(t)
	defer cleanup()

	rc := run(nil, stdout, os.Stderr)
	if rc != 0 {
		t.Fatalf("run with no args returned %d, want 0", rc)
	}
	out := readFile(t, stdout)
	if !strings.Contains(out, "gopy ") {
		t.Fatalf("default output %q must contain %q", out, "gopy ")
	}
}

func TestRunDashCSmoke(t *testing.T) {
	// The -c flag is the v0.6 smoke harness. The parser/VM panel is
	// incomplete, so we accept either a clean exit (0) or a non-zero
	// from the parser/eval bail; the contract here is just that the
	// flag wires through to the pipeline without panicking.
	stdout, cleanupOut := captureFile(t)
	defer cleanupOut()
	stderr, cleanupErr := captureFile(t)
	defer cleanupErr()

	rc := run([]string{"-c", "1 + 2"}, stdout, stderr)
	out := readFile(t, stdout)
	errOut := readFile(t, stderr)
	if rc != 0 && !strings.Contains(errOut, "parse:") && !strings.Contains(errOut, "eval:") && !strings.Contains(errOut, "compile:") {
		t.Fatalf("rc=%d stdout=%q stderr=%q: expected pipeline error or success", rc, out, errOut)
	}
}

func TestRunScriptFile(t *testing.T) {
	// Bare positional argument routes through pythonrun.RunAnyFile.
	// The contract here is the same as TestRunDashCSmoke: the wiring
	// must drive without panicking; success or a parser/eval bail are
	// both acceptable while the panel is incomplete.
	stdout, cleanupOut := captureFile(t)
	defer cleanupOut()
	stderr, cleanupErr := captureFile(t)
	defer cleanupErr()

	script, err := os.CreateTemp(t.TempDir(), "script-*.py")
	if err != nil {
		t.Fatal(err)
	}
	if _, werr := script.WriteString("pass\n"); werr != nil {
		t.Fatal(werr)
	}
	_ = script.Close()

	rc := run([]string{script.Name()}, stdout, stderr)
	out := readFile(t, stdout)
	errOut := readFile(t, stderr)
	if rc != 0 && errOut == "" && out == "" {
		t.Fatalf("rc=%d but no output: should at least surface an error", rc)
	}
}

// TestLexerTokenizerStdlibImports gates the v0.12.4 lexer/tokenizer
// subsystem vendoring. The three modules (keyword, tokenize, tabnanny)
// live byte-equal under stdlib/ and must import cleanly through the
// full CLI surface (which wires __build_class__ and friends, unlike
// the bare stdlibinit-only harness). Each line below prints a small
// witness that proves the module loaded and its top-level dict is
// populated.
//
// CPython: Lib/keyword.py, Lib/tokenize.py, Lib/tabnanny.py.
// Spec: 1710.
func TestLexerTokenizerStdlibImports(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		wantSub string
	}{
		{
			name:    "keyword",
			code:    "import keyword; print('kw', 'False' in keyword.kwlist, 'match' in keyword.softkwlist)",
			wantSub: "kw True True",
		},
		{
			name:    "tokenize",
			code:    "import tokenize; print('tk', tokenize.NEWLINE, hasattr(tokenize, 'tokenize'))",
			wantSub: "tk 4 True",
		},
		{
			name:    "tabnanny",
			code:    "import tabnanny; print('tn', hasattr(tabnanny, 'check'), tabnanny.filename_only)",
			wantSub: "tn True 0",
		},
		{
			// Exercise _tokenize.TokenizerIter end-to-end through
			// tokenize.tokenize: the iterator is the only path
			// Lib/tokenize.py uses to get real tokens, so a green
			// run here proves the Python-tokenize.c port is wired.
			name: "tokenize_iter",
			code: "import io, _tokenize\n" +
				"src = b'x = 1\\n'\n" +
				"rl = io.BytesIO(src).readline\n" +
				"it = _tokenize.TokenizerIter(rl, extra_tokens=True)\n" +
				"toks = list(it)\n" +
				"strings = [t[1] for t in toks]\n" +
				"print('ti', 'x' in strings, '=' in strings, '1' in strings)\n",
			wantSub: "ti True True True",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, cleanupOut := captureFile(t)
			defer cleanupOut()
			stderr, cleanupErr := captureFile(t)
			defer cleanupErr()

			rc := run([]string{"-c", tc.code}, stdout, stderr)
			out := readFile(t, stdout)
			errOut := readFile(t, stderr)
			if rc != 0 {
				t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out, errOut)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Fatalf("stdout=%q stderr=%q: want substring %q", out, errOut, tc.wantSub)
			}
		})
	}
}

func TestRunUnknownFlag(t *testing.T) {
	stdout, cleanupOut := captureFile(t)
	defer cleanupOut()
	stderr, cleanupErr := captureFile(t)
	defer cleanupErr()

	rc := run([]string{"--no-such-flag"}, stdout, stderr)
	if rc == 0 {
		t.Fatalf("run with unknown flag returned 0, want non-zero")
	}
}

func captureFile(t *testing.T) (file *os.File, cleanup func()) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gopy-*.out")
	if err != nil {
		t.Fatal(err)
	}
	return f, func() { _ = f.Close() }
}

func readFile(t *testing.T, f *os.File) string {
	t.Helper()
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
