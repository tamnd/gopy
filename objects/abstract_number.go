// Number protocol entry points. PyNumber_* functions walk the LHS
// number slot, fall through on NotImplemented, then walk the RHS,
// matching binary_op1 / binary_op in CPython. The in-place variants
// try the inplace slot first and fall back to the non-inplace
// dispatch when nil or NotImplemented.
//
// CPython: Objects/abstract.c:1029 binary_op

package objects

import "fmt"

// numberSlot fetches op from the type's NumberMethods. Walks the MRO
// so subclasses without their own NumberMethods inherit the parent's
// slot table. CPython does the equivalent via inherit_slots() during
// PyType_Ready, copying parent slot pointers when the child leaves them
// NULL; here we resolve the inheritance lazily on each lookup.
//
// CPython: Objects/typeobject.c:7895 inherit_slots
func numberSlot(o Object, op func(*NumberMethods) func(a, b Object) (Object, error)) func(a, b Object) (Object, error) {
	for _, base := range o.Type().MRO {
		if base == nil || base.Number == nil {
			continue
		}
		if fn := op(base.Number); fn != nil {
			return fn
		}
	}
	return nil
}

// numberBinary walks LHS then RHS and returns the first concrete
// result, matching binary_op1's NotImplemented loop. opName is the
// operator symbol used in the TypeError message.
//
// CPython: Objects/abstract.c:1029 binary_op
func numberBinary(a, b Object, opName string, pick func(*NumberMethods) func(a, b Object) (Object, error)) (Object, error) {
	if fn := numberSlot(a, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	if fn := numberSlot(b, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	return nil, binopTypeError(a, b, opName)
}

// numberInPlaceBinary tries the in-place slot first, falling back to
// the regular binary op when the in-place slot is nil or returns
// NotImplemented.
//
// CPython: Objects/abstract.c:1217 binary_iop1
func numberInPlaceBinary(a, b Object, opName string,
	pickIn func(*NumberMethods) func(a, b Object) (Object, error),
	pick func(*NumberMethods) func(a, b Object) (Object, error),
) (Object, error) {
	if fn := numberSlot(a, pickIn); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	return numberBinary(a, b, opName, pick)
}

// NumberAdd is a + b. Falls through to Sequence.Concat on LHS when no
// number slot accepts the operands, mirroring CPython's PyNumber_Add.
//
// CPython: Objects/abstract.c:1128 PyNumber_Add
func NumberAdd(a, b Object) (Object, error) {
	out, err := numberBinaryNoErr(a, b, func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Add })
	if err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	if s := a.Type().Sequence; s != nil && s.Concat != nil {
		return s.Concat(a, b)
	}
	return nil, binopTypeError(a, b, "+")
}

// NumberSubtract is a - b.
//
// CPython: Objects/abstract.c:1124 PyNumber_Subtract
func NumberSubtract(a, b Object) (Object, error) {
	return numberBinary(a, b, "-", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Subtract })
}

// NumberMultiply is a * b. Falls through to Sequence.Repeat on either
// side when no number slot accepts the operands, mirroring
// PyNumber_Multiply.
//
// CPython: Objects/abstract.c:1166 PyNumber_Multiply
func NumberMultiply(a, b Object) (Object, error) {
	out, err := numberBinaryNoErr(a, b, func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Multiply })
	if err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	if s := a.Type().Sequence; s != nil && s.Repeat != nil {
		count, err := indexAsInt(b)
		if err != nil {
			return nil, err
		}
		return s.Repeat(a, count)
	}
	if s := b.Type().Sequence; s != nil && s.Repeat != nil {
		count, err := indexAsInt(a)
		if err != nil {
			return nil, err
		}
		return s.Repeat(b, count)
	}
	return nil, binopTypeError(a, b, "*")
}

// NumberMatrixMultiply is a @ b.
//
// CPython: Objects/abstract.c:1184 PyNumber_MatrixMultiply
func NumberMatrixMultiply(a, b Object) (Object, error) {
	return numberBinary(a, b, "@", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.MatrixMultiply })
}

