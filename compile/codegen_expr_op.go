// Port of cpython/Python/codegen.c BoolOp / BinOp / UnaryOp / Compare
// visitors and the IfExp short-circuit.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitBoolOp emits the short-circuit form for `and` / `or`.
//
//	A and B and C  ->  evaluate A; pseudo JUMP_IF_FALSE to end with the
//	                   first falsy value on top; otherwise POP_TOP.
//	A or  B or  C  ->  same shape with the pseudo JUMP_IF_TRUE.
//
// We emit the pseudo JUMP_IF_FALSE / JUMP_IF_TRUE exactly as CPython
// does. convert_pseudo_conditional_jumps later expands each into
// COPY 1 + TO_BOOL + POP_JUMP_IF_X. Keeping the pseudo form through the
// optimizer lets jump threading collapse `a and b or c`-style chains
// (gh-124285): a JUMP_IF_FALSE whose target re-tests the same value
// with the opposite-sense JUMP_IF_TRUE is threaded past the re-test.
// Emitting the expanded form here would hide those pseudo jumps from
// the threader and double-evaluate the operand.
//
// CPython: Python/codegen.c:3290 codegen_boolop
// CPython: Python/flowgraph.c:3485 convert_pseudo_conditional_jumps
func (c *Compiler) visitBoolOp(e *ast.BoolOp) error {
	if len(e.Values) < 2 {
		return fmt.Errorf("compile: BoolOp needs at least two values")
	}
	end := c.newLabel()
	jump := JUMP_IF_FALSE
	if e.Op == ast.Or {
		jump = JUMP_IF_TRUE
	}
	for i, v := range e.Values {
		if err := c.visitExpr(v); err != nil {
			return err
		}
		if i == len(e.Values)-1 {
			break
		}
		c.addOpJump(jump, end, loc(e))
		c.addOp(POP_TOP, loc(e))
	}
	c.useLabel(end)
	return nil
}

// visitBinOp emits BINARY_OP with the operator-specific subcode.
//
// CPython: Python/codegen.c BinOp case in visit_expr
func (c *Compiler) visitBinOp(e *ast.BinOp) error {
	if err := c.visitExpr(e.Left); err != nil {
		return err
	}
	if err := c.visitExpr(e.Right); err != nil {
		return err
	}
	op, err := binaryOpKind(e.Op)
	if err != nil {
		return err
	}
	c.addOpI(BINARY_OP, op, loc(e))
	return nil
}

// binaryOpKind maps the asdl operator to BINARY_OP's NB_* subcode.
//
// CPython: Include/opcode.h NB_* macros
func binaryOpKind(op ast.Operator) (int32, error) {
	switch op {
	case ast.Add:
		return nbAdd, nil
	case ast.BitAnd:
		return nbAnd, nil
	case ast.FloorDiv:
		return nbFloorDiv, nil
	case ast.LShift:
		return nbLShift, nil
	case ast.MatMult:
		return nbMatMult, nil
	case ast.Mult:
		return nbMult, nil
	case ast.ModOperator:
		return nbRemainder, nil
	case ast.BitOr:
		return nbOr, nil
	case ast.Pow:
		return nbPower, nil
	case ast.RShift:
		return nbRShift, nil
	case ast.Sub:
		return nbSubtract, nil
	case ast.Div:
		return nbTrueDiv, nil
	case ast.BitXor:
		return nbXor, nil
	}
	return 0, fmt.Errorf("compile: unknown binary operator %v", op)
}

// BINARY_OP suboperators. CPython: Include/opcode.h NB_*.
const (
	nbAdd       int32 = 0
	nbAnd       int32 = 1
	nbFloorDiv  int32 = 2
	nbLShift    int32 = 3
	nbMatMult   int32 = 4
	nbMult      int32 = 5
	nbRemainder int32 = 6
	nbOr        int32 = 7
	nbPower     int32 = 8
	nbRShift    int32 = 9
	nbSubtract  int32 = 10
	nbTrueDiv   int32 = 11
	nbXor       int32 = 12
	nbSubscr    int32 = 26
)

