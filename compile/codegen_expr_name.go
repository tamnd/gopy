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
// CPython: Python/codegen.c:3186 codegen_nameop
func (c *Compiler) nameOp(name string, mode nameMode, l ast.Pos) error {
	// Mangle against the enclosing class name carried on the active
	// unit. Inside a method, the symtable already records names like
	// `__x` as `_ClassName__x`, so the codegen lookup must mangle
	// with the same private to find the symbol.
	//
	// CPython: Python/codegen.c codegen_nameop (compiler_mangle)
	mangled := symtable.MaybeMangle(c.unit().Private, c.scope, name)
	scope := c.scope.GetScope(mangled)
	inFunc := c.scope.Type == symtable.FunctionBlock
	canSeeClass := c.scope.CanSeeClassScope

	switch scope {
	case symtable.Local:
		if inFunc {
			return c.emitFastLocal(mangled, mode, l)
		}
		// Non-function scope (module or class body). A name that has been
		// temporarily promoted to a fast local while emitting an inlined
		// comprehension (FastHidden value True) uses FAST ops so the
		// comprehension's locals stay isolated from the enclosing
		// namespace; otherwise module/class locals use NAME ops.
		//
		// CPython: Python/compile.c:986 compiler_nameop LOCAL optype
		if active, ok := c.unit().FastHidden[mangled]; ok && active {
			return c.emitFastLocal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	case symtable.Cell, symtable.Free:
		// In a CanSeeClassScope block (type alias body, generic scope),
		// a deref load uses __classdict__ + LOAD_FROM_DICT_OR_DEREF so
		// the class dict shadows the closure cell, mirroring CPython's
		// "ste_can_see_class_scope" DEREF arm.
		//
		// CPython: Python/codegen.c:3225 codegen_load_classdict_freevar arm
		if canSeeClass && mode == opLoad {
			if err := c.emitLoadClassdict(l); err != nil {
				return err
			}
			return c.emitDerefFromDict(mangled, l)
		}
		return c.emitDeref(mangled, mode, l)
	case symtable.GlobalExplicit:
		return c.emitGlobal(mangled, mode, l)
	case symtable.GlobalImplicit:
		// At function scope, GlobalImplicit emits LOAD_GLOBAL; at
		// module/class scope it resolves to LOAD_NAME.
		// In a CanSeeClassScope block, loads go through __classdict__
		// + LOAD_FROM_DICT_OR_GLOBALS so class members are visible.
		//
		// CPython: Python/codegen.c:3251 ste_can_see_class_scope GLOBAL_IMPLICIT arm
		if canSeeClass && mode == opLoad {
			if err := c.emitLoadClassdict(l); err != nil {
				return err
			}
			pool := poolNames
			c.addOpName(LOAD_FROM_DICT_OR_GLOBALS, &pool, mangled, l)
			return nil
		}
		if inFunc {
			return c.emitGlobal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	case 0:
		// 0 means the analyze pass left no scope. CPython treats
		// this as implicit global, so apply the same module / class
		// distinction.
		if canSeeClass && mode == opLoad {
			if err := c.emitLoadClassdict(l); err != nil {
				return err
			}
			pool := poolNames
			c.addOpName(LOAD_FROM_DICT_OR_GLOBALS, &pool, mangled, l)
			return nil
		}
		if inFunc {
			return c.emitGlobal(mangled, mode, l)
		}
		return c.emitNamed(mangled, mode, l)
	}
	return fmt.Errorf("compile: name %q has unknown scope %v", name, scope)
}

// emitLoadClassdict emits LOAD_DEREF __classdict__ from freevars,
// used as the prefix for LOAD_FROM_DICT_OR_* in CanSeeClassScope blocks.
//
// CPython: Python/codegen.c:3179 codegen_load_classdict_freevar
func (c *Compiler) emitLoadClassdict(l ast.Pos) error {
	pool := poolFreeVars
	idx := c.poolIndex(&pool, "__classdict__")
	idx += len(c.unit().CellVars)
	c.seq().Addop(LOAD_DEREF, int32(idx), l)
	return nil
}

// emitDerefFromDict emits LOAD_FROM_DICT_OR_DEREF for the named variable,
// used after emitLoadClassdict in CanSeeClassScope blocks.
//
// CPython: Python/codegen.c:3225 LOAD_FROM_DICT_OR_DEREF arm
func (c *Compiler) emitDerefFromDict(name string, l ast.Pos) error {
	scope := c.scope.GetScope(name)
	pool := poolCellVars
	if scope == symtable.Free {
		pool = poolFreeVars
	}
	idx := c.poolIndex(&pool, name)
	if scope == symtable.Free {
		idx += len(c.unit().CellVars)
	}
	c.seq().Addop(LOAD_FROM_DICT_OR_DEREF, int32(idx), l)
	return nil
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
		// A NAME-optype load emitted from inside an inlined comprehension
		// folded into a class body uses LOAD_GLOBAL instead of LOAD_NAME:
		// the comprehension does not see the class namespace, so a name
		// that is not local to the comprehension resolves against globals
		// rather than the class dict.
		//
		// CPython: Python/codegen.c codegen_nameop COMPILE_OP_NAME Load
		// (ste_type == ClassBlock && _PyCompile_IsInInlinedComp -> LOAD_GLOBAL)
		if c.scope.Type == symtable.ClassBlock && c.unit().InInlinedComp > 0 {
			c.addOpName(LOAD_GLOBAL, &pool, name, l)
		} else {
			c.addOpName(LOAD_NAME, &pool, name, l)
		}
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
// The runtime treats the oparg as the deref-space index (cells first,
// then frees) so the LOAD_DEREF dispatch can index LocalsPlus uniformly
// with NLocals + oparg. Cell vars use their CellVars pool index; free
// vars use len(CellVars) + FreeVars pool index. This matches what
// emitClosure does for LOAD_CLOSURE and what CPython's fix_cell_offsets
// rewrites in flowgraph.c:3844 (where the codegen first records raw
// pool indices and the offset fix-up pass collapses them to deref
// space).
//
// CPython: Python/codegen.c Cell/Free branch in codegen_nameop
// CPython: Python/flowgraph.c:3844 fix_cell_offsets
func (c *Compiler) emitDeref(name string, mode nameMode, l ast.Pos) error {
	scope := c.scope.GetScope(name)
	pool := poolCellVars
	if scope == symtable.Free {
		pool = poolFreeVars
	}
	idx := c.poolIndex(&pool, name)
	if scope == symtable.Free {
		idx += len(c.unit().CellVars)
	}
	var op Opcode
	switch mode {
	case opLoad:
		// In a class body, a deref load goes through
		// LOAD_LOCALS + LOAD_FROM_DICT_OR_DEREF so a class-level
		// `locals()["x"] = ...` can shadow the closure cell. A comprehension
		// inlined into the class body is no longer executing the class
		// namespace, so its deref loads use plain LOAD_DEREF; the
		// in-inlined-comp guard skips the class-dict indirection for them.
		//
		// CPython: Python/codegen.c:3215 codegen_nameop (ClassBlock arm,
		// gated on !_PyCompile_IsInInlinedComp)
		if c.scope.Type == symtable.ClassBlock && c.unit().InInlinedComp == 0 {
			c.seq().Addop(LOAD_LOCALS, 0, l)
			op = LOAD_FROM_DICT_OR_DEREF
		} else {
			op = LOAD_DEREF
		}
	case opStore:
		op = STORE_DEREF
	case opDelete:
		op = DELETE_DEREF
	}
	c.seq().Addop(op, int32(idx), l)
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
