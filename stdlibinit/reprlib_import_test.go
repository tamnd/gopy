package stdlibinit_test

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/pythonrun"
	"github.com/tamnd/gopy/state"

	_ "github.com/tamnd/gopy/stdlibinit"
)

// reprlibStdlibPath returns the absolute path to stdlib/ relative to
// this test file. runtime.Caller is the standard trick that survives
// both `go test ./...` from the module root and `go test
// ./stdlibinit/` from inside this directory.
func reprlibStdlibPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "stdlib")
}

// reprlibCompiler is the parse + compile pipeline expressed as a
// SourceCompiler so PathFinder can load .py files off disk.
func reprlibCompiler(src, filename string) (*objects.Code, error) {
	if src == "" || src[len(src)-1] != '\n' {
		src += "\n"
	}
	mod, err := parser.ParseString(src, filename, parser.ModeFile)
	if err != nil {
		return nil, err
	}
	cco, err := compile.Compile(mod, filename, 0)
	if err != nil {
		return nil, err
	}
	return &objects.Code{
		Argcount:        cco.Argcount,
		PosonlyArgcount: cco.PosOnlyArgCount,
		KwonlyArgcount:  cco.KwOnlyArgCount,
		Stacksize:       cco.Stacksize,
		Flags:           int(cco.Flags),
		Code:            cco.Code,
		Consts:          cco.Consts,
		Names:           cco.Names,
		Varnames:        cco.VarNames,
		Freevars:        cco.FreeVars,
		Cellvars:        cco.CellVars,
		Filename:        cco.Filename,
		Name:            cco.Name,
		Qualname:        cco.Qualname,
		Firstlineno:     cco.Firstlineno,
		Linetable:       cco.Linetable,
		ExceptionTable:  cco.ExceptionTable,
	}, nil
}

// TestReprlibImportResolvesNames pins that `import reprlib` walks
// through the vendored Lib/reprlib.py and exposes the public API
// surface (Repr, repr, recursive_repr) downstream modules lean on.
//
// reprlib imports `builtins`, `itertools.islice`, and
// `_thread.get_ident`. On worktrees where any of those modules are
// not registered yet the test skips with the exact "No module named"
// error string so it auto-flips once the missing module lands.
func TestReprlibImportResolvesNames(t *testing.T) {
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{reprlibStdlibPath(t)},
		Compiler: reprlibCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		imp.RemoveModule("reprlib")
	})

	var stdout bytes.Buffer
	g, err := builtins.Init(&stdout)
	if err != nil {
		t.Fatalf("builtins.Init: %v", err)
	}
	ts := state.NewThread()

	src := "import reprlib\n"
	if _, err := pythonrun.RunString(ts, src, "<reprlib-import>", parser.ModeFile, g, nil); err != nil {
		msg := err.Error()
		for _, missing := range []string{"builtins", "itertools", "_thread"} {
			if strings.Contains(msg, "No module named \""+missing+"\"") {
				t.Skipf("blocked on missing transitive import %q: %v", missing, err)
			}
		}
		t.Fatalf("import reprlib: %v", err)
	}

	mod, ok := imp.GetModule("reprlib")
	if !ok {
		t.Fatal("imp.GetModule(\"reprlib\") missing after import")
	}
	for _, name := range []string{"Repr", "repr", "recursive_repr"} {
		v, err := mod.Dict().GetItem(objects.NewStr(name))
		if err != nil {
			t.Errorf("reprlib.%s lookup: %v", name, err)
			continue
		}
		if v == nil || v == objects.None() {
			t.Errorf("reprlib.%s = %v, want non-None", name, v)
		}
	}
}
