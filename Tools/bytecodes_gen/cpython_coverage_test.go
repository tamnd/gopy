// Real-bytecodes coverage gauge. Walks every `inst` definition in
// CPython 3.14.5's Python/bytecodes.c, runs each through our
// pipeline (parser → analyzer → action translator), and reports how
// many translate cleanly. This is the spec 1714 "100% like CPython"
// north star: when this count reaches the live opcode list the
// translator has reached parity.
//
// Mirrors the methodology of CPython's own
// `tier1_generator.generate_tier1_from_files` (running the full
// pipeline on the real source), but with two scoped differences:
// fragments (`op`) and macros are excluded (their stack effect comes
// from the macro composition, not the fragment body), and adaptive
// variants (`replicate(N) inst`) are counted once (the replicates
// share a body).
//
// CPython: Lib/test/test_generated_cases.py runs the same pipeline
// end-to-end; this test is the bytecodes.c-driven complement.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type coverageBailGroup struct {
	reason string
	names  []string
}

func TestCPythonBytecodesCoverage(t *testing.T) {
	cpython := os.Getenv("CPYTHON")
	if cpython == "" {
		// Fall back to the standard citation mirror documented in
		// the user's memory; many devs and CI agents have it.
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, "cpython-314")
			if _, err := os.Stat(filepath.Join(candidate, "Python", "bytecodes.c")); err == nil {
				cpython = candidate
			}
		}
	}
	if cpython == "" {
		t.Skip("CPYTHON env not set and ~/cpython-314 not found")
	}
	src, err := os.ReadFile(filepath.Join(cpython, "Python", "bytecodes.c"))
	if err != nil {
		t.Skipf("read bytecodes.c: %v", err)
	}
	defs, err := ParseBytecodes(string(src))
	if err != nil {
		t.Fatalf("ParseBytecodes: %v", err)
	}

	var (
		passed []string
		bails  = map[string]*coverageBailGroup{}
		errors = map[string]string{}
	)
	for _, inst := range defs.Insts {
		// Only count real instructions. Fragments (`op(...)`) feed
		// macro composition and never dispatch on their own.
		if inst.Kind != "inst" {
			continue
		}
		sig, aerr := AnalyzeInst(inst)
		if aerr != nil {
			errors[inst.Name] = aerr.Error()
			continue
		}
		_, _, ok, note := TranslateBody(sig.Body, sig)
		if ok {
			passed = append(passed, inst.Name)
			continue
		}
		key := bailKey(note)
		g := bails[key]
		if g == nil {
			g = &coverageBailGroup{reason: key}
			bails[key] = g
		}
		g.names = append(g.names, inst.Name)
	}

	sort.Strings(passed)

	// Print a deterministic, log-friendly summary.
	totalInst := len(passed) + sumGroupSizes(bails) + len(errors)
	t.Logf("bytecodes.c coverage: %d / %d inst() bodies translate",
		len(passed), totalInst)
	if len(passed) > 0 {
		t.Logf("translates: %s", strings.Join(passed, ", "))
	}

	keys := make([]string, 0, len(bails))
	for k := range bails {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(bails[keys[i]].names) > len(bails[keys[j]].names)
	})
	for _, k := range keys {
		g := bails[k]
		sort.Strings(g.names)
		t.Logf("bail (%d) %s\n  %s", len(g.names), k, strings.Join(g.names, ", "))
	}
	if len(errors) > 0 {
		ekeys := make([]string, 0, len(errors))
		for k := range errors {
			ekeys = append(ekeys, k)
		}
		sort.Strings(ekeys)
		for _, k := range ekeys {
			t.Logf("analyze error %s: %s", k, errors[k])
		}
	}

	// Hard floor: refuse to let coverage regress. Bump this number
	// up (never down) as fixtures flip. The value tracks reality;
	// the porting auto-flow reads this constant to know progress.
	const minTranslates = 19
	if len(passed) < minTranslates {
		t.Fatalf("coverage regressed: %d < %d. either the translator broke or upstream bytecodes.c shrank.",
			len(passed), minTranslates)
	}
}

// bailKey normalizes a translator note to its category so similar
// fallbacks (different opcode names but same blocker) cluster.
func bailKey(note string) string {
	// Strip everything after the first colon-space; that's typically
	// the opcode-specific context wrapped around a generic reason.
	if _, rest, ok := strings.Cut(note, ": "); ok {
		return rest
	}
	return note
}

func sumGroupSizes(m map[string]*coverageBailGroup) int {
	var n int
	for _, g := range m {
		n += len(g.names)
	}
	return n
}

// debug helper retained for ad-hoc inspection; not currently used.
var _ = fmt.Sprintf
