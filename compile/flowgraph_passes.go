// Sequence-level optimisation passes. CPython runs these on the CFG;
// the v0.5 port keeps them on the flat instruction sequence so the
// rest of the pipeline can stay simple. Each pass is conservative
// enough that running it on a hand-built sequence cannot change
// observable behavior.
//
// CPython: Python/flowgraph.c:L3659 _PyCfg_OptimizeCodeUnit panel

package compile

import (
	"math/bits"

	"github.com/tamnd/gopy/ast"
)

// MAX_INT_SIZE caps the bit-length of folded integer results. CPython
// runs unlimited-precision arithmetic and refuses to fold once the
// product / power / shift exceeds 128 bits; gopy stores constants as
// int64, so the effective ceiling is 63 (sign bit reserved).
//
// CPython: Python/flowgraph.c:1690 MAX_INT_SIZE
const maxIntFoldBits = 63

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
// CPython: Python/flowgraph.c:1791 eval_const_binop
func evalIntBinop(op int32, x, y int64) (int64, bool) {
	switch op {
	case nbAdd, nbInplAdd:
		return safeAdd(x, y)
	case nbSubtract, nbInplSub:
		return safeSub(x, y)
	case nbMult, nbInplMul:
		return safeMultiply(x, y)
	case nbAnd, nbInplAnd:
		return x & y, true
	case nbOr, nbInplOr:
		return x | y, true
	case nbXor, nbInplXor:
		return x ^ y, true
	case nbLShift, nbInplLsh:
		return safeLshift(x, y)
	case nbRShift, nbInplRsh:
		if y < 0 || y >= 64 {
			return 0, false
		}
		return x >> uint(y), true
	case nbPower:
		return safePower(x, y)
	case nbFloorDiv:
		if y == 0 {
			return 0, false
		}
		// Python floor div rounds toward negative infinity.
		q := x / y
		if (x%y != 0) && ((x < 0) != (y < 0)) {
			q--
		}
		return q, true
	case nbRemainder:
		if y == 0 {
			return 0, false
		}
		r := x % y
		if r != 0 && ((r < 0) != (y < 0)) {
			r += y
		}
		return r, true
	}
	return 0, false
}

// intBitLen reports the bit length of |v|, matching _PyLong_NumBits.
//
// CPython: Objects/longobject.c _PyLong_NumBits
func intBitLen(v int64) int {
	if v < 0 {
		v = -v
	}
	return bits.Len64(uint64(v))
}

// safeAdd returns x + y when the result fits in int64. Matches
// CPython's implicit "fits in MAX_INT_SIZE bits" guard for the
// gopy int64 const pool.
//
// CPython: Python/flowgraph.c:1799 PyNumber_Add (eval_const_binop)
func safeAdd(x, y int64) (int64, bool) {
	r := x + y
	if (y > 0 && r < x) || (y < 0 && r > x) {
		return 0, false
	}
	return r, true
}

// safeSub returns x - y when the result fits in int64.
//
// CPython: Python/flowgraph.c:1802 PyNumber_Subtract (eval_const_binop)
func safeSub(x, y int64) (int64, bool) {
	r := x - y
	if (y > 0 && r > x) || (y < 0 && r < x) {
		return 0, false
	}
	return r, true
}

// safeMultiply mirrors const_folding_safe_multiply for the int64
// path. CPython caps at 128 bits combined; we cap at 63 to keep the
// result in int64.
//
// CPython: Python/flowgraph.c:1696 const_folding_safe_multiply
func safeMultiply(x, y int64) (int64, bool) {
	if x == 0 || y == 0 {
		return 0, true
	}
	if intBitLen(x)+intBitLen(y) > maxIntFoldBits {
		return 0, false
	}
	return x * y, true
}

// safeLshift mirrors const_folding_safe_lshift for the int64 path.
//
// CPython: Python/flowgraph.c:1761 const_folding_safe_lshift
func safeLshift(x, y int64) (int64, bool) {
	if y < 0 {
		return 0, false
	}
	if x == 0 || y == 0 {
		return x, true
	}
	if y > maxIntFoldBits || intBitLen(x)+int(y) > maxIntFoldBits {
		return 0, false
	}
	return x << uint(y), true
}

