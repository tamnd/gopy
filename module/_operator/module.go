// Package _operator is the gopy port of CPython's _operator C
// accelerator. Lib/operator.py imports `from _operator import *`
// after defining its pure-Python fallbacks; those imports overwrite
// the slower pure-Python versions with the fast paths defined here:
// the binary/unary arithmetic, the rich comparisons, the identity
// tests, length_hint, _compare_digest, plus the itemgetter,
// attrgetter and methodcaller callable types.
//
// CPython: Modules/_operator.c

package _operator

import (
	"fmt"
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_operator", buildModule)
}

// buildModule materializes the _operator module dict. Mirrors
// operator_exec.
//
// CPython: Modules/_operator.c:1951 operator_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_operator")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		// Arithmetic.
		{"add", binaryFunc("add", opAdd)},
		{"sub", binaryFunc("sub", opSub)},
		{"mul", binaryFunc("mul", opMul)},
		{"matmul", binaryFunc("matmul", opMatmul)},
		{"floordiv", binaryFunc("floordiv", opFloordiv)},
		{"truediv", binaryFunc("truediv", opTruediv)},
		{"mod", binaryFunc("mod", opMod)},
		{"pow", binaryFunc("pow", opPow)},
		{"lshift", binaryFunc("lshift", opLshift)},
		{"rshift", binaryFunc("rshift", opRshift)},
		{"and_", binaryFunc("and_", opAnd)},
		{"or_", binaryFunc("or_", opOr)},
		{"xor", binaryFunc("xor", opXor)},

		// Unary.
		{"neg", unaryFunc("neg", opNeg)},
		{"pos", unaryFunc("pos", opPos)},
		{"abs", unaryFunc("abs", opAbs)},
		{"inv", unaryFunc("inv", opInvert)},
		{"invert", unaryFunc("invert", opInvert)},
		{"index", unaryFunc("index", opIndex)},

		// In-place arithmetic.
		{"iadd", binaryFunc("iadd", opIAdd)},
		{"isub", binaryFunc("isub", opISub)},
		{"imul", binaryFunc("imul", opIMul)},
		{"imatmul", binaryFunc("imatmul", opIMatmul)},
		{"ifloordiv", binaryFunc("ifloordiv", opIFloordiv)},
		{"itruediv", binaryFunc("itruediv", opITruediv)},
		{"imod", binaryFunc("imod", opIMod)},
		{"ipow", binaryFunc("ipow", opIPow)},
		{"ilshift", binaryFunc("ilshift", opILshift)},
		{"irshift", binaryFunc("irshift", opIRshift)},
		{"iand", binaryFunc("iand", opIAnd)},
		{"ior", binaryFunc("ior", opIOr)},
		{"ixor", binaryFunc("ixor", opIXor)},

		// Sequence.
		{"concat", binaryFunc("concat", opConcat)},
		{"iconcat", binaryFunc("iconcat", opIConcat)},
		{"contains", binaryFunc("contains", opContains)},
		{"indexOf", binaryFunc("indexOf", opIndexOf)},
		{"countOf", binaryFunc("countOf", opCountOf)},
		{"getitem", binaryFunc("getitem", opGetItem)},
		{"setitem", objects.NewBuiltinFunction("setitem", setItemFunc)},
		{"delitem", binaryFunc("delitem", opDelItem)},

		// Comparisons.
		{"eq", binaryFunc("eq", opEq)},
		{"ne", binaryFunc("ne", opNe)},
		{"lt", binaryFunc("lt", opLt)},
		{"le", binaryFunc("le", opLe)},
		{"gt", binaryFunc("gt", opGt)},
		{"ge", binaryFunc("ge", opGe)},

		// Logical / identity.
		{"truth", unaryFunc("truth", opTruth)},
		{"not_", unaryFunc("not_", opNot)},
		{"is_", binaryFunc("is_", opIs)},
		{"is_not", binaryFunc("is_not", opIsNot)},
		{"is_none", unaryFunc("is_none", opIsNone)},
		{"is_not_none", unaryFunc("is_not_none", opIsNotNone)},

		// Misc.
		{"length_hint", objects.NewBuiltinFunction("length_hint", lengthHintFunc)},
		{"_compare_digest", binaryFunc("_compare_digest", opCompareDigest)},
		{"call", objects.NewBuiltinFunction("call", callFunc)},

		// Callable types.
		{"itemgetter", ItemgetterType},
		{"attrgetter", AttrgetterType},
		{"methodcaller", MethodcallerType},

		// Module doc.
		{"__doc__", objects.NewStr(moduleDoc)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

const moduleDoc = "Operator interface.\n\nThis module exports a set of functions implemented in C corresponding\nto the intrinsic operators of Python. For example, operator.add(x, y)\nis equivalent to the expression x+y. The function names are those\nused for special methods; variants without leading and trailing '__'\nare also provided for convenience."

// ---------------------------------------------------------------------------
// Argument plumbing.
// ---------------------------------------------------------------------------

// binaryFunc wraps a 2-arg implementation as a Python callable that
// validates positional arity and forbids keywords, matching the
// generated _OPERATOR_*_METHODDEF entries.
func binaryFunc(name string, fn func(a, b objects.Object) (objects.Object, error)) objects.Object {
	return objects.NewBuiltinFunction(name, func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
		}
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: %s expected 2 arguments, got %d", name, len(args))
		}
		return fn(args[0], args[1])
	})
}

