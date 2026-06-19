package compile

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestIfNoElseEmitsPopJumpIfFalse(t *testing.T) {
	// if x: pass
	body := &ast.If{
		Test: nameLoad("x"),
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",         // x
		"TO_BOOL",           // normalize for conditional jump
		"POP_JUMP_IF_FALSE", // skip body
		"NOP",               // pass
		"LOAD_CONST",        // implicit None
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestIfWithElseEmitsJumpAroundElse(t *testing.T) {
	// if x: pass
	// else: pass
	body := &ast.If{
		Test:   nameLoad("x"),
		Body:   []ast.Stmt{&ast.Pass{}},
		Orelse: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",
		"TO_BOOL",
		"POP_JUMP_IF_FALSE", // -> else
		"NOP",
		"JUMP", // -> end (skip else)
		"NOP",  // else body
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestWhileEmitsLoop(t *testing.T) {
	// while x: pass
	body := &ast.While{
		Test: nameLoad("x"),
		Body: []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",
		"TO_BOOL",
		"POP_JUMP_IF_FALSE", // -> end
		"NOP",
		"JUMP", // -> top
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestForEmitsGetIterAndForIter(t *testing.T) {
	// for i in xs: pass
	body := &ast.For{
		Target: nameStore("i"),
		Iter:   nameLoad("xs"),
		Body:   []ast.Stmt{&ast.Pass{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME", // xs
		"GET_ITER",
		"FOR_ITER",   // -> cleanup
		"NOP",        // line-tracing NOP at the target (codegen_for)
		"STORE_NAME", // i (module scope)
		"NOP",        // pass
		"JUMP",       // -> for_iter
		"END_FOR",
		"POP_ITER",
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestBreakInLoopEmitsJumpToExit(t *testing.T) {
	// while x: break
	body := &ast.While{
		Test: nameLoad("x"),
		Body: []ast.Stmt{&ast.Break{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	want := []string{
		"LOAD_NAME",
		"TO_BOOL",
		"POP_JUMP_IF_FALSE",
		"JUMP", // break -> end
		"JUMP", // bottom-of-body -> top
		"LOAD_CONST",
		"RETURN_VALUE",
	}
	if !equalStrings(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestContinueInLoopEmitsJumpToTop(t *testing.T) {
	// while x: continue
	body := &ast.While{
		Test: nameLoad("x"),
		Body: []ast.Stmt{&ast.Continue{}},
	}
	u := compileMod(t, module(body))
	got := opNames(u)
	// Two JUMPs: continue -> loop top, then bottom-of-body -> loop top.
	if len(got) < 4 {
		t.Fatalf("ops = %v", got)
	}
	if got[0] != "LOAD_NAME" || got[1] != "TO_BOOL" || got[2] != "POP_JUMP_IF_FALSE" {
		t.Errorf("expected LOAD_NAME / TO_BOOL / POP_JUMP_IF_FALSE prefix, got %v", got[:3])
	}
	if got[3] != "JUMP" || got[4] != "JUMP" {
		t.Errorf("expected two JUMPs in body, got %v", got[3:5])
	}
}

func TestBreakOutsideLoopErrors(t *testing.T) {
	st := mustBuildSym(t, module(&ast.Break{}))
	c := NewCompiler("<t>", 0, nil, st)
	_, err := c.Codegen(st.Top, module(&ast.Break{}))
	if err == nil || !strings.Contains(err.Error(), "'break' outside loop") {
		t.Errorf("want break-outside-loop error, got %v", err)
	}
}

func TestContinueOutsideLoopErrors(t *testing.T) {
	st := mustBuildSym(t, module(&ast.Continue{}))
	c := NewCompiler("<t>", 0, nil, st)
	_, err := c.Codegen(st.Top, module(&ast.Continue{}))
	if err == nil || !strings.Contains(err.Error(), "'continue' not properly in loop") {
		t.Errorf("want continue-outside-loop error, got %v", err)
	}
}