// Exported NB_* opargs for the BINARY_OP family. The lowercase
// constants above are the codegen-side spelling; specializers and
// dispatch arms read the exported ones.
//
// CPython: Include/opcode.h NB_*
const (
	NB_ADD                     int32 = 0
	NB_AND                     int32 = 1
	NB_FLOOR_DIVIDE            int32 = 2
	NB_LSHIFT                  int32 = 3
	NB_MATRIX_MULTIPLY         int32 = 4
	NB_MULTIPLY                int32 = 5
	NB_REMAINDER               int32 = 6
	NB_OR                      int32 = 7
	NB_POWER                   int32 = 8
	NB_RSHIFT                  int32 = 9
	NB_SUBTRACT                int32 = 10
	NB_TRUE_DIVIDE             int32 = 11
	NB_XOR                     int32 = 12
	NB_INPLACE_ADD             int32 = 13
	NB_INPLACE_AND             int32 = 14
	NB_INPLACE_FLOOR_DIVIDE    int32 = 15
	NB_INPLACE_LSHIFT          int32 = 16
	NB_INPLACE_MATRIX_MULTIPLY int32 = 17
	NB_INPLACE_MULTIPLY        int32 = 18
	NB_INPLACE_REMAINDER       int32 = 19
	NB_INPLACE_OR              int32 = 20
	NB_INPLACE_POWER           int32 = 21
	NB_INPLACE_RSHIFT          int32 = 22
	NB_INPLACE_SUBTRACT        int32 = 23
	NB_INPLACE_TRUE_DIVIDE     int32 = 24
	NB_INPLACE_XOR             int32 = 25
	NB_SUBSCR                  int32 = 26
)

// visitUnaryOp emits UNARY_NEGATIVE / UNARY_INVERT / UNARY_NOT, or
// CALL_INTRINSIC_1 INTRINSIC_UNARY_POSITIVE for UAdd. The CFG
// const-folder collapses any of the four when the operand is a
// constant, copying the UnaryOp's location onto the resulting
// LOAD_CONST.
//
// CPython: Python/codegen.c:5194 UnaryOp_kind in codegen_visit_expr
func (c *Compiler) visitUnaryOp(e *ast.UnaryOp) error {
	if err := c.visitExpr(e.Operand); err != nil {
		return err
	}
	switch e.Op {
	case ast.UAdd:
		c.addOpI(CALL_INTRINSIC_1, intrinsicUnaryPositive, loc(e))
	case ast.USub:
		c.addOp(UNARY_NEGATIVE, loc(e))
	case ast.Invert:
		c.addOp(UNARY_INVERT, loc(e))
	case ast.Not:
		c.addOp(TO_BOOL, loc(e))
		c.addOp(UNARY_NOT, loc(e))
	default:
		return fmt.Errorf("compile: unknown unary operator %v", e.Op)
	}
	return nil
}

// visitCompare emits a chained comparison.
//
// For each rung but the last we keep the right-hand operand on the
// stack to feed the next rung, run the comparison against a duplicate,
// and branch on the bool form. The final rung leaves the raw
// comparison result on top, with cleanup taking the same shape so the
// stack height matches at the join.
//
// CPython: Python/codegen.c:3552 codegen_compare
func (c *Compiler) visitCompare(e *ast.Compare) error {
	if len(e.Ops) == 0 || len(e.Ops) != len(e.Comparators) {
		return fmt.Errorf("compile: malformed Compare ops=%d comparators=%d",
			len(e.Ops), len(e.Comparators))
	}
	if err := c.checkCompare(e); err != nil {
		return err
	}
	if err := c.visitExpr(e.Left); err != nil {
		return err
	}
	last := len(e.Ops) - 1
	if last == 0 {
		if err := c.visitExpr(e.Comparators[0]); err != nil {
			return err
		}
		c.emitCmpOp(e.Ops[0], loc(e))
		return nil
	}
	cleanup := c.newLabel()
	end := c.newLabel()
	for i := range last {
		if err := c.visitExpr(e.Comparators[i]); err != nil {
			return err
		}
		c.addOpI(SWAP, 2, loc(e))
		c.addOpI(COPY, 2, loc(e))
		c.emitCmpOp(e.Ops[i], loc(e))
		c.addOpI(COPY, 1, loc(e))
		c.addOp(TO_BOOL, loc(e))
		c.addOpJump(POP_JUMP_IF_FALSE, cleanup, loc(e))
		c.addOp(POP_TOP, loc(e))
	}
	if err := c.visitExpr(e.Comparators[last]); err != nil {
		return err
	}
	c.emitCmpOp(e.Ops[last], loc(e))
	c.addOpJump(JUMP_NO_INTERRUPT, end, ast.Pos{})

	c.useLabel(cleanup)
	c.addOpI(SWAP, 2, loc(e))
	c.addOp(POP_TOP, loc(e))

	c.useLabel(end)
	return nil
}