// unaryFunc is the 1-arg analog of binaryFunc.
func unaryFunc(name string, fn func(a objects.Object) (objects.Object, error)) objects.Object {
	return objects.NewBuiltinFunction(name, func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: %s expected 1 argument, got %d", name, len(args))
		}
		return fn(args[0])
	})
}

// ---------------------------------------------------------------------------
// Arithmetic.
// ---------------------------------------------------------------------------

// opAdd is a + b.
//
// CPython: Modules/_operator.c:68 _operator_add_impl
func opAdd(a, b objects.Object) (objects.Object, error) { return objects.NumberAdd(a, b) }

// opSub is a - b.
//
// CPython: Modules/_operator.c:81 _operator_sub_impl
func opSub(a, b objects.Object) (objects.Object, error) { return objects.NumberSubtract(a, b) }

// opMul is a * b.
//
// CPython: Modules/_operator.c:94 _operator_mul_impl
func opMul(a, b objects.Object) (objects.Object, error) { return objects.NumberMultiply(a, b) }

// opMatmul is a @ b.
//
// CPython: Modules/_operator.c:107 _operator_matmul_impl
func opMatmul(a, b objects.Object) (objects.Object, error) {
	return objects.NumberMatrixMultiply(a, b)
}

// opFloordiv is a // b.
//
// CPython: Modules/_operator.c:120 _operator_floordiv_impl
func opFloordiv(a, b objects.Object) (objects.Object, error) {
	return objects.NumberFloorDivide(a, b)
}

// opTruediv is a / b.
//
// CPython: Modules/_operator.c:133 _operator_truediv_impl
func opTruediv(a, b objects.Object) (objects.Object, error) {
	return objects.NumberTrueDivide(a, b)
}

// opMod is a % b.
//
// CPython: Modules/_operator.c:146 _operator_mod_impl
func opMod(a, b objects.Object) (objects.Object, error) { return objects.NumberRemainder(a, b) }

// opPow is a ** b.
//
// CPython: Modules/_operator.c:669 _operator_pow_impl
func opPow(a, b objects.Object) (objects.Object, error) {
	return objects.NumberPower(a, b, objects.None())
}

// opLshift is a << b.
//
// CPython: Modules/_operator.c:227 _operator_lshift_impl
func opLshift(a, b objects.Object) (objects.Object, error) { return objects.NumberLshift(a, b) }

// opRshift is a >> b.
//
// CPython: Modules/_operator.c:240 _operator_rshift_impl
func opRshift(a, b objects.Object) (objects.Object, error) { return objects.NumberRshift(a, b) }

// opAnd is a & b.
//
// CPython: Modules/_operator.c:266 _operator_and__impl
func opAnd(a, b objects.Object) (objects.Object, error) { return objects.NumberAnd(a, b) }

// opOr is a | b.
//
// CPython: Modules/_operator.c:292 _operator_or__impl
func opOr(a, b objects.Object) (objects.Object, error) { return objects.NumberOr(a, b) }

// opXor is a ^ b.
//
// CPython: Modules/_operator.c:279 _operator_xor_impl
func opXor(a, b objects.Object) (objects.Object, error) { return objects.NumberXor(a, b) }

// ---------------------------------------------------------------------------
// Unary arithmetic.
// ---------------------------------------------------------------------------

// opNeg is -a.
//
// CPython: Modules/_operator.c:162 _operator_neg
func opNeg(a objects.Object) (objects.Object, error) { return objects.NumberNegative(a) }

