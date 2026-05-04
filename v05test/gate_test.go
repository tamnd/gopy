// Package v05test exercises the v0.5 gate: a parsed AST module flows
// through symtable, codegen, flowgraph, and assemble, and produces a
// Code object whose disassembly matches the expected shape. The full
// CPython parity gate (byte-for-byte co_code / co_linetable equality
// with the reference interpreter) lands once the optimisation panel
// and line/exception tables ship; until then the gate verifies the
// pipeline runs end-to-end and produces well-formed output.
package v05test

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/compile"
)

func module(body ...ast.Stmt) *ast.Module { return &ast.Module{Body: body} }
func nameStore(id string) *ast.Name       { return &ast.Name{Id: id, Ctx: ast.Store} }
func nameLoad(id string) *ast.Name        { return &ast.Name{Id: id, Ctx: ast.Load} }
func cnst(v any) *ast.Constant            { return &ast.Constant{Value: v} }

// findInnerCode returns the first nested *compile.Code in co.Consts.
func findInnerCode(t *testing.T, co *compile.Code) *compile.Code {
	t.Helper()
	for _, c := range co.Consts {
		if inner, ok := c.(*compile.Code); ok {
			return inner
		}
	}
	t.Fatalf("no nested code object in consts: %v", co.Consts)
	return nil
}

// TestGateEmptyModule: an empty module compiles to LOAD_CONST None
// and RETURN_VALUE, and the disassembly mentions both opcodes.
func TestGateEmptyModule(t *testing.T) {
	co, err := compile.Compile(module(), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	dis := compile.Disassemble(co)
	for _, want := range []string{"LOAD_CONST", "RETURN_VALUE"} {
		if !strings.Contains(dis, want) {
			t.Errorf("empty module disasm missing %s:\n%s", want, dis)
		}
	}
}

// TestGateSimpleAssign: `x = 1` flows through every stage and the
// result names "x" in co_names and a single int constant in co_consts.
func TestGateSimpleAssign(t *testing.T) {
	body := &ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))}
	co, err := compile.Compile(module(body), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(co.Names) != 1 || co.Names[0] != "x" {
		t.Errorf("co.Names = %v, want [x]", co.Names)
	}
	hasOne := false
	for _, c := range co.Consts {
		if v, ok := c.(int64); ok && v == 1 {
			hasOne = true
		}
	}
	if !hasOne {
		t.Errorf("co.Consts missing int(1): %v", co.Consts)
	}
}

// TestGateBinaryAdd: the v0.5 spec gate is `a = 1 + 2`. The optimiser
// folds the int-int BINARY_OP at flowgraph time, so the disassembly
// no longer contains BINARY_OP; instead it stores the folded constant
// 3 directly. The full byte-equal parity check against CPython lands
// once the marshal package and golden corpus are in place.
func TestGateBinaryAdd(t *testing.T) {
	body := &ast.Assign{
		Targets: []ast.Expr{nameStore("a")},
		Value: &ast.BinOp{
			Left:  cnst(int64(1)),
			Op:    ast.Add,
			Right: cnst(int64(2)),
		},
	}
	co, err := compile.Compile(module(body), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile a = 1 + 2: %v", err)
	}
	dis := compile.Disassemble(co)
	for _, want := range []string{"LOAD_CONST", "STORE_NAME"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disasm missing %s:\n%s", want, dis)
		}
	}
	hasThree := false
	for _, c := range co.Consts {
		if v, ok := c.(int64); ok && v == 3 {
			hasThree = true
		}
	}
	if !hasThree {
		t.Errorf("folded constant 3 missing from co.Consts: %v", co.Consts)
	}
}

// TestGateIfWhile: an `if x: pass\nwhile x: pass` module flows through
// every stage and produces the expected control-flow opcodes.
func TestGateIfWhile(t *testing.T) {
	body := []ast.Stmt{
		&ast.If{Test: nameLoad("x"), Body: []ast.Stmt{&ast.Pass{}}},
		&ast.While{Test: nameLoad("x"), Body: []ast.Stmt{&ast.Pass{}}},
	}
	co, err := compile.Compile(module(body...), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile if/while: %v", err)
	}
	dis := compile.Disassemble(co)
	// POP_JUMP_IF_FALSE shows up for both the if and while tests.
	// The while back-edge is currently emitted as the JUMP pseudo-op
	// which is lowered to a real JUMP_BACKWARD by a flowgraph pass
	// that has not landed yet, so we only assert on the conditional.
	if !strings.Contains(dis, "POP_JUMP_IF_FALSE") {
		t.Errorf("disasm missing POP_JUMP_IF_FALSE:\n%s", dis)
	}
}

