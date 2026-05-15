// Package compile holds the AST-to-bytecode pipeline. Mirrors
// cpython/Python/instruction_sequence.c, codegen.c, flowgraph.c,
// assemble.c, and compile.c.
package compile

import "github.com/tamnd/gopy/ast"

// MaxOpcode mirrors MAX_OPCODE in instruction_sequence.c.
//
// CPython: Python/instruction_sequence.c:113 MAX_OPCODE
const MaxOpcode = 511

// MaxOparg is 1<<30. CPython asserts oparg < (1<<30) inside Addop.
//
// CPython: Python/instruction_sequence.c:122 _PyInstructionSequence_Addop
const MaxOparg = 1 << 30

// MIN_SPECIALIZED_OPCODE is the lowest opcode value reserved for a
// specialized variant (LOAD_ATTR_INSTANCE_VALUE and friends).
// Anything below it is either an unspecialized opcode or a CPython
// reserved slot. MIN_INSTRUMENTED_OPCODE is the threshold at which
// the instrumentation shadow opcodes start; the specializer's
// set_opcode helper bails out when the slot already lives in that
// range to avoid stomping an instrumented instruction.
//
// CPython: Include/opcode_ids.h:253 MIN_SPECIALIZED_OPCODE
// CPython: Include/opcode_ids.h:254 MIN_INSTRUMENTED_OPCODE
const (
	MIN_SPECIALIZED_OPCODE  Opcode = 129
	MIN_INSTRUMENTED_OPCODE Opcode = 234
)

// Opcode is a single bytecode opcode. The numeric values match
// cpython/Lib/opcode.py and are filled in by compile/opcodes_gen.go.
type Opcode int32

// JumpTargetLabel is an opaque label id created by NewLabel and bound
// to an instruction position by UseLabel. Mirrors _PyJumpTargetLabel.
//
// CPython: Include/internal/pycore_compile.h _PyJumpTargetLabel
type JumpTargetLabel struct{ id int }

// ID returns the underlying label id. CPython exposes the same
// integer to Python via `InstructionSequence.new_label`.
//
// CPython: Include/internal/pycore_compile.h _PyJumpTargetLabel.id
func (l JumpTargetLabel) ID() int { return l.id }

// ExceptHandlerInfo is the per-instruction exception handler slot.
// h_label < 0 means "no handler". Mirrors _PyExceptHandlerInfo.
//
// CPython: Include/internal/pycore_compile.h _PyExceptHandlerInfo
type ExceptHandlerInfo struct {
	Label         int
	StartDepth    int
	PreserveLasti int
}

// Instr is one entry in a Sequence. Mirrors _PyInstruction.
//
// CPython: Include/internal/pycore_compile.h _PyInstruction
type Instr struct {
	Op      Opcode
	Oparg   int32
	Loc     ast.Pos
	Handler ExceptHandlerInfo
}

// Sequence is the pre-CFG instruction stream emitted by codegen and
// consumed by flowgraph. Mirrors _PyInstructionSequence.
//
// CPython: Include/internal/pycore_compile.h _PyInstructionSequence
type Sequence struct {
	Instrs   []Instr
	Nested   []*Sequence
	AnnoCode *Sequence

	// labelmap[id-1] is the instruction index labeled by id, or
	// labelUnbound if no UseLabel call has bound it yet. Cleared by
	// ApplyLabelMap. id 0 is reserved (NewLabel returns ids >= 1).
	labelmap      []int
	nextFreeLabel int
}

// labelUnbound is the sentinel value CPython spells -111 in
// instruction_sequence.c (left in s_labelmap for unbound slots until
// UseLabel binds them).
//
// CPython: Python/instruction_sequence.c:80 _PyInstructionSequence_UseLabel
const labelUnbound = -111

// NewLabel allocates a fresh label id (1-based, matching CPython's
// post-increment of s_next_free_label).
//
// CPython: Python/instruction_sequence.c:58 _PyInstructionSequence_NewLabel
func (s *Sequence) NewLabel() JumpTargetLabel {
	s.nextFreeLabel++
	return JumpTargetLabel{id: s.nextFreeLabel}
}