// NumberTrueDivide is a / b.
//
// CPython: Objects/abstract.c:1186 PyNumber_TrueDivide
func NumberTrueDivide(a, b Object) (Object, error) {
	return numberBinary(a, b, "/", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.TrueDivide })
}

// NumberFloorDivide is a // b.
//
// CPython: Objects/abstract.c:1185 PyNumber_FloorDivide
func NumberFloorDivide(a, b Object) (Object, error) {
	return numberBinary(a, b, "//", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.FloorDivide })
}

// NumberRemainder is a % b.
//
// CPython: Objects/abstract.c:1187 PyNumber_Remainder
func NumberRemainder(a, b Object) (Object, error) {
	return numberBinary(a, b, "%", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Remainder })
}

// NumberAnd is a & b.
//
// CPython: Objects/abstract.c:1121 PyNumber_And
func NumberAnd(a, b Object) (Object, error) {
	return numberBinary(a, b, "&", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.And })
}

// NumberOr is a | b.
//
// CPython: Objects/abstract.c:1119 PyNumber_Or
func NumberOr(a, b Object) (Object, error) {
	return numberBinary(a, b, "|", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Or })
}

// NumberXor is a ^ b.
//
// CPython: Objects/abstract.c:1120 PyNumber_Xor
func NumberXor(a, b Object) (Object, error) {
	return numberBinary(a, b, "^", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Xor })
}

// NumberLshift is a << b.
//
// CPython: Objects/abstract.c:1122 PyNumber_Lshift
func NumberLshift(a, b Object) (Object, error) {
	return numberBinary(a, b, "<<", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Lshift })
}

// NumberRshift is a >> b.
//
// CPython: Objects/abstract.c:1123 PyNumber_Rshift
func NumberRshift(a, b Object) (Object, error) {
	return numberBinary(a, b, ">>", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Rshift })
}

// NumberPower is pow(a, b, mod). mod may be nil.
//
// CPython: Objects/abstract.c:1190 PyNumber_Power
func NumberPower(a, b, mod Object) (Object, error) {
	return ternaryNumberOp(a, b, mod, "** or pow()", func(n *NumberMethods) func(a, b, mod Object) (Object, error) { return n.Power })
}

// NumberDivmod is divmod(a, b).
//
// CPython: Objects/abstract.c:1125 PyNumber_Divmod
func NumberDivmod(a, b Object) (Object, error) {
	return numberBinary(a, b, "divmod()", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Divmod })
}

// NumberInPlaceAdd is a += b. Falls through to sq_concat / sq_inplace_concat
// on LHS when the number protocol doesn't apply.
//
// CPython: Objects/abstract.c:1297 PyNumber_InPlaceAdd
func NumberInPlaceAdd(a, b Object) (Object, error) {
	out, err := inPlaceBinaryNoErr(a, b,
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceAdd },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Add })
	if err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	if s := a.Type().Sequence; s != nil && s.Concat != nil {
		return s.Concat(a, b)
	}
	return nil, binopTypeError(a, b, "+=")
}

// NumberInPlaceSubtract is a -= b.
//
// CPython: Objects/abstract.c:1290 PyNumber_InPlaceSubtract
func NumberInPlaceSubtract(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "-=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceSubtract },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Subtract })
}

// NumberInPlaceMultiply is a *= b. Falls through to sq_repeat /
// sq_inplace_repeat as PyNumber_InPlaceMultiply does.
//
// CPython: Objects/abstract.c:1320 PyNumber_InPlaceMultiply
func NumberInPlaceMultiply(a, b Object) (Object, error) {
	out, err := inPlaceBinaryNoErr(a, b,
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceMultiply },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Multiply })
	if err != nil {
		return nil, err
	}
	if out != nil {
		return out, nil
	}
	if s := a.Type().Sequence; s != nil && s.Repeat != nil {
		count, err := indexAsInt(b)
		if err != nil {
			return nil, err
		}
		return s.Repeat(a, count)
	}
	if s := b.Type().Sequence; s != nil && s.Repeat != nil {
		count, err := indexAsInt(a)
		if err != nil {
			return nil, err
		}
		return s.Repeat(b, count)
	}
	return nil, binopTypeError(a, b, "*=")
}

