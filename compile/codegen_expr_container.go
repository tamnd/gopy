// Port of cpython/Python/codegen.c container display visitors:
// List, Tuple, Set, Dict, plus the Starred-in-list helper. Also the
// Subscript / Attribute load paths.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// visitList emits a list literal in load context. Stores are routed
// through assignTo. Star elements pivot to LIST_EXTEND.
//
// CPython: Python/codegen.c:L3431 codegen_list
func (c *Compiler) visitList(e *ast.List) error {
	if e.Ctx != ast.Load {
		return fmt.Errorf("compile: List in non-load context handled by assignTo")
	}
	return c.emitListOrSet(e.Elts, BUILD_LIST, loc(e))
}

// visitTuple emits a tuple literal in load context.
//
// CPython: Python/codegen.c codegen_tuple
func (c *Compiler) visitTuple(e *ast.Tuple) error {
	if e.Ctx != ast.Load {
		return fmt.Errorf("compile: Tuple in non-load context handled by assignTo")
	}
	if !hasStarred(e.Elts) {
		for _, elt := range e.Elts {
			if err := c.visitExpr(elt); err != nil {
				return err
			}
		}
		c.addOpI(BUILD_TUPLE, int32(len(e.Elts)), loc(e))
		return nil
	}
	// Tuple with stars: build a list, splat in, convert to tuple.
	if err := c.emitListOrSet(e.Elts, BUILD_LIST, loc(e)); err != nil {
		return err
	}
	c.addOpI(CALL_INTRINSIC_1, intrinsicListToTuple, loc(e))
	return nil
}

// visitSet emits a set literal. Stars splat with SET_UPDATE.
//
// CPython: Python/codegen.c:L3467 codegen_set
func (c *Compiler) visitSet(e *ast.Set) error {
	return c.emitListOrSet(e.Elts, BUILD_SET, loc(e))
}

// emitListOrSet handles the BUILD_LIST / BUILD_SET / BUILD_TUPLE-as-list
// branches by walking elts, switching to LIST_EXTEND / SET_UPDATE when
// a Starred element appears.
//
// CPython: codegen_list / codegen_set share the helper
// starunpack_helper.
func (c *Compiler) emitListOrSet(elts ast.Seq[ast.Expr], op Opcode, l ast.Pos) error {
	if !hasStarred(elts) {
		for _, e := range elts {
			if err := c.visitExpr(e); err != nil {
				return err
			}
		}
		c.addOpI(op, int32(len(elts)), l)
		return nil
	}
	// Stars present: emit BUILD_LIST 0, then for each chunk of
	// non-star elts BUILD_LIST + LIST_EXTEND, and for each star
	// LIST_EXTEND with the value. SET path uses SET_UPDATE in place
	// of LIST_EXTEND. After everything is collected, convert with
	// LIST_TO_TUPLE if the caller wanted a tuple.
	c.addOpI(BUILD_LIST, 0, l)
	pending := 0
	flush := func() {
		if pending == 0 {
			return
		}
		c.addOpI(BUILD_LIST, int32(pending), l)
		c.emitExtend(op, l)
		pending = 0
	}
	for _, elt := range elts {
		if star, ok := elt.(*ast.Starred); ok {
			flush()
			if err := c.visitExpr(star.Value); err != nil {
				return err
			}
			c.emitExtend(op, l)
			continue
		}
		if err := c.visitExpr(elt); err != nil {
			return err
		}
		pending++
	}
	flush()
	// BUILD_SET path leaves a set on the stack; BUILD_LIST path
	// leaves a list. BUILD_TUPLE branch comes through visitTuple
	// which calls CALL_INTRINSIC_1 LIST_TO_TUPLE after this returns.
	return nil
}

// emitExtend emits LIST_EXTEND or SET_UPDATE depending on whether the
// container we are building is a list or a set.
//
// CPython: Python/codegen.c codegen_subkind in starunpack_helper
func (c *Compiler) emitExtend(op Opcode, l ast.Pos) {
	if op == BUILD_SET {
		c.addOpI(SET_UPDATE, 1, l)
	} else {
		c.addOpI(LIST_EXTEND, 1, l)
	}
}

// hasStarred reports whether any expression in elts is a Starred node.
//
// CPython: Python/codegen.c starred check in starunpack_helper
func hasStarred(elts ast.Seq[ast.Expr]) bool {
	for _, e := range elts {
		if _, ok := e.(*ast.Starred); ok {
			return true
		}
	}
	return false
}

// intrinsicListToTuple is INTRINSIC_LIST_TO_TUPLE (CPython:
// Include/internal/pycore_intrinsics.h).
const intrinsicListToTuple int32 = 6

