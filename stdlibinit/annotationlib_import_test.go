package stdlibinit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"
)

// TestImportAnnotationlib is the spec 1706 Phase 8 gate: `import
// annotationlib` must resolve end-to-end, produce a Format enum with
// the correct integer values, and expose get_annotations.
//
// The critical line is annotationlib.py:1162:
//
//	_BASE_GET_ANNOTATIONS = type.__dict__["__annotations__"].__get__
//
// which requires __annotations__ to be in type.__dict__ (a GetSetDescr
// registered on typeType via SetTypeDescr).
func TestImportAnnotationlib(t *testing.T) {
	installAnnotationlibFinder(t)

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	if err := g.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__")); err != nil {
		t.Fatalf("set __name__: %v", err)
	}
	ts := state.NewThread()
	src := `
import annotationlib
print(annotationlib.Format.VALUE.value)
print(annotationlib.Format.STRING.value)
`
	if _, err := pythonrun.RunString(ts, src, "<annotationlib-import>", parser.ModeFile, g, nil); err != nil {
		t.Fatalf("import annotationlib: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	want := "1\n4"
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// installAnnotationlibFinder sets up PathFinder for annotationlib and
// its transitive deps (ast, enum, keyword, types, _ast).
func installAnnotationlibFinder(t *testing.T) {
	t.Helper()
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibPath(t)},
		Compiler: abcCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		for _, name := range []string{
			"annotationlib", "ast", "_ast", "enum", "keyword", "types",
			"abc", "_py_abc", "_weakrefset", "_collections_abc",
		} {
			imp.RemoveModule(name)
		}
	})
}