// safePower mirrors const_folding_safe_power for the int64 path. Only
// folds non-negative exponents; negative exponents would produce a
// float, and gopy keeps the const pool homogeneous per slot.
//
// CPython: Python/flowgraph.c:1741 const_folding_safe_power
func safePower(x, y int64) (int64, bool) {
	if y < 0 {
		return 0, false
	}
	if x == 0 {
		if y == 0 {
			return 1, true
		}
		return 0, true
	}
	if y == 0 {
		return 1, true
	}
	xbits := intBitLen(x)
	if xbits == 0 {
		xbits = 1
	}
	if int64(xbits)*y > int64(maxIntFoldBits) {
		return 0, false
	}
	result := int64(1)
	base := x
	exp := y
	for exp > 0 {
		if exp&1 == 1 {
			r, ok := safeMultiply(result, base)
			if !ok {
				return 0, false
			}
			result = r
		}
		exp >>= 1
		if exp > 0 {
			r, ok := safeMultiply(base, base)
			if !ok {
				return 0, false
			}
			base = r
		}
	}
	return result, true
}

// appendConst returns the index of v in *consts, appending if not
// present. Linear search is fine here: the per-unit pool is small
// and flowgraph runs once per scope.
//
// CPython: Python/flowgraph.c add_const
func appendConst(consts *[]any, v any) int {
	if isComparableConst(v) {
		for i, c := range *consts {
			if isComparableConst(c) && c == v {
				return i
			}
		}
	}
	*consts = append(*consts, v)
	return len(*consts) - 1
}

