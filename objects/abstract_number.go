// Number protocol entry points. PyNumber_* functions walk the LHS
// number slot, fall through on NotImplemented, then walk the RHS,
// matching binary_op1 / binary_op in CPython. The in-place variants
// try the inplace slot first and fall back to the non-inplace
// dispatch when nil or NotImplemented.
//
// CPython: Objects/abstract.c:1029 binary_op

package objects

import "fmt"

// numberSlot fetches op from the type's NumberMethods. Single field
// load: P7.2's inherit_slots port populates t.Number at type-creation
// time by walking the MRO once and deep-copying every nil slot from
// the first ancestor that supplies it, so the per-dispatch MRO walk
// CPython's slot_tp_* dispatchers would otherwise do (via tp_as_number
// indirection + slot_nb_* lookup) collapses to a single field read
// plus a nil check on the function pointer.
//
// CPython: Objects/typeobject.c:8258 inherit_slots COPYNUM block (the
// inherit pass that makes the per-dispatch walk unnecessary)
func numberSlot(o Object, op func(*NumberMethods) func(a, b Object) (Object, error)) func(a, b Object) (Object, error) {
	n := o.Type().Number
	if n == nil {
		return nil
	}
	return op(n)
}

// numberBinary walks LHS then RHS and returns the first concrete
// result, matching binary_op1's NotImplemented loop. opName is the
// operator symbol used in the TypeError message. When neither type
// exposes a Number slot for the op, falls back to looking up the
// __dunder__ / __rdunder__ pair on the type so Python-defined classes
// (decimal.Decimal, fractions.Fraction, ...) get dispatched without
// the slot-wrapper port (CPython's update_one_slot) being live yet.
//
// CPython: Objects/abstract.c:1029 binary_op
// CPython: Objects/typeobject.c:8195 SLOT1BIN slot_nb_* table
func numberBinary(a, b Object, opName string, pick func(*NumberMethods) func(a, b Object) (Object, error)) (Object, error) {
	// When b's type is a strict subtype of a's type, give b's reverse
	// operator a chance before a's forward slot.  Mirrors the subtype-first
	// check in CPython's binary_op1.
	//
	// CPython: Objects/abstract.c:986 binary_op1 (subtype-first block)
	if a.Type() != b.Type() && IsSubtype(b.Type(), a.Type()) {
		if out, ok, err := dunderBinaryReverse(b, a, opName); ok {
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
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
	if out, ok, err := dunderBinary(a, b, opName); ok {
		return out, err
	}
	return nil, binopTypeError(a, b, opName)
}

// DunderBinaryReverse tries only the reverse (__rop__) dunder on b with a as
// the operand. Used for the subtype-first check in numberBinary and the VM.
//
// CPython: Objects/abstract.c:986 binary_op1 (slotw-first block)
func DunderBinaryReverse(b, a Object, opName string) (Object, bool, error) {
	return dunderBinaryReverse(b, a, opName)
}

func dunderBinaryReverse(b, a Object, opName string) (Object, bool, error) {
	names, ok := binaryOpDunders[opName]
	if !ok {
		return nil, false, nil
	}
	// Only try b's reverse dunder if b's own type (not inherited from a's type)
	// actually defines it. This avoids double-trying inherited slots.
	reverseDescr, _ := LookupDescriptor(b.Type(), names[1])
	if reverseDescr == nil {
		return nil, false, nil
	}
	// Check that this dunder is not merely inherited from a's type.
	aDescr, _ := LookupDescriptor(a.Type(), names[1])
	if reverseDescr == aDescr {
		return nil, false, nil
	}
	callable, err := bindDescriptor(reverseDescr, b)
	if err != nil {
		return nil, true, err
	}
	out, err := CallOneArg(callable, a)
	if err != nil {
		return nil, true, err
	}
	return out, true, nil
}

// DunderBinary is the __dunder__ / __rdunder__ fallback the binary
// number path uses when no Number slot accepted the operands. Returns
// (result, true, err) on a hit (including a NotImplemented return),
// (nil, false, nil) when neither side defines a matching dunder. The
// vm's numericForward path consults this directly so user classes whose
// nb_* slots have not been wired by update_one_slot still dispatch
// through their __add__ / __mul__ / etc. methods.
//
// CPython: Objects/typeobject.c:8195 SLOT1BIN
func DunderBinary(a, b Object, opName string) (Object, bool, error) {
	return dunderBinary(a, b, opName)
}

func dunderBinary(a, b Object, opName string) (Object, bool, error) {
	names, ok := binaryOpDunders[opName]
	if !ok {
		return nil, false, nil
	}
	if descr, _ := LookupDescriptor(a.Type(), names[0]); descr != nil {
		callable, err := bindDescriptor(descr, a)
		if err != nil {
			return nil, true, err
		}
		out, err := CallOneArg(callable, b)
		if err != nil {
			return nil, true, err
		}
		if !IsNotImplemented(out) {
			return out, true, nil
		}
	}
	if descr, _ := LookupDescriptor(b.Type(), names[1]); descr != nil {
		callable, err := bindDescriptor(descr, b)
		if err != nil {
			return nil, true, err
		}
		out, err := CallOneArg(callable, a)
		if err != nil {
			return nil, true, err
		}
		if !IsNotImplemented(out) {
			return out, true, nil
		}
	}
	return nil, false, nil
}

// binaryOpDunders maps operator symbols to the __op__ / __rop__ pair
// the dunder fallback walks. Order: forward name, reverse name. Same
// ops the SLOT1BIN macro emits in typeobject.c.
//
// CPython: Objects/typeobject.c:8195 SLOT1BIN list
var binaryOpDunders = map[string][2]string{
	"+":   {"__add__", "__radd__"},
	"-":   {"__sub__", "__rsub__"},
	"*":   {"__mul__", "__rmul__"},
	"@":   {"__matmul__", "__rmatmul__"},
	"/":   {"__truediv__", "__rtruediv__"},
	"//":  {"__floordiv__", "__rfloordiv__"},
	"%":   {"__mod__", "__rmod__"},
	"&":   {"__and__", "__rand__"},
	"|":   {"__or__", "__ror__"},
	"^":   {"__xor__", "__rxor__"},
	"<<":  {"__lshift__", "__rlshift__"},
	">>":  {"__rshift__", "__rrshift__"},
	"**":  {"__pow__", "__rpow__"},
	"+=":  {"__iadd__", "__add__"},
	"-=":  {"__isub__", "__sub__"},
	"*=":  {"__imul__", "__mul__"},
	"@=":  {"__imatmul__", "__matmul__"},
	"/=":  {"__itruediv__", "__truediv__"},
	"//=": {"__ifloordiv__", "__floordiv__"},
	"%=":  {"__imod__", "__mod__"},
	"&=":  {"__iand__", "__and__"},
	"|=":  {"__ior__", "__or__"},
	"^=":  {"__ixor__", "__xor__"},
	"<<=": {"__ilshift__", "__lshift__"},
	">>=": {"__irshift__", "__rshift__"},
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
	out, err := numberBinaryNoErrNamed(a, b, "+", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Add })
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
	out, err := numberBinaryNoErrNamed(a, b, "*", func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Multiply })
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
// NumberLong calls __int__ protocol on o, mirroring PyNumber_Long.
//
// CPython: Objects/abstract.c:1765 PyNumber_Long
func NumberLong(o Object) (Object, error) {
	if _, ok := o.(*Int); ok {
		return o, nil
	}
	if b, ok := o.(*Bool); ok {
		if b == True() {
			return NewInt(1), nil
		}
		return NewInt(0), nil
	}
	n := o.Type().Number
	if n != nil && n.Int != nil {
		return n.Int(o)
	}
	// Fall back to __index__.
	return NumberIndex(o)
}

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
	var i *Int
	switch v := res.(type) {
	case *Int:
		i = v
	case *Bool:
		i = &v.Int
	default:
		return nil, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", res.Type().Name)
	}
	if res.Type() == IntType {
		return i, nil
	}
	// Subclass of int (incl. bool): emit DeprecationWarning and downcast
	// to plain int, mirroring _PyNumber_Index's behaviour for non-exact
	// long subclasses.
	//
	// CPython: Objects/abstract.c:1446 PyNumber_Index
	msg := fmt.Sprintf("__index__ returned non-int (type %s).  "+
		"The ability to return an instance of a strict subclass of int "+
		"is deprecated, and may be removed in a future version of Python.",
		res.Type().Name)
	if DeprecWarnHook != nil {
		if werr := DeprecWarnHook(msg); werr != nil {
			return nil, werr
		}
	}
	return NewIntFromBig(i.BigInt()), nil
}

