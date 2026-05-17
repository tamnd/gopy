// Sequence-level optimisation passes. CPython runs these on the CFG;
// the v0.5 port keeps them on the flat instruction sequence so the
// rest of the pipeline can stay simple. Each pass is conservative
// enough that running it on a hand-built sequence cannot change
// observable behavior.
//
// CPython: Python/flowgraph.c:L3659 _PyCfg_OptimizeCodeUnit panel

package compile

import "github.com/tamnd/gopy/ast"

// In-place BINARY_OP suboperators not declared in codegen_expr_op.go.
// Values come from CPython's NB_INPLACE_* enum.
//
// CPython: Include/opcode.h NB_INPLACE_ADD etc.
const (
	nbInplAdd int32 = 13
	nbInplAnd int32 = 14
	nbInplLsh int32 = 16
	nbInplMul int32 = 18
	nbInplOr  int32 = 22
	nbInplRsh int32 = 24
	nbInplSub int32 = 23
	nbInplXor int32 = 25
)

// foldBinaryIntConst folds `LOAD_CONST a; LOAD_CONST b; BINARY_OP op`
// triples into a single `LOAD_CONST <result>` when both operands are
// int64 constants and op has a defined integer answer. The two loads
// turn into NOPs and the BINARY_OP becomes the new LOAD_CONST. The
// pass is length-preserving so label targets stay valid; a follow-on
// NOP removal compacts the stream once ApplyLabelMap has rewritten
// jump opargs.
//
// CPython: Python/flowgraph.c:L1894 fold_const_binop
func foldBinaryIntConst(seq *Sequence, consts *[]any) int {
	if consts == nil {
		return 0
	}
	folded := 0
	for i := 0; i+2 < len(seq.Instrs); i++ {
		a := &seq.Instrs[i]
		b := &seq.Instrs[i+1]
		c := &seq.Instrs[i+2]
		if a.Op != LOAD_CONST || b.Op != LOAD_CONST || c.Op != BINARY_OP {
			continue
		}
		ai, bi := int(a.Oparg), int(b.Oparg)
		if ai < 0 || ai >= len(*consts) || bi < 0 || bi >= len(*consts) {
			continue
		}
		x, xok := (*consts)[ai].(int64)
		y, yok := (*consts)[bi].(int64)
		if !xok || !yok {
			continue
		}
		result, ok := evalIntBinop(c.Oparg, x, y)
		if !ok {
			continue
		}
		idx := appendConst(consts, result)
		a.Op = NOP
		a.Oparg = 0
		b.Op = NOP
		b.Oparg = 0
		c.Op = LOAD_CONST
		c.Oparg = int32(idx)
		folded++
		// skip past the rewritten triple
		i += 2
	}
	return folded
}

// evalIntBinop computes the result of x <op> y for integer operands,
// or returns ok=false if the operator is one we do not fold (TRUE_DIVIDE,
// MATRIX_MULTIPLY, FLOOR_DIVIDE on a zero divisor, POWER with a
// negative exponent, etc.).
//
// CPython: Python/flowgraph.c eval_const_binop_int
func evalIntBinop(op int32, x, y int64) (int64, bool) {
	switch op {
	case nbAdd, nbInplAdd:
		return x + y, true
	case nbSubtract, nbInplSub:
		return x - y, true
	case nbMult, nbInplMul:
		return x * y, true
	case nbAnd, nbInplAnd:
		return x & y, true
	case nbOr, nbInplOr:
		return x | y, true
	case nbXor, nbInplXor:
		return x ^ y, true
	case nbLShift, nbInplLsh:
		if y < 0 || y >= 64 {
			return 0, false
		}
		return x << uint(y), true
	case nbRShift, nbInplRsh:
		if y < 0 || y >= 64 {
			return 0, false
		}
		return x >> uint(y), true
	}
	return 0, false
}

// appendConst returns the index of v in *consts, appending if not
// present. Linear search is fine here: the per-unit pool is small
// and flowgraph runs once per scope.
//
// CPython: Python/flowgraph.c add_const
func appendConst(consts *[]any, v any) int {
	for i, c := range *consts {
		if c == v {
			return i
		}
	}
	*consts = append(*consts, v)
	return len(*consts) - 1
}

