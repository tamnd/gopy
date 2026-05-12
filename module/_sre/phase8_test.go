package _sre

// Phase 8 regression guard. The RE2 wrapper retirement landed in
// Phase 4; this test makes sure nobody quietly drops `import "regexp"`
// back into the package. If you legitimately need stdlib regexp here,
// the right move is to revisit spec 1703, not to slip an import in.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSreHasNoRegexpImport scans every Go source file in this package
// (excluding tests) and fails if any of them import "regexp".
func TestSreHasNoRegexpImport(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "regexp" {
				t.Errorf("%s imports %q; the RE2 wrapper was retired in spec 1703 Phase 4", name, path)
			}
		}
	}
}