// isComparableConst reports whether v can be used with the == operator
// without panicking. Tuples (modeled as []any in the const pool) are
// not directly comparable; CPython's add_const reuses by hash+eq, which
// gopy will revisit when 1713 lands the const-key port. Until then,
// duplicate tuple consts simply both land in the pool.
func isComparableConst(v any) bool {
	switch v.(type) {
	case nil, bool, int64, float64, string, complex128:
		return true
	}
	return false
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

// foldConstUnaryop folds `LOAD_CONST x; UNARY_NEGATIVE | UNARY_INVERT |
// UNARY_NOT` pairs into a single LOAD_CONST holding the evaluated
// result. CPython also folds `CALL_INTRINSIC_1 INTRINSIC_UNARY_POSITIVE`,
// which only matters for explicit unary `+` on a literal; UAdd on a
// numeric constant is already collapsed in the parser-folding pass.
//
// CPython: Python/flowgraph.c:1935 fold_const_unaryop
// CPython: Python/flowgraph.c:1894 eval_const_unaryop
func foldConstUnaryop(seq *Sequence, consts *[]any) int {
	if consts == nil || len(seq.Instrs) < 2 {
		return 0
	}
	pinned := pinnedTargets(seq)
	folded := 0
	for i := 1; i < len(seq.Instrs); i++ {
		ins := &seq.Instrs[i]
		if !isFoldableUnary(ins.Op) {
			continue
		}
		if pinned[i] {
			continue
		}
		operand, ok := loadsConstValue(&seq.Instrs[i-1], *consts)
		if !ok {
			continue
		}
		result, ok := evalConstUnaryop(ins.Op, operand)
		if !ok {
			continue
		}
		setNop(&seq.Instrs[i-1])
		ins.Op = LOAD_CONST
		ins.Oparg = int32(appendConst(consts, result))
		folded++
	}
	return folded
}

// isFoldableUnary reports whether op is one of the three unary opcodes
// the folder rewrites.
//
// CPython: Python/flowgraph.c:1898 eval_const_unaryop opcode switch
func isFoldableUnary(op Opcode) bool {
	switch op {
	case UNARY_NEGATIVE, UNARY_INVERT, UNARY_NOT:
		return true
	}
	return false
}

// evalConstUnaryop applies one unary opcode to a const operand, mirroring
// CPython's PyNumber_Negative / PyNumber_Invert / bool(!x) dispatch.
// Returns ok=false when the operand type is not foldable (e.g. unary
// negate of a string) or would overflow the int64 representation.
//
// CPython: Python/flowgraph.c:1894 eval_const_unaryop
func evalConstUnaryop(op Opcode, operand any) (any, bool) {
	switch op {
	case UNARY_NEGATIVE:
		switch v := operand.(type) {
		case int64:
			if v == minInt64 {
				return nil, false
			}
			return -v, true
		case float64:
			return -v, true
		}
	case UNARY_INVERT:
		// CPython rejects ~bool with a SyntaxWarning. gopy's const pool
		// doesn't carry bool through this path (the AST folder promotes
		// `True` / `False` to int in numeric context), so the bool
		// guard is unreachable here.
		if v, ok := operand.(int64); ok {
			return ^v, true
		}
	case UNARY_NOT:
		b, ok := constTruthValue(operand)
		if !ok {
			return nil, false
		}
		return !b, true
	}
	return nil, false
}

const minInt64 int64 = -1 << 63

// constTruthValue mirrors PyObject_IsTrue for the const-pool value
// shapes gopy stores. Returns ok=false when the value's truthiness is
// not statically determinable (e.g. a user-typed value reaching the
// const pool via marshal round-trip).
//
// CPython: Python/flowgraph.c:1916 eval_const_unaryop UNARY_NOT case
func constTruthValue(v any) (bool, bool) {
	switch x := v.(type) {
	case nil:
		return false, true
	case bool:
		return x, true
	case int64:
		return x != 0, true
	case float64:
		return x != 0, true
	case string:
		return x != "", true
	case complex128:
		return real(x) != 0 || imag(x) != 0, true
	}
	return false, false
}

// loadsConstValue reports whether ins is a const-loading instruction
// (LOAD_CONST or LOAD_SMALL_INT) and returns the underlying Python
// value. For LOAD_SMALL_INT the value is the literal oparg as int64.
//
// CPython: Python/flowgraph.c:1389 loads_const + get_const_value
func loadsConstValue(ins *Instr, consts []any) (any, bool) {
	switch ins.Op {
	case LOAD_CONST:
		idx := int(ins.Oparg)
		if idx < 0 || idx >= len(consts) {
			return nil, false
		}
		return consts[idx], true
	case LOAD_SMALL_INT:
		return int64(ins.Oparg), true
	}
	return nil, false
}

// minConstSequenceSize mirrors CPython's MIN_CONST_SEQUENCE_SIZE: a
// list / set literal shorter than this stays as N pushes + BUILD_X
// because the LIST_EXTEND prelude would not pay off.
//
// CPython: Python/flowgraph.c:1585 MIN_CONST_SEQUENCE_SIZE
const minConstSequenceSize = 3

// foldTupleOfConstants rewrites `LOAD_CONST c1; ...; LOAD_CONST cN;
// BUILD_TUPLE N` into `NOP; ...; NOP; LOAD_CONST (c1, ..., cN)` when
// every preceding loader is a constant. The orphaned const slots get
// reclaimed by removeUnusedConsts.
//
// We only fold when the run of N const loaders sits immediately before
// the BUILD_TUPLE with no jump landing in the middle. The pinned set
// is computed once over the whole sequence.
//
// CPython: Python/flowgraph.c:1454 fold_tuple_of_constants
func foldTupleOfConstants(seq *Sequence, consts *[]any) int {
	if consts == nil || len(seq.Instrs) == 0 {
		return 0
	}
	pinned := pinnedTargets(seq)
	folded := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Op != BUILD_TUPLE {
			continue
		}
		n := int(ins.Oparg)
		if n <= 0 || i < n {
			continue
		}
		start := i - n
		ok, values := collectConstLoaders(seq, *consts, pinned, start, n)
		if !ok {
			continue
		}
		tuple := &ConstTuple{Values: append([]any(nil), values...)}
		for k := start; k < i; k++ {
			setNop(&seq.Instrs[k])
		}
		idx := appendConst(consts, tuple)
		ins.Op = LOAD_CONST
		ins.Oparg = int32(idx)
		folded++
	}
	return folded
}