// NumberInPlaceMatrixMultiply is a @= b.
//
// CPython: Objects/abstract.c:1291 PyNumber_InPlaceMatrixMultiply
func NumberInPlaceMatrixMultiply(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "@=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceMatrixMultiply },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.MatrixMultiply })
}

// NumberInPlaceTrueDivide is a /= b.
//
// CPython: Objects/abstract.c:1293 PyNumber_InPlaceTrueDivide
func NumberInPlaceTrueDivide(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "/=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceTrueDivide },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.TrueDivide })
}

// NumberInPlaceFloorDivide is a //= b.
//
// CPython: Objects/abstract.c:1292 PyNumber_InPlaceFloorDivide
func NumberInPlaceFloorDivide(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "//=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceFloorDivide },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.FloorDivide })
}

// NumberInPlaceRemainder is a %= b.
//
// CPython: Objects/abstract.c:1294 PyNumber_InPlaceRemainder
func NumberInPlaceRemainder(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "%=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceRemainder },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Remainder })
}

// NumberInPlaceAnd is a &= b.
//
// CPython: Objects/abstract.c:1287 PyNumber_InPlaceAnd
func NumberInPlaceAnd(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "&=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceAnd },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.And })
}

// NumberInPlaceOr is a |= b.
//
// CPython: Objects/abstract.c:1285 PyNumber_InPlaceOr
func NumberInPlaceOr(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "|=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceOr },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Or })
}

// NumberInPlaceXor is a ^= b.
//
// CPython: Objects/abstract.c:1286 PyNumber_InPlaceXor
func NumberInPlaceXor(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "^=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceXor },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Xor })
}

// NumberInPlaceLshift is a <<= b.
//
// CPython: Objects/abstract.c:1288 PyNumber_InPlaceLshift
func NumberInPlaceLshift(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, "<<=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceLshift },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Lshift })
}

// NumberInPlaceRshift is a >>= b.
//
// CPython: Objects/abstract.c:1289 PyNumber_InPlaceRshift
func NumberInPlaceRshift(a, b Object) (Object, error) {
	return numberInPlaceBinary(a, b, ">>=",
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceRshift },
		func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Rshift })
}

