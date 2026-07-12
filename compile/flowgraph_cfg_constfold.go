// Constant folding for the CFG optimizer: get_const_loading_instrs,
// nop_out, the const_folding_safe_* guards, and eval_const_binop /
// eval_const_unaryop. The evaluator drives the real runtime number and
// object protocols (objects.Number*, objects.GetItem) against the
// native const-pool values so float, complex, str, bytes, tuple, and
// subscript folds match CPython, not just the int64 cases.
//
// CPython: Python/flowgraph.c:1367 get_const_loading_instrs ff.

package compile

import (
	"math/big"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/objects" //nolint:depguard // const folding evaluates the real number/object protocols
)

// const_folding size guards.
//
// CPython: Python/flowgraph.c:1690 MAX_INT_SIZE ff.
const (
	maxIntSize        = 128  // bits
	maxCollectionSize = 256  // items
	maxStrSize        = 4096 // characters
	maxTotalItems     = 1024 // including nested collections
)

// cfgGetConstLoadingInstrs walks backward from start collecting size
// const-loading instructions, skipping NOPs. Returns the indices in
// operand order (instrs[0] is the first operand pushed) and ok=false if
// any non-NOP, non-const instruction is hit before size are found.
//
// CPython: Python/flowgraph.c:1367 get_const_loading_instrs
func cfgGetConstLoadingInstrs(bb *basicblock, start, size int) ([]int, bool) {
	if size < 0 {
		return nil, false
	}
	idxs := make([]int, size)
	remaining := size
	for s := start; s >= 0 && remaining > 0; s-- {
		ins := &bb.Instr[s]
		if ins.Op == NOP {
			continue
		}
		if _, ok := cfgLoadsConstValue(ins, nil); !ok && ins.Op != LOAD_CONST && ins.Op != LOAD_SMALL_INT {
			return nil, false
		}
		remaining--
		idxs[remaining] = s
	}
	if remaining != 0 {
		return nil, false
	}
	return idxs, true
}

// cfgNopOut turns each listed instruction into a NOP at NO_LOCATION.
//
// CPython: Python/flowgraph.c:1391 nop_out
func cfgNopOut(bb *basicblock, idxs []int) {
	for _, k := range idxs {
		bb.Instr[k].Op = NOP
		bb.Instr[k].Oparg = 0
		bb.Instr[k].Target = nil
		bb.Instr[k].Loc = noLocation
	}
}

// cfgConstToObject lifts a native const-pool value into the Python
// object the folding protocols operate on. ok=false for shapes that are
// never const-foldable operands (nested code objects, unknown types).
func cfgConstToObject(v any) (objects.Object, bool) {
	switch x := v.(type) {
	case nil:
		return objects.None(), true
	case bool:
		return objects.NewBool(x), true
	case int64:
		return objects.NewInt(x), true
	case int:
		return objects.NewInt(int64(x)), true
	case int32:
		return objects.NewInt(int64(x)), true
	case *big.Int:
		return objects.NewIntFromBig(x), true
	case float64:
		return objects.NewFloat(x), true
	case complex128:
		return objects.NewComplex(real(x), imag(x)), true
	case string:
		return objects.NewStr(x), true
	case []byte:
		return objects.NewBytes(x), true
	case ast.EllipsisType:
		return objects.Ellipsis(), true
	case *ConstTuple:
		items := make([]objects.Object, len(x.Values))
		for i, e := range x.Values {
			o, ok := cfgConstToObject(e)
			if !ok {
				return nil, false
			}
			items[i] = o
		}
		return objects.NewTuple(items), true
	case *ConstSlice:
		start, ok := cfgConstToObject(x.Start)
		if !ok {
			return nil, false
		}
		stop, ok := cfgConstToObject(x.Stop)
		if !ok {
			return nil, false
		}
		step, ok := cfgConstToObject(x.Step)
		if !ok {
			return nil, false
		}
		return objects.NewSlice(start, stop, step), true
	case ast.FrozenSet:
		items := make([]objects.Object, len(x))
		for i, e := range x {
			o, ok := cfgConstToObject(e)
			if !ok {
				return nil, false
			}
			items[i] = o
		}
		fs, err := objects.NewFrozenset(items)
		if err != nil {
			return nil, false
		}
		return fs, true
	case objects.Object:
		return x, true
	}
	return nil, false
}