// opPos is +a.
//
// CPython: Modules/_operator.c:175 _operator_pos
func opPos(a objects.Object) (objects.Object, error) { return objects.NumberPositive(a) }

// opAbs is abs(a).
//
// CPython: Modules/_operator.c:188 _operator_abs
func opAbs(a objects.Object) (objects.Object, error) { return objects.NumberAbsolute(a) }

// opInvert is ~a. Wires both inv and invert.
//
// CPython: Modules/_operator.c:201 _operator_inv / :214 _operator_invert
func opInvert(a objects.Object) (objects.Object, error) { return objects.NumberInvert(a) }

// opIndex is a.__index__().
//
// CPython: Modules/_operator.c:698 _operator_index
func opIndex(a objects.Object) (objects.Object, error) { return objects.NumberIndex(a) }

// ---------------------------------------------------------------------------
// In-place arithmetic.
// ---------------------------------------------------------------------------

// opIAdd is a += b.
//
// CPython: Modules/_operator.c:305 _operator_iadd_impl
func opIAdd(a, b objects.Object) (objects.Object, error) { return objects.NumberInPlaceAdd(a, b) }

// opISub is a -= b.
//
// CPython: Modules/_operator.c:318 _operator_isub_impl
func opISub(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceSubtract(a, b)
}

// opIMul is a *= b.
//
// CPython: Modules/_operator.c:331 _operator_imul_impl
func opIMul(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceMultiply(a, b)
}

// opIMatmul is a @= b.
//
// CPython: Modules/_operator.c:344 _operator_imatmul_impl
func opIMatmul(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceMatrixMultiply(a, b)
}

// opIFloordiv is a //= b.
//
// CPython: Modules/_operator.c:357 _operator_ifloordiv_impl
func opIFloordiv(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceFloorDivide(a, b)
}

// opITruediv is a /= b.
//
// CPython: Modules/_operator.c:370 _operator_itruediv_impl
func opITruediv(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceTrueDivide(a, b)
}

// opIMod is a %= b.
//
// CPython: Modules/_operator.c:383 _operator_imod_impl
func opIMod(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceRemainder(a, b)
}

// opIPow is a **= b.
//
// CPython: Modules/_operator.c:682 _operator_ipow_impl
func opIPow(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlacePower(a, b, objects.None())
}

// opILshift is a <<= b.
//
// CPython: Modules/_operator.c:396 _operator_ilshift_impl
func opILshift(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceLshift(a, b)
}

// opIRshift is a >>= b.
//
// CPython: Modules/_operator.c:409 _operator_irshift_impl
func opIRshift(a, b objects.Object) (objects.Object, error) {
	return objects.NumberInPlaceRshift(a, b)
}

// opIAnd is a &= b.
//
// CPython: Modules/_operator.c:422 _operator_iand_impl
func opIAnd(a, b objects.Object) (objects.Object, error) { return objects.NumberInPlaceAnd(a, b) }

// opIOr is a |= b.
//
// CPython: Modules/_operator.c:448 _operator_ior_impl
func opIOr(a, b objects.Object) (objects.Object, error) { return objects.NumberInPlaceOr(a, b) }

// opIXor is a ^= b.
//
// CPython: Modules/_operator.c:435 _operator_ixor_impl
func opIXor(a, b objects.Object) (objects.Object, error) { return objects.NumberInPlaceXor(a, b) }

// ---------------------------------------------------------------------------
// Sequence protocol.
// ---------------------------------------------------------------------------

// opConcat is PySequence_Concat(a, b).
//
// CPython: Modules/_operator.c:461 _operator_concat_impl
func opConcat(a, b objects.Object) (objects.Object, error) { return objects.SequenceConcat(a, b) }

// opIConcat is PySequence_InPlaceConcat(a, b).
//
// CPython: Modules/_operator.c:474 _operator_iconcat_impl
func opIConcat(a, b objects.Object) (objects.Object, error) {
	return objects.SequenceInPlaceConcat(a, b)
}

// opContains is b in a.
//
// CPython: Modules/_operator.c:491 _operator_contains_impl
func opContains(a, b objects.Object) (objects.Object, error) {
	in, err := objects.Contains(a, b)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(in), nil
}

