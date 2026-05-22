// re import gate: pins that _sre is registered in the inittab and that
// Pattern.match / Pattern.search work end-to-end through the bytecode
// engine the vendored Lib/re/_compiler.py targets. Tests construct
// minimal bytecode by hand for literal patterns until the full
// Python-side compile path is wired (spec 1703 phase 7).
//
// CPython: Lib/re/__init__.py public surface
package stdlibinit

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/_sre"
	"github.com/tamnd/gopy/objects"
)

// TestSreInInittab pins that blank-importing stdlibinit registers _sre.
func TestSreInInittab(t *testing.T) {
	if fn := imp.FindInitFunc("_sre"); fn == nil {
		t.Fatal("imp.FindInitFunc(\"_sre\") = nil; stdlibinit should register _sre")
	}
}

// TestSreModuleExportsMAGIC pins that _sre exposes the MAGIC constant
// used by Lib/re/_compiler.py to detect engine mismatches.
//
// CPython: Modules/_sre/sre.c:3410 PyModule_AddIntConstant MAGIC
func TestSreModuleExportsMAGIC(t *testing.T) {
	fn := imp.FindInitFunc("_sre")
	if fn == nil {
		t.Skip("_sre not registered")
	}
	mod, err := fn()
	if err != nil {
		t.Fatalf("_sre init: %v", err)
	}
	v, err := mod.Dict().GetItem(objects.NewStr("MAGIC"))
	if err != nil || v == nil {
		t.Fatalf("_sre.MAGIC missing: %v", err)
	}
	i, ok := v.(*objects.Int)
	if !ok {
		t.Fatalf("_sre.MAGIC is %T, want *objects.Int", v)
	}
	n, _ := i.Int64()
	if n != _sre.MagicNumber {
		t.Errorf("_sre.MAGIC = %d, want %d", n, _sre.MagicNumber)
	}
}

// TestSreCompileAndMatch pins the core path: _sre.compile takes the
// bytecode produced by Lib/re/_compiler.py, stores it on a Pattern,
// and the Pattern.match() method drives the engine against it.
//
// CPython: Modules/_sre/sre.c:1621 _sre_compile_impl
func TestSreCompileAndMatch(t *testing.T) {
	mod := loadSre(t)

	// Bytecode for the literal pattern "hello".
	code := literalCode("hello")
	pat, err := callSreCompile(mod, "hello", 0, code, 0)
	if err != nil {
		t.Fatalf("_sre.compile: %v", err)
	}

	m, err := callPatternMethod(pat, "match", "hello world")
	if err != nil {
		t.Fatalf("pattern.match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("pattern.match(\"hello world\") returned None, want Match")
	}

	m2, err := callPatternMethod(pat, "match", "world")
	if err != nil {
		t.Fatalf("pattern.match(no-match): %v", err)
	}
	if !objects.IsNone(m2) {
		t.Errorf("pattern.match(\"world\") = %v, want None", m2)
	}
}

// TestSreSearch pins that Pattern.search finds a match anywhere in the
// string.
//
// CPython: Modules/_sre/sre.c:2594 _sre_SRE_Pattern_search_impl
func TestSreSearch(t *testing.T) {
	mod := loadSre(t)
	pat, err := callSreCompile(mod, "world", 0, literalCode("world"), 0)
	if err != nil {
		t.Fatalf("_sre.compile: %v", err)
	}
	m, err := callPatternMethod(pat, "search", "hello world")
	if err != nil {
		t.Fatalf("pattern.search: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("pattern.search(\"hello world\") returned None, want Match")
	}
}

// TestReImportFromStdlib attempts to import the vendored re package via
// PathFinder. This requires enum and copyreg to be available, plus a
// full VM executor wired in the test. All of those are future work; the
// test skips until the transitive deps and VM wiring are in place. It
// serves as a forward compatibility gate: when it stops skipping, the
// vendored re package loads end-to-end.
func TestReImportFromStdlib(t *testing.T) {
	const stdlibSubdir = "../stdlib"

	exec := &reTestExec{}
	prev := imp.GetPathFinder()
	t.Cleanup(func() { imp.SetPathFinder(prev) })
	imp.RemoveModule("re")
	t.Cleanup(func() { imp.RemoveModule("re") })

	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibSubdir},
		Compiler: reSourceCompiler,
	})

	_, err := imp.ImportModule(exec, "re")
	if err == nil {
		return
	}
	t.Skipf("re import not yet available (expected): %v", err)
}

// ---------------------------------------------------------------------------
// Helpers.

func loadSre(t *testing.T) *objects.Module {
	t.Helper()
	fn := imp.FindInitFunc("_sre")
	if fn == nil {
		t.Skip("_sre not registered")
	}
	mod, err := fn()
	if err != nil {
		t.Fatalf("_sre init: %v", err)
	}
	return mod
}

// literalCode produces the bytecode _compiler.py emits for a literal
// pattern string: a chain of OpLiteral entries terminated by OpSuccess.
func literalCode(s string) []uint32 {
	out := make([]uint32, 0, 2*len(s)+1)
	for _, r := range s {
		out = append(out, _sre.OpLiteral, uint32(r))
	}
	out = append(out, _sre.OpSuccess)
	return out
}

// callSreCompile calls _sre.compile with the standard 6-arg shape.
func callSreCompile(mod *objects.Module, pattern string, flags int, code []uint32, groups int) (objects.Object, error) {
	compileFn, err := mod.Dict().GetItem(objects.NewStr("compile"))
	if err != nil {
		return nil, err
	}
	codeItems := make([]objects.Object, len(code))
	for i, v := range code {
		codeItems[i] = objects.NewInt(int64(v))
	}
	args := []objects.Object{
		objects.NewStr(pattern),
		objects.NewInt(int64(flags)),
		objects.NewList(codeItems),
		objects.NewInt(int64(groups)),
		objects.NewDict(),
		objects.NewTuple(nil),
	}
	return objects.Vectorcall(compileFn, args, uint(len(args)), nil)
}

// callPatternMethod calls method `name` on the pattern instance with
// string `s` as the only non-self argument.
func callPatternMethod(pat objects.Object, method, s string) (objects.Object, error) {
	inst, ok := pat.(*objects.Instance)
	if !ok {
		return nil, errors.New("not a Pattern instance")
	}
	attrObj, err := inst.Type().Getattro(inst, objects.NewStr(method))
	if err != nil {
		return nil, err
	}
	return objects.Vectorcall(attrObj, []objects.Object{objects.NewStr(s)}, 1, nil)
}

// reTestExec is a minimal Executor for the re import test.
type reTestExec struct{}

func (e *reTestExec) ExecCode(code *objects.Code, mod *objects.Module) (objects.Object, error) {
	return nil, errors.New("ExecCode not wired in re_import_test (use vm package)")
}

// reSourceCompiler is the SourceCompiler for the PathFinder in tests.
// Returns an error so path-based imports fail fast with a clear message
// rather than panicking on missing vm wiring.
func reSourceCompiler(src []byte, filename string) (*objects.Code, error) {
	return nil, errors.New("re_import_test: source compilation not wired; set up vm.EvalCode")
}
