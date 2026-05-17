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

// rewriteLoadSmallInt promotes every `LOAD_CONST <int n>` with
// 0 <= n <= 255 to `LOAD_SMALL_INT n`. The new oparg is the literal
// value, not a consts-pool index. The orphaned const slot is pruned
// later by removeUnusedConsts; until that pass runs the slot stays
// in the pool so every subsequent LOAD_CONST oparg is still valid.
//
// CPython: Python/flowgraph.c:1408 maybe_instr_make_load_smallint
// CPython: Python/flowgraph.c:2169 basicblock_optimize_load_const
func rewriteLoadSmallInt(seq *Sequence, consts *[]any) int {
	if consts == nil {
		return 0
	}
	rewritten := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Op != LOAD_CONST {
			continue
		}
		idx := int(ins.Oparg)
		if idx < 0 || idx >= len(*consts) {
			continue
		}
		v, ok := (*consts)[idx].(int64)
		if !ok {
			continue
		}
		if v < 0 || v > 255 {
			continue
		}
		ins.Op = LOAD_SMALL_INT
		ins.Oparg = int32(v)
		rewritten++
	}
	return rewritten
}

// removeUnusedConsts prunes entries from the per-unit const pool that
// no surviving instruction references. Runs after rewriteLoadSmallInt
// so the small-int slots, which now point at LOAD_SMALL_INT literals
// rather than const-pool indices, get reclaimed. Slot 0 is always
// preserved: CPython treats it as the docstring slot whether or not
// the body actually has a docstring.
//
// CPython: Python/flowgraph.c:3174 remove_unused_consts
func removeUnusedConsts(seq *Sequence, consts *[]any) {
	if consts == nil {
		return
	}
	nconsts := len(*consts)
	if nconsts == 0 {
		return
	}
	indexMap := make([]int, nconsts)
	for i := 1; i < nconsts; i++ {
		indexMap[i] = -1
	}
	// The first constant may be the docstring; keep it always.
	indexMap[0] = 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if !ins.Op.HasConst() {
			continue
		}
		idx := int(ins.Oparg)
		if idx < 0 || idx >= nconsts {
			continue
		}
		indexMap[idx] = idx
	}
	// Condense: keep used entries in their original order.
	nUsed := 0
	for i := range nconsts {
		if indexMap[i] != -1 {
			indexMap[nUsed] = indexMap[i]
			nUsed++
		}
	}
	if nUsed == nconsts {
		return
	}
	newConsts := make([]any, nUsed)
	for i := range nUsed {
		newConsts[i] = (*consts)[indexMap[i]]
	}
	reverse := make([]int, nconsts)
	for i := range nconsts {
		reverse[i] = -1
	}
	for i := range nUsed {
		reverse[indexMap[i]] = i
	}
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if !ins.Op.HasConst() {
			continue
		}
		idx := int(ins.Oparg)
		if idx < 0 || idx >= nconsts || reverse[idx] < 0 {
			continue
		}
		ins.Oparg = int32(reverse[idx])
	}
	*consts = newConsts
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

// duplicateExitsWithoutLineno clones every exit-without-lineno block
// that has more than one predecessor, retargeting each incoming jump
// at its own copy. CPython does this so the trailing
// `LOAD_CONST None; RETURN_VALUE` epilogue can carry a different
// source line on each control-flow path (the line of the jump that
// reaches it vs. the line of the fall-through). Without it, both
// paths share one exit block and the linetable has to encode a
// NO_LOCATION row for the second arrival.
//
// Block detection on the flat sequence: starts are position 0, every
// labeled position, and every position after a terminator or
// unconditional jump. An "exit block" is one whose first instruction
// carries no lineno (Lineno == -1) and which ends in a terminator
// (RETURN_VALUE / RAISE_VARARGS / RERAISE / INTERPRETER_EXIT) before
// the next block start. Predecessor count combines jump targets (from
// labelmap) with fall-through from the previous instruction.
//
// Runs before ApplyLabelMap so target lookups go through labelmap and
// fresh duplicates can bind a new label via NewLabel.
//
// CPython: Python/flowgraph.c:3563 duplicate_exits_without_lineno
//
//nolint:gocognit // 1:1 port of CPython's CFG-based pass adapted to flat sequence
func duplicateExitsWithoutLineno(seq *Sequence) {
	blockStarts := computeBlockStarts(seq)
	preds := computePredecessorCounts(seq, blockStarts)

	n := len(seq.Instrs)
	for i := range n {
		ins := &seq.Instrs[i]
		if !isAnyJump(ins.Op) {
			continue
		}
		id := int(ins.Oparg)
		if id <= 0 || id >= len(seq.labelmap) {
			continue
		}
		target := seq.labelmap[id]
		if target < 0 || target >= n {
			continue
		}
		if seq.Instrs[target].Loc.Lineno != -1 {
			continue
		}
		end := findExitBlockEnd(seq, blockStarts, target, n)
		if end < 0 {
			continue
		}
		if preds[target] <= 1 {
			continue
		}
		copyStart := len(seq.Instrs)
		for k := target; k <= end; k++ {
			cp := seq.Instrs[k]
			if k == target {
				cp.Loc = ins.Loc
			}
			seq.Instrs = append(seq.Instrs, cp)
		}
		newLbl := seq.NewLabel()
		seq.ensureLabelmap(newLbl.id)
		seq.labelmap[newLbl.id] = copyStart
		ins.Oparg = int32(newLbl.id)
		preds[target]--
	}

	// Pass two from CPython: any remaining exit-without-lineno that is
	// only reachable by fall-through gets its first instruction stamped
	// with the predecessor's last lineno.
	for start := range blockStarts {
		if start == 0 || start >= len(seq.Instrs) {
			continue
		}
		prev := seq.Instrs[start-1]
		if isTerminator(prev.Op) || isUnconditionalJump(prev.Op) {
			continue
		}
		if seq.Instrs[start].Loc.Lineno != -1 {
			continue
		}
		end := findExitBlockEnd(seq, blockStarts, start, len(seq.Instrs))
		if end < 0 {
			continue
		}
		seq.Instrs[start].Loc = prev.Loc
	}
}

