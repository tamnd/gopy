// Port of cpython/Python/ast.c per-kind validators (validate_name,
// validate_comprehension, validate_expr_context, validate_starred,
// validate_pattern, validate_type_param). The foundation visitor in
// validate.go dispatches to these per kind.
//
// CPython: Python/ast.c

package ast

import (
	"errors"
	"fmt"
)

// forbiddenNames is the set of keywords CPython rejects as Name.id.
// They can only appear as Constant values (Py_None, Py_True, Py_False).
//
// CPython: Python/ast.c validate_name forbidden table
var forbiddenNames = map[string]struct{}{
	"None":  {},
	"True":  {},
	"False": {},
}

// validateName rejects identifiers that collide with the reserved
// singleton spellings.
//
// CPython: Python/ast.c validate_name
func validateName(id string) error {
	if _, bad := forbiddenNames[id]; bad {
		return fmt.Errorf("identifier field can't represent '%s' constant", id)
	}
	return nil
}

// validateComprehension enforces non-empty generator lists and walks
// every component (target / iter / ifs).
//
// CPython: Python/ast.c validate_comprehension
func validateComprehension(gens Seq[*Comprehension]) error {
	if gens.Len() == 0 {
		return errors.New("comprehension with no generators")
	}
	for i := 0; i < gens.Len(); i++ {
		g := gens.Get(i)
		if g == nil {
			return errors.New("comprehension: nil generator")
		}
		if err := validateExprCtx(g.Target, Store); err != nil {
			return err
		}
		if err := validateExpr(g.Iter); err != nil {
			return err
		}
		for j := 0; j < g.Ifs.Len(); j++ {
			if err := validateExpr(g.Ifs.Get(j)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateExprCtx checks the .Ctx slot of any expression that carries
// one matches the expected context. CPython enforces the same rule via
// VALIDATE_CTX in validate_expr.
//
// CPython: Python/ast.c validate_expr / VALIDATE_CTX
func validateExprCtx(e Expr, want ExprContext) error {
	if e == nil {
		return errors.New("validate: nil expression")
	}
	switch n := e.(type) {
	case *Name:
		if err := validateName(n.Id); err != nil {
			return err
		}
		if n.Ctx != want {
			return fmt.Errorf("Name context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
	case *Attribute:
		if n.Ctx != want {
			return fmt.Errorf("Attribute context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
		return validateExpr(n.Value)
	case *Subscript:
		if n.Ctx != want {
			return fmt.Errorf("Subscript context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
		if err := validateExpr(n.Value); err != nil {
			return err
		}
		return validateExpr(n.Slice)
	case *Starred:
		if n.Ctx != want {
			return fmt.Errorf("Starred context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
		return validateExprCtx(n.Value, want)
	case *List:
		if n.Ctx != want {
			return fmt.Errorf("List context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
		return validateSeqCtx(n.Elts, want)
	case *Tuple:
		if n.Ctx != want {
			return fmt.Errorf("Tuple context %s does not match expected %s",
				n.Ctx.String(), want.String())
		}
		return validateSeqCtx(n.Elts, want)
	default:
		// Other expressions are not assignment-shaped; if a non-Store
		// context is requested for a non-target node, the caller is
		// confused.
		if want != Load {
			return fmt.Errorf("expression of type %T cannot appear in %s context",
				e, want.String())
		}
		return validateExpr(e)
	}
	return nil
}

// validateSeqCtx walks elements of a List/Tuple in the given context.
//
// CPython: Python/ast.c VISIT_SEQ over expr_context_seq
func validateSeqCtx(seq Seq[Expr], want ExprContext) error {
	for i := 0; i < seq.Len(); i++ {
		if err := validateExprCtx(seq.Get(i), want); err != nil {
			return err
		}
	}
	return nil
}

// validatePattern walks one pattern subtree.
//
// CPython: Python/ast.c validate_pattern
func validatePattern(p Pattern) error {
	if p == nil {
		return errors.New("validate: nil pattern")
	}
	if err := validatePos(p.Position()); err != nil {
		return err
	}
	switch n := p.(type) {
	case *MatchValue:
		return validateExpr(n.Value)
	case *MatchSingleton:
		switch n.Value.(type) {
		case nil:
			return nil
		case bool:
			return nil
		}
		return errors.New("MatchSingleton can only contain True, False and None")
	case *MatchSequence:
		return validatePatternSeq(n.Patterns)
	case *MatchMapping:
		if n.Keys.Len() != n.Patterns.Len() {
			return errors.New("MatchMapping doesn't have the same number of keys as patterns")
		}
		for i := 0; i < n.Keys.Len(); i++ {
			if err := validateExpr(n.Keys.Get(i)); err != nil {
				return err
			}
		}
		if n.Rest != nil {
			if err := validateName(*n.Rest); err != nil {
				return err
			}
		}
		return validatePatternSeq(n.Patterns)
	case *MatchClass:
		if n.KwdAttrs.Len() != n.KwdPatterns.Len() {
			return errors.New("MatchClass doesn't have the same number of keyword attributes as patterns")
		}
		if err := validateExpr(n.Cls); err != nil {
			return err
		}
		for i := 0; i < n.KwdAttrs.Len(); i++ {
			if err := validateName(n.KwdAttrs.Get(i)); err != nil {
				return err
			}
		}
		if err := validatePatternSeq(n.Patterns); err != nil {
			return err
		}
		return validatePatternSeq(n.KwdPatterns)
	case *MatchStar:
		if n.Name != nil {
			return validateName(*n.Name)
		}
		return nil
	case *MatchAs:
		if n.Name != nil {
			if err := validateName(*n.Name); err != nil {
				return err
			}
		}
		if n.Pattern == nil {
			if n.Name == nil {
				return errors.New("MatchAs must specify a target name if a pattern is not given")
			}
			return nil
		}
		// MatchAs with sub-pattern but no name is the bare wildcard
		// form; CPython rejects it.
		if n.Name == nil {
			return errors.New("MatchAs must specify a target name if a pattern is given")
		}
		return validatePattern(n.Pattern)
	case *MatchOr:
		if n.Patterns.Len() < 2 {
			return errors.New("MatchOr requires at least 2 patterns")
		}
		return validatePatternSeq(n.Patterns)
	}
	return fmt.Errorf("validate: unknown pattern kind %T", p)
}

// validatePatternSeq walks a sequence of patterns.
//
// CPython: Python/ast.c VISIT_SEQ over pattern_seq
func validatePatternSeq(seq Seq[Pattern]) error {
	for i := 0; i < seq.Len(); i++ {
		if err := validatePattern(seq.Get(i)); err != nil {
			return err
		}
	}
	return nil
}

// validateTypeParams walks a PEP 695 type-parameter list.
//
// CPython: Python/ast.c validate_type_params
func validateTypeParams(params Seq[TypeParam]) error {
	for i := 0; i < params.Len(); i++ {
		tp := params.Get(i)
		if tp == nil {
			return errors.New("validate: nil type_param")
		}
		if err := validatePos(tp.Position()); err != nil {
			return err
		}
		switch n := tp.(type) {
		case *TypeVar:
			if err := validateName(n.Name); err != nil {
				return err
			}
			if n.Bound != nil {
				if err := validateExpr(n.Bound); err != nil {
					return err
				}
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue); err != nil {
					return err
				}
			}
		case *ParamSpec:
			if err := validateName(n.Name); err != nil {
				return err
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue); err != nil {
					return err
				}
			}
		case *TypeVarTuple:
			if err := validateName(n.Name); err != nil {
				return err
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("validate: unknown type_param kind %T", tp)
		}
	}
	return nil
}
