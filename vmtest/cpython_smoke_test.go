// CPython side-by-side smoke. For each entry in the panel we run the
// source through `python3 -c "print(<expr>)"` to capture CPython's
// rendering, then through the gopy parser->compile->eval pipeline and
// compare the rendered top-level value. The harness skips when
// python3 is not on PATH or when the gopy pipeline still bails on the
// case (the parser does not yet emit every rule body, and the v0.6 vm
// panel deliberately leaves async/generator opcodes out).
//
// CPython: Lib/test/test_compile.py + Lib/test/test_grammar.py
// (subset chosen to overlap with the v0.6 dispatch panel).

package vmtest

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/vm"
)

// cpythonSmokeCase is one (source, mode) panel entry. The expected
// rendering is computed at test time by running the source through
// python3 -c "print(<src>)", which keeps the panel honest against the
// real CPython surface for every value type the gopy renderer can
// produce.
type cpythonSmokeCase struct {
	name   string
	src    string
	mode   parser.Mode
	skipIf string // skip the case when this substring appears in the gopy err
}

func cpythonSmokePanel() []cpythonSmokeCase {
	return []cpythonSmokeCase{
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
		// String + string drives BINARY_OP NB_ADD for the str type; the
		// v0.6 number-protocol panel covers ints only, so skip until the
		// str slot lands in v0.7.
		{name: "string_concat", src: `"a" + "b"`, mode: parser.ModeEval, skipIf: "unsupported operand type"},
	}
}

// renderObject collapses an objects.Object into the same string
// CPython's print() would emit. Our renderer is simpler than CPython's
// repr/str split; for the v0.6 panel only the print-shape is asked
// for. The gate is still meaningful: a str() drift here would have to
// be deliberate.
func renderObject(o objects.Object) (string, error) {
	if o == nil {
		return "None", nil
	}
	return objects.Str(o)
}

// cpythonRender runs `python3 -c "print(<src>)"` and returns the
// captured stdout (trimmed of the trailing newline). Errors propagate;
// callers t.Skip when python3 is not on PATH.
func cpythonRender(src string) (string, error) {
	cmd := exec.Command("python3", "-c", "print("+src+")")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", errors.New("python3: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func TestCPythonSmokePanel(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; cpython smoke needs CPython for the parity comparison")
	}
	for _, tc := range cpythonSmokePanel() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			want, err := cpythonRender(tc.src)
			if err != nil {
				t.Fatalf("python3 render of %q: %v", tc.src, err)
			}

			v, err := runSource(t, tc.src, tc.mode)
			if shouldSkipUnimplemented(err) {
				t.Skipf("gopy gap: %v", err)
			}
			if err != nil {
				if tc.skipIf != "" && strings.Contains(err.Error(), tc.skipIf) {
					t.Skipf("known v0.6 gap: %v", err)
				}
				t.Fatalf("gopy eval of %q: %v", tc.src, err)
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

// TestCPythonSmokePanelCoversCorePanel pins that the panel keeps
// covering the same shapes the gate exercises: drift between the two
// (someone deletes a gate entry but forgets to drop the parity
// counterpart) shows up as a missing case here.
func TestCPythonSmokePanelCoversCorePanel(t *testing.T) {
	have := map[string]bool{}
	for _, tc := range cpythonSmokePanel() {
		have[tc.name] = true
	}
	required := []string{
		"int_constant", "int_arithmetic_add", "ternary_true",
		"list_build", "comparison_eq",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("cpython smoke panel must include %q", name)
		}
	}
	_ = vm.ErrNotImplemented // keeps vm import non-cyclical even if every gopy case skips
}
