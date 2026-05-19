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
	stdlib := filepath.Join(root, "stdlib")
	gopyEnv := append(os.Environ(), "GOPY_STDLIB="+stdlib)
	smokeProbe(t, gopyBin, gopyEnv)
	batchCompile(t, cpython, nil, files)
	batchCompile(t, gopyBin, gopyEnv, files)

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

// batchCompile invokes `bin -m py_compile <files...>` for the whole
// batch, chunked so a single invocation's command line stays under
// Windows' CreateProcess limit (~32 KiB). py_compile.main accepts an
// arbitrary file list and compiles each into its own __pycache__
// entry, so we still pay interpreter startup only a handful of times
// per side instead of once per fixture. When a chunk fails we bisect
// to locate a single failing fixture in O(log N) interpreter starts
// instead of O(N), then fail loudly with its name and stderr. The env
// argument is passed verbatim to the child (use nil to inherit); the
// gopy invocation needs GOPY_STDLIB so the spawned binary doesn't have
// to discover the repo stdlib by walking the cwd (which is unreliable
// on Windows where the test's tempdir lives on a different drive).
func batchCompile(t *testing.T, bin string, env, files []string) {
	t.Helper()
	if len(files) == 0 {
		return
	}
	for _, chunk := range chunkForCmdline(files, 24000) {
		if out, err := tryCompile(t, bin, env, chunk); err != nil {
			bad, badOut, badErr := bisectCompile(t, bin, env, chunk)
			if bad != "" {
				t.Fatalf("%s -m py_compile %s: %v\noutput:\n%s\n(batch of %d failed: %v\nbatch output:\n%s)",
					bin, bad, badErr, badOut, len(chunk), err, out)
			}
			t.Fatalf("%s -m py_compile <%d files>: batch failed (%v) but every file passed in isolation\noutput:\n%s",
				bin, len(chunk), err, out)
		}
	}
}

// smokeProbe runs a couple of trivial invocations of gopy and logs
// their output so a silent exit-1 from gopy.exe on Windows surfaces
// something we can read. It does not fail the test on smoke errors,
// only on a follow-up batch failure.
func smokeProbe(t *testing.T, bin string, env []string) {
	t.Helper()
	probes := [][]string{
		{"--version"},
		{"-c", "import sys; print(sys.platform)"},
		{"-c", "import runpy; print('runpy ok')"},
	}
	for _, args := range probes {
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		t.Logf("smoke %s %v: err=%v len(out)=%d output=%q", bin, args, err, len(out), out)
	}
}

func tryCompile(t *testing.T, bin string, env, files []string) ([]byte, error) {
	t.Helper()
	args := append([]string{"-m", "py_compile"}, files...)
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

// bisectCompile narrows a failing chunk down to a single fixture by
// halving until the remaining slice is one file. Returns the file
// path plus its captured output and error, or ("", nil, nil) if every
// fixture passes in isolation (an env-level batch failure with no
// per-file culprit).
func bisectCompile(t *testing.T, bin string, env, files []string) (string, []byte, error) {
	t.Helper()
	for len(files) > 1 {
		mid := len(files) / 2
		left, right := files[:mid], files[mid:]
		if _, err := tryCompile(t, bin, env, left); err != nil {
			files = left
			continue
		}
		if _, err := tryCompile(t, bin, env, right); err != nil {
			files = right
			continue
		}
		return "", nil, nil
	}
	if len(files) == 0 {
		return "", nil, nil
	}
	out, err := tryCompile(t, bin, env, files)
	if err == nil {
		return "", nil, nil
	}
	return files[0], out, err
}

// chunkForCmdline groups paths so the joined command line stays below
// budget bytes. Windows' CreateProcess caps the lpCommandLine string
// at 32767 chars; we target 24 KiB so the bin path + "-m py_compile"
// prefix and per-arg quoting overhead stay safely inside the limit.
func chunkForCmdline(files []string, budget int) [][]string {
	var chunks [][]string
	start, used := 0, 0
	for i, f := range files {
		// +1 accounts for the separating space and worst-case quoting.
		cost := len(f) + 1
		if i > start && used+cost > budget {
			chunks = append(chunks, files[start:i])
			start = i
			used = 0
		}
		used += cost
	}
	if start < len(files) {
		chunks = append(chunks, files[start:])
	}
	return chunks
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