// opIndexOf returns the first index of b in a.
//
// CPython: Modules/_operator.c:508 _operator_indexOf_impl
func opIndexOf(a, b objects.Object) (objects.Object, error) {
	i, err := objects.SequenceIndex(a, b)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(i)), nil
}

// opCountOf returns the number of occurrences of b in a.
//
// CPython: Modules/_operator.c:521 _operator_countOf_impl
func opCountOf(a, b objects.Object) (objects.Object, error) {
	n, err := objects.SequenceCount(a, b)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(n)), nil
}

// opGetItem is a[b].
//
// CPython: Modules/_operator.c:538 _operator_getitem_impl
func opGetItem(a, b objects.Object) (objects.Object, error) { return objects.GetItem(a, b) }

// setItemFunc is a[b] = c. Wired through a 3-arg builtin because
// binaryFunc only covers the 2-arg shape.
//
// CPython: Modules/_operator.c:556 _operator_setitem_impl
func setItemFunc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: setitem() takes no keyword arguments")
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: setitem expected 3 arguments, got %d", len(args))
	}
	if err := objects.SetItem(args[0], args[1], args[2]); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// opDelItem is del a[b].
//
// CPython: Modules/_operator.c:572 _operator_delitem_impl
func opDelItem(a, b objects.Object) (objects.Object, error) {
	if err := objects.DelItem(a, b); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// ---------------------------------------------------------------------------
// Rich comparisons.
// ---------------------------------------------------------------------------

// opEq is a == b.
//
// CPython: Modules/_operator.c:591 _operator_eq_impl
func opEq(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareEQ)
}

// opNe is a != b.
//
// CPython: Modules/_operator.c:604 _operator_ne_impl
func opNe(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareNE)
}

// opLt is a < b.
//
// CPython: Modules/_operator.c:617 _operator_lt_impl
func opLt(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareLT)
}

// opLe is a <= b.
//
// CPython: Modules/_operator.c:630 _operator_le_impl
func opLe(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareLE)
}

// opGt is a > b.
//
// CPython: Modules/_operator.c:643 _operator_gt_impl
func opGt(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareGT)
}

// opGe is a >= b.
//
// CPython: Modules/_operator.c:656 _operator_ge_impl
func opGe(a, b objects.Object) (objects.Object, error) {
	return objects.RichCmp(a, b, objects.CompareGE)
}

// ---------------------------------------------------------------------------
// Logical / identity tests.
// ---------------------------------------------------------------------------

// opTruth is bool(a).
//
// CPython: Modules/_operator.c:51 _operator_truth_impl
func opTruth(a objects.Object) (objects.Object, error) {
	b, err := objects.IsTruthy(a)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(b), nil
}

// opNot is not a.
//
// CPython: Modules/_operator.c:253 _operator_not__impl
func opNot(a objects.Object) (objects.Object, error) {
	b, err := objects.IsTruthy(a)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(!b), nil
}

// opIs is a is b. Identity uses pointer equality on the singletons.
//
// CPython: Modules/_operator.c:711 _operator_is__impl
func opIs(a, b objects.Object) (objects.Object, error) {
	return objects.NewBool(a == b), nil
}

// opIsNot is a is not b.
//
// CPython: Modules/_operator.c:725 _operator_is_not_impl
func opIsNot(a, b objects.Object) (objects.Object, error) {
	return objects.NewBool(a != b), nil
}

// opIsNone is a is None.
//
// CPython: Modules/_operator.c:740 _operator_is_none
func opIsNone(a objects.Object) (objects.Object, error) {
	return objects.NewBool(objects.IsNone(a)), nil
}

// opIsNotNone is a is not None.
//
// CPython: Modules/_operator.c:754 _operator_is_not_none
func opIsNotNone(a objects.Object) (objects.Object, error) {
	return objects.NewBool(!objects.IsNone(a)), nil
}

// ---------------------------------------------------------------------------
// length_hint.
// ---------------------------------------------------------------------------

// lengthHintFunc is length_hint(obj, default=0). Tries len(obj), then
// type(obj).__length_hint__(obj), and falls back to default. Mirrors
// PyObject_LengthHint.
//
// CPython: Modules/_operator.c:826 _operator_length_hint_impl
// CPython: Objects/abstract.c:64 PyObject_LengthHint
func lengthHintFunc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: length_hint() takes no keyword arguments")
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: length_hint expected 1 or 2 arguments, got %d", len(args))
	}
	obj := args[0]
	var defaultVal int64
	if len(args) == 2 {
		di, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: '%s' object cannot be interpreted as an integer", args[1].Type().Name)
		}
		v, fits := di.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: default value too large")
		}
		defaultVal = v
	}

	// Delegate to PyObject_LengthHint: len(obj) first, then
	// __length_hint__, else default. Only a TypeError out of either
	// dunder is swallowed; any other exception (ZeroDivisionError from a
	// pathological __len__, say) propagates rather than collapsing to the
	// default.
	//
	// CPython: Objects/abstract.c:64 PyObject_LengthHint
	n, err := objects.LengthHint(obj, defaultVal)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(n), nil
}

