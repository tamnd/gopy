// CPython parity harness for the action translator.
//
// CPython's Lib/test/test_generated_cases.py runs each DSL snippet
// through Tools/cases_generator and golden-matches the emitted C.
// We can't golden-match against C (we emit Go), but we *can* run the
// same DSL snippets through our pipeline and assert the translator
// succeeds on every shape CPython's tests cover. Each fixture below
// is lifted verbatim from CPython's test file; the `want` field
// records which Go idioms our emitter must produce. A `bail` flag
// marks fixtures that are still in flight (translator falls back to
// panic-stub).
//
// The coverage metric (passed / total) is printed at the end of the
// run so the porting auto-flow can watch it climb.
//
// CPython: Lib/test/test_generated_cases.py.

package main

import (
	"strings"
	"testing"
)

type parityFixture struct {
	// name is the CPython test method (without the test_ prefix).
	name string
	// cpyLine is the line in Lib/test/test_generated_cases.py where
	// the fixture's input string starts; used for grepping during
	// drift checks.
	cpyLine int
	// input is the DSL body verbatim from CPython's test. The
	// harness wraps it in BEGIN/END markers before parsing.
	input string
	// want is a list of substrings the generated Go body must
	// contain. Empty when bail=true.
	want []string
	// bail records that our translator currently falls back; the
	// harness checks that we emit the panic-stub marker instead of
	// failing the test, so coverage growth is monotonic.
	bail bool
	// bailReason matches the prefix of the translator's note; used
	// to detect when a bail flips category (e.g. from "sized
	// output" to "Py_Is helper") without us noticing.
	bailReason string
}

// parityFixtures mirrors the test_generated_cases.py method order.
// Add new fixtures as the translator gains coverage; do not remove
// rows (a flipped fixture moves from bail=true to bail=false).
var parityFixtures = []parityFixture{
	{
		name:    "inst_no_args",
		cpyLine: 130,
		input: `
		inst(OP, (--)) {
			SPAM();
		}
	`,
		bail:       true,
		bailReason: "unrecognized token at action body start",
	},
	{
		name:    "inst_one_pop",
		cpyLine: 151,
		input: `
		inst(OP, (value --)) {
			SPAM(value);
			DEAD(value);
		}
	`,
		bail:       true,
		bailReason: "unrecognized token at action body start",
	},
	{
		name:    "inst_one_push",
		cpyLine: 177,
		input: `
		inst(OP, (-- res)) {
			res = SPAM();
		}
	`,
		bail:       true,
		bailReason: "output assign \"res\": unexpected token \"SPAM\"",
	},
	{
		name:    "error_if_plain",
		cpyLine: 419,
		input: `
		inst(OP, (--)) {
			ERROR_IF(cond);
		}
	`,
		want: []string{"if cond {", `e.error(`},
	},
	{
		// Comments inside the body must not break the translator;
		// stripWhitespace drops tokComment before the statement walker
		// runs.
		name:    "error_if_plain_with_comment",
		cpyLine: 442,
		input: `
		inst(OP, (--)) {
			ERROR_IF(cond);  // Comment is ok
		}
	`,
		want: []string{"if cond {", `e.error(`},
	},
	{
		// Passthrough output: when an output name matches an input
		// name, the input ref flows into the output slot. CPython's
		// analyzer marks the output as pre-assigned; we mirror by
		// seeding actionTranslator.assigned[name] = true.
		name:    "passthrough_value",
		cpyLine: 0, // synthetic but mirrors TO_BOOL_BOOL shape
		input: `
		inst(OP, (value -- value)) {
			DEAD(value);
		}
	`,
		want: []string{}, // body collapses to nothing; just must not bail
	},
	// LOAD_FAST family, real-world equivalents that already flipped.
	{
		name:    "load_fast_real",
		cpyLine: 277, // Python/bytecodes.c LOAD_FAST itself
		input: `
		inst(LOAD_FAST, (-- value)) {
			assert(!PyStackRef_IsNull(GETLOCAL(oparg)));
			value = PyStackRef_DUP(GETLOCAL(oparg));
		}
	`,
		want: []string{"value = e.localAt(int(oparg)).Dup()"},
	},
	{
		name:    "inst_one_push_one_pop",
		cpyLine: 202,
		input: `
		inst(OP, (value -- res)) {
			res = SPAM(value);
			DEAD(value);
		}
	`,
		bail:       true,
		bailReason: `output assign "res": unexpected token "SPAM"`,
	},
	{
		name:    "binary_op_unknown_callee",
		cpyLine: 228,
		input: `
		inst(OP, (left, right -- res)) {
			res = SPAM(left, right);
			INPUTS_DEAD();
		}
	`,
		bail:       true,
		bailReason: `output assign "res": unexpected token "SPAM"`,
	},
	{
		name:    "store_fast_real",
		cpyLine: 1839, // Python/bytecodes.c STORE_FAST itself
		input: `
		inst(STORE_FAST, (value --)) {
			_PyStackRef tmp = GETLOCAL(oparg);
			GETLOCAL(oparg) = value;
			DEAD(value);
			PyStackRef_XCLOSE(tmp);
		}
	`,
		want: []string{
			"tmp := e.localAt(int(oparg))",
			"e.setLocal(int(oparg), value)",
			"tmp.Close()",
		},
	},
}

func runParityFixture(t *testing.T, f parityFixture) (body string, bailed bool, note string) {
	t.Helper()
	src := "\n// BEGIN BYTECODES //\n" + f.input + "\n// END BYTECODES //\n"
	defs, err := ParseBytecodes(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(defs.Insts) != 1 {
		t.Fatalf("fixture %q: expected 1 inst, got %d", f.name, len(defs.Insts))
	}
	sig, err := AnalyzeInst(defs.Insts[0])
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	out, _, ok, n := TranslateBody(sig.Body, sig)
	if !ok {
		return "", true, n
	}
	return out, false, ""
}

func TestCPythonParityFixtures(t *testing.T) {
	var passed, bailed int
	for _, f := range parityFixtures {
		t.Run(f.name, func(t *testing.T) {
			body, didBail, note := runParityFixture(t, f)
			if f.bail {
				if !didBail {
					t.Fatalf("fixture %q now translates; flip bail=false and add 'want' substrings.\nbody:\n%s",
						f.name, body)
				}
				if f.bailReason != "" && !strings.HasPrefix(note, f.bailReason) {
					t.Fatalf("fixture %q bail reason drifted.\n want prefix: %q\n got: %q",
						f.name, f.bailReason, note)
				}
				bailed++
				return
			}
			if didBail {
				t.Fatalf("fixture %q bailed but bail=false. note: %s", f.name, note)
			}
			for _, w := range f.want {
				if !strings.Contains(body, w) {
					t.Errorf("fixture %q body missing %q\n--- body ---\n%s", f.name, w, body)
				}
			}
			passed++
		})
	}
	t.Logf("CPython-parity coverage: %d / %d fixtures translate (bail=%d)",
		passed, len(parityFixtures), bailed)
}
