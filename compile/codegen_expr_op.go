// Port of cpython/Python/codegen.c BoolOp / BinOp / UnaryOp / Compare
// visitors and the IfExp short-circuit.
//
// Spec: notes/Spec/1600/1626_gopy_codegen.md

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// visitBoolOp emits the short-circuit form for `and` / `or`.
//
//	A and B and C  ->  evaluate A; copy and to-bool; if false jump
//	                   to end with the first falsy value on top.
//	A or  B or  C  ->  same shape with POP_JUMP_IF_TRUE.
//
// CPython: Python/codegen.c:L3290 codegen_boolop
func (c *Compiler) visitBoolOp(e *ast.BoolOp) error {
	if len(e.Values) < 2 {
		return fmt.Errorf("compile: BoolOp needs at least two values")
	}
	end := c.newLabel()
	jump := POP_JUMP_IF_FALSE
	if e.Op == ast.Or {
		jump = POP_JUMP_IF_TRUE
	}
	for i, v := range e.Values {
		if err := c.visitExpr(v); err != nil {
			return err
		}
		if i == len(e.Values)-1 {
			break
		}
		// Keep the value alive: COPY 1 leaves the operand on the
		// stack for the jump-on-truthiness check, popping only when
		// the chain continues.
		c.addOpI(COPY, 1, loc(e))
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

// visitUnaryOp emits UNARY_NEGATIVE / UNARY_INVERT / UNARY_NOT or, for
// `not`, TO_BOOL + UNARY_NOT. CPython 3.14 collapsed unary plus on
// numeric constants in the folder, so UAdd just visits the operand
// without an opcode.
//
// CPython: Python/codegen.c UnaryOp case in visit_expr
func (c *Compiler) visitUnaryOp(e *ast.UnaryOp) error {
	if err := c.visitExpr(e.Operand); err != nil {
		return err
	}
	switch e.Op {
	case ast.UAdd:
		// Identity: leave the value on the stack.
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
//	a < b < c  ->  a; b; SWAP 2; COPY 2; COMPARE_OP <;
//	               POP_JUMP_IF_FALSE end_short;
//	               c; COMPARE_OP <;
//	               JUMP end;
//	             end_short: SWAP 2; POP_TOP;
//	             end:
//
// For a single op we skip the chain bookkeeping and just emit
// COMPARE_OP / CONTAINS_OP / IS_OP.
//
// CPython: Python/codegen.c:L3552 codegen_compare
func (c *Compiler) visitCompare(e *ast.Compare) error {
	if len(e.Ops) == 0 || len(e.Ops) != len(e.Comparators) {
		return fmt.Errorf("compile: malformed Compare ops=%d comparators=%d",
			len(e.Ops), len(e.Comparators))
	}
	if err := c.visitExpr(e.Left); err != nil {
		return err
	}
	if len(e.Ops) == 1 {
		if err := c.visitExpr(e.Comparators[0]); err != nil {
			return err
		}
		c.emitCmpOp(e.Ops[0], loc(e))
		return nil
	}
	cleanup := c.newLabel()
	end := c.newLabel()
	last := len(e.Ops) - 1
	for i, op := range e.Ops {
		if err := c.visitExpr(e.Comparators[i]); err != nil {
			return err
		}
		if i < last {
			c.addOpI(SWAP, 2, loc(e))
			c.addOpI(COPY, 2, loc(e))
			c.emitCmpOp(op, loc(e))
			c.addOpJump(POP_JUMP_IF_FALSE, cleanup, loc(e))
			continue
		}
		c.emitCmpOp(op, loc(e))
		c.addOpJump(JUMP, end, loc(e))
	}
	c.useLabel(cleanup)
	c.addOpI(SWAP, 2, loc(e))
	c.addOp(POP_TOP, loc(e))
	c.useLabel(end)
	return nil
}

// emitCmpOp picks COMPARE_OP / CONTAINS_OP / IS_OP for one rung of a
// Compare. CPython packs the operator into the oparg's high bits
// alongside a TO_BOOL-eligibility mask; we leave the high bits clear
// and let the flowgraph's super-instruction pass fix them up.
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

// COMPARE_OP suboperators (low 4 bits). CPython:
// Include/internal/pycore_opcode_metadata.h Cmp_kind.
const (
	cmpLt    = 0
	cmpLtE   = 1
	cmpEq    = 2
	cmpNotEq = 3
	cmpGt    = 4
	cmpGtE   = 5
)

// visitIfExp emits a conditional expression: `body if test else else_`.
//
// CPython: Python/codegen.c:L1979 codegen_ifexp
func (c *Compiler) visitIfExp(e *ast.IfExp) error {
	if err := c.visitExpr(e.Test); err != nil {
		return err
	}
	elseLab := c.newLabel()
	endLab := c.newLabel()
	c.addOpJump(POP_JUMP_IF_FALSE, elseLab, loc(e))
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
