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
// CPython: Python/ast.c:52 validate_name
func validateName(id string) error {
	if _, bad := forbiddenNames[id]; bad {
		return fmt.Errorf("identifier field can't represent '%s' constant", id)
	}
	return nil
}

// validateCapture rejects "_" and reserved names used as capture targets.
//
// CPython: Python/ast.c:546 validate_capture
func validateCapture(name string) error {
	if name == "_" {
		return errors.New("can't capture name '_' in patterns")
	}
	return validateName(name)
}

// validateComprehension enforces non-empty generator lists and walks
// every component (target / iter / ifs).
//
// CPython: Python/ast.c:71 validate_comprehension
func validateComprehension(gens Seq[*Comprehension]) error {
	if gens.Len() == 0 {
		return errors.New("comprehension with no generators")
	}
	for i := 0; i < gens.Len(); i++ {
		g := gens.Get(i)
		if g == nil {
			return errors.New("None disallowed in comprehension generators")
		}
		if g.Target == nil {
			return errors.New("field 'target' is required for comprehension")
		}
		if g.Iter == nil {
			return errors.New("field 'iter' is required for comprehension")
		}
		if err := validateExpr(g.Target, Store); err != nil {
			return err
		}
		if err := validateExpr(g.Iter, Load); err != nil {
			return err
		}
		if err := validateExprs(g.Ifs, Load, false); err != nil {
			return err
		}
	}
	return nil
}

// validatePattern walks one pattern subtree.
//
// CPython: Python/ast.c:551 validate_pattern
//
//nolint:gocognit,gocyclo // mirrors CPython's per-pattern-kind switch.
func validatePattern(p Pattern, starOk bool) error {
	if p == nil {
		return errors.New("validate: nil pattern")
	}
	if err := validatePos(p.Position()); err != nil {
		return err
	}
	switch n := p.(type) {
	case *MatchValue:
		return validatePatternMatchValue(n.Value)
	case *MatchSingleton:
		switch n.Value.(type) {
		case nil:
			return nil
		case bool:
			return nil
		}
		return errors.New("MatchSingleton can only contain True, False and None")
	case *MatchSequence:
		return validatePatterns(n.Patterns, true)
	case *MatchMapping:
		if n.Keys.Len() != n.Patterns.Len() {
			return errors.New("MatchMapping doesn't have the same number of keys as patterns")
		}
		if n.Rest != nil {
			if err := validateCapture(*n.Rest); err != nil {
				return err
			}
		}
		for i := 0; i < n.Keys.Len(); i++ {
			key := n.Keys.Get(i)
			if c, ok := key.(*Constant); ok {
				switch c.Value.(type) {
				case nil, bool:
					continue
				}
			}
			if err := validatePatternMatchValue(key); err != nil {
				return err
			}
		}
		return validatePatterns(n.Patterns, false)
	case *MatchClass:
		if n.KwdAttrs.Len() != n.KwdPatterns.Len() {
			return errors.New("MatchClass doesn't have the same number of keyword attributes as patterns")
		}
		if err := validateExpr(n.Cls, Load); err != nil {
			return err
		}
		// cls must be Name or chain of Attribute.value accesses to a Name
		cls := n.Cls
		for {
			switch inner := cls.(type) {
			case *Name:
			case *Attribute:
				cls = inner.Value
				continue
			default:
				return errors.New("MatchClass cls field can only contain Name or Attribute nodes")
			}
			break
		}
		for i := 0; i < n.KwdAttrs.Len(); i++ {
			if err := validateName(n.KwdAttrs.Get(i)); err != nil {
				return err
			}
		}
		if err := validatePatterns(n.Patterns, false); err != nil {
			return err
		}
		return validatePatterns(n.KwdPatterns, false)
	case *MatchStar:
		if !starOk {
			return errors.New("can't use MatchStar here")
		}
		if n.Name != nil {
			return validateCapture(*n.Name)
		}
		return nil
	case *MatchAs:
		if n.Name != nil {
			if err := validateCapture(*n.Name); err != nil {
				return err
			}
		}
		if n.Pattern == nil {
			return nil
		}
		if n.Name == nil {
			return errors.New("MatchAs must specify a target name if a pattern is given")
		}
		return validatePattern(n.Pattern, false)
	case *MatchOr:
		if n.Patterns.Len() < 2 {
			return errors.New("MatchOr requires at least 2 patterns")
		}
		return validatePatterns(n.Patterns, false)
	}
	return fmt.Errorf("validate: unknown pattern kind %T", p)
}

// validatePatternMatchValue checks that a match value expression is a
// valid literal or attribute lookup.
//
// CPython: Python/ast.c:476 validate_pattern_match_value
//
//nolint:gocognit,gocyclo // mirrors CPython validate_pattern_match_value linear switch across expression kinds
func validatePatternMatchValue(e Expr) error {
	if err := validateExpr(e, Load); err != nil {
		return err
	}
	switch n := e.(type) {
	case *Constant:
		switch n.Value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64,
			complex64, complex128,
			string, []byte:
			return nil
		}
		return errors.New("unexpected constant inside of a literal pattern")
	case *Attribute:
		return nil
	case *UnaryOp:
		if n.Op == USub {
			if inner, ok := n.Operand.(*Constant); ok {
				switch inner.Value.(type) {
				case int, int8, int16, int32, int64,
					uint, uint8, uint16, uint32, uint64,
					float32, float64,
					complex64, complex128:
					return nil
				}
			}
		}
	case *BinOp:
		if n.Op == Add || n.Op == Sub {
			leftOk := false
			switch l := n.Left.(type) {
			case *Constant:
				switch l.Value.(type) {
				case int, int8, int16, int32, int64,
					uint, uint8, uint16, uint32, uint64,
					float32, float64:
					leftOk = true
				}
			case *UnaryOp:
				if l.Op == USub {
					if inner, ok := l.Operand.(*Constant); ok {
						switch inner.Value.(type) {
						case int, int8, int16, int32, int64,
							uint, uint8, uint16, uint32, uint64,
							float32, float64:
							leftOk = true
						}
					}
				}
			}
			rightOk := false
			if r, ok := n.Right.(*Constant); ok {
				switch r.Value.(type) {
				case complex64, complex128:
					rightOk = true
				}
			}
			if leftOk && rightOk {
				return nil
			}
		}
	case *JoinedStr, *TemplateStr:
		return nil
	}
	return errors.New("patterns may only match literals and attribute lookups")
}

// validatePatterns walks a sequence of patterns.
//
// CPython: Python/ast.c validate_patterns
func validatePatterns(seq Seq[Pattern], starOk bool) error {
	for i := 0; i < seq.Len(); i++ {
		if err := validatePattern(seq.Get(i), starOk); err != nil {
			return err
		}
	}
	return nil
}

// validateTypeParams walks a PEP 695 type-parameter list.
//
// CPython: Python/ast.c validate_type_params
//
//nolint:gocognit // mirrors CPython's TypeVar/ParamSpec/TypeVarTuple switch.
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
				if err := validateExpr(n.Bound, Load); err != nil {
					return err
				}
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue, Load); err != nil {
					return err
				}
			}
		case *ParamSpec:
			if err := validateName(n.Name); err != nil {
				return err
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue, Load); err != nil {
					return err
				}
			}
		case *TypeVarTuple:
			if err := validateName(n.Name); err != nil {
				return err
			}
			if n.DefaultValue != nil {
				if err := validateExpr(n.DefaultValue, Load); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("validate: unknown type_param kind %T", tp)
		}
	}
	return nil
}