// cfgObjectToConst is the inverse of cfgConstToObject: it turns a folded
// result object back into the native const-pool value. ok=false for
// objects that cannot live in the const pool.
func cfgObjectToConst(o objects.Object) (any, bool) {
	switch x := o.(type) {
	case *objects.Bool:
		v, _ := x.Int64()
		return v != 0, true
	case *objects.Int:
		if v, ok := x.Int64(); ok {
			return v, true
		}
		return x.BigInt(), true
	case *objects.Float:
		return x.Float64(), true
	case *objects.Complex:
		return x.Complex128(), true
	case *objects.Unicode:
		return x.Value(), true
	case *objects.Bytes:
		return x.Bytes(), true
	case *objects.Tuple:
		vals := make([]any, x.Len())
		for i := 0; i < x.Len(); i++ {
			c, ok := cfgObjectToConst(x.Item(i))
			if !ok {
				return nil, false
			}
			vals[i] = c
		}
		return &ConstTuple{Values: vals}, true
	}
	if objects.IsNone(o) {
		return nil, true
	}
	if o == objects.Ellipsis() {
		return ast.EllipsisType{}, true
	}
	if x, ok := o.(*objects.Set); ok && o.Type() == objects.FrozensetType {
		items := x.Items()
		vals := make(ast.FrozenSet, len(items))
		for i, it := range items {
			c, ok := cfgObjectToConst(it)
			if !ok {
				return nil, false
			}
			vals[i] = c
		}
		return vals, true
	}
	return nil, false
}

// objAsInt returns the *objects.Int when o is an int (but not a bool, to
// mirror PyLong_Check excluding the seq*int guards never seeing bools as
// counts in practice; bool is a subtype so PyLong_Check is true, kept).
func objAsInt(o objects.Object) (*objects.Int, bool) {
	switch x := o.(type) {
	case *objects.Bool:
		return &x.Int, true
	case *objects.Int:
		return x, true
	}
	return nil, false
}

// objNumBits returns the bit length of |v| for an int object.
//
// CPython: Objects/longobject.c _PyLong_NumBits
func objNumBits(i *objects.Int) int64 {
	if v, ok := i.Int64(); ok {
		if v < 0 {
			v = -v
		}
		return int64(bigLen64(uint64(v)))
	}
	b := new(big.Int).Abs(i.BigInt())
	return int64(b.BitLen())
}

func bigLen64(v uint64) int {
	n := 0
	for v > 0 {
		v >>= 1
		n++
	}
	return n
}

func objSeqLen(o objects.Object) (int, bool) {
	switch x := o.(type) {
	case *objects.Tuple:
		return x.Len(), true
	case *objects.Unicode:
		return x.Length(), true
	case *objects.Bytes:
		return x.Len(), true
	}
	return 0, false
}

// constFoldingCheckComplexity returns a negative number when the total
// item count of the (possibly nested) tuple exceeds limit.
//
// CPython: Python/flowgraph.c:1674 const_folding_check_complexity
func constFoldingCheckComplexity(o objects.Object, limit int) int {
	if t, ok := o.(*objects.Tuple); ok {
		limit -= t.Len()
		for i := 0; limit >= 0 && i < t.Len(); i++ {
			limit = constFoldingCheckComplexity(t.Item(i), limit)
			if limit < 0 {
				return limit
			}
		}
	}
	return limit
}

// constFoldingSafeMultiply reports whether v*w is small enough to fold.
//
// CPython: Python/flowgraph.c:1696 const_folding_safe_multiply
func constFoldingSafeMultiply(v, w objects.Object) bool {
	vi, vIsInt := objAsInt(v)
	wi, wIsInt := objAsInt(w)
	switch {
	case vIsInt && wIsInt && vi.Sign() != 0 && wi.Sign() != 0:
		if objNumBits(vi)+objNumBits(wi) > maxIntSize {
			return false
		}
	case vIsInt:
		if size, ok := objSeqLen(w); ok && size > 0 {
			n, ok := vi.Int64()
			if !ok || n < 0 || n > int64(maxCollectionSize/size) {
				return false
			}
			if _, isTuple := w.(*objects.Tuple); isTuple && n != 0 &&
				constFoldingCheckComplexity(w, maxTotalItems/int(n)) < 0 {
				return false
			}
			if _, isTuple := w.(*objects.Tuple); !isTuple && n > int64(maxStrSize/size) {
				return false
			}
		}
	case wIsInt:
		if _, ok := objSeqLen(v); ok {
			return constFoldingSafeMultiply(w, v)
		}
	}
	return true
}

// constFoldingSafePower reports whether v**w is small enough to fold.
//
// CPython: Python/flowgraph.c:1740 const_folding_safe_power
func constFoldingSafePower(v, w objects.Object) bool {
	vi, vIsInt := objAsInt(v)
	wi, wIsInt := objAsInt(w)
	if vIsInt && wIsInt && vi.Sign() != 0 && wi.Sign() > 0 {
		wbits, ok := wi.Int64()
		if !ok || wbits <= 0 {
			return false
		}
		if uint64(objNumBits(vi)) > uint64(maxIntSize)/uint64(wbits) {
			return false
		}
	}
	return true
}

