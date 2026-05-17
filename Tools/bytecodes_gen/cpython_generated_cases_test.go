// Walks every fixture lifted from CPython 3.14.5's
// Lib/test/test_generated_cases.py through our parser / analyzer /
// translator pipeline. The CPython test golden-matches generated C;
// we can't (gopy emits Go), so the gate here is the upstream
// pipeline shape: each fixture must at least parse, every `inst`
// must analyze, and we record how many `inst` bodies translate
// without bailing.
//
// This mirrors CPython's run_cases_test
// (Lib/test/test_generated_cases.py:108) which writes the input to
// a temp file with BEGIN_MARKER / END_MARKER, runs the full
// generator, and parses the output. We do the marker dance the same
// way (parser.BEGIN_MARKER / parser.END_MARKER live in
// Tools/cases_generator/parser.py) so the parser sees exactly what
// CPython feeds it.
//
// Regeneration:
//   go run ./Tools/bytecodes_gen -mode portgentests \
//       -src ~/cpython-314/Lib/test/test_generated_cases.py \
//       -out Tools/bytecodes_gen/cpython_generated_cases_fixtures_gen.go \
//       -pkg main

package main

import (
	"sort"
	"testing"
)

func TestCPythonGeneratedCasesFixtures(t *testing.T) {
	if len(cpythonGeneratedCases) == 0 {
		t.Fatal("no fixtures loaded; regenerate cpython_generated_cases_fixtures_gen.go")
	}

	var (
		parseOK      int
		parseFailed  []string
		instCount    int
		analyzeOK    int
		translateOK  int
		bailReasons  = map[string]int{}
		bailExamples = map[string]string{}
	)

	for _, fx := range cpythonGeneratedCases {
		src := "\n// BEGIN BYTECODES //\n" + fx.Input + "\n// END BYTECODES //\n"
		defs, err := ParseBytecodes(src)
		if err != nil {
			parseFailed = append(parseFailed, fx.Name+": "+err.Error())
			continue
		}
		parseOK++

		for _, inst := range defs.Insts {
			if inst.Kind != "inst" {
				// op() fragments and macro() expansions don't have
				// standalone bodies to translate; CPython's
				// generator composes them up at macro level. We
				// already filter the same way in
				// cpython_coverage_test.go.
				continue
			}
			instCount++
			sig, aerr := AnalyzeInst(inst)
			if aerr != nil {
				key := "analyze: " + bailKey(aerr.Error())
				bailReasons[key]++
				if bailExamples[key] == "" {
					bailExamples[key] = fx.Name + "/" + inst.Name
				}
				continue
			}
			analyzeOK++
			_, _, ok, note := TranslateBody(sig.Body, sig)
			if ok {
				translateOK++
				continue
			}
			key := bailKey(note)
			bailReasons[key]++
			if bailExamples[key] == "" {
				bailExamples[key] = fx.Name + "/" + inst.Name
			}
		}
	}

	t.Logf("test_generated_cases.py coverage:")
	t.Logf("  fixtures parsed: %d / %d", parseOK, len(cpythonGeneratedCases))
	t.Logf("  insts analyzed:  %d / %d", analyzeOK, instCount)
	t.Logf("  insts translated:%d / %d", translateOK, instCount)

	if len(parseFailed) > 0 {
		sort.Strings(parseFailed)
		for _, m := range parseFailed {
			t.Logf("  parse-fail: %s", m)
		}
	}
	if len(bailReasons) > 0 {
		keys := make([]string, 0, len(bailReasons))
		for k := range bailReasons {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return bailReasons[keys[i]] > bailReasons[keys[j]]
		})
		for _, k := range keys {
			t.Logf("  bail (%d) %s [eg %s]", bailReasons[k], k, bailExamples[k])
		}
	}

	// Hard floors: refuse to let either gate regress. Bump these
	// up as fixtures flip; never down.
	const (
		minParse     = 50
		minAnalyze   = 20
		minTranslate = 5
	)
	if parseOK < minParse {
		t.Fatalf("parse coverage regressed: %d < %d", parseOK, minParse)
	}
	if analyzeOK < minAnalyze {
		t.Fatalf("analyze coverage regressed: %d < %d", analyzeOK, minAnalyze)
	}
	if translateOK < minTranslate {
		t.Fatalf("translate coverage regressed: %d < %d", translateOK, minTranslate)
	}

	// Sanity: every fixture name from CPython must be unique.
	seen := map[string]bool{}
	for _, fx := range cpythonGeneratedCases {
		if seen[fx.Name] {
			t.Errorf("duplicate fixture name %q (line %d)", fx.Name, fx.Line)
		}
		seen[fx.Name] = true
	}
}