// eliminateDeadCodeAfterTerminator replaces every instruction
// following an unconditional terminator with NOP, up to (but not
// including) the next instruction that is the target of a label or
// the end of the sequence. The pass is length-preserving so label
// indices stay valid. removeRedundantNops compacts the stream once
// ApplyLabelMap has resolved jump opargs.
//
// CPython: Python/flowgraph.c:L1453 remove_unreachable
func eliminateDeadCodeAfterTerminator(seq *Sequence) int {
	if len(seq.Instrs) == 0 {
		return 0
	}
	labelTargets := map[int]bool{}
	for _, off := range seq.labelmap {
		if off >= 0 {
			labelTargets[off] = true
		}
	}
	// Per-instruction handler labels also pin targets (the assembler
	// references them by index).
	for _, ins := range seq.Instrs {
		if ins.Handler.Label >= 0 {
			labelTargets[ins.Handler.Label] = true
		}
	}
	dropped := 0
	dead := false
	for i := range seq.Instrs {
		if labelTargets[i] {
			dead = false
		}
		// POP_BLOCK must survive into labelExceptionTargets (PASS 2b),
		// which converts it to NOP while tracking exception-frame depth.
		// Zeroing it here corrupts the handler stack for all instructions
		// that follow in the same scope (the outer SETUP_X frame never
		// gets popped, so it bleeds past the except block into module-
		// level code). CPython avoids this via CFG: dead blocks are
		// unreachable but their b_exceptstack is still propagated before
		// removal. The flat-sequence port preserves POP_BLOCK so the
		// same invariant holds.
		if dead && seq.Instrs[i].Op != NOP && seq.Instrs[i].Op != POP_BLOCK {
			seq.Instrs[i].Op = NOP
			seq.Instrs[i].Oparg = 0
			dropped++
		}
		if isTerminator(seq.Instrs[i].Op) {
			dead = true
		}
	}
	return dropped
}

// hasJumpTarget reports whether op carries a label oparg, including the
// pseudo JUMP / JUMP_NO_INTERRUPT opcodes that have no opcode-metadata
// row. HasTarget alone returns false for the pseudo forms, which would
// leave their opargs unrewritten by the NOP-compaction pass.
func hasJumpTarget(op Opcode) bool {
	switch op {
	case JUMP, JUMP_NO_INTERRUPT, SETUP_FINALLY, SETUP_WITH, SETUP_CLEANUP:
		return true
	}
	return HasTarget(op)
}

// propagateLineNumbers fills every NO_LOCATION (Lineno == -1)
// instruction with the most recent valid location seen earlier in the
// sequence. CPython runs this on the CFG: within each block,
// prev_location starts at NO_LOCATION and walks forward; at block
// boundaries the value may seed the first instruction of a single-
// predecessor successor. The flat-sequence port collapses both edges
// into one linear walk that never resets prev_location. The net effect
// matches CPython for the common cases driving spec 1713: the trailing
// `LOAD_CONST None; RETURN_VALUE` epilogue _PyCodegen_AddReturnAtEnd
// emits with NO_LOCATION inherits the module body's last line, so the
// linetable no longer encodes a stray PY_CODE_LOCATION_INFO_NONE row
// that findlinestarts surfaces as an extra (offset, None) transition.
//
// CPython: Python/flowgraph.c:3616 propagate_line_numbers
func propagateLineNumbers(seq *Sequence) {
	prev := ast.NoPos
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Loc.Lineno == -1 {
			ins.Loc = prev
		} else {
			prev = ins.Loc
		}
	}
}

// removeRedundantNops compacts the sequence by deleting NOP
// instructions that no jump or handler points at. Must run AFTER
// ApplyLabelMap because it rewrites the absolute oparg of every jump
// to account for the index shift.
//
// CPython: Python/flowgraph.c:L1325 remove_redundant_nops
func removeRedundantNops(seq *Sequence) int {
	if len(seq.Instrs) == 0 {
		return 0
	}
	// Pin every index that is a jump target or exception-handler
	// target. ApplyLabelMap has rewritten jump opargs to indices, so
	// we can read them directly.
	pinned := map[int]bool{}
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if hasJumpTarget(ins.Op) {
			pinned[int(ins.Oparg)] = true
		}
		if ins.Handler.Label >= 0 {
			pinned[ins.Handler.Label] = true
		}
	}
	// Build a forward index map: oldIndex -> newIndex.
	newIdx := make([]int, len(seq.Instrs))
	out := seq.Instrs[:0]
	for i, ins := range seq.Instrs {
		if ins.Op == NOP && !pinned[i] {
			// Map the dropped NOP onto the next surviving slot so that
			// any pre-existing reference (none should remain after the
			// pinned check, but defense in depth) lands somewhere
			// reasonable.
			newIdx[i] = len(out)
			continue
		}
		newIdx[i] = len(out)
		out = append(out, ins)
	}
	dropped := len(seq.Instrs) - len(out)
	// Rewrite jump and handler opargs to reflect the compaction.
	for i := range out {
		ins := &out[i]
		if hasJumpTarget(ins.Op) && int(ins.Oparg) < len(newIdx) {
			ins.Oparg = int32(newIdx[ins.Oparg])
		}
		if ins.Handler.Label >= 0 && ins.Handler.Label < len(newIdx) {
			ins.Handler.Label = newIdx[ins.Handler.Label]
		}
	}
	seq.Instrs = out
	return dropped
}
