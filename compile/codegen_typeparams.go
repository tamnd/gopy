// PEP 695 type-parameter list emitter. Drives the inside of the
// "<generic parameters of X>" synthetic scope that wraps generic
// classes, generic functions, and generic `type X[...] = ...` aliases.
//
// CPython: Python/codegen.c:1222 codegen_type_params

package compile

import (
	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// emitTypeParams compiles a TypeParams sequence inside the generic
// wrapper scope. The resulting stack effect is one fresh value per
// parameter, finalized into a BUILD_TUPLE of the same length. Caller
// is responsible for storing that tuple under `.type_params` (and any
// other names the surrounding codegen needs).
//
// CPython: Python/codegen.c:1222 codegen_type_params
func (c *Compiler) emitTypeParams(params ast.Seq[ast.TypeParam]) error {
	if len(params) == 0 {
		return nil
	}
	seenDefault := false
	for _, p := range params {
		l := loc(p)
		switch tp := p.(type) {
		case *ast.TypeVar:
			c.addLoadConst(tp.Name, l)
			if tp.Bound != nil {
				// Bound is compiled as a lazy thunk: TypeVar bound uses slot=0.
				if err := c.emitTypeParamDefault(tp.Bound, tp, 0, l); err != nil {
					return err
				}
				if _, isTuple := tp.Bound.(*ast.Tuple); isTuple {
					c.addOpI(CALL_INTRINSIC_2, intrinsicTypeVarWithConstraints, l)
				} else {
					c.addOpI(CALL_INTRINSIC_2, intrinsicTypeVarWithBound, l)
				}
			} else {
				c.addOpI(CALL_INTRINSIC_1, intrinsicTypeVar, l)
			}
			if tp.DefaultValue != nil {
				// TypeVar bound uses slot=0; default uses slot=1.
				if err := c.emitTypeParamDefault(tp.DefaultValue, tp, 1, l); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return c.errorAt(l, "non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		case *ast.TypeVarTuple:
			c.addLoadConst(tp.Name, l)
			c.addOpI(CALL_INTRINSIC_1, intrinsicTypeVarTuple, l)
			if tp.DefaultValue != nil {
				if err := c.emitTypeParamDefault(tp.DefaultValue, tp, 0, l); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return c.errorAt(l, "non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		case *ast.ParamSpec:
			c.addLoadConst(tp.Name, l)
			c.addOpI(CALL_INTRINSIC_1, intrinsicParamSpec, l)
			if tp.DefaultValue != nil {
				if err := c.emitTypeParamDefault(tp.DefaultValue, tp, 0, l); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return c.errorAt(l, "non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		default:
			return c.errorAt(l, "unknown type param kind %T", p)
		}
	}
	c.addOpI(BUILD_TUPLE, int32(len(params)), loc(params[0]))
	return nil
}

// emitTypeParamDefault compiles a type-param default expression as a
// lazy thunk closure. The thunk is pushed on the stack; the caller then
// passes it to CALL_INTRINSIC_2 INTRINSIC_SET_TYPEPARAM_DEFAULT.
//
// The TypeVariableBlock for this default was pre-populated by the
// symtable build pass (visitTypeParamSubexpr). We enter it, emit the
// expression and RETURN_VALUE, then assemble the closure.
//
// CPython: Python/codegen.c:1195 codegen_type_param_bound_or_default
func (c *Compiler) emitTypeParamDefault(expr ast.Expr, astNode any, slot int, l ast.Pos) error {
	innerScope := c.Symtable.LookupTypeParamSubexpr(astNode, slot)
	if innerScope == nil {
		// Fallback: emit eagerly (no TypeVariableBlock found). This path
		// should not be reached in well-formed input.
		return c.visitExpr(expr)
	}

	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	// CPython loads a (1,) defaults tuple in the outer scope before the
	// closure const, then makes the closure with MAKE_FUNCTION_DEFAULTS so
	// the lazy thunk's lone "format" parameter defaults to 1 (VALUE).
	//
	// CPython: Python/codegen.c:1199 codegen_type_param_bound_or_default
	// (PyTuple_Pack(1, _PyLong_GetOne())). Emit the prebuilt (1,) const so
	// it lands in co_consts ahead of the thunk code objects, matching
	// CPython's const ordering.
	c.addLoadConst(&ConstTuple{Values: []any{int64(1)}}, l)

	closureFlag, err := c.emitClosure(innerScope, l)
	if err != nil {
		return err
	}

	c.enterScope(innerScope)
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	// The thunk takes a single positional-only "format" argument and
	// raises NotImplementedError for format > VALUE_WITH_FAKE_GLOBALS (2),
	// matching codegen_setup_annotations_scope. annotationlib drives the
	// FORWARDREF/STRING paths through the higher format values.
	//
	// CPython: Python/codegen.c:666 codegen_setup_annotations_scope
	c.declareArg("format")
	guardBody := c.newLabel()
	c.addOpI(LOAD_FAST, 0, l)
	c.addLoadConst(int64(2), l) // VALUE_WITH_FAKE_GLOBALS
	c.addOpI(COMPARE_OP, int32(cmpGt), l)
	c.addOpJump(POP_JUMP_IF_FALSE, guardBody, l)
	c.addOpI(LOAD_COMMON_CONSTANT, constantNotImplementedError, l)
	c.addOpI(RAISE_VARARGS, 1, l)
	c.useLabel(guardBody)

	// For starred TypeVarTuple defaults (*U), unwrap the starred value.
	if starExpr, ok := expr.(*ast.Starred); ok {
		if innerScope.Type == symtable.TypeVariableBlock {
			if err := c.visitExpr(starExpr.Value); err != nil {
				c.leaveScope()
				c.scope = outerScope
				c.fblocks = outerFblocks
				c.restoreCaches(outerCaches)
				return err
			}
			c.addOpI(UNPACK_SEQUENCE, 1, l)
		} else {
			if err := c.visitExpr(expr); err != nil {
				c.leaveScope()
				c.scope = outerScope
				c.fblocks = outerFblocks
				c.restoreCaches(outerCaches)
				return err
			}
		}
	} else {
		if err := c.visitExpr(expr); err != nil {
			c.leaveScope()
			c.scope = outerScope
			c.fblocks = outerFblocks
			c.restoreCaches(outerCaches)
			return err
		}
	}
	c.addOp(RETURN_VALUE, l)

	innerUnit := c.unit()
	// "format" is positional-only, like the __annotate__ scope.
	// CPython: Python/codegen.c:669 codegen_setup_annotations_scope
	innerUnit.Argcount = 0
	innerUnit.PosOnlyArgCount = 1
	innerUnit.KwOnlyArgCount = 0

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, l)
	// MAKE_FUNCTION_DEFAULTS (0x01) attaches the (1,) defaults tuple so the
	// thunk's "format" parameter defaults to 1 (VALUE).
	c.emitMakeFunction(closureFlag|0x01, l)
	return nil
}
