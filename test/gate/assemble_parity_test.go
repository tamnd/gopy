// L3 + L4 parity gates: drive the same Python source through the
// patched CPython interpreter's optimize_oracle.py /
// assemble_oracle.py and gopy's compile.CompileAndDumpL3 /
// CompileAndDumpL4, then diff the resulting dumps byte-for-byte.
//
// L3 pins to the post-cfg-optimize instruction sequence, exposing
// divergence in the cfg pipeline (basic-block construction +
// optimize_basic_block + dead-block elimination + push_cold + ...).
// L4 pins to the final code-object fields, exposing divergence in
// the assembler (emit, jumps, makecode, location-table emit, ...).
//
// CPython side: optimize_oracle.py / assemble_oracle.py drive
// _testinternalcapi.optimize_cfg and the compile() builtin
// respectively, writing unit000.l3.txt / unit<idx>.l4.txt.
//
// gopy side: compile.CompileAndDumpL3 (nparams=0, firstlineno=1) and
// compile.CompileAndDumpL4 render the matching text from a full
// gopy compile run.

package gate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/parser"
)

func TestOptimizeParity(t *testing.T) {
	cpy := patchedCPython()
	if cpy == "" {
		t.Skip("GOPY_PATCHED_CPYTHON not set; build via test/cpython/patches/README.md")
	}
	if runtime.GOOS == "windows" {
		t.Skip("patched CPython gate runs on Linux/macOS only for now")
	}

	root := repoRoot(t)
	corpusPath := filepath.Join(root, "test", "gate", "assemble_parity_corpus.txt")
	skipPath := filepath.Join(root, "test", "gate", "assemble_parity_skip.txt")
	corpus := readCorpus(t, corpusPath)
	skips := readSkip(t, skipPath)

	oracle := filepath.Join(root, "test", "cpython", "patches", "optimize_oracle.py")
	if _, err := os.Stat(oracle); err != nil {
		t.Fatalf("optimize_oracle.py missing: %v", err)
	}

	for _, rel := range corpus {
		t.Run(rel, func(t *testing.T) {
			if reason, ok := skips[rel]; ok {
				t.Skipf("skip per assemble_parity_skip.txt: %s", reason)
			}

			abs := filepath.Join(root, rel)
			src, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}

			tmp := t.TempDir()
			cmd := exec.CommandContext(t.Context(), cpy, oracle,
				"--src", abs, "--out", tmp, "--filename", rel)
			cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("optimize_oracle.py %s: %v\nstderr:\n%s", rel, err, stderr.String())
			}

			cpyBytes, err := os.ReadFile(filepath.Join(tmp, "unit000.l3.txt"))
			if err != nil {
				t.Fatalf("read cpython dump: %v", err)
			}

			mod, err := parser.ParseString(string(src), rel, parser.ModeFile)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			gopyDump, err := compile.CompileAndDumpL3(mod, rel, 0, 0)
			if err != nil {
				t.Fatalf("CompileAndDumpL3 %s: %v", rel, err)
			}

			if !bytes.Equal(cpyBytes, []byte(gopyDump)) {
				t.Errorf("L3 dump mismatch\n--- cpython ---\n%s\n--- gopy ---\n%s",
					string(cpyBytes), gopyDump)
			}
		})
	}
}

func TestAssembleParity(t *testing.T) {
	cpy := patchedCPython()
	if cpy == "" {
		t.Skip("GOPY_PATCHED_CPYTHON not set; build via test/cpython/patches/README.md")
	}
	if runtime.GOOS == "windows" {
		t.Skip("patched CPython gate runs on Linux/macOS only for now")
	}

	root := repoRoot(t)
	corpusPath := filepath.Join(root, "test", "gate", "assemble_parity_corpus.txt")
	skipPath := filepath.Join(root, "test", "gate", "assemble_parity_skip.txt")
	corpus := readCorpus(t, corpusPath)
	skips := readSkip(t, skipPath)

	oracle := filepath.Join(root, "test", "cpython", "patches", "assemble_oracle.py")
	if _, err := os.Stat(oracle); err != nil {
		t.Fatalf("assemble_oracle.py missing: %v", err)
	}

	for _, rel := range corpus {
		t.Run(rel, func(t *testing.T) {
			if reason, ok := skips[rel]; ok {
				t.Skipf("skip per assemble_parity_skip.txt: %s", reason)
			}

			abs := filepath.Join(root, rel)
			src, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}

			tmp := t.TempDir()
			cmd := exec.CommandContext(t.Context(), cpy, oracle,
				"--src", abs, "--out", tmp, "--filename", rel)
			cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("assemble_oracle.py %s: %v\nstderr:\n%s", rel, err, stderr.String())
			}

			cpyFiles := listL4Dumps(t, tmp)

			mod, err := parser.ParseString(string(src), rel, parser.ModeFile)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			gopyDumps, err := compile.CompileAndDumpL4(mod, rel, 0)
			if err != nil {
				t.Fatalf("CompileAndDumpL4 %s: %v", rel, err)
			}

			gopyFiles := make([]string, len(gopyDumps))
			for i := range gopyDumps {
				gopyFiles[i] = fmt.Sprintf("unit%03d.l4.txt", i)
			}
			if !equalSlices(cpyFiles, gopyFiles) {
				t.Fatalf("unit set mismatch:\n--- cpython ---\n%s\n--- gopy ---\n%s",
					strings.Join(cpyFiles, "\n"), strings.Join(gopyFiles, "\n"))
			}

			for i, name := range cpyFiles {
				cpyBytes, err := os.ReadFile(filepath.Join(tmp, name))
				if err != nil {
					t.Fatalf("read cpython %s: %v", name, err)
				}
				if !bytes.Equal(cpyBytes, []byte(gopyDumps[i])) {
					t.Errorf("%s: L4 dump mismatch\n--- cpython ---\n%s\n--- gopy ---\n%s",
						name, string(cpyBytes), gopyDumps[i])
					return
				}
			}
		})
	}
}

func listL4Dumps(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "unit") || !strings.HasSuffix(name, ".l4.txt") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