// ---------------------------------------------------------------------------
// _compare_digest.
// ---------------------------------------------------------------------------

// opCompareDigest is a constant-time bytes/str equality test used by
// the hmac module to mitigate timing attacks.
//
// CPython: Modules/_operator.c:852 _operator__compare_digest_impl
func opCompareDigest(a, b objects.Object) (objects.Object, error) {
	if sa, ok := a.(*objects.Unicode); ok {
		sb, ok := b.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: unsupported operand types(s) or combination of types: '%s' and '%s'", a.Type().Name, b.Type().Name)
		}
		if !sa.IsASCII() || !sb.IsASCII() {
			return nil, fmt.Errorf("TypeError: comparing strings with non-ASCII characters is not supported")
		}
		return objects.NewBool(tscmp([]byte(sa.Value()), []byte(sb.Value()))), nil
	}
	ba, okA := bytesLike(a)
	bb, okB := bytesLike(b)
	if !okA || !okB {
		return nil, fmt.Errorf("TypeError: unsupported operand types(s) or combination of types: '%s' and '%s'", a.Type().Name, b.Type().Name)
	}
	return objects.NewBool(tscmp(ba, bb)), nil
}

// bytesLike extracts a byte slice from a bytes or bytearray object;
// other types come back as (_, false). Mirrors the PyObject_GetBuffer
// fast-path used by _compare_digest for buffer-protocol arguments.
func bytesLike(o objects.Object) ([]byte, bool) {
	switch v := o.(type) {
	case *objects.Bytes:
		return v.Bytes(), true
	case *objects.ByteArray:
		return v.Bytes(), true
	}
	return nil, false
}

// tscmp is a timing-safe byte comparison. It always loops len(b)
// times so the elapsed time depends on b alone; mismatched lengths
// short-circuit to false but still walk b.
//
// CPython: Modules/_operator.c:772 _tscmp
func tscmp(a, b []byte) bool {
	lenA := len(a)
	lenB := len(b)
	var left []byte
	var result byte
	if lenA == lenB {
		left = a
		result = 0
	} else {
		left = b
		result = 1
	}
	for i := 0; i < lenB; i++ {
		result |= left[i] ^ b[i]
	}
	return result == 0
}

// ---------------------------------------------------------------------------
// call.
// ---------------------------------------------------------------------------

// callFunc is operator.call(obj, /, *args, **kwargs). The runtime
// already has a Call helper that takes a tuple/dict; this just
// repackages the positional tail and forwards.
//
// CPython: Modules/_operator.c:928 _operator_call
func callFunc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: call expected at least 1 argument, got 0")
	}
	fn := args[0]
	tup := objects.NewTuple(args[1:])
	var kwd *objects.Dict
	if len(kwargs) > 0 {
		kwd = objects.NewDict()
		for k, v := range kwargs {
			if err := kwd.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return objects.Call(fn, tup, kwd)
}

// ---------------------------------------------------------------------------
// itemgetter.
// ---------------------------------------------------------------------------

// ItemgetterType is operator.itemgetter. Calling itemgetter(i) builds
// a callable that returns obj[i]; itemgetter(i, j) builds one that
// returns (obj[i], obj[j]).
//
// CPython: Modules/_operator.c:1234 itemgetter_type_spec
var ItemgetterType = objects.NewType("itemgetter", []*objects.Type{objects.ObjectType()})

// Itemgetter is the concrete shape backing an itemgetter instance.
// Mirrors itemgetterobject.
//
// CPython: Modules/_operator.c:1016 itemgetterobject
type Itemgetter struct {
	objects.Header
	items []objects.Object
	// single is true when the constructor received exactly one
	// argument; the call path then returns the scalar obj[items[0]]
	// rather than building a tuple.
	single bool
}

func init() {
	t := ItemgetterType
	t.Module = "operator"
	t.Repr = itemgetterRepr
	t.Str = itemgetterRepr
	t.Call = itemgetterCall
	t.TpNew = itemgetterNew
	t.Getattro = objects.GenericGetAttr
	// CPython: Modules/_operator.c:1158 itemgetter_traverse
	t.TpTraverse = itemgetterTraverse
	// CPython: Modules/_operator.c:1145 itemgetter_dealloc
	t.Dealloc = itemgetterDealloc
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescr(t, "__reduce__", itemgetterReduce))
}

