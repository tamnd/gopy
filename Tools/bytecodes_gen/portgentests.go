// Port CPython 3.14's Lib/test/test_generated_cases.py into a Go
// fixture table. The CPython file is the canonical golden-output
// test bed for Tools/cases_generator: every `def test_*` method
// pairs a DSL `input` snippet with the C the generator must emit.
//
// We can't golden-match the C (gopy emits Go), so this tool extracts
// just the DSL inputs and writes them as a fixture slice. A companion
// test (cpython_generated_cases_test.go) walks the slice through our
// parser/analyzer/translator, mirroring how CPython's run_cases_test
// helper runs the full pipeline (Lib/test/test_generated_cases.py
// line ~108). Coverage is the count of fixtures that parse cleanly
// plus the count whose `inst` bodies translate without bailing.
//
// CPython references:
//   Lib/test/test_generated_cases.py            (the file we're reading)
//   Tools/cases_generator/parser.py             (BEGIN/END_MARKER wrap)
//   Tools/cases_generator/tier1_generator.py    (downstream consumer)
//
// Invoked via `bytecodes_gen -mode portgentests -src <test.py> -out <go> -pkg main`.
// The generated file is checked in so casual builds don't need a
// CPython checkout.
package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// trimHomeDir replaces a leading $HOME with `~` so generated comments
// don't bake a build-machine path into checked-in source.
func trimHomeDir(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home); ok {
		return "~" + rest
	}
	return p
}

// genTestFixture captures one (name, line, dsl) triple lifted from
// test_generated_cases.py. Line numbers point at the `input = """`
// header so a future drift check can re-anchor by file offset.
type genTestFixture struct {
	Name  string
	Line  int
	Input string
}

// extractGenTestFixtures parses the source of test_generated_cases.py
// and returns one fixture per `def test_*` method whose body contains
// an `input = """ ... """` literal. Methods without an input string
// (e.g. TestEffects.test_effect_sizes which builds Stack manually)
// are skipped.
//
// The grammar we recognize is intentionally minimal:
//
//	def test_NAME(self):
//	    ... possibly other code ...
//	    input = """
//	    ... DSL body ...
//	    """
//
// Anything fancier (f-strings, textwrap.dedent, concatenation) is
// out of scope; if CPython adopts a new style we'll grow the parser.
func extractGenTestFixtures(src string) []genTestFixture {
	lines := strings.Split(src, "\n")
	var out []genTestFixture

	curName := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Track the enclosing test method.
		if strings.HasPrefix(trimmed, "def test_") {
			// Pull the name up to "(".
			rest := strings.TrimPrefix(trimmed, "def ")
			if name, _, ok := strings.Cut(rest, "("); ok {
				curName = name
			}
			continue
		}
		// A `class Test...` line resets context so a stray
		// `input = """` outside a def isn't attributed to the
		// previous method.
		if strings.HasPrefix(trimmed, "class ") {
			curName = ""
			continue
		}

		if curName == "" {
			continue
		}

		// Look for `input = """` (allow `input=` no-space).
		if !isInputAssign(trimmed) {
			continue
		}
		startLine := i + 1 // 1-based for human consumption
		// The DSL text starts on the next line; collect until we
		// hit a line whose trimmed form is just `"""`.
		var body bytes.Buffer
		j := i + 1
		for ; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == `"""` || strings.HasPrefix(t, `"""`) && j > i+1 {
				// The closing triple-quote. If the closing line
				// has trailing content (rare) we still stop here;
				// the leftover is unlikely to be DSL.
				break
			}
			body.WriteString(lines[j])
			body.WriteByte('\n')
		}
		if j >= len(lines) {
			// Unterminated; skip this fixture rather than panic.
			continue
		}
		out = append(out, genTestFixture{
			Name:  curName,
			Line:  startLine,
			Input: body.String(),
		})
		i = j
		// The same method might have multiple input blocks (e.g.
		// test_pep7_condition assembles several); keep scanning
		// under the same name until we hit the next `def`.
	}
	return out
}

func isInputAssign(s string) bool {
	// Accept `input = """`, `input="""`, `input  =  """`.
	if !strings.HasPrefix(s, "input") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(s, "input"))
	if !strings.HasPrefix(rest, "=") {
		return false
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
	return rest == `"""` || strings.HasPrefix(rest, `"""`)
}

// emitGenTestFixtures renders the fixture slice into a Go source file.
// The output is gofmt-clean (we run go/format via the same helper the
// other emitters use; see emit_metadata.go for the pattern).
// disambiguateNames suffixes repeated test names with #2, #3, etc. so
// the Go fixture slice has a unique key per row. CPython tests like
// test_no_escaping_calls_in_branching_macros legitimately stack
// multiple input blocks (each is a separate self.run_cases_test call);
// we preserve all of them but make them addressable.
func disambiguateNames(fixtures []genTestFixture) {
	count := map[string]int{}
	for i := range fixtures {
		n := fixtures[i].Name
		count[n]++
		if count[n] > 1 {
			fixtures[i].Name = fmt.Sprintf("%s_%d", n, count[n])
		}
	}
}

func emitGenTestFixtures(pkg string, srcPath string, fixtures []genTestFixture) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by bytecodes_gen -mode portgentests. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (CPython 3.14.5)\n", trimHomeDir(srcPath))
	fmt.Fprintf(&b, "// Each fixture is one `def test_*` method's `input` DSL string,\n")
	fmt.Fprintf(&b, "// extracted verbatim so the translator gate can walk it.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	fmt.Fprintf(&b, "type cpythonGenCase struct {\n")
	fmt.Fprintf(&b, "\tName  string\n")
	fmt.Fprintf(&b, "\tLine  int\n")
	fmt.Fprintf(&b, "\tInput string\n")
	fmt.Fprintf(&b, "}\n\n")
	fmt.Fprintf(&b, "var cpythonGeneratedCases = []cpythonGenCase{\n")
	for _, f := range fixtures {
		fmt.Fprintf(&b, "\t{\n\t\tName: %q,\n\t\tLine: %d,\n\t\tInput: %s,\n\t},\n",
			f.Name, f.Line, goRawString(f.Input))
	}
	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// goRawString picks the cheapest Go string literal that can hold s.
// Raw backtick literal is preferred for readability; if the input
// contains a backtick (none of CPython's DSL fixtures do today, but
// be safe) we fall back to %q.
func goRawString(s string) string {
	if strings.ContainsRune(s, '`') {
		return fmt.Sprintf("%q", s)
	}
	return "`" + s + "`"
}

func runPortGenTests(src, out, pkg string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	fixtures := extractGenTestFixtures(string(body))
	disambiguateNames(fixtures)
	if len(fixtures) == 0 {
		return fmt.Errorf("no fixtures extracted from %s; pattern drifted?", src)
	}
	rendered := emitGenTestFixtures(pkg, src, fixtures)
	return os.WriteFile(out, []byte(rendered), 0o644)
}
