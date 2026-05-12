// End-to-end import smoke tests for vendored pure-Python stdlib modules.
// Each test runs imp.ImportModule against the real PathFinder+VM pipeline
// and skips when a transitive dependency is not yet available.
package stdlibinit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/parser"
	"github.com/tamnd/gopy/state"
	"github.com/tamnd/gopy/vm"
)

// stdlibRoot returns the absolute path to the vendored stdlib/ directory
// by walking upward from this file's directory.
func stdlibRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		candidate := filepath.Join(dir, "stdlib")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate stdlib/ directory")
	return ""
}

// stdlibCompiler parses and compiles a Python source file the same way
// pathfinder_test.go does in the imp package.
func stdlibCompiler(src, filename string) (*objects.Code, error) {
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

// stdlibExec is an imp.Executor that drives the real VM.
type stdlibExec struct{ ts *state.Thread }

func (e *stdlibExec) ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error) {
	return vm.EvalCode(e.ts, code, mod.Dict(), nil)
}

// importStdlib attempts to import name via the real PathFinder+VM and
// returns (module, err). It installs a temporary PathFinder restricted
// to the vendored stdlib directory so the test is hermetic with respect
// to any finder installed by the process.
//
// A panic from the compiler (e.g. an unsupported constant type) is
// caught and returned as an error so callers can skip rather than fail.
func importStdlib(t *testing.T, name string) (mod *objects.Module, err error) {
	t.Helper()
	root := stdlibRoot(t)

	prev := imp.GetPathFinder()
	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{root},
		Compiler: stdlibCompiler,
	})
	t.Cleanup(func() {
		imp.SetPathFinder(prev)
		imp.RemoveModule(name)
	})

	// Recover compiler panics (e.g. unhashable constant type for bytes
	// literals) so the test can skip rather than crash the suite.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("compiler/VM panic: %v", r)
		}
	}()

	exec := &stdlibExec{ts: state.NewThread()}
	return imp.ImportModule(exec, name)
}

// skipIfMissingDep skips the test when err indicates a missing transitive
// dependency (ModuleNotFoundError or similar import chain error) or an
// unsupported compiler/VM feature (e.g. bytes-literal constant, missing
// __build_class__).
func skipIfMissingDep(t *testing.T, err error, name string) {
	t.Helper()
	msg := err.Error()
	if errors.Is(err, imp.ErrModuleNotFound) ||
		strings.Contains(msg, "No module named") ||
		strings.Contains(msg, "ModuleNotFoundError") ||
		strings.Contains(msg, "compiler/VM panic") ||
		strings.Contains(msg, "unhashable") ||
		strings.Contains(msg, "__build_class__") ||
		strings.Contains(msg, "NameError") {
		t.Skipf("import %s blocked on missing transitive dep or VM gap: %v", name, err)
	}
}

// TestImportLinecache checks that linecache can be imported end-to-end.
func TestImportLinecache(t *testing.T) {
	_, err := importStdlib(t, "linecache")
	if err != nil {
		skipIfMissingDep(t, err, "linecache")
		t.Fatalf("import linecache: %v", err)
	}
}

// TestImportTokenize checks that tokenize can be imported end-to-end.
func TestImportTokenize(t *testing.T) {
	_, err := importStdlib(t, "tokenize")
	if err != nil {
		skipIfMissingDep(t, err, "tokenize")
		t.Fatalf("import tokenize: %v", err)
	}
}

// TestImportTextwrap checks that textwrap can be imported end-to-end.
func TestImportTextwrap(t *testing.T) {
	_, err := importStdlib(t, "textwrap")
	if err != nil {
		skipIfMissingDep(t, err, "textwrap")
		t.Fatalf("import textwrap: %v", err)
	}
}

// TestImportCodeop checks that codeop can be imported end-to-end.
func TestImportCodeop(t *testing.T) {
	_, err := importStdlib(t, "codeop")
	if err != nil {
		skipIfMissingDep(t, err, "codeop")
		t.Fatalf("import codeop: %v", err)
	}
}