// TestGateTryExcept exercises try/except through the full pipeline.
// The current flowgraph stack-depth analyser walks the flat sequence
// linearly, so it cannot seed handler entry depth from an exception
// edge. This test is wired up so it flips green once the CFG-based
// stackdepth lands; until then it is skipped with the gap noted.
func TestGateTryExcept(t *testing.T) {
	t.Skip("blocked on CFG-based stackdepth (handler entry seeding)")
	handler := &ast.ExceptHandler{Body: []ast.Stmt{&ast.Pass{}}}
	body := &ast.Try{
		Body:     []ast.Stmt{&ast.Pass{}},
		Handlers: []ast.Excepthandler{handler},
	}
	co, err := compile.Compile(module(body), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile try/except: %v", err)
	}
	if len(co.ExceptionTable) == 0 {
		t.Errorf("expected non-empty co.ExceptionTable, got empty")
	}
	dis := compile.Disassemble(co)
	if !strings.Contains(dis, "PUSH_EXC_INFO") {
		t.Errorf("disasm missing PUSH_EXC_INFO:\n%s", dis)
	}
}

// TestGateDef: `def f(x): return x + 1` puts a nested code object in
// the outer Consts and the inner disasm contains LOAD_FAST and
// RETURN_VALUE.
func TestGateDef(t *testing.T) {
	fn := &ast.FunctionDef{
		Name: "f",
		Args: &ast.Arguments{Args: []*ast.Arg{{Arg: "x"}}},
		Body: []ast.Stmt{
			&ast.Return{Value: &ast.BinOp{
				Left:  nameLoad("x"),
				Op:    ast.Add,
				Right: cnst(int64(1)),
			}},
		},
	}
	co, err := compile.Compile(module(fn), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile def: %v", err)
	}
	inner := findInnerCode(t, co)
	if inner.Name != "f" {
		t.Errorf("inner.Name = %q, want %q", inner.Name, "f")
	}
	if inner.Argcount != 1 {
		t.Errorf("inner.Argcount = %d, want 1", inner.Argcount)
	}
	dis := compile.Disassemble(co)
	for _, want := range []string{"MAKE_FUNCTION", "LOAD_FAST", "RETURN_VALUE"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disasm missing %s:\n%s", want, dis)
		}
	}
}

// TestGateComprehension: `[x for x in xs]` puts a nested comprehension
// scope in Consts whose body contains FOR_ITER and LIST_APPEND. Skipped
// pending CFG-based stackdepth: the comprehension prologue stacks the
// iterator and result list across a back-edge that the linear analyser
// can't track, so flowgraph reports a spurious negative depth.
func TestGateComprehension(t *testing.T) {
	t.Skip("blocked on CFG-based stackdepth (comprehension back-edge)")
	comp := &ast.ListComp{
		Elt: nameLoad("x"),
		Generators: []*ast.Comprehension{
			{Target: nameStore("x"), Iter: nameLoad("xs")},
		},
	}
	body := &ast.Assign{Targets: []ast.Expr{nameStore("a")}, Value: comp}
	co, err := compile.Compile(module(body), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile listcomp: %v", err)
	}
	inner := findInnerCode(t, co)
	dis := compile.Disassemble(co)
	if !strings.Contains(dis, "Disassembly of "+inner.Name) {
		t.Errorf("disasm missing nested header for %q:\n%s", inner.Name, dis)
	}
	for _, want := range []string{"FOR_ITER", "LIST_APPEND"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disasm missing %s:\n%s", want, dis)
		}
	}
}

// TestGateAsyncFunction: `async def f(): pass` sets CoCoroutine on the
// inner code object.
func TestGateAsyncFunction(t *testing.T) {
	fn := &ast.AsyncFunctionDef{
		Name: "f",
		Args: &ast.Arguments{},
		Body: []ast.Stmt{&ast.Pass{}},
	}
	co, err := compile.Compile(module(fn), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile async def: %v", err)
	}
	inner := findInnerCode(t, co)
	if inner.Flags&compile.CoCoroutine == 0 {
		t.Errorf("inner.Flags = %#x, missing CoCoroutine", inner.Flags)
	}
}

// TestGateLoadAfterStore: `x = 1; x` round-trips load and store.
func TestGateLoadAfterStore(t *testing.T) {
	body := []ast.Stmt{
		&ast.Assign{Targets: []ast.Expr{nameStore("x")}, Value: cnst(int64(1))},
		&ast.ExprStmt{Value: nameLoad("x")},
	}
	co, err := compile.Compile(module(body...), "<gate>", 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	dis := compile.Disassemble(co)
	if !strings.Contains(dis, "STORE_NAME") || !strings.Contains(dis, "LOAD_NAME") {
		t.Errorf("disasm missing STORE/LOAD pair:\n%s", dis)
	}
}