// emitCmpOp picks COMPARE_OP / CONTAINS_OP / IS_OP for one rung of a
// Compare. CPython packs the operator into the oparg as
// `(cmp << 5) | compare_masks[cmp]`: high 5 bits hold the kind that
// dispatch reads back, low 4 bits hold the TO_BOOL-eligibility mask.
//
// CPython: Python/codegen.c codegen_addcompare
func (c *Compiler) emitCmpOp(op ast.Cmpop, l ast.Pos) {
	switch op {
	case ast.Eq:
		c.addOpI(COMPARE_OP, int32(cmpEq), l)
	case ast.NotEq:
		c.addOpI(COMPARE_OP, int32(cmpNotEq), l)
	case ast.Lt:
		c.addOpI(COMPARE_OP, int32(cmpLt), l)
	case ast.LtE:
		c.addOpI(COMPARE_OP, int32(cmpLtE), l)
	case ast.Gt:
		c.addOpI(COMPARE_OP, int32(cmpGt), l)
	case ast.GtE:
		c.addOpI(COMPARE_OP, int32(cmpGtE), l)
	case ast.Is:
		c.addOpI(IS_OP, 0, l)
	case ast.IsNot:
		c.addOpI(IS_OP, 1, l)
	case ast.In:
		c.addOpI(CONTAINS_OP, 0, l)
	case ast.NotIn:
		c.addOpI(CONTAINS_OP, 1, l)
	}
}

// COMPARE_OP oparg encoding: high 5 bits = kind (Py_LT..Py_GE),
// low 4 bits = comparison-result mask (UNORDERED=1, LESS_THAN=2,
// GREATER_THAN=4, EQUALS=8). Values pre-encode `(cmp << 5) | mask`.
//
// CPython: Python/codegen.c compare_masks, Include/object.h Py_LT..Py_GE.
const (
	cmpLt    = (0 << 5) | 2       // Py_LT, LESS_THAN
	cmpLtE   = (1 << 5) | (2 | 8) // Py_LE, LESS_THAN | EQUALS
	cmpEq    = (2 << 5) | 8       // Py_EQ, EQUALS
	cmpNotEq = (3 << 5) | 7       // Py_NE, UNORDERED | LESS_THAN | GREATER_THAN
	cmpGt    = (4 << 5) | 4       // Py_GT, GREATER_THAN
	cmpGtE   = (5 << 5) | (4 | 8) // Py_GE, GREATER_THAN | EQUALS
)

// visitIfExp emits a conditional expression: `body if test else else_`.
//
// CPython: Python/codegen.c:L1979 codegen_ifexp
func (c *Compiler) visitIfExp(e *ast.IfExp) error {
	elseLab := c.newLabel()
	endLab := c.newLabel()
	if err := c.codegenJumpIf(e.Test, elseLab, false); err != nil {
		return err
	}
	if err := c.visitExpr(e.Body); err != nil {
		return err
	}
	c.addOpJump(JUMP, endLab, loc(e))
	c.useLabel(elseLab)
	if err := c.visitExpr(e.Orelse); err != nil {
		return err
	}
	c.useLabel(endLab)
	return nil
}
