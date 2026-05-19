// Spec 1713 byte-equality gate. The gate copies every corpus fixture
// into a shared tempdir, then issues a single `python3.14 -m py_compile`
// and a single `gopy -m py_compile` invocation covering the whole batch
// so interpreter startup is paid once per side rather than once per
// fixture. CPython writes __pycache__/foo.cpython-314.pyc and gopy
// writes __pycache__/foo.gopy-3140.pyc; each per-fixture subtest then
// diffs the two .pyc files byte-for-byte. The header check confirms
// magic + flags + mtime + size; the body check confirms the marshal
// stream itself.
//
// The gate self-skips when CPython 3.14 is not on PATH. The corpus
// lives in test/gate/pyc_parity_corpus.txt and the skip list in
// test/gate/pyc_parity_skip.txt, both repo-relative paths.

package gate

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPycParity(t *testing.T) {
	cpython := FindCPython(t)
	if cpython == "" {
		t.Skip("CPython 3.14 not on PATH")
	}
	gopyBin := BuildGopy(t)

	root := repoRoot(t)
	corpusPath := filepath.Join(root, "test", "gate", "pyc_parity_corpus.txt")
	skipPath := filepath.Join(root, "test", "gate", "pyc_parity_skip.txt")
	corpus := readCorpus(t, corpusPath)
	skips := readSkip(t, skipPath)

	tmp := t.TempDir()
	type fixture struct {
		rel  string
		dst  string
		stem string
	}
	var batch []fixture
	seen := map[string]string{}
	for _, rel := range corpus {
		if _, ok := skips[rel]; ok {
			continue
		}
		base := filepath.Base(rel)
		if !strings.HasSuffix(base, ".py") {
			t.Fatalf("fixture %s does not end in .py", rel)
		}
		if prev, ok := seen[base]; ok {
			t.Fatalf("duplicate fixture basename %q (from %s and %s); pyc batch needs unique stems", base, prev, rel)
		}
		seen[base] = rel
		src := filepath.Join(root, rel)
		dst := filepath.Join(tmp, base)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write fixture copy %s: %v", dst, err)
		}
		batch = append(batch, fixture{rel: rel, dst: dst, stem: strings.TrimSuffix(base, ".py")})
	}

	files := make([]string, len(batch))
	for i, f := range batch {
		files[i] = f.dst
	}
	batchCompile(t, cpython, files)
	batchCompile(t, gopyBin, files)

	cacheDir := filepath.Join(tmp, "__pycache__")
	for _, rel := range corpus {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			if reason, ok := skips[rel]; ok {
				t.Skipf("skip per pyc_parity_skip.txt: %s", reason)
			}
			stem := strings.TrimSuffix(filepath.Base(rel), ".py")
			diffPyc(t, cacheDir, stem)
		})
	}
}

// batchCompile invokes `bin -m py_compile <files...>` once for the
// whole batch. py_compile.main accepts an arbitrary file list and
// compiles each into its own __pycache__ entry, so we pay interpreter
// startup just once per side.
func batchCompile(t *testing.T, bin string, files []string) {
	t.Helper()
	if len(files) == 0 {
		return
	}
	args := append([]string{"-m", "py_compile"}, files...)
	cmd := exec.CommandContext(t.Context(), bin, args...)
	// Leave cwd alone so gopy's findStdlibRoot can walk up from the
	// test/gate directory to the repo root; CPython resolves its
	// own stdlib independently.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s -m py_compile <%d files>: %v\noutput:\n%s", bin, len(files), err, out)
	}
}

func diffPyc(t *testing.T, cacheDir, stem string) {
	t.Helper()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read __pycache__: %v", err)
	}
	var cpyPyc, gopyPyc string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, stem+".") || !strings.HasSuffix(name, ".pyc") {
			continue
		}
		switch {
		case strings.Contains(name, "cpython"):
			cpyPyc = filepath.Join(cacheDir, name)
		case strings.Contains(name, "gopy"):
			gopyPyc = filepath.Join(cacheDir, name)
		}
	}
	if cpyPyc == "" {
		t.Fatalf("cpython .pyc not produced for stem %q under %s", stem, cacheDir)
	}
	if gopyPyc == "" {
		t.Fatalf("gopy .pyc not produced for stem %q under %s", stem, cacheDir)
	}

	cpyBytes, err := os.ReadFile(cpyPyc)
	if err != nil {
		t.Fatalf("read %s: %v", cpyPyc, err)
	}
	gopyBytes, err := os.ReadFile(gopyPyc)
	if err != nil {
		t.Fatalf("read %s: %v", gopyPyc, err)
	}

	if len(cpyBytes) < 16 || len(gopyBytes) < 16 {
		t.Fatalf("pyc too short: cpython=%d gopy=%d", len(cpyBytes), len(gopyBytes))
	}
	cpyHeader, gopyHeader := cpyBytes[:16], gopyBytes[:16]
	if !bytes.Equal(cpyHeader[:4], gopyHeader[:4]) {
		t.Errorf("magic mismatch: cpython=%x gopy=%x", cpyHeader[:4], gopyHeader[:4])
	}
	cpyFlags := binary.LittleEndian.Uint32(cpyHeader[4:8])
	gopyFlags := binary.LittleEndian.Uint32(gopyHeader[4:8])
	if cpyFlags != gopyFlags {
		t.Errorf("flags mismatch: cpython=%d gopy=%d", cpyFlags, gopyFlags)
	}
	if !bytes.Equal(cpyHeader[8:16], gopyHeader[8:16]) {
		t.Errorf("timestamp/size mismatch: cpython=%x gopy=%x", cpyHeader[8:16], gopyHeader[8:16])
	}

	if !bytes.Equal(cpyBytes[16:], gopyBytes[16:]) {
		t.Errorf("marshaled body mismatch:\n cpython %d bytes\n gopy    %d bytes\n cpython hex: %x\n gopy    hex: %x",
			len(cpyBytes)-16, len(gopyBytes)-16, cpyBytes[16:], gopyBytes[16:])
	}
}
