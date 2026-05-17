package compile

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

// TestOpcodeIDParity is the spec 1714 Phase 2.3 gate. It diffs
// the generated compile/opcode_ids_gen.go (a //go:build ignore
// file maintained by Tools/cases_generator/gopy_opcode_id_generator.py)
// against the hand-rolled compile/opcodes_gen.go. Once green and
// the spec ships Phase 2.4, the hand-rolled file is deleted.
//
// Both files are parsed as text; we never reach into the active
// package symbols because the generated file deliberately does
// not contribute to the build during the parity window.
func TestOpcodeIDParity(t *testing.T) {
	dir := pkgDir(t)
	gen := parseOpcodeFile(t, filepath.Join(dir, "opcode_ids_gen.go"))
	hand := parseOpcodeFile(t, filepath.Join(dir, "opcodes_gen.go"))
	// HAVE_ARGUMENT / MIN_SPECIALIZED_OPCODE / MIN_INSTRUMENTED_OPCODE
	// live in instrseq.go in the hand-rolled tree; pull them in so
	// the generator's boundary constants compare cleanly.
	maps.Copy(hand, parseOpcodeFile(t, filepath.Join(dir, "instrseq.go")))

	// Every generated opcode that the hand-rolled tree also names
	// must agree on the value. Generator-only names (HAVE_ARGUMENT,
	// new opcodes pulled from CPython that gopy has not yet wired)
	// are surfaced as log lines so the next porting step can pick
	// them up.
	for name, v := range gen {
		got, ok := hand[name]
		if !ok {
			t.Logf("generator declares %s = %d, hand-rolled tree does not yet name it", name, v)
			continue
		}
		if got != v {
			t.Errorf("opcode %s: generated %d, hand-rolled %d", name, v, got)
		}
	}
	// Names present hand-rolled but missing from the generator
	// flag a real divergence: the gopy hand-rolled set drifted
	// from CPython 3.14.5. Phase 2.4 only deletes opcodes_gen.go
	// when this set is empty (or every extra is documented).
	for name, v := range hand {
		if _, ok := gen[name]; !ok {
			t.Logf("hand-rolled declares %s = %d not in generator (likely gopy-only or stale)", name, v)
		}
	}
}

var opcodeRow = regexp.MustCompile(`^\s*([A-Z][A-Z0-9_]*)\s+Opcode\s*=\s*(\d+)\s*$`)

func parseOpcodeFile(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := opcodeRow.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		v, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("%s: parse %q: %v", path, m[0], err)
		}
		out[m[1]] = v
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: no opcode rows parsed", path)
	}
	return out
}

func pkgDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(thisFile)
}