// optimizeListsAndSets rewrites `LOAD_CONST c1; ...; LOAD_CONST cN;
// BUILD_LIST N` (and BUILD_SET) into
//
//	NOP; ...; BUILD_LIST 0; LOAD_CONST (c1, ..., cN); LIST_EXTEND 1
//
// when the run of N loaders is all constants and N is large enough to
// pay for the prelude. CPython's pass also handles the GET_ITER /
// CONTAINS_OP follow-up (collapse the literal to a tuple/frozenset and
// drop the BUILD_X), but that branch is not on spec 1713's hot path
// yet. The body-of-loop case is the one disdata/for_simple.py exercises.
//
// CPython: Python/flowgraph.c:1597 optimize_lists_and_sets
func optimizeListsAndSets(seq *Sequence, consts *[]any) int {
	if consts == nil || len(seq.Instrs) < 3 {
		return 0
	}
	pinned := pinnedTargets(seq)
	folded := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if ins.Op != BUILD_LIST && ins.Op != BUILD_SET {
			continue
		}
		n := int(ins.Oparg)
		if n < minConstSequenceSize || i < n {
			continue
		}
		start := i - n
		ok, values := collectConstLoaders(seq, *consts, pinned, start, n)
		if !ok {
			continue
		}
		tuple := &ConstTuple{Values: append([]any(nil), values...)}
		// Need at least two preceding instruction slots: one for the
		// BUILD_X 0 prelude and one for the LOAD_CONST. The const
		// loaders run takes care of that (n >= 3).
		idx := appendConst(consts, tuple)
		// NOP every loader except the last two slots, which carry the
		// prelude and the LOAD_CONST.
		for k := start; k < i-2; k++ {
			setNop(&seq.Instrs[k])
		}
		preludeOp := ins.Op
		seq.Instrs[i-2].Op = preludeOp
		seq.Instrs[i-2].Oparg = 0
		seq.Instrs[i-2].Loc = ins.Loc
		seq.Instrs[i-1].Op = LOAD_CONST
		seq.Instrs[i-1].Oparg = int32(idx)
		seq.Instrs[i-1].Loc = ins.Loc
		if preludeOp == BUILD_LIST {
			ins.Op = LIST_EXTEND
		} else {
			ins.Op = SET_UPDATE
		}
		ins.Oparg = 1
		folded++
	}
	return folded
}

// pinnedTargets returns a bool slice marking every position that is a
// jump target or exception-handler target, so length-preserving folds
// don't reach across a label.
func pinnedTargets(seq *Sequence) []bool {
	pinned := make([]bool, len(seq.Instrs))
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if hasJumpTarget(ins.Op) {
			id := int(ins.Oparg)
			if id > 0 && id < len(seq.labelmap) {
				off := seq.labelmap[id]
				if off >= 0 && off < len(pinned) {
					pinned[off] = true
				}
			}
		}
		if ins.Handler.Label >= 0 && ins.Handler.Label < len(pinned) {
			pinned[ins.Handler.Label] = true
		}
	}
	return pinned
}