// UseLabel binds lbl to the position of the next instruction to be
// appended. Calling UseLabel with the same label twice rebinds it
// (mirrors CPython, which simply overwrites s_labelmap[lbl]).
//
// CPython: Python/instruction_sequence.c:65 _PyInstructionSequence_UseLabel
func (s *Sequence) UseLabel(lbl JumpTargetLabel) {
	s.ensureLabelmap(lbl.id)
	s.labelmap[lbl.id] = len(s.Instrs)
}

// ensureLabelmap grows s.labelmap so that lbl is a valid index. New
// slots are filled with labelUnbound. Index 0 is reserved.
//
// CPython: Python/instruction_sequence.c:65 _PyInstructionSequence_UseLabel
// (the s_labelmap grow loop inside).
func (s *Sequence) ensureLabelmap(lbl int) {
	for len(s.labelmap) <= lbl {
		s.labelmap = append(s.labelmap, labelUnbound)
	}
}

// Addop appends an instruction. Caller is responsible for ensuring
// op is in [0, MaxOpcode] and oparg in [0, MaxOparg). The C code
// asserts these; here a panic via slice growth would mask the bug,
// so we panic explicitly to match the assertion semantics.
//
// CPython: Python/instruction_sequence.c:116 _PyInstructionSequence_Addop
func (s *Sequence) Addop(op Opcode, oparg int32, loc ast.Pos) {
	if op < 0 || op > MaxOpcode {
		panic("compile: opcode out of range")
	}
	if oparg < 0 || oparg >= MaxOparg {
		panic("compile: oparg out of range")
	}
	s.Instrs = append(s.Instrs, Instr{
		Op:      op,
		Oparg:   oparg,
		Loc:     loc,
		Handler: ExceptHandlerInfo{Label: -1},
	})
}

// Insert inserts at pos and shifts following entries right by one.
// Any previously-bound label that pointed at pos or later is bumped
// up by one to preserve its target.
//
// CPython: Python/instruction_sequence.c:134 _PyInstructionSequence_InsertInstruction
func (s *Sequence) Insert(pos int, op Opcode, oparg int32, loc ast.Pos) {
	if pos < 0 || pos > len(s.Instrs) {
		panic("compile: insert position out of range")
	}
	in := Instr{
		Op:      op,
		Oparg:   oparg,
		Loc:     loc,
		Handler: ExceptHandlerInfo{Label: -1},
	}
	s.Instrs = append(s.Instrs, Instr{})
	copy(s.Instrs[pos+1:], s.Instrs[pos:])
	s.Instrs[pos] = in
	for i, off := range s.labelmap {
		if i == 0 || off == labelUnbound {
			continue
		}
		if off >= pos {
			s.labelmap[i] = off + 1
		}
	}
}

// AddNested appends a nested instruction sequence (one per nested
// scope: a function or class definition emits its own Sequence and
// hangs it off the parent here).
//
// CPython: Python/instruction_sequence.c:167 _PyInstructionSequence_AddNested
func (s *Sequence) AddNested(child *Sequence) {
	s.Nested = append(s.Nested, child)
}

// SetAnnotationsCode mirrors _PyInstructionSequence_SetAnnotationsCode.
// CPython asserts s_annotations_code is unset; we do the same.
//
// CPython: Python/instruction_sequence.c:158 _PyInstructionSequence_SetAnnotationsCode
func (s *Sequence) SetAnnotationsCode(annot *Sequence) {
	if s.AnnoCode != nil {
		panic("compile: annotations code already set")
	}
	s.AnnoCode = annot
}

// ApplyLabelMap rewrites every jump opcode's oparg from a label id
// into the bound instruction offset, using hasTarget to recognize
// which opcodes carry a jump target. Callers pass the OPCODE_HAS_TARGET
// predicate (defined in the generated opcode metadata, which lands
// alongside the opcode generator).
//
// Idempotent: a second call is a no-op (s.labelmap == nil). The
// per-instruction ExceptHandlerInfo.Label is also resolved.
//
// CPython: Python/instruction_sequence.c:87 _PyInstructionSequence_ApplyLabelMap
func (s *Sequence) ApplyLabelMap(hasTarget func(Opcode) bool) {
	if s.labelmap == nil {
		return
	}
	for i := range s.Instrs {
		instr := &s.Instrs[i]
		if hasTarget(instr.Op) {
			instr.Oparg = int32(s.labelmap[instr.Oparg])
		}
		if instr.Handler.Label >= 0 {
			instr.Handler.Label = s.labelmap[instr.Handler.Label]
		}
	}
	s.labelmap = nil
}
