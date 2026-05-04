package ast

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
)

// Validate is the gopy port of _PyAST_Validate. It walks mod and
// returns nil if the tree is well-formed, or an error matching
// CPython's ValueError/TypeError text.
//
// This is the foundation pass: covers Module/Interactive/Expression/
// FunctionType plus the node kinds emitted by future and other early
// pipeline stages (ImportFrom, ExprStmt, Constant). The full
// validator grows as the asdl-generated nodes land in nodes_gen.go.
//
// CPython: Python/ast.c:L1047 _PyAST_Validate
func Validate(mod Mod) error {
	switch m := mod.(type) {
	case *Module:
		return validateBody(m.Body)
	case *Interactive:
		return validateBody(m.Body)
	case *Expression:
		return validateExpr(m.Body)
	case *FunctionType:
		for i := 0; i < m.Argtypes.Len(); i++ {
			if err := validateExpr(m.Argtypes.Get(i)); err != nil {
				return err
			}
		}
		return validateExpr(m.Returns)
	case nil:
		return errors.New("validate: nil module")
	default:
		return fmt.Errorf("validate: unknown module kind %T", mod)
	}
}

// validateBody walks a statement sequence, validating each entry.
//
// CPython: Python/ast.c VISIT_SEQ(state, stmt, body)
func validateBody(body Seq[Stmt]) error {
	for i := 0; i < body.Len(); i++ {
		if err := validateStmt(body.Get(i)); err != nil {
			return err
		}
	}
	return nil
}

// validateStmt validates one statement.
//
// CPython: Python/ast.c:L686 validate_stmt
func validateStmt(s Stmt) error {
	if s == nil {
		return errors.New("validate: nil statement")
	}
	if err := validatePos(s.Position()); err != nil {
		return err
	}
	switch n := s.(type) {
	case *ExprStmt:
		return validateExpr(n.Value)
	case *ImportFrom:
		return validateImportFrom(n)
	case *Assign:
		for i := 0; i < n.Targets.Len(); i++ {
			if err := validateExprCtx(n.Targets.Get(i), Store); err != nil {
				return err
			}
		}
		return validateExpr(n.Value)
	case *AugAssign:
		if err := validateExprCtx(n.Target, Store); err != nil {
			return err
		}
		return validateExpr(n.Value)
	case *AnnAssign:
		if err := validateExprCtx(n.Target, Store); err != nil {
			return err
		}
		if err := validateExpr(n.Annotation); err != nil {
			return err
		}
		if n.Value != nil {
			return validateExpr(n.Value)
		}
		return nil
	case *Delete:
		for i := 0; i < n.Targets.Len(); i++ {
			if err := validateExprCtx(n.Targets.Get(i), Del); err != nil {
				return err
			}
		}
		return nil
	case *For:
		if err := validateExprCtx(n.Target, Store); err != nil {
			return err
		}
		return validateExpr(n.Iter)
	case *AsyncFor:
		if err := validateExprCtx(n.Target, Store); err != nil {
			return err
		}
		return validateExpr(n.Iter)
	case *FunctionDef:
		return validateTypeParams(n.TypeParams)
	case *AsyncFunctionDef:
		return validateTypeParams(n.TypeParams)
	case *ClassDef:
		return validateTypeParams(n.TypeParams)
	case *TypeAlias:
		if err := validateExprCtx(n.Name, Store); err != nil {
			return err
		}
		if err := validateTypeParams(n.TypeParams); err != nil {
			return err
		}
		return validateExpr(n.Value)
	case *Match:
		if err := validateExpr(n.Subject); err != nil {
			return err
		}
		for i := 0; i < n.Cases.Len(); i++ {
			cs := n.Cases.Get(i)
			if cs == nil {
				return errors.New("validate: nil match case")
			}
			if err := validatePattern(cs.Pattern); err != nil {
				return err
			}
		}
		return nil
	}
	// Other statement kinds are accepted by this v0.5 foundation
	// pass; the full visitor expands as nodes_gen.go lands.
	return nil
}