// numberBinaryNoErr is numberBinary that returns (nil, nil) when no
// slot accepted the operands rather than raising TypeError. The
// outer entry point handles the sequence-protocol fallback first and
// raises the error itself.
func numberBinaryNoErr(a, b Object, pick func(*NumberMethods) func(a, b Object) (Object, error)) (Object, error) {
	return numberBinaryNoErrNamed(a, b, "", pick)
}

// numberBinaryNoErrNamed is numberBinaryNoErr that also takes the
// operator label so it can run the __dunder__ fallback when the C-slot
// walk comes up empty. Callers that already know the op symbol pass it
// in; the unnamed wrapper above keeps the old call sites unchanged.
func numberBinaryNoErrNamed(a, b Object, opName string, pick func(*NumberMethods) func(a, b Object) (Object, error)) (Object, error) {
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
	if opName != "" {
		if out, ok, err := dunderBinary(a, b, opName); ok {
			return out, err
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
// when neither the type's nb_* slot nor a __dunder__ method on the type
// supplies the operation. The __dunder__ fallback covers Python-defined
// classes whose nb_* slots are not filled in by the slot-wrapper port
// (decimal.Decimal, fractions.Fraction, and friends): CPython's
// update_one_slot would synthesize slot_nb_negative; this branch is the
// minimal equivalent until that port lands.
//
// CPython: Objects/abstract.c:1364 UNARY_FUNC
// CPython: Objects/typeobject.c:7900 slot_nb_negative
func unaryNumberOp(o Object, opName string, pick func(*NumberMethods) func(Object) (Object, error)) (Object, error) {
	if n := o.Type().Number; n != nil {
		if fn := pick(n); fn != nil {
			out, err := fn(o)
			if err != nil {
				return nil, err
			}
			if !IsNotImplemented(out) {
				return out, nil
			}
		}
	}
	if dunder, ok := unaryOpDunders[opName]; ok {
		if descr, _ := LookupDescriptor(o.Type(), dunder); descr != nil {
			callable, err := bindDescriptor(descr, o)
			if err != nil {
				return nil, err
			}
			return CallNoArgs(callable)
		}
	}
	return nil, fmt.Errorf("TypeError: bad operand type for %s: '%s'", opName, o.Type().Name)
}

// unaryOpDunders maps the operation labels unaryNumberOp uses to the
// matching __dunder__ name. Kept here so the dunder fallback stays in
// lockstep with the NumberPositive / NumberNegative / NumberAbsolute /
// NumberInvert call sites.
//
// CPython: Objects/typeobject.c:8195 slot_nb_negative table
var unaryOpDunders = map[string]string{
	"unary +": "__pos__",
	"unary -": "__neg__",
	"unary ~": "__invert__",
	"abs()":   "__abs__",
}

// binopTypeError formats the canonical "unsupported operand type(s)"
// message PyNumber_* raises when neither operand accepts the op.
//
// CPython: Objects/abstract.c:1004 binop_type_error
func binopTypeError(a, b Object, opName string) error {
	return fmt.Errorf("TypeError: unsupported operand type(s) for %s: '%s' and '%s'", opName, a.Type().Name, b.Type().Name)
}
