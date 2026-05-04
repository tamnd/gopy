// Port of cpython/Python/codegen.c addop helpers (L254-L461).
//
// These wrap Sequence.Addop with the dedup-into-pool semantics the
// CPython macros encode (ADDOP_LOAD_CONST, ADDOP_NAME, ADDOP_O,
// ADDOP_JUMP).

package compile

import (
	"github.com/tamnd/gopy/ast"
)

// addOp emits a no-arg opcode. Mirrors codegen_addop_noarg.
//
// CPython: Python/codegen.c:L276 codegen_addop_noarg
func (c *Compiler) addOp(op Opcode, l ast.Pos) {
	c.seq().Addop(op, 0, l)
}

// addOpI emits opcode with a literal oparg. Mirrors codegen_addop_i.
//
// CPython: Python/codegen.c:L254 codegen_addop_i
func (c *Compiler) addOpI(op Opcode, oparg int32, l ast.Pos) {
	c.seq().Addop(op, oparg, l)
}

// addLoadConst emits LOAD_CONST with the index of value in the
// per-unit consts pool. Equal-by-CPython-rules constants share a
// slot. Mirrors codegen_addop_load_const.
//
// CPython: Python/codegen.c:L290 codegen_addop_load_const
func (c *Compiler) addLoadConst(value any, l ast.Pos) {
	idx := c.constIndex(value)
	c.seq().Addop(LOAD_CONST, int32(idx), l)
}

// addOpName emits an opcode whose oparg is an index into one of the
// per-unit string pools (Names / VarNames / FreeVars / CellVars).
// Mirrors codegen_addop_name.
//
// CPython: Python/codegen.c:L354 codegen_addop_name
func (c *Compiler) addOpName(op Opcode, pool *poolKind, name string, l ast.Pos) {
	idx := c.poolIndex(pool, name)
	c.seq().Addop(op, int32(idx), l)
}

// poolKind names which per-unit string pool a name slot lives in.
type poolKind int

const (
	poolNames poolKind = iota
	poolVarNames
	poolFreeVars
	poolCellVars
)

// poolIndex returns the dedup index for name in the requested pool,
// allocating a fresh slot if needed.
//
// CPython: Python/compile.c dict_add_o (per-pool dedup helper)
func (c *Compiler) poolIndex(kind *poolKind, name string) int {
	u := c.unit()
	switch *kind {
	case poolNames:
		if i, ok := c.nameCache[name]; ok {
			return i
		}
		i := len(u.Names)
		u.Names = append(u.Names, name)
		c.nameCache[name] = i
		return i
	case poolVarNames:
		if i, ok := c.varnameCache[name]; ok {
			return i
		}
		i := len(u.VarNames)
		u.VarNames = append(u.VarNames, name)
		c.varnameCache[name] = i
		return i
	case poolFreeVars:
		if i, ok := c.freeCache[name]; ok {
			return i
		}
		i := len(u.FreeVars)
		u.FreeVars = append(u.FreeVars, name)
		c.freeCache[name] = i
		return i
	case poolCellVars:
		if i, ok := c.cellCache[name]; ok {
			return i
		}
		i := len(u.CellVars)
		u.CellVars = append(u.CellVars, name)
		c.cellCache[name] = i
		return i
	}
	panic("compile: unknown pool kind")
}

// constIndex deduplicates value into the per-unit Consts pool.
// Equality follows CPython compiler_add_const: identical values of
// the same concrete type share an index. NaN floats compare by bits;
// for now we lean on Go map equality, which uses ==. This handles
// every constant kind codegen emits except floats with NaN bits and
// nested tuples; those need stricter dedup which the flowgraph
// const-fold pass provides.
//
// CPython: Python/codegen.c:L290 codegen_addop_load_const ->
//
//	Python/compile.c compiler_add_const
func (c *Compiler) constIndex(value any) int {
	u := c.unit()
	if i, ok := c.constCache[value]; ok {
		return i
	}
	i := len(u.Consts)
	u.Consts = append(u.Consts, value)
	c.constCache[value] = i
	return i
}