// itemgetterNew is operator.itemgetter.__new__: stash the requested
// items.
//
// CPython: Modules/_operator.c:1034 itemgetter_new
func itemgetterNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: itemgetter() takes no keyword arguments")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: itemgetter expected at least 1 argument, got 0")
	}
	ig := &Itemgetter{items: append([]objects.Object(nil), args...), single: len(args) == 1}
	// itemgetter owns a reference to each stashed item the way
	// itemgetter_new keeps the args tuple alive with Py_NewRef. Without
	// this a stashed slice (which recycles through a freelist on dealloc)
	// gets reclaimed once the constructor's stack temporary is dropped,
	// leaving Itemgetter.items pointing at a slice whose bounds are nil.
	//
	// CPython: Modules/_operator.c:1034 itemgetter_new (Py_NewRef item)
	for _, it := range ig.items {
		objects.Incref(it)
	}
	ig.Init(cls)
	return ig, nil
}

// itemgetterTraverse visits each stashed item so the cycle collector
// can see references the getter owns.
//
// CPython: Modules/_operator.c:1158 itemgetter_traverse
func itemgetterTraverse(o objects.Object, visit objects.Visitor) error {
	ig, ok := o.(*Itemgetter)
	if !ok {
		return nil
	}
	for _, it := range ig.items {
		if it == nil {
			continue
		}
		if err := visit(it); err != nil {
			return err
		}
	}
	return nil
}

// itemgetterDealloc releases the references the getter owns, mirroring
// itemgetter_clear followed by the tp_free in itemgetter_dealloc.
//
// CPython: Modules/_operator.c:1145 itemgetter_dealloc
func itemgetterDealloc(o objects.Object) {
	ig, ok := o.(*Itemgetter)
	if !ok {
		return
	}
	for _, it := range ig.items {
		objects.Decref(it)
	}
	ig.items = nil
}

// itemgetterCall is the tp_call slot: pull the saved items from obj
// and either return the single value or pack them into a tuple.
//
// CPython: Modules/_operator.c:1108 itemgetter_call / :1135 itemgetter_call_impl
func itemgetterCall(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: itemgetter expected no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: itemgetter expected 1 argument, got %d", len(args))
	}
	ig := o.(*Itemgetter)
	obj := args[0]
	if ig.single {
		return objects.GetItem(obj, ig.items[0])
	}
	out := make([]objects.Object, len(ig.items))
	for i, key := range ig.items {
		v, err := objects.GetItem(obj, key)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return objects.NewTuple(out), nil
}

// itemgetterRepr mirrors itemgetter_repr: a single-item getter renders
// "itemgetter(repr(item))", a multi-item getter renders
// "itemgetter(item1, item2, ...)".
//
// CPython: Modules/_operator.c:1172 itemgetter_repr
func itemgetterRepr(o objects.Object) (string, error) {
	ig := o.(*Itemgetter)
	parts := make([]string, len(ig.items))
	for i, it := range ig.items {
		r, err := objects.Repr(it)
		if err != nil {
			return "", err
		}
		parts[i] = r
	}
	return "operator.itemgetter(" + strings.Join(parts, ", ") + ")", nil
}

// itemgetterReduce is __reduce__: pickling support. Returns
// (type, items) where items is a tuple.
//
// CPython: Modules/_operator.c:1192 itemgetter_reduce
func itemgetterReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ig := args[0].(*Itemgetter)
	return objects.NewTuple([]objects.Object{ItemgetterType, objects.NewTuple(ig.items)}), nil
}

// ---------------------------------------------------------------------------
// attrgetter.
// ---------------------------------------------------------------------------

// AttrgetterType is operator.attrgetter. Each instance carries a list
// of dotted attribute paths; calling it returns the single value or a
// tuple of values, exactly the way attrgetter_call_impl does.
//
// CPython: Modules/_operator.c:1601 attrgetter_type_spec
var AttrgetterType = objects.NewType("attrgetter", []*objects.Type{objects.ObjectType()})

