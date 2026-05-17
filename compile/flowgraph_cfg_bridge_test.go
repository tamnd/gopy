package compile

import (
	"testing"

	"github.com/tamnd/gopy/ast"
)

// addInstr is a tiny helper for hand-building sequences in these
// tests. Mirrors the codegen-side Sequence.Addop but lets the test
// directly populate handler info too.
func bridgeAddInstr(seq *Sequence, op Opcode, oparg int32, lineno int) {
	seq.Instrs = append(seq.Instrs, Instr{
		Op:      op,
		Oparg:   oparg,
		Loc:     ast.Pos{Lineno: lineno},
		Handler: ExceptHandlerInfo{Label: -1},
	})
}

func TestCfgFromSequenceLinearProgram(t *testing.T) {
	// LOAD_CONST 0; RETURN_VALUE  (no jumps)
	seq := &Sequence{}
	bridgeAddInstr(seq, LOAD_CONST, 0, 1)
	bridgeAddInstr(seq, RETURN_VALUE, 0, 1)

	g := cfgFromSequence(seq)
	if g.EntryBlock == nil {
		t.Fatal("entry block nil")
	}
	if got := len(g.EntryBlock.Instr); got != 2 {
		t.Fatalf("entry block instr count = %d, want 2", got)
	}
	if g.EntryBlock.Next != nil {
		t.Error("linear program should have only one block")
	}
}

func TestCfgFromSequenceSplitsAtJumpTarget(t *testing.T) {
	// JUMP 2; NOP; LOAD_CONST 0; RETURN_VALUE
	// Index 2 (LOAD_CONST) is the jump target -> block boundary.
	seq := &Sequence{}
	bridgeAddInstr(seq, JUMP, 2, 1)
	bridgeAddInstr(seq, NOP, 0, 1)
	bridgeAddInstr(seq, LOAD_CONST, 0, 1)
	bridgeAddInstr(seq, RETURN_VALUE, 0, 1)

	g := cfgFromSequence(seq)
	blocks := g.blocks()
	if len(blocks) < 2 {
		t.Fatalf("blocks = %d, want >= 2 (split at jump target)", len(blocks))
	}

	// Find the JUMP instruction and verify its Target points at the
	// block whose first instruction is LOAD_CONST.
	jump := &g.EntryBlock.Instr[0]
	if jump.Op != JUMP {
		t.Fatalf("entry[0] = %v, want JUMP", jump.Op)
	}
	if jump.Target == nil {
		t.Fatal("JUMP target pointer not rewritten")
	}
	if jump.Target.Instr[0].Op != LOAD_CONST {
		t.Errorf("JUMP target first instr = %v, want LOAD_CONST", jump.Target.Instr[0].Op)
	}
}

func TestCfgFromSequencePreservesLineno(t *testing.T) {
	seq := &Sequence{}
	bridgeAddInstr(seq, LOAD_CONST, 0, 7)
	bridgeAddInstr(seq, RETURN_VALUE, 0, 8)

	g := cfgFromSequence(seq)
	if g.EntryBlock.Instr[0].Loc.Lineno != 7 {
		t.Errorf("instr[0] lineno = %d, want 7", g.EntryBlock.Instr[0].Loc.Lineno)
	}
	if g.EntryBlock.Instr[1].Loc.Lineno != 8 {
		t.Errorf("instr[1] lineno = %d, want 8", g.EntryBlock.Instr[1].Loc.Lineno)
	}
}

func TestCfgFromSequenceEmptySequenceReturnsEntryOnly(t *testing.T) {
	seq := &Sequence{}
	g := cfgFromSequence(seq)
	if g.EntryBlock == nil {
		t.Fatal("entry block nil")
	}
	if len(g.EntryBlock.Instr) != 0 {
		t.Errorf("empty seq should produce empty entry block, got %d instrs", len(g.EntryBlock.Instr))
	}
}

func TestCfgFromSequenceBackwardJump(t *testing.T) {
	// LOAD_CONST 0; JUMP 0
	// JUMP target is index 0 (entry block start).
	seq := &Sequence{}
	bridgeAddInstr(seq, LOAD_CONST, 0, 1)
	bridgeAddInstr(seq, JUMP, 0, 1)

	g := cfgFromSequence(seq)
	// Entry block holds LOAD_CONST + JUMP, JUMP targets entry itself.
	jump := &g.EntryBlock.Instr[1]
	if jump.Op != JUMP {
		t.Fatalf("instr[1] = %v, want JUMP", jump.Op)
	}
	if jump.Target != g.EntryBlock {
		t.Error("backward JUMP should target entry block")
	}
}

func TestCfgToSequenceLinearRoundtrip(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	g.addOp(LOAD_CONST, 0, loc)
	g.addOp(RETURN_VALUE, 0, loc)
	seq := &Sequence{}
	cfgToSequence(g, seq)
	if len(seq.Instrs) != 2 {
		t.Fatalf("got %d instrs, want 2", len(seq.Instrs))
	}
	if seq.Instrs[0].Op != LOAD_CONST || seq.Instrs[1].Op != RETURN_VALUE {
		t.Fatalf("ops = %s %s", seq.Instrs[0].Op.Name(), seq.Instrs[1].Op.Name())
	}
}

func TestCfgToSequenceResolvesJumpTargets(t *testing.T) {
	var loc ast.Pos
	g := newCfgBuilder()
	tgt := g.newBlock()
	g.addOp(LOAD_CONST, 0, loc)
	g.CurBlock.addJump(JUMP, tgt, loc)
	g.useNextBlock(tgt)
	g.addOp(RETURN_VALUE, 0, loc)

	seq := &Sequence{}
	cfgToSequence(g, seq)
	// JUMP at index 1 should point at the RETURN_VALUE at index 2.
	if seq.Instrs[1].Op != JUMP {
		t.Fatalf("instr[1] = %s, want JUMP", seq.Instrs[1].Op.Name())
	}
	if got := int(seq.Instrs[1].Oparg); got != 2 {
		t.Fatalf("JUMP oparg = %d, want 2 (offset of RETURN_VALUE)", got)
	}
}

func TestCfgRoundtripThroughBridge(t *testing.T) {
	// Build a sequence with a forward JUMP, round-trip it through the
	// graph and back, expect identical opcode/oparg layout.
	seq := &Sequence{}
	bridgeAddInstr(seq, JUMP, 2, 1)
	bridgeAddInstr(seq, NOP, 0, 1)
	bridgeAddInstr(seq, LOAD_CONST, 0, 1)
	bridgeAddInstr(seq, RETURN_VALUE, 0, 1)

	g := cfgFromSequence(seq)
	out := &Sequence{}
	cfgToSequence(g, out)

	if len(out.Instrs) != len(seq.Instrs) {
		t.Fatalf("len = %d, want %d", len(out.Instrs), len(seq.Instrs))
	}
	for i := range out.Instrs {
		if out.Instrs[i].Op != seq.Instrs[i].Op {
			t.Errorf("instr[%d] op = %s, want %s", i, out.Instrs[i].Op.Name(), seq.Instrs[i].Op.Name())
		}
	}
	if out.Instrs[0].Oparg != 2 {
		t.Fatalf("JUMP oparg = %d, want 2 after roundtrip", out.Instrs[0].Oparg)
	}
}
