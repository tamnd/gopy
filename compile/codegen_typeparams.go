// PEP 695 type-parameter list emitter. Drives the inside of the
// "<generic parameters of X>" synthetic scope that wraps generic
// classes, generic functions, and generic `type X[...] = ...` aliases.
//
// CPython: Python/codegen.c:1222 codegen_type_params

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
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
				if err := c.visitExpr(tp.Bound); err != nil {
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
				if err := c.visitExpr(tp.DefaultValue); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return fmt.Errorf("non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		case *ast.TypeVarTuple:
			c.addLoadConst(tp.Name, l)
			c.addOpI(CALL_INTRINSIC_1, intrinsicTypeVarTuple, l)
			if tp.DefaultValue != nil {
				if err := c.visitExpr(tp.DefaultValue); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return fmt.Errorf("non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		case *ast.ParamSpec:
			c.addLoadConst(tp.Name, l)
			c.addOpI(CALL_INTRINSIC_1, intrinsicParamSpec, l)
			if tp.DefaultValue != nil {
				if err := c.visitExpr(tp.DefaultValue); err != nil {
					return err
				}
				c.addOpI(CALL_INTRINSIC_2, intrinsicSetTypeParamDefault, l)
				seenDefault = true
			} else if seenDefault {
				return fmt.Errorf("non-default type parameter '%s' follows default type parameter", tp.Name)
			}
			c.addOpI(COPY, 1, l)
			if err := c.nameOpStore(tp.Name, l); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compile: unknown type param kind %T", p)
		}
	}
	c.addOpI(BUILD_TUPLE, int32(len(params)), loc(params[0]))
	return nil
}
