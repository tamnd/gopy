// Port of cpython/Python/codegen.c codegen_nameop (L3186-L3287).
// Spec: notes/Spec/1600/1626_gopy_codegen.md
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

	switch scope {
	case symtable.Local:
		return c.emitLocal(mangled, mode, l)
	case symtable.Cell, symtable.Free:
		return c.emitDeref(mangled, mode, l)
	case symtable.GlobalExplicit, symtable.GlobalImplicit:
		return c.emitGlobal(mangled, mode, l)
	case 0:
		// 0 means the analyze pass left no scope; treat as global.
		return c.emitGlobal(mangled, mode, l)
	}
	return fmt.Errorf("compile: name %q has unknown scope %v", name, scope)
}

func (c *Compiler) emitLocal(name string, mode nameMode, l ast.Pos) error {
	pool := poolVarNames
	switch c.scope.Type {
	case symtable.FunctionBlock:
		switch mode {
		case opLoad:
			c.addOpName(LOAD_FAST, &pool, name, l)
		case opStore:
			c.addOpName(STORE_FAST, &pool, name, l)
		case opDelete:
			c.addOpName(DELETE_FAST, &pool, name, l)
		}
		return nil
	case symtable.ModuleBlock, symtable.ClassBlock:
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
	return fmt.Errorf("compile: emitLocal in scope kind %v", c.scope.Type)
}

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
