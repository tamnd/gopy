package compile

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/ast"
)

func TestDumpCfgRendersHeaderAndBlocks(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1, ColOffset: 0, EndLineno: 1, EndColOffset: 4}
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)

	got := DumpCfg(g, CfgDumpHeader{
		Phase:       "test",
		FirstLineno: 1,
		NLocals:     0,
		NParams:     0,
		CodeFlags:   0x42,
	})
	want := `# cfg dump: test
firstlineno: 1
nlocals: 0
nparams: 0
codeflags: 0x42

block 0 (label=0 preds=0 startdepth=0 warm=false cold=false):
  0 LOAD_CONST(0) loc=1:0-1:4
  1 RETURN_VALUE(0) loc=1:0-1:4
  -> next=nil
`
	if got != want {
		t.Fatalf("DumpCfg mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestDumpCfgResolvesJumpTargetsAsBlockIndices(t *testing.T) {
	g := newCfgBuilder()
	loc := ast.Pos{Lineno: 1}
	g.addOp(LOAD_CONST, 0, loc)
	// Force a fresh block via a label.
	lbl := JumpTargetLabel{id: 1}
	g.useLabel(lbl)
	g.addOp(RETURN_VALUE, 0, loc)

	target := g.EntryBlock.Next
	jump := &cfgInstr{Op: JUMP_FORWARD, Oparg: 0, Loc: loc, Target: target}
	g.EntryBlock.Instr = append(g.EntryBlock.Instr, *jump)

	dump := DumpCfg(g, CfgDumpHeader{Phase: "jumps"})
	if !strings.Contains(dump, "target=block1") {
		t.Fatalf("expected jump target rendered as block1, got:\n%s", dump)
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
	a := DumpCfg(build(), CfgDumpHeader{Phase: "p"})
	b := DumpCfg(build(), CfgDumpHeader{Phase: "p"})
	if a != b {
		t.Fatalf("DumpCfg is non-deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
