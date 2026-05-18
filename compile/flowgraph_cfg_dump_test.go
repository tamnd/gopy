package compile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestDumpCfgRendersBlocksCPythonFormat(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1, ColOffset: 0, EndLineno: 1, EndColOffset: 4}
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)

	got := DumpCfg(g)
	want := fmt.Sprintf(
		"B0: [EH=0 CLD=0 WRM=0 NO_FT=1] used: 2, depth: 0, preds: 0 return \n"+
			"  [00] line: 1, LOAD_CONST (%d) arg: 0 \n"+
			"  [01] line: 1, RETURN_VALUE (%d) \n",
		int(LOAD_CONST), int(RETURN_VALUE))
	if got != want {
		t.Fatalf("DumpCfg mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDumpCfgResolvesJumpTargetsAsBlockLabels(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	lbl := JumpTargetLabel{id: 1}
	g.useLabel(lbl)
	g.addOp(RETURN_VALUE, 0, loc)

	target := g.EntryBlock.Next
	g.EntryBlock.Instr = append(g.EntryBlock.Instr, cfgInstr{
		Op: JUMP_FORWARD, Oparg: 0, Loc: loc, Target: target,
	})

	dump := DumpCfg(g)
	if !strings.Contains(dump, "target: B1 [0] jump \n") {
		t.Fatalf("expected jump target rendered as B1 with jump marker, got:\n%s", dump)
	}
}

func TestDumpCfgIsDeterministic(t *testing.T) {
	build := func() *cfgBuilder {
		g := newCfgBuilder()
		loc := ast.Pos{Lineno: 1}
		g.addOp(LOAD_CONST, 0, loc)
		g.useLabel(JumpTargetLabel{id: 1})
		g.addOp(LOAD_CONST, 1, loc)
		g.useLabel(JumpTargetLabel{id: 2})
		g.addOp(RETURN_VALUE, 0, loc)
		return g
	}
	a := DumpCfg(build())
	b := DumpCfg(build())
	if a != b {
		t.Fatalf("DumpCfg is non-deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