// visitDict emits a dict literal. CPython lays this out as a series
// of LOAD_CONST keys + BUILD_MAP for each contiguous run, with
// DICT_UPDATE for `**other` splat keys.
//
// CPython: Python/codegen.c:L3497 codegen_dict
func (c *Compiler) visitDict(e *ast.Dict) error {
	if len(e.Keys) != len(e.Values) {
		return fmt.Errorf("compile: Dict keys/values mismatch %d/%d",
			len(e.Keys), len(e.Values))
	}
	hasSplat := false
	for _, k := range e.Keys {
		if k == nil {
			hasSplat = true
			break
		}
	}
	if !hasSplat {
		for i, k := range e.Keys {
			if err := c.visitExpr(k); err != nil {
				return err
			}
			if err := c.visitExpr(e.Values[i]); err != nil {
				return err
			}
		}
		c.addOpI(BUILD_MAP, int32(len(e.Keys)), loc(e))
		return nil
	}
	// Splat path: BUILD_MAP 0, accumulate runs of non-splat keys,
	// then DICT_UPDATE for each `**v`.
	c.addOpI(BUILD_MAP, 0, loc(e))
	pending := 0
	flush := func() error {
		if pending == 0 {
			return nil
		}
		c.addOpI(BUILD_MAP, int32(pending), loc(e))
		c.addOpI(DICT_UPDATE, 1, loc(e))
		pending = 0
		return nil
	}
	for i, k := range e.Keys {
		if k == nil {
			if err := flush(); err != nil {
				return err
			}
			if err := c.visitExpr(e.Values[i]); err != nil {
				return err
			}
			c.addOpI(DICT_UPDATE, 1, loc(e))
			continue
		}
		if err := c.visitExpr(k); err != nil {
			return err
		}
		if err := c.visitExpr(e.Values[i]); err != nil {
			return err
		}
		pending++
	}
	return flush()
}

// visitAttribute emits a LOAD_ATTR / STORE_ATTR / DELETE_ATTR
// depending on context.
//
// CPython: Python/codegen.c Attribute case in visit_expr
func (c *Compiler) visitAttribute(e *ast.Attribute) error {
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	// Mangle private attribute names against the enclosing class. A
	// reference like `self.__x` inside a method of class Tokenizer is
	// rewritten to `self._Tokenizer__x` before reaching LOAD_ATTR.
	//
	// CPython: Python/codegen.c codegen_visit_expr (Attribute_kind, mangle branch)
	attr := symtable.Mangle(c.unit().Private, e.Attr)
	pool := poolNames
	switch e.Ctx {
	case ast.Load:
		// LOAD_ATTR oparg low bit is the "push self" hint used by
		// LOAD_METHOD; codegen leaves it clear and the flowgraph
		// optimizes it.
		c.addOpName(LOAD_ATTR, &pool, attr, loc(e))
	case ast.Store:
		c.addOpName(STORE_ATTR, &pool, attr, loc(e))
	case ast.Del:
		c.addOpName(DELETE_ATTR, &pool, attr, loc(e))
	default:
		return fmt.Errorf("compile: Attribute with unknown context %v", e.Ctx)
	}
	return nil
}

// visitSubscript emits BINARY_SUBSCR / STORE_SUBSCR / DELETE_SUBSCR.
// Slice expressions go through visitSlice and still leave a Slice
// object on the stack.
//
// CPython: Python/codegen.c:L5548 codegen_subscript
func (c *Compiler) visitSubscript(e *ast.Subscript) error {
	if err := c.visitExpr(e.Value); err != nil {
		return err
	}
	if err := c.visitExpr(e.Slice); err != nil {
		return err
	}
	switch e.Ctx {
	case ast.Load:
		// CPython 3.14 lowered BINARY_SUBSCR into BINARY_OP NB_SUBSCR.
		c.addOpI(BINARY_OP, nbSubscr, loc(e))
	case ast.Store:
		c.addOpI(STORE_SUBSCR, 0, loc(e))
	case ast.Del:
		c.addOpI(DELETE_SUBSCR, 0, loc(e))
	default:
		return fmt.Errorf("compile: Subscript with unknown context %v", e.Ctx)
	}
	return nil
}

// visitSlice emits BUILD_SLICE 2 or 3 depending on whether step is
// present. Missing lower / upper become LOAD_CONST None.
//
// CPython: Python/codegen.c codegen_slice
func (c *Compiler) visitSlice(e *ast.Slice) error {
	if err := c.visitOptExpr(e.Lower, loc(e)); err != nil {
		return err
	}
	if err := c.visitOptExpr(e.Upper, loc(e)); err != nil {
		return err
	}
	n := int32(2)
	if e.Step != nil {
		if err := c.visitExpr(e.Step); err != nil {
			return err
		}
		n = 3
	}
	c.addOpI(BUILD_SLICE, n, loc(e))
	return nil
}

// visitOptExpr visits an optional expression, emitting LOAD_CONST None
// if it is missing.
//
// CPython: Python/codegen.c optional-arg helper used by codegen_slice
func (c *Compiler) visitOptExpr(e ast.Expr, l ast.Pos) error {
	if e == nil {
		c.addLoadConst(nil, l)
		return nil
	}
	return c.visitExpr(e)
}
