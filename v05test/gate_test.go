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

// TestGateBinaryAdd: the v0.5 spec gate is `a = 1 + 2`. The pipeline
// must compile it; the byte-equal parity comes once the optimiser
// runs constant folding and the line-table emits.
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
	for _, want := range []string{"LOAD_CONST", "BINARY_OP", "STORE_NAME"} {
		if !strings.Contains(dis, want) {
			t.Errorf("disasm missing %s:\n%s", want, dis)
		}
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
