// PEP 695 `type X = ...` codegen. CPython's compile_type_alias is in
// Python/codegen.c; the v0.5 port covers the non-generic shape and
// emits the value evaluator inline rather than wrapping it in a lazy
// thunk. The generic form (TypeParams non-empty) and lazy-thunk
// wrapping land alongside the runtime work in v0.6.
//
// CPython: Python/codegen.c compile_type_alias

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// intrinsicTypeAlias is INTRINSIC_TYPEALIAS.
//
// CPython: Include/internal/pycore_intrinsics.h INTRINSIC_TYPEALIAS
const intrinsicTypeAlias int32 = 12

// visitTypeAlias compiles `type Name = Value`. The shape CPython emits
// for non-generic aliases is:
//
//	LOAD_CONST <name str>
//	LOAD_CONST None
//	<value>                     ; v0.5 evaluates inline
//	CALL_INTRINSIC_1 INTRINSIC_TYPEALIAS
//	STORE_NAME Name
//
// CPython wraps the value in a lazy-evaluation function so __value__
// is computed on first access; that wrapper depends on runtime
// machinery that lands in v0.6.
//
// CPython: Python/codegen.c compile_type_alias
func (c *Compiler) visitTypeAlias(s *ast.TypeAlias) error {
	nameNode, ok := s.Name.(*ast.Name)
	if !ok {
		return fmt.Errorf("compile: TypeAlias target is %T, want *ast.Name", s.Name)
	}
	if s.TypeParams.Len() > 0 {
		return fmt.Errorf("compile: generic `type %s[...]` aliases are deferred to v0.6", nameNode.Id)
	}

	c.addLoadConst(nameNode.Id, loc(s))
	c.addLoadConst(nil, loc(s))
	if err := c.visitExpr(s.Value); err != nil {
		return err
	}
	c.addOpI(CALL_INTRINSIC_1, intrinsicTypeAlias, loc(s))
	return c.nameOpStore(nameNode.Id, loc(s))
}