// Attrgetter is the runtime shape backing an attrgetter instance.
// chains[i] is the split list of dotted names for the ith attribute.
// raw[i] is the original string the user passed in (kept for repr
// and reduce).
//
// CPython: Modules/_operator.c:1245 attrgetterobject
type Attrgetter struct {
	objects.Header
	raw    []string
	chains [][]string
}

func init() {
	t := AttrgetterType
	t.Module = "operator"
	t.Repr = attrgetterRepr
	t.Str = attrgetterRepr
	t.Call = attrgetterCall
	t.TpNew = attrgetterNew
	t.Getattro = objects.GenericGetAttr
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescr(t, "__reduce__", attrgetterReduce))
}

// attrgetterNew is operator.attrgetter.__new__: validate that every
// argument is a str and pre-split dotted names.
//
// CPython: Modules/_operator.c:1262 attrgetter_new
func attrgetterNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: attrgetter() takes no keyword arguments")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: attrgetter expected at least 1 argument, got 0")
	}
	ag := &Attrgetter{
		raw:    make([]string, len(args)),
		chains: make([][]string, len(args)),
	}
	for i, a := range args {
		s, ok := a.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: attribute name must be a string")
		}
		ag.raw[i] = s.Value()
		ag.chains[i] = strings.Split(s.Value(), ".")
	}
	ag.Init(cls)
	return ag, nil
}

// dottedGetattr walks chain across obj, returning the final value.
//
// CPython: Modules/_operator.c:1397 dotted_getattr
func dottedGetattr(obj objects.Object, chain []string) (objects.Object, error) {
	cur := obj
	for _, name := range chain {
		v, err := objects.GetAttr(cur, objects.NewStr(name))
		if err != nil {
			return nil, err
		}
		cur = v
	}
	return cur, nil
}

// attrgetterCall is the tp_call slot.
//
// CPython: Modules/_operator.c:1430 attrgetter_call / :1456 attrgetter_call_impl
func attrgetterCall(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: attrgetter expected no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: attrgetter expected 1 argument, got %d", len(args))
	}
	ag := o.(*Attrgetter)
	obj := args[0]
	if len(ag.chains) == 1 {
		return dottedGetattr(obj, ag.chains[0])
	}
	out := make([]objects.Object, len(ag.chains))
	for i, chain := range ag.chains {
		v, err := dottedGetattr(obj, chain)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return objects.NewTuple(out), nil
}

// attrgetterRepr renders "operator.attrgetter('a', 'b.c', ...)" with
// each raw name quoted via Repr.
//
// CPython: Modules/_operator.c:1525 attrgetter_repr
func attrgetterRepr(o objects.Object) (string, error) {
	ag := o.(*Attrgetter)
	parts := make([]string, len(ag.raw))
	for i, name := range ag.raw {
		r, err := objects.Repr(objects.NewStr(name))
		if err != nil {
			return "", err
		}
		parts[i] = r
	}
	return "operator.attrgetter(" + strings.Join(parts, ", ") + ")", nil
}

// attrgetterReduce is __reduce__: returns (type, (name1, name2, ...)).
//
// CPython: Modules/_operator.c:1558 attrgetter_reduce
func attrgetterReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	ag := args[0].(*Attrgetter)
	names := make([]objects.Object, len(ag.raw))
	for i, n := range ag.raw {
		names[i] = objects.NewStr(n)
	}
	return objects.NewTuple([]objects.Object{AttrgetterType, objects.NewTuple(names)}), nil
}

// ---------------------------------------------------------------------------
// methodcaller.
// ---------------------------------------------------------------------------

// MethodcallerType is operator.methodcaller. Calling
// methodcaller(name, *args, **kw)(obj) invokes obj.name(*args, **kw).
//
// CPython: Modules/_operator.c:1942 methodcaller_type_spec
var MethodcallerType = objects.NewType("methodcaller", []*objects.Type{objects.ObjectType()})

// Methodcaller is the runtime shape.
//
// CPython: Modules/_operator.c:1613 methodcallerobject
type Methodcaller struct {
	objects.Header
	name string
	args []objects.Object
	kwds map[string]objects.Object
}

func init() {
	t := MethodcallerType
	t.Module = "operator"
	t.Repr = methodcallerRepr
	t.Str = methodcallerRepr
	t.Call = methodcallerCall
	t.TpNew = methodcallerNew
	t.Getattro = objects.GenericGetAttr
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescr(t, "__reduce__", methodcallerReduce))
}