// validateExpr validates one expression.
//
// CPython: Python/ast.c:L379 validate_expr
func validateExpr(e Expr) error {
	if e == nil {
		return errors.New("validate: nil expression")
	}
	if err := validatePos(e.Position()); err != nil {
		return err
	}
	switch n := e.(type) {
	case *Constant:
		return validateConstant(n.Value)
	case *Name:
		return validateName(n.Id)
	case *Starred:
		return errors.New("can't use starred expression here")
	case *ListComp:
		if err := validateExpr(n.Elt); err != nil {
			return err
		}
		return validateComprehension(n.Generators)
	case *SetComp:
		if err := validateExpr(n.Elt); err != nil {
			return err
		}
		return validateComprehension(n.Generators)
	case *DictComp:
		if err := validateExpr(n.Key); err != nil {
			return err
		}
		if err := validateExpr(n.Value); err != nil {
			return err
		}
		return validateComprehension(n.Generators)
	case *GeneratorExp:
		if err := validateExpr(n.Elt); err != nil {
			return err
		}
		return validateComprehension(n.Generators)
	case *Call:
		// Starred is allowed inside Call args; List/Tuple/Set already
		// allow starred elts via their own validators when reachable.
		if err := validateExpr(n.Func); err != nil {
			return err
		}
		for i := 0; i < n.Args.Len(); i++ {
			arg := n.Args.Get(i)
			if _, ok := arg.(*Starred); ok {
				if err := validateExprCtx(arg, Load); err != nil {
					return err
				}
				continue
			}
			if err := validateExpr(arg); err != nil {
				return err
			}
		}
		return nil
	case *List:
		return validateLoadElts(n.Elts)
	case *Tuple:
		return validateLoadElts(n.Elts)
	case *Set:
		return validateLoadElts(n.Elts)
	}
	return nil
}

// validateLoadElts walks a List/Tuple/Set element sequence in Load
// context, allowing Starred entries (CPython parity for iterable
// unpacking inside a display).
//
// CPython: Python/ast.c validate_expr List/Tuple/Set arms
func validateLoadElts(elts Seq[Expr]) error {
	for i := 0; i < elts.Len(); i++ {
		el := elts.Get(i)
		if _, ok := el.(*Starred); ok {
			if err := validateExprCtx(el, Load); err != nil {
				return err
			}
			continue
		}
		if err := validateExpr(el); err != nil {
			return err
		}
	}
	return nil
}

// validatePos rejects negative positions except for the canonical
// "no-position" sentinel ast.NoPos. CPython encodes the same rule
// in VALIDATE_POSITIONS.
//
// CPython: Python/ast.c VALIDATE_POSITIONS
func validatePos(p Pos) error {
	if p == NoPos {
		return nil
	}
	if p.Lineno < 0 || p.ColOffset < 0 || p.EndLineno < 0 || p.EndColOffset < 0 {
		return fmt.Errorf("AST node has negative position: %+v", p)
	}
	if p.EndLineno < p.Lineno {
		return fmt.Errorf("AST node end_lineno %d < lineno %d", p.EndLineno, p.Lineno)
	}
	if p.EndLineno == p.Lineno && p.EndColOffset < p.ColOffset {
		return fmt.Errorf("AST node end_col_offset %d < col_offset %d on line %d",
			p.EndColOffset, p.ColOffset, p.Lineno)
	}
	return nil
}

// validateImportFrom rejects a negative level. CPython:
//
//	"Negative ImportFrom level"
//
// CPython: Python/ast.c:L916
func validateImportFrom(imp *ImportFrom) error {
	if imp.Level != nil && *imp.Level < 0 {
		return errors.New("Negative ImportFrom level") //nolint:staticcheck // ST1005: mirrors CPython's exact error text in Python/ast.c.
	}
	for i := 0; i < imp.Names.Len(); i++ {
		if err := validatePos(imp.Names.Get(i).Pos); err != nil {
			return err
		}
	}
	return nil
}

// validateConstant mirrors validate_constant. The accepted Go types
// for Constant.Value are the marshallable subset:
//
//   - nil           (Py_None)
//   - Ellipsis      (Py_Ellipsis; we use a sentinel below)
//   - bool
//   - int8/16/32/64, uint8/16/32/64, int, uint
//   - *big.Int      (PyLong with magnitude beyond int64)
//   - float32/64
//   - complex64/128
//   - string        (str)
//   - []byte        (bytes)
//   - tuple         (Seq[any] of validated values; we accept []any)
//   - frozenset     (FrozenSet wrapper of validated values)
//
// CPython: Python/ast.c:L156 validate_constant
func validateConstant(v any) error {
	if v == nil {
		return nil
	}
	if _, ok := v.(EllipsisType); ok {
		return nil
	}
	switch v.(type) {
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		complex64, complex128,
		string,
		[]byte:
		return nil
	case *big.Int:
		return nil
	}
	if items, ok := v.([]any); ok {
		for _, it := range items {
			if err := validateConstant(it); err != nil {
				return err
			}
		}
		return nil
	}
	if fs, ok := v.(FrozenSet); ok {
		for _, it := range fs {
			if err := validateConstant(it); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("got an invalid type in Constant: %s", reflect.TypeOf(v))
}

// EllipsisType is the singleton type used to spell Python's `...` as
// a Constant value. CPython uses the dedicated Py_Ellipsis singleton.
type EllipsisType struct{}

// Ellipsis is the canonical sentinel for `...` as a Constant value.
var Ellipsis = EllipsisType{}

// FrozenSet wraps a slice of constant items for a frozenset literal.
// CPython tracks frozenset as a real type; v0.5 only needs a marker
// so Validate can recurse.
type FrozenSet []any
