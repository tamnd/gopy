// Port of cpython/Python/codegen.c codegen_nameop (L3186-L3287).
//
// nameOp{Load,Store,Delete} pick the right opcode family based on
// the name's resolved scope in the symtable: LOAD_FAST for function
// locals, LOAD_DEREF for cells and free vars, LOAD_GLOBAL for
// explicit/implicit globals, LOAD_NAME for module and class scopes.

package compile

import (
	"fmt"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/symtable"
)

// nameOpLoad emits the load form for a Name reference.
//
// CPython: Python/codegen.c:L3186 codegen_nameop ctx=Load
func (c *Compiler) nameOpLoad(name string, l ast.Pos) error {
	return c.nameOp(name, opLoad, l)
}

// nameOpStore emits the store form.
//
// CPython: Python/codegen.c:L3186 codegen_nameop ctx=Store
func (c *Compiler) nameOpStore(name string, l ast.Pos) error {
	return c.nameOp(name, opStore, l)
}

// nameOpDelete emits the delete form.
//
// CPython: Python/codegen.c:L3186 codegen_nameop ctx=Del
func (c *Compiler) nameOpDelete(name string, l ast.Pos) error {
	return c.nameOp(name, opDelete, l)
}

// nameMode picks among the load / store / delete variants.
type nameMode int

const (
	opLoad nameMode = iota
	opStore
	opDelete
)

// nameOp picks the (opcode, pool) pair based on the name's symtable
// scope. Mirrors the dispatch table in codegen_nameop.
//
// CPython: Python/codegen.c:L3186 codegen_nameop
func (c *Compiler) nameOp(name string, mode nameMode, l ast.Pos) error {
	mangled := symtable.MaybeMangle(c.scope.Name, c.scope, name)
	scope := c.scope.GetScope(mangled)
	inFunc := c.scope.Type == symtable.FunctionBlock

	switch scope {
	case symtable.Local:
		if inFunc {
			return c.emitFastLocal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	case symtable.Cell, symtable.Free:
		return c.emitDeref(mangled, mode, l)
	case symtable.GlobalExplicit:
		return c.emitGlobal(mangled, mode, l)
	case symtable.GlobalImplicit:
		// At function scope, GlobalImplicit prints LOAD_GLOBAL; at
		// module/class scope, the same scope value resolves to
		// LOAD_NAME because the namespace is the module dict.
		if inFunc {
			return c.emitGlobal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	case 0:
		// 0 means the analyze pass left no scope. CPython treats
		// this as implicit global, so apply the same module / class
		// distinction.
		if inFunc {
			return c.emitGlobal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	}
	return fmt.Errorf("compile: name %q has unknown scope %v", name, scope)
}

// emitFastLocal emits LOAD_FAST / STORE_FAST / DELETE_FAST against
// the per-unit varnames pool. Used inside FunctionBlock only.
//
// CPython: Python/codegen.c LOAD_FAST/STORE_FAST/DELETE_FAST branch
// in codegen_nameop
func (c *Compiler) emitFastLocal(name string, mode nameMode, l ast.Pos) error {
	pool := poolVarNames
	switch mode {
	case opLoad:
		c.addOpName(LOAD_FAST, &pool, name, l)
	case opStore:
		c.addOpName(STORE_FAST, &pool, name, l)
	case opDelete:
		c.addOpName(DELETE_FAST, &pool, name, l)
	}
	return nil
}

// emitNamed emits LOAD_NAME / STORE_NAME / DELETE_NAME against the
// per-unit names pool. Used at module and class scope.
//
// CPython: Python/codegen.c LOAD_NAME/STORE_NAME/DELETE_NAME branch
// in codegen_nameop
func (c *Compiler) emitNamed(name string, mode nameMode, l ast.Pos) error {
	pool := poolNames
	switch mode {
	case opLoad:
		c.addOpName(LOAD_NAME, &pool, name, l)
	case opStore:
		c.addOpName(STORE_NAME, &pool, name, l)
	case opDelete:
		c.addOpName(DELETE_NAME, &pool, name, l)
	}
	return nil
}

// emitDeref emits LOAD_DEREF / STORE_DEREF / DELETE_DEREF for cells
// and free variables.
//
// CPython: Python/codegen.c Cell/Free branch in codegen_nameop
func (c *Compiler) emitDeref(name string, mode nameMode, l ast.Pos) error {
	scope := c.scope.GetScope(name)
	pool := poolCellVars
	if scope == symtable.Free {
		pool = poolFreeVars
	}
	switch mode {
	case opLoad:
		c.addOpName(LOAD_DEREF, &pool, name, l)
	case opStore:
		c.addOpName(STORE_DEREF, &pool, name, l)
	case opDelete:
		c.addOpName(DELETE_DEREF, &pool, name, l)
	}
	return nil
}

// emitGlobal emits LOAD_GLOBAL / STORE_GLOBAL / DELETE_GLOBAL.
//
// CPython: Python/codegen.c GlobalExplicit/GlobalImplicit branch in
// codegen_nameop
func (c *Compiler) emitGlobal(name string, mode nameMode, l ast.Pos) error {
	pool := poolNames
	switch mode {
	case opLoad:
		// LOAD_GLOBAL takes a 1-bit "push null" flag in bit 0 of
		// the oparg for super-instructions; codegen leaves the bit
		// off and the flowgraph sets it during super-instruction
		// fixup. CPython: codegen_nameop comment about the bit.
		c.addOpName(LOAD_GLOBAL, &pool, name, l)
	case opStore:
		c.addOpName(STORE_GLOBAL, &pool, name, l)
	case opDelete:
		c.addOpName(DELETE_GLOBAL, &pool, name, l)
	}
	return nil
}
