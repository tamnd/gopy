package regrtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseManifestRoundTrip(t *testing.T) {
	src := strings.Join([]string{
		"# header comment",
		"",
		"test_grammar\tdone\tv0.10.2\tparser drop landed in v0.10.2",
		"test_dis\tdone\tv0.5.0",
		"test_subprocess\tdeferred\tv0.10.1\tsubprocess module not shipped",
		"test_capi/\tout-of-scope\t\tC-API surface; gopy is pure Go",
	}, "\n")

	m, err := ParseManifest(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(m.Entries) != 4 {
		t.Fatalf("len(Entries) = %d, want 4", len(m.Entries))
	}

	gram, ok := m.ByName("test_grammar")
	if !ok {
		t.Fatal("test_grammar not found")
	}
	if gram.Status != StatusDone {
		t.Errorf("test_grammar.Status = %q, want done", gram.Status)
	}
	if gram.Version != "v0.10.2" {
		t.Errorf("test_grammar.Version = %q, want v0.10.2", gram.Version)
	}
	if gram.Note != "parser drop landed in v0.10.2" {
		t.Errorf("test_grammar.Note = %q", gram.Note)
	}

	capi, _ := m.ByName("test_capi/")
	if !capi.IsPackage() {
		t.Errorf("test_capi/ should report IsPackage=true")
	}

	counts := m.CountByStatus()
	if counts[StatusDone] != 2 || counts[StatusDeferred] != 1 || counts[StatusOutOfScope] != 1 {
		t.Errorf("CountByStatus = %v", counts)
	}

	ready := m.Filter(StatusDone)
	if len(ready) != 2 {
		t.Errorf("Filter(done) returned %d entries", len(ready))
	}
}

func TestParseManifestRejectsUnknownStatus(t *testing.T) {
	src := "test_x\twibble\tv0.1.0"
	if _, err := ParseManifest(strings.NewReader(src)); err == nil {
		t.Fatal("ParseManifest should have rejected wibble status")
	}
}

func TestParseManifestRejectsDuplicate(t *testing.T) {
	src := "test_a\tready\tv0.2.0\ntest_a\tready\tv0.3.0"
	if _, err := ParseManifest(strings.NewReader(src)); err == nil {
		t.Fatal("ParseManifest should have rejected duplicate test_a")
	}
}

func TestParseManifestRejectsEmpty(t *testing.T) {
	src := "# only a comment\n\n"
	if _, err := ParseManifest(strings.NewReader(src)); err == nil {
		t.Fatal("ParseManifest should have rejected empty manifest")
	}
}

// TestRepoManifestParses pins the real test/cpython/MANIFEST.txt:
// CI fails as soon as a malformed line is committed.
func TestRepoManifestParses(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	path := filepath.Join(wd, "..", "cpython", "MANIFEST.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("MANIFEST.txt not present at %s: %v", path, err)
	}
	defer f.Close()

	m, err := ParseManifest(f)
	if err != nil {
		t.Fatalf("ParseManifest(%s): %v", path, err)
	}
	if len(m.Entries) == 0 {
		t.Fatal("manifest has no entries")
	}
	t.Logf("manifest entries: %d", len(m.Entries))
	for status, n := range m.CountByStatus() {
		t.Logf("  %s: %d", status, n)
	}
}
