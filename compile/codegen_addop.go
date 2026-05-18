// Port of cpython/Python/codegen.c addop helpers (L254-L461).
//
// These wrap Sequence.Addop with the dedup-into-pool semantics the
// CPython macros encode (ADDOP_LOAD_CONST, ADDOP_NAME, ADDOP_O,
// ADDOP_JUMP).

package compile

import (
	"fmt"
	"math"
	"strings"

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
// LOAD_ATTR / LOAD_GLOBAL / LOAD_SUPER_ATTR pack an extra low bit
// into oparg (the "push NULL self slot" / "method form" bit); the
// real name index lives in bits 1+. The bit itself is left zero
// here. The flowgraph fixes it up later when a following CALL turns
// the load + call into a method-style call.
//
// CPython: Python/codegen.c:L354 codegen_addop_name
func (c *Compiler) addOpName(op Opcode, pool *poolKind, name string, l ast.Pos) {
	idx := c.poolIndex(pool, name)
	arg := int32(idx)
	switch op {
	case LOAD_ATTR, LOAD_GLOBAL, LOAD_SUPER_ATTR:
		arg <<= 1
	}
	c.seq().Addop(op, arg, l)
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
	key := constCacheKey(value)
	if i, ok := c.constCache[key]; ok {
		return i
	}
	i := len(u.Consts)
	u.Consts = append(u.Consts, value)
	c.constCache[key] = i
	return i
}

// constCacheKey returns a hashable key that distinguishes constants
// by CPython's _PyCode_ConstantKey rules so equal-by-value constants
// of the same Python type collapse to one consts slot.
//
// The encoding is a string so tuples can recurse into their items
// without needing Go-comparable slice types. Each leaf is tagged with
// a one-character type code; tuples wrap their items in `(...)`.
// CPython packs (type, op) for bool/bytes/float/complex specifically
// to keep them distinct from objects that would otherwise compare
// equal (True == 1, b"x" might warn vs "x", -0.0 == 0.0); the same
// type-tag prefix gives that here.
//
// CPython: Objects/codeobject.c:3035 _PyCode_ConstantKey
func constCacheKey(value any) any {
	var b strings.Builder
	appendConstKey(&b, value)
	return b.String()
}

func appendConstKey(b *strings.Builder, value any) {
	switch x := value.(type) {
	case nil:
		b.WriteString("N")
	case bool:
		if x {
			b.WriteString("B1")
		} else {
			b.WriteString("B0")
		}
	case int:
		fmt.Fprintf(b, "i%d;", x)
	case int32:
		fmt.Fprintf(b, "i%d;", x)
	case int64:
		fmt.Fprintf(b, "i%d;", x)
	case uint64:
		fmt.Fprintf(b, "u%d;", x)
	case float64:
		// CPython distinguishes -0.0 from 0.0 via an extra slot
		// (PyTuple_Pack(3, type, op, Py_None)); float bits give that
		// for free since -0.0 has a different bit pattern.
		fmt.Fprintf(b, "f%x;", math.Float64bits(x))
	case float32:
		fmt.Fprintf(b, "F%x;", math.Float32bits(x))
	case complex128:
		// CPython tags each (real-negzero, imag-negzero) combination
		// with True / False / None to keep all four complex zeros
		// distinct; bit-pattern encoding captures the same.
		fmt.Fprintf(b, "c%x:%x;", math.Float64bits(real(x)), math.Float64bits(imag(x)))
	case string:
		fmt.Fprintf(b, "s%d:%s", len(x), x)
	case []byte:
		fmt.Fprintf(b, "y%d:", len(x))
		b.Write(x)
	case *ConstTuple:
		b.WriteString("(")
		for _, item := range x.Values {
			appendConstKey(b, item)
		}
		b.WriteString(")")
	default:
		// Inner code units and any other reference-typed value get a
		// pointer-identity tag so two distinct objects never collide
		// even if they share a structural representation. This
		// matches CPython's fallback branch which uses the object id.
		fmt.Fprintf(b, "p%T:%p;", value, value)
	}
}
