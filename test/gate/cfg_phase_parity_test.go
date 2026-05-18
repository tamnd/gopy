// Spec 1716 B.3 parity gate: drive the same Python source through
// the patched CPython interpreter and gopy's CompileWithCfgPhaseHook,
// capture the per-phase CFG dumps on both sides, and diff the
// resulting `unit<idx>.<phase>.cfg` trees byte-for-byte. Any
// divergence between gopy's flowgraph optimization passes and
// CPython's pins to a single (unit, phase) pair, so the debug work
// lands on one pass rather than the whole back-end.
//
// CPython side: test/cpython/patches/dump_phases.py drives a hook
// installed via _testinternalcapi.set_cfg_phase_hook (added by
// 0001-cfg-phase-dump.patch). The hook fires once per (code unit,
// pass) and writes one file per fire. CPython numbers units by the
// order the "entry" callback arrives in; gopy's runCfgPhaseHook walks
// units post-order to match, so unit000 on both sides corresponds to
// the innermost child, unit001 to its next sibling, and so on.
//
// gopy side: compile.CompileWithCfgPhaseHook builds the cfg in
// process and feeds a Go-level CompilePhaseHook the same (unit, phase,
// dump) triple. The harness writes the dump out under the matching
// filename so a direct directory diff works.
//
// The gate self-skips when GOPY_PATCHED_CPYTHON is not set or when
// the patched interpreter cannot be exec'd, so contributors without
// a local patched build still run the rest of the suite. CI sets the
// env var after building + caching the patched python.exe.

package gate

import (
	"bufio"
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

// patchedCPython returns the absolute path to the patched CPython
// interpreter from $GOPY_PATCHED_CPYTHON, or "" if unset or missing.
// The patched build lives at $HOME/github/python/cpython/python.exe
// on macOS and ./python on Linux after running the apply-and-build
// steps in test/cpython/patches/README.md.
func patchedCPython() string {
	p := os.Getenv("GOPY_PATCHED_CPYTHON")
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// repoRoot walks up from the gate package directory until it finds
// the go.mod that owns this repository. The gate compares against
// files vendored under test/cpython/Lib/, so it needs the absolute
// path independent of where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

// readCorpus reads cfg_phase_corpus.txt one path per line. Blank
// lines and lines starting with `#` are ignored. Paths are
// repository-relative so the file stays diffable across machines.
func readCorpus(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close() //nolint:errcheck

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}
	return out
}

// readSkip reads cfg_phase_skip.txt and returns the set of
// repo-relative paths flagged for L0 skip. Each entry tracks an
// upstream gopy bug, recorded as `<path> # <reason>` (the comment
// suffix is kept for human reviewers only).
func readSkip(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out
		}
		t.Fatalf("open skip: %v", err)
	}
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// path # reason
		path, reason, _ := strings.Cut(trimmed, "#")
		out[strings.TrimSpace(path)] = strings.TrimSpace(reason)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan skip: %v", err)
	}
	return out
}

