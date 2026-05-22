// PEP 695 `type X = ...` codegen. Mirrors CPython's codegen_typealias.
//
// CPython: Python/codegen.c:1727 codegen_typealias
// CPython: Python/codegen.c:1700 codegen_typealias_body

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
)

// intrinsicTypeAlias is INTRINSIC_TYPEALIAS. Numbering matches
// pycore_intrinsics.h so the VM dispatch table indexes line up.
//
// CPython: Include/internal/pycore_intrinsics.h INTRINSIC_TYPEALIAS
const intrinsicTypeAlias int32 = 11

// visitTypeAlias compiles `type Name = Value` and the generic form
// `type Name[...] = Value`. The non-generic emission shape is:
//
//	LOAD_CONST <name>
//	LOAD_CONST None
//	<value-thunk closure>           ; function that returns Value on call
//	BUILD_TUPLE 3
//	CALL_INTRINSIC_1 INTRINSIC_TYPEALIAS
//	STORE_NAME Name
//
// The generic emission wraps the whole sequence (with the type-params
// tuple swapped in for None) inside a "<generic parameters of Name>"
// function scope, then CALL 0s that wrapper in the outer scope.
//
// CPython: Python/codegen.c:1727 codegen_typealias
func (c *Compiler) visitTypeAlias(s *ast.TypeAlias) error {
	nameNode, ok := s.Name.(*ast.Name)
	if !ok {
		return fmt.Errorf("compile: TypeAlias target is %T, want *ast.Name", s.Name)
	}
	if len(s.TypeParams) > 0 {
		if err := c.emitGenericTypeAlias(s, nameNode.Id); err != nil {
			return err
		}
		return c.nameOpStore(nameNode.Id, loc(s))
	}

	c.addLoadConst(nameNode.Id, loc(s))
	c.addLoadConst(nil, loc(s))
	if err := c.emitTypeAliasBody(s); err != nil {
		return err
	}
	c.addOpI(BUILD_TUPLE, 3, loc(s))
	c.addOpI(CALL_INTRINSIC_1, intrinsicTypeAlias, loc(s))
	return c.nameOpStore(nameNode.Id, loc(s))
}

// emitGenericTypeAlias wraps the alias build inside the synthetic
// "<generic parameters of X>" wrapper scope.
//
// CPython: Python/codegen.c:1727 codegen_typealias (is_generic arm)
func (c *Compiler) emitGenericTypeAlias(s *ast.TypeAlias, name string) error {
	wrapperScope := c.Symtable.LookupTypeParams(s)
	if wrapperScope == nil {
		return fmt.Errorf("compile: no TypeParametersBlock for type alias %q", name)
	}

	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	c.enterScope(wrapperScope)
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	c.addLoadConst(name, loc(s))
	if err := c.emitTypeParams(s.TypeParams); err != nil {
		return err
	}
	if err := c.emitTypeAliasBody(s); err != nil {
		return err
	}
	c.addOpI(BUILD_TUPLE, 3, loc(s))
	c.addOpI(CALL_INTRINSIC_1, intrinsicTypeAlias, loc(s))
	c.addOp(RETURN_VALUE, loc(s))

	wrapperUnit := c.unit()
	wrapperUnit.Name = "<generic parameters of " + name + ">"

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	closureFlag, err := c.emitClosure(wrapperScope, loc(s))
	if err != nil {
		return err
	}
	c.addLoadConst(wrapperUnit, loc(s))
	c.emitMakeFunction(closureFlag, loc(s))
	c.addOp(PUSH_NULL, loc(s))
	c.addOpI(CALL, 0, loc(s))
	return nil
}

// emitTypeAliasBody emits the lazy compute_value function. The inner
// scope is the TypeAliasBlock keyed by the TypeAlias node. The body
// just evaluates Value and returns it (CPython adds a .format
// PEP 649 short-circuit, but the alias path never returns annotation
// formats, so the gate-relevant slice keeps the simpler shape).
//
// CPython: Python/codegen.c:1700 codegen_typealias_body
func (c *Compiler) emitTypeAliasBody(s *ast.TypeAlias) error {
	innerScope := c.Symtable.Lookup(s)
	if innerScope == nil {
		return fmt.Errorf("compile: no TypeAliasBlock for type alias")
	}
	outerScope := c.scope
	outerFblocks := c.fblocks
	outerCaches := c.savedCaches()

	closureFlag, err := c.emitClosure(innerScope, loc(s))
	if err != nil {
		return err
	}

	c.enterScope(innerScope)
	first := c.unit().FirstLineno
	c.addOpI(RESUME, 0, ast.Pos{Lineno: first, EndLineno: first})

	if err := c.visitExpr(s.Value); err != nil {
		return err
	}
	c.addOp(RETURN_VALUE, loc(s))

	innerUnit := c.unit()
	if nameNode, ok := s.Name.(*ast.Name); ok {
		innerUnit.Name = nameNode.Id
	}

	c.leaveScope()
	c.scope = outerScope
	c.fblocks = outerFblocks
	c.restoreCaches(outerCaches)

	c.addLoadConst(innerUnit, loc(s))
	c.emitMakeFunction(closureFlag, loc(s))
	return nil
}
