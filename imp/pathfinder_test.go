package imp_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// pathTestExec mirrors what cmd/gopy injects: a thread-state-backed
// Executor that runs a code object in the module's namespace via the
// real VM. Reaching for vm here is fine because the test lives in
// package imp_test.
type pathTestExec struct{ ts *state.Thread }

func (e *pathTestExec) ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error) {
	return vm.EvalCode(e.ts, code, mod.Dict(), nil)
}

// pathTestCompiler is the gopy parse + compile pipeline expressed as
// a SourceCompiler.
func pathTestCompiler(src, filename string) (*objects.Code, error) {
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
		Code:           cco.Code,
		Consts:         cco.Consts,
		Names:          cco.Names,
		Varnames:       cco.VarNames,
		Freevars:       cco.FreeVars,
		Cellvars:       cco.CellVars,
		Stacksize:      cco.Stacksize,
		ExceptionTable: cco.ExceptionTable,
	}, nil
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestPathFinderFindsTopLevelModule pins the FileFinder fast path:
// a `<dir>/<name>.py` file resolves to a module whose globals reflect
// the source.
func TestPathFinderFindsTopLevelModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "foo.py"), "value = 42\n")

	pf := &imp.PathFinder{Paths: []string{dir}, Compiler: pathTestCompiler}
	exec := &pathTestExec{ts: state.NewThread()}

	mod, err := pf.FindModule(exec, "foo")
	if err != nil {
		t.Fatalf("FindModule: %v", err)
	}
	got, err := mod.Dict().GetItem(objects.NewStr("value"))
	if err != nil {
		t.Fatalf("GetItem value: %v", err)
	}
	if !intEquals(got, 42) {
		t.Fatalf("value = %v, want 42", got)
	}
}

// TestPathFinderFindsPackageInit pins the package branch:
// `<dir>/<name>/__init__.py` wins over a sibling `<name>.py`.
func TestPathFinderFindsPackageInit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pkg", "__init__.py"), "tag = 'pkg'\n")
	writeFile(t, filepath.Join(dir, "pkg.py"), "tag = 'flat'\n")

	pf := &imp.PathFinder{Paths: []string{dir}, Compiler: pathTestCompiler}
	exec := &pathTestExec{ts: state.NewThread()}

	mod, err := pf.FindModule(exec, "pkg")
	if err != nil {
		t.Fatalf("FindModule: %v", err)
	}
	got, err := mod.Dict().GetItem(objects.NewStr("tag"))
	if err != nil {
		t.Fatalf("GetItem tag: %v", err)
	}
	s, ok := got.(*objects.Unicode)
	if !ok || s.Value() != "pkg" {
		t.Fatalf("tag = %v, want 'pkg' (package __init__.py should win)", got)
	}
}

// TestPathFinderMissingReturnsErrModuleNotFound pins the negative path
// so ImportModuleLevel can distinguish "not found here, try the next
// finder" from a real load error.
func TestPathFinderMissingReturnsErrModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	pf := &imp.PathFinder{Paths: []string{dir}, Compiler: pathTestCompiler}
	exec := &pathTestExec{ts: state.NewThread()}

	_, err := pf.FindModule(exec, "absent")
	if err == nil {
		t.Fatal("FindModule: nil err, want ErrModuleNotFound")
	}
	if !errors.Is(err, imp.ErrModuleNotFound) {
		t.Fatalf("err = %v, want ErrModuleNotFound chain", err)
	}
}

// intEquals reports whether o is an *objects.Int with value want.
// objects.Int.Int64 returns (val, ok) so an inline cast is awkward;
// the helper keeps the call sites readable.
func intEquals(o objects.Object, want int64) bool {
	i, ok := o.(*objects.Int)
	if !ok {
		return false
	}
	v, ok := i.Int64()
	return ok && v == want
}

// TestImportModuleConsultsPathFinder pins the splice in import.go:
// once SetPathFinder has installed a finder, ImportModule walks past
// the inittab miss into the path-based branch.
func TestImportModuleConsultsPathFinder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "viapath.py"), "answer = 7\n")

	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{Paths: []string{dir}, Compiler: pathTestCompiler})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		// Drop the module we just registered so it does not bleed
		// between tests via the package-level sys.modules map.
		imp.RemoveModule("viapath")
	})

	exec := &pathTestExec{ts: state.NewThread()}
	mod, err := imp.ImportModule(exec, "viapath")
	if err != nil {
		t.Fatalf("ImportModule: %v", err)
	}
	got, err := mod.Dict().GetItem(objects.NewStr("answer"))
	if err != nil {
		t.Fatalf("GetItem answer: %v", err)
	}
	if !intEquals(got, 7) {
		t.Fatalf("answer = %v, want 7", got)
	}
}

// TestImportModuleFallsThroughEmptyFinder confirms the negative tail:
// a finder with empty paths still surfaces ErrModuleNotFound rather
// than masking real failures.
func TestImportModuleFallsThroughEmptyFinder(t *testing.T) {
	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{Paths: nil, Compiler: pathTestCompiler})
	t.Cleanup(func() { imp.SetPathFinder(prev) })

	exec := &pathTestExec{ts: state.NewThread()}
	_, err := imp.ImportModule(exec, "nope_no_such_module")
	if err == nil {
		t.Fatal("err = nil, want ModuleNotFound")
	}
	if !errors.Is(err, imp.ErrModuleNotFound) {
		t.Fatalf("err = %v, want ErrModuleNotFound chain", err)
	}
}
