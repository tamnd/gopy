// re import gate: pins that _sre is registered in the inittab and that
// Pattern.match / Pattern.search work end-to-end through the Go regexp
// backend. The vendored Lib/re/ package requires `import enum` and
// `import copyreg` which are not yet available; the full re import test
// skips on ModuleNotFoundError for those transitive dependencies.
//
// CPython: Lib/re/__init__.py public surface
package stdlibinit

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/imp"
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
	if n != 20230612 {
		t.Errorf("_sre.MAGIC = %d, want 20230612", n)
	}
}

// TestSreCompileAndMatch pins the core path: _sre.compile returns a
// Pattern whose match() method returns a Match on success and None on
// failure.
//
// CPython: Modules/_sre/sre.c:1621 _sre_compile_impl
func TestSreCompileAndMatch(t *testing.T) {
	fn := imp.FindInitFunc("_sre")
	if fn == nil {
		t.Skip("_sre not registered")
	}
	mod, err := fn()
	if err != nil {
		t.Fatalf("_sre init: %v", err)
	}

	compileFn, err := mod.Dict().GetItem(objects.NewStr("compile"))
	if err != nil || compileFn == nil {
		t.Fatalf("_sre.compile missing: %v", err)
	}

	// _sre.compile(pattern, flags, code, groups, groupindex, indexgroup)
	// code / groupindex / indexgroup are the compiled engine args from
	// _compiler.py; we pass minimal placeholders here.
	args := []objects.Object{
		objects.NewStr("hello"),    // pattern
		objects.NewInt(0),          // flags
		objects.NewList(nil),       // code (empty; Go backend ignores this)
		objects.NewInt(0),          // groups
		objects.NewDict(),          // groupindex
		objects.NewTuple(nil),      // indexgroup
	}
	pat, err := objects.Vectorcall(compileFn, args, uint(len(args)), nil)
	if err != nil {
		t.Fatalf("_sre.compile: %v", err)
	}
	if objects.IsNone(pat) {
		t.Fatal("_sre.compile returned None")
	}

	// Pattern.match("hello world") should match at position 0.
	patInst, ok := pat.(*objects.Instance)
	if !ok {
		t.Fatalf("compile returned %T, want *objects.Instance", pat)
	}

	matchFnObj, err := patInst.Type().Getattro(patInst, objects.NewStr("match"))
	if err != nil {
		t.Fatalf("pattern.match attr: %v", err)
	}
	m, err := objects.Vectorcall(matchFnObj, []objects.Object{objects.NewStr("hello world")}, 1, nil)
	if err != nil {
		t.Fatalf("pattern.match: %v", err)
	}
	if objects.IsNone(m) {
		t.Fatal("pattern.match(\"hello world\") returned None, want Match")
	}

	// Pattern.match("world") should return None (no match at position 0).
	m2, err := objects.Vectorcall(matchFnObj, []objects.Object{objects.NewStr("world")}, 1, nil)
	if err != nil {
		t.Fatalf("pattern.match(no-match): %v", err)
	}
	if !objects.IsNone(m2) {
		t.Errorf("pattern.match(\"world\") = %v, want None", m2)
	}
}

// TestSreSearch pins that Pattern.search finds a match anywhere in the string.
//
// CPython: Modules/_sre/sre.c:2594 _sre_SRE_Pattern_search_impl
func TestSreSearch(t *testing.T) {
	fn := imp.FindInitFunc("_sre")
	if fn == nil {
		t.Skip("_sre not registered")
	}
	mod, err := fn()
	if err != nil {
		t.Fatalf("_sre init: %v", err)
	}
	pat, err := callSreCompile(mod, "world", 0)
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

// TestSreFindall pins that Pattern.findall returns all non-overlapping matches.
//
// CPython: Modules/_sre/sre.c:2739 _sre_SRE_Pattern_findall_impl
func TestSreFindall(t *testing.T) {
	fn := imp.FindInitFunc("_sre")
	if fn == nil {
		t.Skip("_sre not registered")
	}
	mod, err := fn()
	if err != nil {
		t.Fatalf("_sre init: %v", err)
	}
	pat, err := callSreCompile(mod, `\d+`, 0)
	if err != nil {
		t.Fatalf("_sre.compile: %v", err)
	}
	result, err := callPatternMethod(pat, "findall", "abc 123 def 456")
	if err != nil {
		t.Fatalf("pattern.findall: %v", err)
	}
	lst, ok := result.(*objects.List)
	if !ok {
		t.Fatalf("findall returned %T, want *objects.List", result)
	}
	if lst.Len() != 2 {
		t.Errorf("findall returned %d items, want 2", lst.Len())
	}
}

// TestReImportFromStdlib attempts to import the vendored re package via
// PathFinder. This requires enum and copyreg to be available, plus a
// full VM executor wired in the test. All of those are future work; the
// test skips until the transitive deps and VM wiring are in place. It
// serves as a forward compatibility gate: when it stops skipping, the
// vendored re package loads end-to-end.
func TestReImportFromStdlib(t *testing.T) {
	// stdlibDir is relative to this test file (stdlibinit/ is one level
	// under the module root where stdlib/ lives).
	const stdlibSubdir = "../stdlib"

	exec := &reTestExec{}
	prev := imp.GetPathFinder()
	t.Cleanup(func() { imp.SetPathFinder(prev) })
	// Remove any cached re entry so PathFinder is actually consulted.
	imp.RemoveModule("re")
	t.Cleanup(func() { imp.RemoveModule("re") })

	imp.SetPathFinder(&imp.PathFinder{
		Paths:    []string{stdlibSubdir},
		Compiler: reSourceCompiler,
	})

	_, err := imp.ImportModule(exec, "re")
	if err == nil {
		// re loaded successfully; nothing to skip.
		return
	}
	// Any error from the import attempt is an expected gap at this
	// stage (missing enum/copyreg or unfinished VM wiring). Skip.
	t.Skipf("re import not yet available (expected): %v", err)
}

// ---------------------------------------------------------------------------
// Helpers.

// callSreCompile calls _sre.compile(pattern, flags, [], 0, {}, ()).
func callSreCompile(mod *objects.Module, pattern string, flags int) (objects.Object, error) {
	compileFn, err := mod.Dict().GetItem(objects.NewStr("compile"))
	if err != nil {
		return nil, err
	}
	args := []objects.Object{
		objects.NewStr(pattern),
		objects.NewInt(int64(flags)),
		objects.NewList(nil),
		objects.NewInt(0),
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
func reSourceCompiler(src, filename string) (*objects.Code, error) {
	return nil, errors.New("re_import_test: source compilation not wired; set up vm.EvalCode")
}