// methodcallerNew validates the name is a str and stashes the
// remaining args/kwds for the call site.
//
// CPython: Modules/_operator.c:1690 methodcaller_new
func methodcallerNew(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: methodcaller needs at least one argument, the method name")
	}
	name, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: method name must be a string")
	}
	mc := &Methodcaller{
		name: name.Value(),
		args: append([]objects.Object(nil), args[1:]...),
	}
	if len(kwargs) > 0 {
		mc.kwds = make(map[string]objects.Object, len(kwargs))
		for k, v := range kwargs {
			mc.kwds[k] = v
		}
	}
	mc.Init(cls)
	return mc, nil
}

// methodcallerCall looks up the method on obj then invokes it with
// the stashed (args, kwds).
//
// CPython: Modules/_operator.c:1778 methodcaller_call
func methodcallerCall(o objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: methodcaller expected no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: methodcaller expected 1 argument, got %d", len(args))
	}
	mc := o.(*Methodcaller)
	method, err := objects.GetAttr(args[0], objects.NewStr(mc.name))
	if err != nil {
		return nil, err
	}
	tup := objects.NewTuple(mc.args)
	var kwd *objects.Dict
	if len(mc.kwds) > 0 {
		kwd = objects.NewDict()
		for k, v := range mc.kwds {
			if err := kwd.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return objects.Call(method, tup, kwd)
}

// methodcallerRepr renders "operator.methodcaller('name', args..., k=v, ...)".
//
// CPython: Modules/_operator.c:1798 methodcaller_repr
func methodcallerRepr(o objects.Object) (string, error) {
	mc := o.(*Methodcaller)
	parts := []string{"'" + mc.name + "'"}
	for _, a := range mc.args {
		r, err := objects.Repr(a)
		if err != nil {
			return "", err
		}
		parts = append(parts, r)
	}
	// Stable key order so the repr is reproducible.
	for _, k := range sortedKeys(mc.kwds) {
		vr, err := objects.Repr(mc.kwds[k])
		if err != nil {
			return "", err
		}
		parts = append(parts, k+"="+vr)
	}
	return "operator.methodcaller(" + strings.Join(parts, ", ") + ")", nil
}

// methodcallerReduce is __reduce__. Without kwargs, returns
// (type, (name, *args)); with kwargs, the C version builds a
// functools.partial(type, name, **kwds) factory. gopy mirrors the
// behavior: when kwds is empty, return the simple tuple; otherwise
// pull functools.partial through the module system and return
// (partial(type, name, **kwds), args).
//
// CPython: Modules/_operator.c:1875 methodcaller_reduce
func methodcallerReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	mc := args[0].(*Methodcaller)
	if len(mc.kwds) == 0 {
		out := make([]objects.Object, 0, 1+len(mc.args))
		out = append(out, objects.NewStr(mc.name))
		out = append(out, mc.args...)
		return objects.NewTuple([]objects.Object{MethodcallerType, objects.NewTuple(out)}), nil
	}
	// Build functools.partial(type(mc), name, **kwds), pair with tuple of args.
	partial, err := loadFunctoolsPartial()
	if err != nil {
		return nil, err
	}
	pkw := objects.NewDict()
	for k, v := range mc.kwds {
		if err := pkw.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}
	ctor, err := objects.Call(partial, objects.NewTuple([]objects.Object{MethodcallerType, objects.NewStr(mc.name)}), pkw)
	if err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{ctor, objects.NewTuple(mc.args)}), nil
}

// loadFunctoolsPartial fetches functools.partial through the import
// system. Equivalent to PyImport_ImportModuleAttrString("functools",
// "partial") in the C source. The simpler GetModule lookup suffices
// for __reduce__: by the time pickle calls into methodcaller, the
// functools module is already loaded (operator.py imports it
// transitively at module-init time anyway).
func loadFunctoolsPartial() (objects.Object, error) {
	mod, ok := imp.GetModule("functools")
	if !ok {
		return nil, fmt.Errorf("ImportError: functools not imported")
	}
	return objects.GetAttr(mod, objects.NewStr("partial"))
}

// sortedKeys returns the keys of m in ascending order. methodcaller
// repr / reduce use insertion order in CPython; Go's map iteration is
// randomized so we sort to keep output stable.
func sortedKeys(m map[string]objects.Object) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
