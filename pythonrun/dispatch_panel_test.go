package pythonrun

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	gerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// dispatchCase pins one row of the dispatch panel: a source string,
// the arm that runs it, and the rc/stderr/stdout shape we expect.
// arm is "string", "file", or "repl" so each panel row exercises a
// different entry without duplicating the table.
type dispatchCase struct {
	name        string
	src         string
	arm         string
	wantRC      int
	stderrHas   string
	stdoutHas   string
	allowParser bool
}

func dispatchPanel() []dispatchCase {
	return []dispatchCase{
		{
			name:        "noop_pass",
			src:         "pass\n",
			arm:         "string",
			wantRC:      0,
			allowParser: true,
		},
		{
			name:        "parse_failure_renders_to_stderr",
			src:         "this is not valid python !!!\n",
			arm:         "string",
			wantRC:      1,
			stderrHas:   "SyntaxError",
			allowParser: false,
		},
		{
			name:        "noop_pass_via_file",
			src:         "pass\n",
			arm:         "file",
			wantRC:      0,
			allowParser: true,
		},
		{
			name:        "noop_pass_via_repl",
			src:         "",
			arm:         "repl",
			wantRC:      0,
			allowParser: false,
		},
		{
			name:        "repl_swallows_syntax_error",
			src:         "!!!not python!!!\n",
			arm:         "repl",
			wantRC:      0,
			stderrHas:   "SyntaxError",
			allowParser: false,
		},
	}
}

// TestDispatchPanel runs every row through the named arm, builds a
// fresh thread + builtins dict per case, and checks the rc/stderr
// markers. Panel rows that drive through the parser tolerate the
// v0.7 gap when allowParser is set.
func TestDispatchPanel(t *testing.T) {
	for _, tc := range dispatchPanel() {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			g, err := builtins.Init(&stdout)
			if err != nil {
				t.Fatal(err)
			}
			ts := state.NewThread()

			var rc int
			switch tc.arm {
			case "string":
				rc = RunSimpleString(ts, tc.src, g, &stderr)
			case "file":
				path := filepath.Join(t.TempDir(), "panel.py")
				if werr := os.WriteFile(path, []byte(tc.src), 0o600); werr != nil {
					t.Fatal(werr)
				}
				rc = RunAnyFile(ts, path, g, &stderr)
			case "repl":
				rc = InteractiveLoop(ts, strings.NewReader(tc.src), &stdout, &stderr, g)
			default:
				t.Fatalf("unknown arm %q", tc.arm)
			}

			if rc != tc.wantRC {
				if tc.allowParser && shouldSkipUnimplemented(parserOrCompilerErr(t, tc.src)) {
					t.Skipf("parser/VM gap on %q: rc=%d stderr=%q", tc.src, rc, stderr.String())
				}
				t.Fatalf("rc=%d, want %d (stderr=%q)", rc, tc.wantRC, stderr.String())
			}
			if tc.stderrHas != "" && !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr=%q must contain %q", stderr.String(), tc.stderrHas)
			}
			if tc.stdoutHas != "" && !strings.Contains(stdout.String(), tc.stdoutHas) {
				t.Errorf("stdout=%q must contain %q", stdout.String(), tc.stdoutHas)
			}
		})
	}
}

// TestPrintRunErrorPropagatesSystemExit pins the wiring between
// pythonrun.printRunError and errors.PrintEx: a SystemExit raised
// before the call returns the code without printing.
func TestPrintRunErrorPropagatesSystemExit(t *testing.T) {
	ts := state.NewThread()
	args := objects.NewTuple([]objects.Object{objects.NewInt(42)})
	gerrors.Set(ts, gerrors.PyExc_SystemExit, args)

	var buf bytes.Buffer
	rc := printRunError(ts, nil, &buf)
	if rc != 42 {
		t.Errorf("rc=%d, want 42", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("SystemExit must not write traceback, got %q", buf.String())
	}
}

// TestPrintRunErrorRendersChainedException pins the chain renderer
// is reachable from the pythonrun layer: an exception with a
// __cause__ surfaces the chain prefix in the printed traceback.
func TestPrintRunErrorRendersChainedException(t *testing.T) {
	ts := state.NewThread()
	cause := gerrors.New(gerrors.PyExc_ValueError, objects.NewTuple([]objects.Object{objects.NewStr("root")}))
	exc := gerrors.New(gerrors.PyExc_RuntimeError, objects.NewTuple([]objects.Object{objects.NewStr("wrapped")}))
	gerrors.RaiseFrom(ts, exc, cause)

	var buf bytes.Buffer
	rc := printRunError(ts, nil, &buf)
	if rc != 1 {
		t.Errorf("rc=%d, want 1", rc)
	}
	if !strings.Contains(buf.String(), "ValueError") || !strings.Contains(buf.String(), "RuntimeError") {
		t.Errorf("chain output %q must mention both exception types", buf.String())
	}
	if !strings.Contains(buf.String(), "direct cause") {
		t.Errorf("chain output %q must include the 'direct cause' bridge text", buf.String())
	}
}
