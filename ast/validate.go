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
// This is the v0.5 foundation pass: covers Module/Interactive/
// Expression/FunctionType plus the node kinds emitted by future and
// other early-stage code (ImportFrom, ExprStmt, Constant). The full
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

func validateBody(body Seq[Stmt]) error {
	for i := 0; i < body.Len(); i++ {
		if err := validateStmt(body.Get(i)); err != nil {
			return err
		}
	}
	return nil
}

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
	}
	// Other statement kinds are accepted by this v0.5 foundation
	// pass; the full visitor expands as nodes_gen.go lands.
	return nil
}

func validateExpr(e Expr) error {
	if e == nil {
		return errors.New("validate: nil expression")
	}
	if err := validatePos(e.Position()); err != nil {
		return err
	}
	if c, ok := e.(*Constant); ok {
		return validateConstant(c.Value)
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
