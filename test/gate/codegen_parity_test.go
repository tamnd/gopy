// L1 parity gate: drive the same Python source through the patched
// CPython interpreter's codegen_oracle.py and gopy's
// compile.CompileAndDumpL1, then diff the resulting unit000.l1.txt
// files byte-for-byte. A divergence pins to the _PyCompile_CodeGen
// output (instruction sequence + metadata) before any cfg pass
// touches it.
//
// CPython side: test/cpython/patches/codegen_oracle.py drives
// _testinternalcapi.compiler_codegen(tree, filename, optimize) and
// writes one unit000.l1.txt per run.
//
// gopy side: compile.CompileAndDumpL1 runs the front-end (future +
// preprocess + symtable + Codegen) and renders the same
// dump_instruction_sequence text from the resulting Unit.
//
// The gate self-skips when GOPY_PATCHED_CPYTHON is not set or when
// the patched interpreter cannot be exec'd. CI sets the env var
// after building the patched python.exe.

package gate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/parser"
)

func TestCodegenParity(t *testing.T) {
	cpy := patchedCPython()
	if cpy == "" {
		t.Skip("GOPY_PATCHED_CPYTHON not set; build via test/cpython/patches/README.md")
	}
	if runtime.GOOS == "windows" {
		t.Skip("patched CPython gate runs on Linux/macOS only for now")
	}

	root := repoRoot(t)
	corpusPath := filepath.Join(root, "test", "gate", "codegen_parity_corpus.txt")
	skipPath := filepath.Join(root, "test", "gate", "codegen_parity_skip.txt")
	corpus := readCorpus(t, corpusPath)
	skips := readSkip(t, skipPath)

	oracle := filepath.Join(root, "test", "cpython", "patches", "codegen_oracle.py")
	if _, err := os.Stat(oracle); err != nil {
		t.Fatalf("codegen_oracle.py missing: %v", err)
	}

	for _, rel := range corpus {
		t.Run(rel, func(t *testing.T) {
			if reason, ok := skips[rel]; ok {
				t.Skipf("skip per codegen_parity_skip.txt: %s", reason)
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
				t.Fatalf("codegen_oracle.py %s: %v\nstderr:\n%s", rel, err, stderr.String())
			}

			cpyBytes, err := os.ReadFile(filepath.Join(tmp, "unit000.l1.txt"))
			if err != nil {
				t.Fatalf("read cpython dump: %v", err)
			}

			mod, err := parser.ParseString(string(src), rel, parser.ModeFile)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			gopyDump, err := compile.CompileAndDumpL1(mod, rel, 0)
			if err != nil {
				t.Fatalf("CompileAndDumpL1 %s: %v", rel, err)
			}

			if !bytes.Equal(cpyBytes, []byte(gopyDump)) {
				t.Errorf("L1 dump mismatch\n--- cpython ---\n%s\n--- gopy ---\n%s",
					string(cpyBytes), gopyDump)
			}
		})
	}
}