// constFoldingSafeLshift reports whether v<<w is small enough to fold.
//
// CPython: Python/flowgraph.c:1760 const_folding_safe_lshift
func constFoldingSafeLshift(v, w objects.Object) bool {
	vi, vIsInt := objAsInt(v)
	wi, wIsInt := objAsInt(w)
	if vIsInt && wIsInt && vi.Sign() != 0 && wi.Sign() != 0 {
		wbits, ok := wi.Int64()
		if !ok || wbits < 0 {
			return false
		}
		if wbits > maxIntSize || uint64(objNumBits(vi)) > uint64(maxIntSize)-uint64(wbits) {
			return false
		}
	}
	return true
}

func objIsStrOrBytes(o objects.Object) bool {
	switch o.(type) {
	case *objects.Unicode, *objects.Bytes:
		return true
	}
	return false
}

// cfgEvalConstBinop folds a BINARY_OP over two const operands, returning
// ok=false when the operands are not foldable, the result would be too
// large, or the operation raises (matching CPython's PyErr_Clear path).
//
// CPython: Python/flowgraph.c:1790 eval_const_binop
func cfgEvalConstBinop(leftC any, op int32, rightC any) (any, bool) {
	left, ok := cfgConstToObject(leftC)
	if !ok {
		return nil, false
	}
	right, ok := cfgConstToObject(rightC)
	if !ok {
		return nil, false
	}
	var (
		res objects.Object
		err error
	)
	switch op {
	case NB_ADD, NB_INPLACE_ADD:
		res, err = objects.NumberAdd(left, right)
	case NB_SUBTRACT, NB_INPLACE_SUBTRACT:
		res, err = objects.NumberSubtract(left, right)
	case NB_MULTIPLY, NB_INPLACE_MULTIPLY:
		if !constFoldingSafeMultiply(left, right) {
			return nil, false
		}
		res, err = objects.NumberMultiply(left, right)
	case NB_TRUE_DIVIDE, NB_INPLACE_TRUE_DIVIDE:
		res, err = objects.NumberTrueDivide(left, right)
	case NB_FLOOR_DIVIDE, NB_INPLACE_FLOOR_DIVIDE:
		res, err = objects.NumberFloorDivide(left, right)
	case NB_REMAINDER, NB_INPLACE_REMAINDER:
		if objIsStrOrBytes(left) {
			return nil, false
		}
		res, err = objects.NumberRemainder(left, right)
	case NB_POWER, NB_INPLACE_POWER:
		if !constFoldingSafePower(left, right) {
			return nil, false
		}
		res, err = objects.NumberPower(left, right, objects.None())
	case NB_LSHIFT, NB_INPLACE_LSHIFT:
		if !constFoldingSafeLshift(left, right) {
			return nil, false
		}
		res, err = objects.NumberLshift(left, right)
	case NB_RSHIFT, NB_INPLACE_RSHIFT:
		res, err = objects.NumberRshift(left, right)
	case NB_OR, NB_INPLACE_OR:
		res, err = objects.NumberOr(left, right)
	case NB_XOR, NB_INPLACE_XOR:
		res, err = objects.NumberXor(left, right)
	case NB_AND, NB_INPLACE_AND:
		res, err = objects.NumberAnd(left, right)
	case NB_SUBSCR:
		res, err = objects.GetItem(left, right)
	default:
		return nil, false
	}
	if err != nil || res == nil {
		return nil, false
	}
	return cfgObjectToConst(res)
}

// cfgEvalConstUnaryop folds a unary opcode over a const operand.
//
// CPython: Python/flowgraph.c:1893 eval_const_unaryop
func cfgEvalConstUnaryop(operandC any, op Opcode, oparg int32) (any, bool) {
	operand, ok := cfgConstToObject(operandC)
	if !ok {
		return nil, false
	}
	var (
		res objects.Object
		err error
	)
	switch op {
	case UNARY_NEGATIVE:
		res, err = objects.NumberNegative(operand)
	case UNARY_INVERT:
		// ~bool stays unfolded until the deprecation expires.
		if _, isBool := operand.(*objects.Bool); isBool {
			return nil, false
		}
		res, err = objects.NumberInvert(operand)
	case UNARY_NOT:
		t, terr := objects.IsTruthy(operand)
		if terr != nil {
			return nil, false
		}
		return !t, true
	case CALL_INTRINSIC_1:
		if oparg != intrinsicUnaryPositive {
			return nil, false
		}
		res, err = objects.NumberPositive(operand)
	default:
		return nil, false
	}
	if err != nil || res == nil {
		return nil, false
	}
	return cfgObjectToConst(res)
}