// computeBlockStarts returns the set of instruction positions that
// begin a basic block: position 0, every labeled position, and every
// position immediately after a terminator or unconditional jump.
//
// CPython: Python/flowgraph.c basicblock cuts inside
// _PyCfgBuilder_FromInstructionSequence
func computeBlockStarts(seq *Sequence) map[int]bool {
	starts := map[int]bool{0: true}
	for i, ins := range seq.Instrs {
		if (isTerminator(ins.Op) || isUnconditionalJump(ins.Op)) && i+1 < len(seq.Instrs) {
			starts[i+1] = true
		}
	}
	for id := 1; id < len(seq.labelmap); id++ {
		off := seq.labelmap[id]
		if off >= 0 {
			starts[off] = true
		}
	}
	return starts
}

// computePredecessorCounts tallies how many predecessors each block
// start has: jump targets via labelmap plus the at-most-one
// fall-through from the previous instruction.
//
// CPython: Python/flowgraph.c basicblock->b_predecessors maintenance
func computePredecessorCounts(seq *Sequence, starts map[int]bool) map[int]int {
	preds := map[int]int{}
	for _, ins := range seq.Instrs {
		if !isAnyJump(ins.Op) {
			continue
		}
		id := int(ins.Oparg)
		if id <= 0 || id >= len(seq.labelmap) {
			continue
		}
		off := seq.labelmap[id]
		if off >= 0 {
			preds[off]++
		}
	}
	for start := range starts {
		if start == 0 || start > len(seq.Instrs) {
			continue
		}
		prev := seq.Instrs[start-1]
		if !isTerminator(prev.Op) && !isUnconditionalJump(prev.Op) {
			preds[start]++
		}
	}
	return preds
}

// findExitBlockEnd returns the index of the terminator that closes
// the block starting at start, or -1 if the block runs into another
// block boundary first (meaning it is not an exit block).
//
// CPython: is_exit_without_lineno predicate
func findExitBlockEnd(seq *Sequence, starts map[int]bool, start, n int) int {
	for k := start; k < n; k++ {
		if k > start && starts[k] {
			return -1
		}
		if isTerminator(seq.Instrs[k].Op) {
			return k
		}
	}
	return -1
}

// isAnyJump reports whether op is any conditional or unconditional
// jump (including the JUMP / JUMP_NO_INTERRUPT pseudo-ops that codegen
// emits and the back-direction variants lowering produces).
func isAnyJump(op Opcode) bool {
	return isConditionalJump(op) || isUnconditionalJump(op)
}

