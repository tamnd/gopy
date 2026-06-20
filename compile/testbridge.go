// Bridge entry points for the _testinternalcapi compiler-pipeline
// helpers (new_instruction_sequence, compiler_codegen, optimize_cfg,
// assemble_code_object). They expose the codegen / cfg / assemble
// passes to module/_testinternalcapi so the vendored test harness in
// test/support/bytecode_helper.py and the test_peepholer /
// test_compiler_assemble suites can drive each stage directly.
//
// CPython: Modules/_testinternalcapi.c:713 new_instruction_sequence ff.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/future"
	"github.com/tamnd/gopy/symtable"
)

// NewSequence allocates an empty instruction sequence. Mirrors
// _PyInstructionSequence_New, which _testinternalcapi exposes as
// new_instruction_sequence.
//
// CPython: Python/instruction_sequence.c:38 _PyInstructionSequence_New
func NewSequence() *Sequence {
	return &Sequence{}
}

// AddopRaw appends an instruction with the location stored verbatim,
// bypassing normalizeLoc. The InstructionSequence Python object hands
// out whatever six-tuple round-trips through get_instructions, so a
// fully-zero (1,0,0,0)-style span must survive unchanged rather than
// collapse onto NO_LOCATION.
//
// CPython: Python/instruction_sequence.c:116 _PyInstructionSequence_Addop
func (s *Sequence) AddopRaw(op Opcode, oparg int32, lineno, colOffset, endLineno, endColOffset int) error {
	if op < 0 || op > MaxOpcode {
		return fmt.Errorf("compile: opcode out of range")
	}
	if oparg < 0 || oparg >= MaxOparg {
		return fmt.Errorf("compile: oparg out of range")
	}
	s.Instrs = append(s.Instrs, Instr{
		Op:    op,
		Oparg: oparg,
		Loc: ast.Pos{
			Lineno:       lineno,
			ColOffset:    colOffset,
			EndLineno:    endLineno,
			EndColOffset: endColOffset,
		},
		Handler: ExceptHandlerInfo{Label: -1},
	})
	return nil
}

// NewLabelID allocates a fresh label and returns its integer id, the
// value InstructionSequence.new_label hands to Python.
//
// CPython: Python/instruction_sequence.c:58 _PyInstructionSequence_NewLabel
func (s *Sequence) NewLabelID() int {
	return s.NewLabel().ID()
}

// UseLabelID binds the label id to the next instruction position.
//
// CPython: Python/instruction_sequence.c:65 _PyInstructionSequence_UseLabel
func (s *Sequence) UseLabelID(id int) {
	s.UseLabel(JumpTargetLabel{id: id})
}

// ApplyJumpLabels resolves every jump oparg from a label id into the
// bound instruction offset. Idempotent (see ApplyLabelMap).
//
// CPython: Python/instruction_sequence.c get_instructions calls
// instr_sequence_apply_jumps before building the tuples.
func (s *Sequence) ApplyJumpLabels() {
	s.ApplyLabelMap(hasJumpTarget)
}

// CodeGenForTest runs the front-end (future + preprocess + symtable +
// Codegen) and returns the post-codegen Unit, the same value
// _PyCompile_CodeGen builds before handing back (instr_seq, metadata).
// saveNestedSeqs is set so the returned Unit's Seq carries the nested
// child sequences get_nested walks. gopy's Codegen already emits the
// trailing implicit return, so we do NOT call AddReturnAtEnd again.
//
// CPython: Python/compile.c:1607 _PyCompile_CodeGen
func CodeGenForTest(mod ast.Mod, filename string, optimize int) (*Unit, error) {
	ff, err := future.FromAST(mod, filename)
	if err != nil {
		return nil, err
	}
	ast.Preprocess(mod, ast.PreprocessOptions{
		Filename:       filename,
		OptimizeLevel:  optimize,
		FFFeatures:     ff.Bits,
		EnableWarnings: true,
	})
	st, err := symtable.Build(mod, filename, ff)
	if err != nil {
		return nil, err
	}
	c := NewCompiler(filename, optimize, ff, st)
	c.saveNestedSeqs = true
	scope := c.Symtable.Top
	if scope == nil {
		return nil, fmt.Errorf("compile: symtable has no top entry")
	}
	return c.Codegen(scope, mod)
}

// OptimizeCfgForTest mirrors _PyCompile_OptimizeCfg: build a cfg from
// seq, run the optimize pass with nparams=0 firstlineno=1 (the
// optimize_cfg entry's fixed arguments), recompute stack depth,
// optimize LOAD_FAST, and convert back to an instruction sequence.
// The consts slice is mutated in place so the caller can read the
// optimized const table back out.
//
// CPython: Python/flowgraph.c:4126 _PyCompile_OptimizeCfg
func OptimizeCfgForTest(seq *Sequence, consts *[]any, nlocals int) (*Sequence, error) {
	g := cfgFromSequence(seq)
	if err := cfgOptimizeCodeUnit(g, consts, nlocals, 0, 1); err != nil {
		return nil, err
	}
	if _, err := cfgCalculateStackdepth(g); err != nil {
		return nil, err
	}
	if err := optimizeLoadFast(g); err != nil {
		return nil, err
	}
	out := &Sequence{}
	cfgToSequence(g, out)
	return out, nil
}

// AssembleForTest mirrors _PyCompile_Assemble: build a cfg from seq,
// translate jump labels to targets, convert the optimized cfg back
// into an instruction sequence (recomputing stack depth and the
// localsplus layout), and assemble the code object.
//
// CPython: Python/compile.c:1657 _PyCompile_Assemble
func AssembleForTest(unit *Unit, seq *Sequence, filename string) (*Code, error) {
	g := cfgFromSequence(seq)
	cfgTranslateJumpLabelsToTargets(g)
	optimized := &Sequence{}
	stackdepth, nlocalsplus, err := cfgOptimizedCfgToInstructionSequence(g, unit, 0, optimized)
	if err != nil {
		return nil, err
	}
	return assembleMakeCodeObject(unit, unit.Consts, stackdepth, optimized, nlocalsplus, 0, filename), nil
}