// NumberInPlacePower is a **= b.
//
// CPython: Objects/abstract.c:1349 PyNumber_InPlacePower
func NumberInPlacePower(a, b, mod Object) (Object, error) {
	if n := a.Type().Number; n != nil && n.InPlacePower != nil {
		out, err := n.InPlacePower(a, b, mod)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	return NumberPower(a, b, mod)
}

// NumberNegative is -o.
//
// CPython: Objects/abstract.c:1381 PyNumber_Negative
func NumberNegative(o Object) (Object, error) {
	return unaryNumberOp(o, "unary -", func(n *NumberMethods) func(Object) (Object, error) { return n.Negative })
}

// NumberPositive is +o.
//
// CPython: Objects/abstract.c:1382 PyNumber_Positive
func NumberPositive(o Object) (Object, error) {
	return unaryNumberOp(o, "unary +", func(n *NumberMethods) func(Object) (Object, error) { return n.Positive })
}

// NumberAbsolute is abs(o).
//
// CPython: Objects/abstract.c:1384 PyNumber_Absolute
func NumberAbsolute(o Object) (Object, error) {
	return unaryNumberOp(o, "abs()", func(n *NumberMethods) func(Object) (Object, error) { return n.Absolute })
}

// NumberInvert is ~o.
//
// CPython: Objects/abstract.c:1383 PyNumber_Invert
func NumberInvert(o Object) (Object, error) {
	return unaryNumberOp(o, "unary ~", func(n *NumberMethods) func(Object) (Object, error) { return n.Invert })
}

// IndexCheck reports whether o exposes nb_index. Mirrors PyIndex_Check
// (which is _PyIndex_Check at the C level).
//
// CPython: Objects/abstract.c:1387 PyIndex_Check
func IndexCheck(o Object) bool {
	if _, ok := o.(*Int); ok {
		return true
	}
	if _, ok := o.(*Bool); ok {
		return true
	}
	n := o.Type().Number
	return n != nil && n.Index != nil
}

// NumberIndex returns o coerced to int via __index__. Returns the
// receiver unchanged when it is already an int. Bool routes through
// IntFromBool to drop the bool subtype.
//
// CPython: Objects/abstract.c:1446 PyNumber_Index
func NumberIndex(o Object) (Object, error) {
	if i, ok := o.(*Int); ok {
		return i, nil
	}
	if b, ok := o.(*Bool); ok {
		if b == True() {
			return NewInt(1), nil
		}
		return NewInt(0), nil
	}
	n := o.Type().Number
	if n == nil || n.Index == nil {
		return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", o.Type().Name)
	}
	res, err := n.Index(o)
	if err != nil {
		return nil, err
	}
	if _, ok := res.(*Int); !ok {
		return nil, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", res.Type().Name)
	}
	return res, nil
}

// numberBinaryNoErr is numberBinary that returns (nil, nil) when no
// slot accepted the operands rather than raising TypeError. The
// outer entry point handles the sequence-protocol fallback first and
// raises the error itself.
func numberBinaryNoErr(a, b Object, pick func(*NumberMethods) func(a, b Object) (Object, error)) (Object, error) {
	if fn := numberSlot(a, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	if fn := numberSlot(b, pick); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	return nil, nil
}

// inPlaceBinaryNoErr is the in-place counterpart. Tries the in-place
// slot first, falls through to the regular binary walk, and returns
// (nil, nil) when nothing accepted the operands.
func inPlaceBinaryNoErr(a, b Object,
	pickIn func(*NumberMethods) func(a, b Object) (Object, error),
	pick func(*NumberMethods) func(a, b Object) (Object, error),
) (Object, error) {
	if fn := numberSlot(a, pickIn); fn != nil {
		out, err := fn(a, b)
		if err != nil {
			return nil, err
		}
		if !IsNotImplemented(out) {
			return out, nil
		}
	}
	return numberBinaryNoErr(a, b, pick)
}

// ternaryNumberOp dispatches Power-style ops that take an optional
// third argument. Walks LHS then RHS.
//
// CPython: Objects/abstract.c:1041 ternary_op
func ternaryNumberOp(a, b, c Object, opName string, pick func(*NumberMethods) func(a, b, mod Object) (Object, error)) (Object, error) {
	if n := a.Type().Number; n != nil {
		if fn := pick(n); fn != nil {
			out, err := fn(a, b, c)
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	if n := b.Type().Number; n != nil {
		if fn := pick(n); fn != nil {
			out, err := fn(a, b, c)
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("TypeError: unsupported operand type(s) for %s: '%s' and '%s'", opName, a.Type().Name, b.Type().Name)
}

// unaryNumberOp dispatches a unary arithmetic op. Returns a TypeError
// when the type doesn't expose the slot.
//
// CPython: Objects/abstract.c:1364 UNARY_FUNC
func unaryNumberOp(o Object, opName string, pick func(*NumberMethods) func(Object) (Object, error)) (Object, error) {
	n := o.Type().Number
	if n == nil {
		return nil, fmt.Errorf("TypeError: bad operand type for %s: '%s'", opName, o.Type().Name)
	}
	fn := pick(n)
	if fn == nil {
		return nil, fmt.Errorf("TypeError: bad operand type for %s: '%s'", opName, o.Type().Name)
	}
	return fn(o)
}

// binopTypeError formats the canonical "unsupported operand type(s)"
// message PyNumber_* raises when neither operand accepts the op.
//
// CPython: Objects/abstract.c:1004 binop_type_error
func binopTypeError(a, b Object, opName string) error {
	return fmt.Errorf("TypeError: unsupported operand type(s) for %s: '%s' and '%s'", opName, a.Type().Name, b.Type().Name)
}