// normalizeJumps walks the sequence and gives every POP_JUMP_IF_X the
// shape CPython emits: forward jumps gain a NOT_TAKEN at the
// fall-through edge, backward jumps are flipped to a forward reversed
// jump followed by a NOT_TAKEN + unconditional JUMP to the original
// target. Monitoring and the disassembler key off NOT_TAKEN to
// attribute the not-taken branch back to its source.
//
// Runs before ApplyLabelMap so target lookups go through s.labelmap
// and Insert handles the label index bump for us.
//
// CPython: Python/flowgraph.c:535 normalize_jumps_in_block
func normalizeJumps(seq *Sequence) {
	for i := 0; i < len(seq.Instrs); i++ {
		ins := &seq.Instrs[i]
		if !isConditionalJump(ins.Op) {
			continue
		}
		id := int(ins.Oparg)
		if id <= 0 || id >= len(seq.labelmap) {
			continue
		}
		target := seq.labelmap[id]
		if target == labelUnbound {
			continue
		}
		if target > i {
			seq.Insert(i+1, NOT_TAKEN, 0, ins.Loc)
			i++
			continue
		}
		if i+1 >= len(seq.Instrs) {
			// Backward conditional with no fall-through instruction
			// is malformed bytecode. Bail rather than synthesize a
			// target that points past the end.
			continue
		}
		reversed, ok := reverseConditionalJump(ins.Op)
		if !ok {
			continue
		}
		loc := ins.Loc
		origTarget := int32(id)
		// Insert [NOT_TAKEN, JUMP origTarget] at i+1 / i+2. Insert
		// leaves origTarget's labelmap entry alone because the label
		// is bound to a position <= i, which is < i+1.
		seq.Insert(i+1, NOT_TAKEN, 0, loc)
		seq.Insert(i+2, JUMP, origTarget, loc)
		// Allocate a fresh label bound to the instruction that used
		// to sit at i+1 and now sits at i+3, after the two inserts.
		next := seq.NewLabel()
		seq.ensureLabelmap(next.id)
		seq.labelmap[next.id] = i + 3
		seq.Instrs[i].Op = reversed
		seq.Instrs[i].Oparg = int32(next.id)
		i += 2
	}
}

// reverseConditionalJump returns the opcode that branches on the
// opposite truth value. Used by normalizeJumps to flip a backward
// POP_JUMP_IF_X into a forward jump past the inserted JUMP block.
//
// CPython: Python/flowgraph.c:551 normalize_jumps_in_block switch
func reverseConditionalJump(op Opcode) (Opcode, bool) {
	switch op {
	case POP_JUMP_IF_FALSE:
		return POP_JUMP_IF_TRUE, true
	case POP_JUMP_IF_TRUE:
		return POP_JUMP_IF_FALSE, true
	case POP_JUMP_IF_NONE:
		return POP_JUMP_IF_NOT_NONE, true
	case POP_JUMP_IF_NOT_NONE:
		return POP_JUMP_IF_NONE, true
	}
	return 0, false
}

// peepholeOpcodePairs runs the two-instruction rewrites from CPython's
// optimize_basic_block. Each rewrite folds a (compare-like)+(TO_BOOL or
// UNARY_NOT) pair into a single instruction by NOP-ing the first and
// updating the second's opcode/oparg. The pass is length-preserving;
// removeRedundantNops compacts the NOPs afterwards.
//
// Safety: the rewrite changes the pop arity at i+1 for the COMPARE_OP /
// CONTAINS_OP / IS_OP cases (TO_BOOL pops 1 → fused op pops 2), so we
// must not fuse if anything jumps into i+1. The pinned set mirrors
// removeRedundantNops: every absolute jump target plus every
// Handler.Label that points at an instruction inside the sequence.
//
// CPython: Python/flowgraph.c:2449 optimize_basic_block (case
// COMPARE_OP / CONTAINS_OP / IS_OP / TO_BOOL / UNARY_NOT)
func peepholeOpcodePairs(seq *Sequence) int {
	if len(seq.Instrs) < 2 {
		return 0
	}
	pinned := make([]bool, len(seq.Instrs))
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if hasJumpTarget(ins.Op) {
			t := int(ins.Oparg)
			if t >= 0 && t < len(pinned) {
				pinned[t] = true
			}
		}
		if ins.Handler.Label >= 0 && ins.Handler.Label < len(pinned) {
			pinned[ins.Handler.Label] = true
		}
	}
	folded := 0
	for i := 0; i+1 < len(seq.Instrs); i++ {
		if pinned[i+1] {
			continue
		}
		if tryPeepholePair(&seq.Instrs[i], &seq.Instrs[i+1]) {
			folded++
		}
	}
	return folded
}

// tryPeepholePair applies the (a, b) → (NOP, fused) rewrite at one
// adjacent pair. Returns true if the pair matched a rewrite rule.
func tryPeepholePair(a, b *Instr) bool {
	origArg := a.Oparg
	switch a.Op {
	case COMPARE_OP:
		if b.Op == TO_BOOL {
			setNop(a)
			b.Op = COMPARE_OP
			b.Oparg = origArg | 16
			return true
		}
	case CONTAINS_OP, IS_OP:
		switch b.Op {
		case TO_BOOL:
			fused := a.Op
			setNop(a)
			b.Op = fused
			b.Oparg = origArg
			return true
		case UNARY_NOT:
			fused := a.Op
			setNop(a)
			b.Op = fused
			b.Oparg = origArg ^ 1
			return true
		}
	case TO_BOOL:
		if b.Op == TO_BOOL {
			setNop(a)
			return true
		}
	case UNARY_NOT:
		switch b.Op {
		case TO_BOOL:
			setNop(a)
			b.Op = UNARY_NOT
			b.Oparg = 0
			return true
		case UNARY_NOT:
			setNop(a)
			setNop(b)
			return true
		}
	}
	return false
}

func setNop(ins *Instr) {
	ins.Op = NOP
	ins.Oparg = 0
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