// collectConstLoaders checks that the run of length n starting at start
// is entirely const loaders (LOAD_CONST / LOAD_SMALL_INT) with no
// pinned position inside the run (apart from start itself, which is
// fine: the rewrite preserves the position of start). Returns the list
// of loaded values when the check passes.
func collectConstLoaders(seq *Sequence, consts []any, pinned []bool, start, n int) (bool, []any) {
	values := make([]any, 0, n)
	for k := range n {
		pos := start + k
		if k > 0 && pinned[pos] {
			return false, nil
		}
		v, ok := loadsConstValue(&seq.Instrs[pos], consts)
		if !ok {
			return false, nil
		}
		values = append(values, v)
	}
	return true, values
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

// removeRedundantJumps drops every unconditional JUMP whose target is
// the immediately following non-NOP instruction. Runs after
// ApplyLabelMap so we can read the jump oparg as an absolute index and
// scan forward to find the next live instruction.
//
// CPython runs this inside remove_redundant_nops_and_jumps in a loop
// with remove_redundant_nops until both reach zero changes. The flat-
// sequence version mirrors that loop in OptimizeWithFlags.
//
// CPython: Python/flowgraph.c:1158 remove_redundant_jumps
// CPython: Python/flowgraph.c:2529 remove_redundant_nops_and_jumps
func removeRedundantJumps(seq *Sequence) int {
	dropped := 0
	for i := range seq.Instrs {
		ins := &seq.Instrs[i]
		if !isUnconditionalJump(ins.Op) {
			continue
		}
		target := int(ins.Oparg)
		if target < 0 || target >= len(seq.Instrs) {
			continue
		}
		next := i + 1
		for next < len(seq.Instrs) && seq.Instrs[next].Op == NOP {
			next++
		}
		// Resolve target by skipping NOPs too (CPython's
		// next_nonempty_block does the same on the CFG side).
		landed := target
		for landed < len(seq.Instrs) && seq.Instrs[landed].Op == NOP {
			landed++
		}
		if landed == next {
			setNop(ins)
			dropped++
		}
	}
	return dropped
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

// isSwappable returns true for opcodes apply_static_swaps is willing
// to slide a SWAP past. Mirrors the SWAPPABLE macro.
//
// CPython: Python/flowgraph.c:2082 SWAPPABLE
func isSwappable(op Opcode) bool {
	return op == STORE_FAST || op == STORE_FAST_MAYBE_NULL || op == POP_TOP
}

// storesTo returns the local index a STORE_FAST(_MAYBE_NULL) writes,
// or -1 for any other opcode.
//
// CPython: Python/flowgraph.c:2087 STORES_TO
func storesTo(ins *Instr) int32 {
	if ins.Op == STORE_FAST || ins.Op == STORE_FAST_MAYBE_NULL {
		return ins.Oparg
	}
	return -1
}

// swaptimize replaces an arbitrary run of SWAP / NOP starting at i with
// the optimal SWAP sequence that produces the same stack permutation.
// Returns the new index (advanced past the run) and the number of
// swaps it rewrote. pinned[k] true means k is a jump or handler
// target; the run is cut short before any pinned position past i so we
// never move or NOP an instruction something else points at.
//
// CPython: Python/flowgraph.c:1982 swaptimize
func swaptimize(seq *Sequence, pinned []bool, i int) (int, int) {
	instrs := seq.Instrs
	if i >= len(instrs) || instrs[i].Op != SWAP {
		return i + 1, 0
	}
	depth := int(instrs[i].Oparg)
	limit := len(instrs) - i
	length := 1
	more := false
	for length < limit {
		pos := i + length
		if pinned[pos] {
			break
		}
		op := instrs[pos].Op
		if op == SWAP {
			if d := int(instrs[pos].Oparg); d > depth {
				depth = d
			}
			more = true
		} else if op != NOP {
			break
		}
		length++
	}
	if !more {
		return i + length, 0
	}
	// Simulate the combined permutation on a {0, 1, ..., depth-1} stack.
	stack := make([]int, depth)
	for k := range stack {
		stack[k] = k
	}
	for k := range length {
		ins := &instrs[i+k]
		if ins.Op != SWAP {
			continue
		}
		oparg := int(ins.Oparg)
		stack[0], stack[oparg-1] = stack[oparg-1], stack[0]
	}
	current := emitSwapCycles(instrs[i:i+length], stack, length-1)
	rewrites := nopOutRemaining(instrs[i:i+length], current)
	return i + length, rewrites
}

// emitSwapCycles cycle-decomposes stack[] and emits the SWAPs that
// realize the permutation, filling instructions from index `current`
// backward. Returns the next free slot index (which may be -1).
//
// CPython: Python/flowgraph.c:2036 swaptimize cycle loop
func emitSwapCycles(run []Instr, stack []int, current int) int {
	const visited = -1
	for k := range stack {
		if stack[k] == visited || stack[k] == k {
			continue
		}
		j := k
		for {
			if j != 0 {
				run[current].Op = SWAP
				run[current].Oparg = int32(j + 1)
				current--
			}
			if stack[j] == visited {
				break
			}
			nextJ := stack[j]
			stack[j] = visited
			j = nextJ
		}
	}
	return current
}

// nopOutRemaining NOPs the prefix of run[:current+1] left unused by
// emitSwapCycles. Returns the number of opcodes turned into NOPs.
//
// CPython: Python/flowgraph.c:2069 swaptimize NOP-out tail
func nopOutRemaining(run []Instr, current int) int {
	rewrites := 0
	for current >= 0 {
		if run[current].Op != NOP {
			setNop(&run[current])
			rewrites++
		}
		current--
	}
	return rewrites
}

// runSwaptimize sweeps the sequence, applying swaptimize at every SWAP.
//
// CPython: Python/flowgraph.c:2515 basicblock_optimize_load_const swaptimize call
func runSwaptimize(seq *Sequence) int {
	pinned := pinnedTargets(seq)
	rewrites := 0
	i := 0
	for i < len(seq.Instrs) {
		if seq.Instrs[i].Op != SWAP {
			i++
			continue
		}
		next, n := swaptimize(seq, pinned, i)
		rewrites += n
		if next <= i {
			i++
		} else {
			i = next
		}
	}
	return rewrites
}

// nextSwappableInstruction returns the index of the next swappable
// instruction past i in the same line group, or -1 if scanning hits a
// non-swappable opcode, a different line number, or a pinned position.
//
// CPython: Python/flowgraph.c:2093 next_swappable_instruction
func nextSwappableInstruction(seq *Sequence, pinned []bool, i, lineno int) int {
	for k := i + 1; k < len(seq.Instrs); k++ {
		if pinned[k] {
			return -1
		}
		ins := &seq.Instrs[k]
		if lineno >= 0 && ins.Loc.Lineno != lineno {
			return -1
		}
		if ins.Op == NOP {
			continue
		}
		if isSwappable(ins.Op) {
			return k
		}
		return -1
	}
	return -1
}

// applyStaticSwaps walks back from index i, turning every SWAP it
// passes into a direct reorder of the following swappable
// instructions. Mirrors the SWAP(2), POP_TOP, STORE_FAST(42) -> NOP,
// STORE_FAST(42), POP_TOP rewrite called out in the CPython comment.
//
// CPython: Python/flowgraph.c:2117 apply_static_swaps
func applyStaticSwaps(seq *Sequence, pinned []bool, i int) {
	instrs := seq.Instrs
	for ; i >= 0; i-- {
		if pinned[i] {
			return
		}
		swap := &instrs[i]
		if swap.Op != SWAP {
			if swap.Op == NOP || isSwappable(swap.Op) {
				continue
			}
			return
		}
		j := nextSwappableInstruction(seq, pinned, i, -1)
		if j < 0 {
			return
		}
		k := j
		lineno := instrs[j].Loc.Lineno
		for count := int(swap.Oparg) - 1; count > 0; count-- {
			k = nextSwappableInstruction(seq, pinned, k, lineno)
			if k < 0 {
				return
			}
		}
		if !staticSwapSafe(instrs, j, k) {
			return
		}
		setNop(swap)
		instrs[j], instrs[k] = instrs[k], instrs[j]
	}
}

// staticSwapSafe checks whether swapping instrs[j] and instrs[k] is
// safe given the STORE_FAST aliasing rules: the two endpoints must
// not write the same local, and no intervening instruction may write
// to either endpoint's local.
//
// CPython: Python/flowgraph.c:2144 apply_static_swaps store-aliasing block
func staticSwapSafe(instrs []Instr, j, k int) bool {
	storeJ := storesTo(&instrs[j])
	storeK := storesTo(&instrs[k])
	if storeJ < 0 && storeK < 0 {
		return true
	}
	if storeJ == storeK {
		return false
	}
	for idx := j + 1; idx < k; idx++ {
		storeIdx := storesTo(&instrs[idx])
		if storeIdx >= 0 && (storeIdx == storeJ || storeIdx == storeK) {
			return false
		}
	}
	return true
}

// runApplyStaticSwaps scans for SWAP opcodes and invokes
// applyStaticSwaps at each one. CPython does the walk from inside
// basicblock_optimize_load_const; gopy runs it as its own pass over
// the flat sequence.
//
// CPython: Python/flowgraph.c:2518 basicblock_optimize_load_const apply_static_swaps call
func runApplyStaticSwaps(seq *Sequence) {
	pinned := pinnedTargets(seq)
	for i := range seq.Instrs {
		if seq.Instrs[i].Op == SWAP {
			applyStaticSwaps(seq, pinned, i)
		}
	}
}
