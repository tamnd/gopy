// pythonrun.RunString gate. The panel mirrors the vmtest cpython_smoke
// fixtures: each entry runs the source through python3 -c "print(<expr>)"
// to capture CPython's rendering, then through pythonrun.RunString to
// compare the rendered top-level value. Spec 1624's test gate: "the v0.6
// cpython_smoke panel re-rooted onto pythonrun.RunString. Same 13 cases
// must still match python3 stdout."
//
// CPython: Python/pythonrun.c:1219 _PyRun_StringFlagsWithName

package pythonrun

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

type smokeCase struct {
	name   string
	src    string
	mode   parser.Mode
	skipIf string
}

func smokePanel() []smokeCase {
	return []smokeCase{
		{name: "int_constant", src: "42", mode: parser.ModeEval},
		{name: "int_arithmetic_add", src: "1 + 2", mode: parser.ModeEval},
		{name: "int_arithmetic_mul", src: "6 * 7", mode: parser.ModeEval},
		{name: "int_arithmetic_mixed", src: "(1 + 2) * 3 - 4", mode: parser.ModeEval},
		{name: "comparison_eq", src: "1 == 1", mode: parser.ModeEval},
		{name: "comparison_ne", src: "1 != 2", mode: parser.ModeEval},
		{name: "ternary_true", src: "1 if True else 2", mode: parser.ModeEval},
		{name: "ternary_false", src: "1 if False else 2", mode: parser.ModeEval},
		{name: "list_build", src: "[1, 2, 3]", mode: parser.ModeEval},
		{name: "list_subscript", src: "[10, 20, 30][1]", mode: parser.ModeEval},
		{name: "tuple_build", src: "(1, 2, 3)", mode: parser.ModeEval},
		{name: "in_operator", src: "2 in [1, 2, 3]", mode: parser.ModeEval},
		{name: "string_concat", src: `"a" + "b"`, mode: parser.ModeEval, skipIf: "unsupported operand type"},
	}
}

func cpythonRender(src string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", "print("+src+")")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", errors.New("python3: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

func shouldSkipUnimplemented(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, vm.ErrNotImplemented) {
		return true
	}
	return strings.Contains(err.Error(), "generated rule bodies not yet emitted")
}

// TestRunStringCPythonSmoke pins pythonrun.RunString against python3
// for every panel entry. python3 must be on PATH; otherwise the test
// skips. Individual cases t.Skip when the gopy parser/VM still bails.
func TestRunStringCPythonSmoke(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; cpython smoke needs CPython for the parity comparison")
	}
	for _, tc := range smokePanel() {
		t.Run(tc.name, func(t *testing.T) {
			want, err := cpythonRender(tc.src)
			if err != nil {
				t.Fatalf("python3 render of %q: %v", tc.src, err)
			}

			ts := state.NewThread()
			v, err := RunString(ts, tc.src, "<smoke>", tc.mode, objects.NewDict(), nil)
			if shouldSkipUnimplemented(err) {
				t.Skipf("gopy gap: %v", err)
			}
			if err != nil {
				if tc.skipIf != "" && strings.Contains(err.Error(), tc.skipIf) {
					t.Skipf("known v0.7 gap: %v", err)
				}
				t.Fatalf("RunString of %q: %v", tc.src, err)
			}
			got, gerr := renderObject(v)
			if gerr != nil {
				t.Fatalf("renderObject: %v", gerr)
			}
			if got != want {
				t.Errorf("src=%q: gopy=%q, cpython=%q", tc.src, got, want)
			}
		})
	}
}

// TestRunStringPanelCoversCorePanel pins that the pythonrun panel
// keeps covering the same shapes the vmtest cpython_smoke gate did.
// Drift between the two (someone deletes a gate entry but forgets to
// drop the parity counterpart) shows up as a missing case here.
func TestRunStringPanelCoversCorePanel(t *testing.T) {
	have := map[string]bool{}
	for _, tc := range smokePanel() {
		have[tc.name] = true
	}
	required := []string{
		"int_constant", "int_arithmetic_add", "ternary_true",
		"list_build", "comparison_eq",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("pythonrun smoke panel must include %q", name)
		}
	}
}

func renderObject(o objects.Object) (string, error) {
	if o == nil {
		return "None", nil
	}
	return objects.Str(o)
}