// dumpGopyPhases compiles src under CompileWithCfgPhaseHook and
// writes one file per (unit, phase) fire into outDir, using the
// same `unit<idx:03d>.<phase>.cfg` naming as dump_phases.py. The
// hook is fired in the same post-order CPython uses, so unit000
// on both sides refers to the deepest nested code object.
func dumpGopyPhases(t *testing.T, src, filename, outDir string) {
	t.Helper()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}

	mod, err := parser.ParseString(src, filename, parser.ModeFile)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	idx := -1
	hook := func(_, phase, dump string) {
		if phase == "entry" {
			idx++
		}
		name := filepath.Join(outDir, gopyDumpName(idx, phase))
		if err := os.WriteFile(name, []byte(dump), 0o644); err != nil { //nolint:gosec
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := compile.CompileWithCfgPhaseHook(mod, filename, 0, hook); err != nil {
		t.Fatalf("CompileWithCfgPhaseHook %s: %v", filename, err)
	}
}

// gopyDumpName mirrors dump_phases.py's
// f"unit{idx:03d}.{phase_name}.cfg" naming so a directory walk on
// both sides yields the same set of basenames.
func gopyDumpName(idx int, phase string) string {
	return fmt.Sprintf("unit%03d.%s.cfg", idx, phase)
}

// listCfgDumps returns the sorted list of `unit*.cfg` basenames in
// dir. Sorting gives a deterministic comparison order so the first
// missing or extra file shows up at a stable position in the test
// failure message.
func listCfgDumps(t *testing.T, dir string) []string {
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
		if !strings.HasPrefix(name, "unit") || !strings.HasSuffix(name, ".cfg") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func TestCfgPhaseParity(t *testing.T) {
	cpy := patchedCPython()
	if cpy == "" {
		t.Skip("GOPY_PATCHED_CPYTHON not set; build via test/cpython/patches/README.md")
	}
	if runtime.GOOS == "windows" {
		// dump_phases.py's _testinternalcapi import only succeeds
		// against a debug-built python; the Windows build pipeline
		// isn't set up for it yet. Skip rather than fail noisily.
		t.Skip("patched CPython gate runs on Linux/macOS only for now")
	}

	root := repoRoot(t)
	corpusPath := filepath.Join(root, "test", "gate", "cfg_phase_corpus.txt")
	skipPath := filepath.Join(root, "test", "gate", "cfg_phase_skip.txt")
	corpus := readCorpus(t, corpusPath)
	skips := readSkip(t, skipPath)

	dumpScript := filepath.Join(root, "test", "cpython", "patches", "dump_phases.py")
	if _, err := os.Stat(dumpScript); err != nil {
		t.Fatalf("dump_phases.py missing: %v", err)
	}

	for _, rel := range corpus {
		t.Run(rel, func(t *testing.T) {
			if reason, ok := skips[rel]; ok {
				t.Skipf("skip per cfg_phase_skip.txt: %s", reason)
			}

			abs := filepath.Join(root, rel)
			src, err := os.ReadFile(abs) //nolint:gosec // path comes from vetted corpus file
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}

			tmp := t.TempDir()
			cpyDir := filepath.Join(tmp, "cpython")
			gopyDir := filepath.Join(tmp, "gopy")
			if err := os.MkdirAll(cpyDir, 0o755); err != nil {
				t.Fatalf("mkdir cpython: %v", err)
			}

			cmd := exec.CommandContext(t.Context(), cpy, dumpScript, //nolint:gosec // cpy from env, dumpScript repo-local
				"--src", abs, "--out", cpyDir, "--filename", rel)
			cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("dump_phases.py %s: %v\nstderr:\n%s", rel, err, stderr.String())
			}

			dumpGopyPhases(t, string(src), rel, gopyDir)

			cpyFiles := listCfgDumps(t, cpyDir)
			gopyFiles := listCfgDumps(t, gopyDir)
			if !equalSlices(cpyFiles, gopyFiles) {
				t.Fatalf("phase-dump file set mismatch:\n--- cpython ---\n%s\n--- gopy ---\n%s",
					strings.Join(cpyFiles, "\n"), strings.Join(gopyFiles, "\n"))
			}

			for _, name := range cpyFiles {
				cpyBytes, err := os.ReadFile(filepath.Join(cpyDir, name))
				if err != nil {
					t.Fatalf("read cpython %s: %v", name, err)
				}
				gopyBytes, err := os.ReadFile(filepath.Join(gopyDir, name))
				if err != nil {
					t.Fatalf("read gopy %s: %v", name, err)
				}
				if !bytes.Equal(cpyBytes, gopyBytes) {
					t.Errorf("%s: dump mismatch\n--- cpython ---\n%s\n--- gopy ---\n%s",
						name, string(cpyBytes), string(gopyBytes))
					return
				}
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
