// Port of the constructor wrappers from Python/bltinmodule.c. The
// type singletons (PyLong_Type, PyFloat_Type, PyBool_Type, PyList_Type,
// PyTuple_Type, PyDict_Type) are exposed as builtins; calling them
// runs the constructor shape CPython exposes through tp_call. v0.7
// covers the call surface that user code relies on; the type
// singletons themselves stay parked until the type port lands.
//
// CPython: Objects/longobject.c long_new, Objects/floatobject.c
// float_new, Objects/boolobject.c bool_new, Objects/listobject.c
// list_init, Objects/tupleobject.c tuple_new, Objects/dictobject.c
// dict_init.

package builtins

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/tamnd/gopy/abstract"
	"github.com/tamnd/gopy/codecs"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/pystrconv"
)

// IntCtor ports long_new. 0 args returns 0; one positional converts
// via PyNumber_Long; two arguments parse a string in the given base.
//
// CPython: Objects/longobject.c long_new_impl
func IntCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewInt(0), nil
	}
	if base, ok := kwargs["base"]; ok {
		args = append(args, base)
	}
	if len(args) == 1 {
		return numberToInt(args[0])
	}
	if len(args) == 2 {
		// CPython: Objects/longobject.c:5904 long_new_impl accepts
		// str, bytes, or bytearray when a base is given.
		var s string
		switch v := args[0].(type) {
		case *objects.Bytes:
			s = string(v.Bytes())
		case *objects.ByteArray:
			s = string(v.Bytes())
		default:
			if args[0].Type() != objects.StrType() {
				return nil, fmt.Errorf("TypeError: int() can't convert non-string with explicit base")
			}
			s, _ = objects.Str(args[0])
		}
		baseInt, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: int() base must be an integer")
		}
		base, fits := baseInt.Int64()
		if !fits || base < 0 || base == 1 || base > 36 {
			return nil, fmt.Errorf("ValueError: int() base must be 0 or 2-36")
		}
		return parseIntString(s, int(base))
	}
	return nil, fmt.Errorf("TypeError: int expected at most 2 arguments, got %d", len(args))
}

func numberToInt(o objects.Object) (objects.Object, error) {
	switch v := o.(type) {
	case *objects.Bool:
		// bool is a subclass of int; PyNumber_Long(True) returns the
		// plain int 1 (not the True singleton), so strip the bool
		// wrapper and rebuild as an Int.
		//
		// CPython: Objects/boolobject.c bool_int / long_new_impl PyNumber_Long
		n, _ := v.Int64()
		return objects.NewInt(n), nil
	case *objects.Int:
		return v, nil
	case *objects.Float:
		// CPython: Objects/longobject.c:456 PyLong_FromDouble
		f := v.Float64()
		if math.IsInf(f, 0) {
			return nil, fmt.Errorf("OverflowError: cannot convert float infinity to integer")
		}
		if math.IsNaN(f) {
			return nil, fmt.Errorf("ValueError: cannot convert float NaN to integer")
		}
		out, _ := new(big.Float).SetFloat64(f).Int(nil)
		return objects.NewIntFromBig(out), nil
	case *objects.Bytes:
		return parseIntString(string(v.Bytes()), 10)
	case *objects.ByteArray:
		return parseIntString(string(v.Bytes()), 10)
	}
	if o.Type() == objects.StrType() {
		s, _ := objects.Str(o)
		return parseIntString(s, 10)
	}
	if n := o.Type().Number; n != nil && n.Int != nil {
		return n.Int(o)
	}
	return nil, fmt.Errorf("TypeError: int() argument must be a string or a number, not '%s'", o.Type().Name)
}

func parseIntString(s string, base int) (objects.Object, error) {
	out := new(big.Int)
	_, ok := out.SetString(stripIntLiteral(s, base), parseBase(s, base))
	if !ok {
		return nil, fmt.Errorf("ValueError: invalid literal for int() with base %d: %q", base, s)
	}
	return objects.NewIntFromBig(out), nil
}

// stripIntLiteral matches PyLong_FromString's handling of the 0b/0o/0x
// prefix when base is 0 or matches the prefix.
func stripIntLiteral(s string, base int) string {
	t := trimSpace(s)
	sign := ""
	if t != "" && (t[0] == '+' || t[0] == '-') {
		sign = string(t[0])
		t = t[1:]
	}
	if len(t) > 2 && t[0] == '0' {
		switch {
		case (base == 0 || base == 2) && (t[1] == 'b' || t[1] == 'B'):
			t = t[2:]
		case (base == 0 || base == 8) && (t[1] == 'o' || t[1] == 'O'):
			t = t[2:]
		case (base == 0 || base == 16) && (t[1] == 'x' || t[1] == 'X'):
			t = t[2:]
		}
	}
	out := make([]byte, 0, len(t))
	for i := 0; i < len(t); i++ {
		if t[i] == '_' {
			continue
		}
		out = append(out, t[i])
	}
	return sign + string(out)
}

func parseBase(s string, base int) int {
	if base != 0 {
		return base
	}
	t := trimSpace(s)
	if t != "" && (t[0] == '+' || t[0] == '-') {
		t = t[1:]
	}
	if len(t) > 1 && t[0] == '0' {
		switch t[1] {
		case 'b', 'B':
			return 2
		case 'o', 'O':
			return 8
		case 'x', 'X':
			return 16
		}
	}
	return 10
}

func trimSpace(s string) string {
	for s != "" && pystrconv.IsSpace(s[0]) {
		s = s[1:]
	}
	for s != "" && pystrconv.IsSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

// FloatCtor ports float_new. 0 args returns 0.0; one positional
// converts via PyNumber_Float, including PyOS_string_to_double for
// strings.
//
// CPython: Objects/floatobject.c float_new_impl
func FloatCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: float() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.NewFloat(0), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: float expected at most 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case *objects.Float:
		// CPython: Objects/floatobject.c:1595 float_new_impl
		// For exact float, return self. For subclasses, call __float__
		// if user-defined; otherwise extract value and return a new
		// plain float.
		if v.Type() == objects.FloatType {
			return v, nil
		}
		// Only dispatch through __float__ when the subclass defines it.
		// If not user-defined, the inherited slot returns self (a subclass
		// instance), which would produce a spurious DeprecationWarning.
		if objects.IsOwnDescriptor(v.Type(), "__float__") {
			if n := v.Type().Number; n != nil && n.Float != nil {
				res, err := n.Float(v)
				if err != nil {
					return nil, err
				}
				// __float__ must return a float.
				rf, isFloat := res.(*objects.Float)
				if !isFloat {
					return nil, fmt.Errorf("TypeError: __float__ returned non-float (type %s)", res.Type().Name)
				}
				if rf.Type() == objects.FloatType {
					return rf, nil
				}
				// __float__ returned a float subclass: emit DeprecationWarning
				// and unwrap to a plain float.
				// CPython: Objects/floatobject.c _PyFloat_FromNumberWithBase
				msg := fmt.Sprintf("__float__ returned non-float (type %s). "+
					"The ability to return an instance of a strict subclass of float "+
					"is deprecated, and may be removed in a future version of Python.",
					rf.Type().Name)
				if objects.DeprecWarnHook != nil {
					if werr := objects.DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
				return objects.NewFloat(rf.Float64()), nil
			}
		}
		return objects.NewFloat(v.Float64()), nil
	case *objects.Int:
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		return objects.NewFloat(f), nil
	}
	// CPython: Objects/abstract.c:1592 PyNumber_Float — try nb_float first
	if n := args[0].Type().Number; n != nil {
		if n.Float != nil {
			res, err := n.Float(args[0])
			if err != nil {
				return nil, err
			}
			// If __float__ returned a non-exact float subclass, emit
			// DeprecationWarning and unwrap.
			// CPython: Objects/abstract.c:1614 PyNumber_Float
			if rf, ok := res.(*objects.Float); ok && rf.Type() != objects.FloatType {
				msg := fmt.Sprintf("__float__ returned non-float (type %s). "+
					"The ability to return an instance of a strict subclass of float "+
					"is deprecated, and may be removed in a future version of Python.",
					rf.Type().Name)
				if objects.DeprecWarnHook != nil {
					if werr := objects.DeprecWarnHook(msg); werr != nil {
						return nil, werr
					}
				}
				return objects.NewFloat(rf.Float64()), nil
			}
			return res, nil
		}
		// CPython: Objects/abstract.c:1630 PyNumber_Float — nb_index fallback
		if n.Index != nil {
			idx, err := n.Index(args[0])
			if err != nil {
				return nil, err
			}
			i, ok := idx.(*objects.Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __index__ returned non-int (type %s)", idx.Type().Name)
			}
			f, _ := new(big.Float).SetInt(i.BigInt()).Float64()
			if math.IsInf(f, 0) {
				return nil, fmt.Errorf("OverflowError: int too large to convert to float")
			}
			return objects.NewFloat(f), nil
		}
	}
	// CPython: Objects/floatobject.c:190 PyFloat_FromString
	// Bytes, ByteArray, MemoryView, and buffer-protocol objects (array.array).
	if buf, ok := objects.AsBytesLike(args[0]); ok {
		f, err := pystrconv.ParseFloat(trimSpace(string(buf)))
		if err != nil {
			r, _ := objects.Repr(args[0])
			return nil, fmt.Errorf("ValueError: could not convert string to float: %s", r)
		}
		return objects.NewFloat(f), nil
	}
	// CPython: Objects/floatobject.c:205 PyFloat_FromString — Unicode (str and subclasses)
	if objects.IsSubtype(args[0].Type(), objects.StrType()) {
		s, _ := objects.Str(args[0])
		f, err := pystrconv.ParseFloat(trimSpace(s))
		if err != nil {
			r, _ := objects.Repr(args[0])
			return nil, fmt.Errorf("ValueError: could not convert string to float: %s", r)
		}
		return objects.NewFloat(f), nil
	}
	return nil, fmt.Errorf("TypeError: float() argument must be a string or a real number, not '%s'", args[0].Type().Name)
}

// BoolCtor ports bool_new. 0 args returns False; one positional runs
// through PyObject_IsTrue. Keyword arguments are rejected.
//
// CPython: Objects/boolobject.c bool_new
func BoolCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("TypeError: bool() takes no keyword arguments")
	}
	if len(args) == 0 {
		return objects.False(), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: bool expected at most 1 argument, got %d", len(args))
	}
	t, err := objects.IsTruthy(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewBool(t), nil
}

// ListCtor ports list_init. 0 args returns []; one positional drains
// the iterable into a fresh list.
//
// CPython: Objects/listobject.c list_init
func ListCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewList(nil), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: list expected at most 1 argument, got %d", len(args))
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewList(items), nil
}

// bindListCtor wires list's constructor as separate TpNew (allocate) and
// __init__ (populate). bindCtor would conflate them, so subclasses like
// `class S(list): pass` would lose their type because TpNew always
// returned a plain *List instead of binding the requested cls.
//
// CPython: Objects/listobject.c:3380 PyList_Type (tp_new = list_new, tp_init = list___init__)
func bindListCtor(t *objects.Type) {
	// TpNew is set in objects/list.go to allocate a bare *List bound to
	// the requested class. __init__ clears it, then drains an optional
	// iterable into it.
	//
	// CPython: Objects/listobject.c:2716 list___init___impl
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("TypeError: list expected at most 1 argument, got %d", len(args)-1)
		}
		l, ok := args[0].(*objects.List)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor '__init__' requires a 'list' object but received a '%s'", args[0].Type().Name)
		}
		l.Clear()
		if len(args) == 2 {
			items, err := drainIterable(args[1])
			if err != nil {
				return nil, err
			}
			for _, v := range items {
				l.Append(v)
			}
		}
		return objects.None(), nil
	}))
	bindCtorDescr(t, ListCtor)
}

// TupleCtor ports tuple_new. 0 args returns the empty tuple; one
// positional drains the iterable into a tuple.
//
// CPython: Objects/tupleobject.c tuple_new_impl
func TupleCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewTuple(nil), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: tuple expected at most 1 argument, got %d", len(args))
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewTuple(items), nil
}

// SetCtor ports set_new. Allocates an empty set of the correct subtype.
// Population is deferred to set_init (__init__) so single-pass iterables
// (map, filter, generator) are consumed only once.
//
// CPython: Objects/setobject.c:2436 set_new (allocate only, no iterable drain)
func SetCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return setCtorWithType(objects.SetType, args)
}

func setCtorWithType(cls *objects.Type, args []objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: set expected at most 1 argument, got %d", len(args))
	}
	return objects.NewSetOfType(cls), nil
}

// FrozensetCtor ports frozenset_new. 0 args returns an empty frozenset; one
// positional arg that is already an exact frozenset is returned as-is
// (CPython optimization: immutable object, no copy needed). cls is the
// concrete type to allocate so frozenset subclasses carry their own type.
//
// CPython: Objects/setobject.c:1195 frozenset_new
func FrozensetCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return frozensetCtorWithType(objects.FrozensetType, args, kwargs)
}

// frozensetCtorWithType is the TpNew body for frozenset and its subclasses.
// Keyword arguments are rejected when cls is exact frozenset or when cls
// has not overridden __init__ (tp_init == NULL), matching CPython's
// frozenset_new guard:
//
//	if (type == &PyFrozenSet_Type || type->tp_init == PyFrozenSet_Type.tp_init)
//
// CPython: Objects/setobject.c:1195 frozenset_new
func frozensetCtorWithType(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) > 0 {
		// Reject kwargs unless the subclass has a user-defined __init__.
		// CPython: Objects/setobject.c:1198-1201 frozenset_new kwds guard:
		//   (type == &PyFrozenSet_Type || type->tp_init == PyFrozenSet_Type.tp_init)
		// PyFrozenSet_Type.tp_init is NULL; the equivalent in gopy is that
		// no __init__ defined in the class's own MRO between the subclass
		// and object/frozenset. If __init__ is found only on object or not at
		// all, it counts as tp_init == NULL → reject kwargs.
		rejectKwargs := true
		if cls != objects.FrozensetType {
			if _, owner := objects.LookupDescriptor(cls, "__init__"); owner != nil &&
				owner != objects.FrozensetType && owner != objects.ObjectType() {
				rejectKwargs = false
			}
		}
		if rejectKwargs {
			return nil, fmt.Errorf("TypeError: frozenset() does not support keyword arguments")
		}
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: frozenset expected at most 1 argument, got %d", len(args))
	}
	if len(args) == 0 {
		return objects.NewFrozensetOfType(cls, nil)
	}
	// CPython: Objects/setobject.c:2375 frozenset_new — if the argument is
	// already an exact frozenset of the same type, return it unchanged.
	if fs, ok := args[0].(*objects.Set); ok && fs.Type() == cls && cls == objects.FrozensetType {
		return fs, nil
	}
	// Use SetUpdateFrom for dict/set fast path (no rehashing of cached hashes).
	fs, err := objects.NewFrozensetOfType(cls, nil)
	if err != nil {
		return nil, err
	}
	if err := objects.SetUpdateFrom(fs, args[0]); err != nil {
		return nil, err
	}
	return fs, nil
}

// DictCtor ports dict_init. Accepts a mapping or an iterable of
// 2-element iterables, and merges keyword arguments.
//
// CPython: Objects/dictobject.c dict_init
func DictCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: dict expected at most 1 argument, got %d", len(args))
	}
	out := objects.NewDict()
	if len(args) == 1 {
		if d, ok := args[0].(*objects.Dict); ok {
			if err := mergeDict(out, d); err != nil {
				return nil, err
			}
		} else {
			if err := mergeFromPairs(out, args[0]); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range kwargs {
		if err := out.SetItem(objects.NewStr(k), v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// bindDictCtor wires dict's constructor as separate TpNew (allocate) and
// __init__ (populate). bindCtor would conflate them, breaking dict subclasses
// whose __init__ calls super().__init__() with extra args.
//
// CPython: Objects/dictobject.c:4023 PyDict_Type (tp_new = dict_new, tp_init = dict_init)
func bindDictCtor(t *objects.Type) {
	// TpNew is already set in objects/dict.go to allocate a bare *Dict.
	// __init__ populates it from an optional mapping/iterable + kwargs.
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("TypeError: dict expected at most 1 argument, got %d", len(args)-1)
		}
		d := args[0].(*objects.Dict)
		if len(args) == 2 {
			if src, ok := args[1].(*objects.Dict); ok {
				if err := mergeDict(d, src); err != nil {
					return nil, err
				}
			} else if dictHasKeys(args[1]) {
				if err := mergeMappingInto(d, args[1]); err != nil {
					return nil, err
				}
			} else {
				if err := mergeFromPairs(d, args[1]); err != nil {
					return nil, err
				}
			}
		}
		for k, v := range kwargs {
			if err := d.SetItem(objects.NewStr(k), v); err != nil {
				return nil, err
			}
		}
		return objects.None(), nil
	}))
}

func dictHasKeys(o objects.Object) bool {
	_, err := objects.GetAttr(o, objects.NewStr("keys"))
	return err == nil
}

func mergeMappingInto(dst *objects.Dict, m objects.Object) error {
	keysAttr, err := objects.GetAttr(m, objects.NewStr("keys"))
	if err != nil {
		return err
	}
	keysObj, err := objects.Call(keysAttr, objects.NewTuple(nil), nil)
	if err != nil {
		return err
	}
	it, err := abstract.Iter(keysObj)
	if err != nil {
		return err
	}
	for {
		k, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return nil
		}
		if err != nil {
			return err
		}
		v, err := objects.GetItem(m, k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
}

func drainIterable(o objects.Object) ([]objects.Object, error) {
	it, err := abstract.Iter(o)
	if err != nil {
		return nil, err
	}
	var items []objects.Object
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
}

func mergeDict(dst, src *objects.Dict) error {
	keys := src.Keys()
	for _, k := range keys {
		v, err := src.GetItem(k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
	return nil
}

// BytesCtor ports bytes_new.
// bytes()                       -> b""
// bytes(int)                    -> zero-filled bytes of that length
// bytes(iterable)               -> bytes from ints in iterable
// bytes(bytes/bytearray)        -> copy
// bytes(str, encoding[, errors]) -> str.encode(encoding, errors)
//
// CPython: Objects/bytesobject.c bytes_new_impl
func BytesCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewBytes(nil), nil
	}
	// Look for a str source first so the encoding/errors kwargs are
	// honored before the iterable fallback catches str (which is iterable).
	srcIsStr := args[0].Type() == objects.StrType()
	if srcIsStr {
		encoding := ""
		errorsName := "strict"
		if len(args) > 1 {
			if u := args[1].Type() == objects.StrType(); u {
				s, _ := objects.Str(args[1])
				encoding = s
			} else {
				return nil, fmt.Errorf("TypeError: bytes() encoding must be str, not '%s'", args[1].Type().Name)
			}
		}
		if v, ok := kwargs["encoding"]; ok {
			if v.Type() != objects.StrType() {
				return nil, fmt.Errorf("TypeError: bytes() encoding must be str, not '%s'", v.Type().Name)
			}
			s, _ := objects.Str(v)
			encoding = s
		}
		if len(args) > 2 {
			if args[2].Type() != objects.StrType() {
				return nil, fmt.Errorf("TypeError: bytes() errors must be str, not '%s'", args[2].Type().Name)
			}
			s, _ := objects.Str(args[2])
			errorsName = s
		}
		if v, ok := kwargs["errors"]; ok {
			if v.Type() != objects.StrType() {
				return nil, fmt.Errorf("TypeError: bytes() errors must be str, not '%s'", v.Type().Name)
			}
			s, _ := objects.Str(v)
			errorsName = s
		}
		if encoding == "" {
			return nil, fmt.Errorf("TypeError: string argument without an encoding")
		}
		s, _ := objects.Str(args[0])
		out, _, encErr := codecs.Encode(s, encoding, errorsName)
		if encErr != nil {
			return nil, encErr
		}
		return objects.NewBytes(out), nil
	}
	switch v := args[0].(type) {
	case *objects.Bytes:
		return objects.NewBytes(v.Bytes()), nil
	case *objects.ByteArray:
		return objects.NewBytes(v.Bytes()), nil
	case *objects.Int:
		n, ok := v.Int64()
		if !ok || n < 0 {
			return nil, fmt.Errorf("ValueError: bytes(): negative count")
		}
		return objects.NewBytes(make([]byte, n)), nil
	}
	// iterable of ints
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, fmt.Errorf("TypeError: cannot convert '%s' object to bytes", args[0].Type().Name)
	}
	buf := make([]byte, len(items))
	for i, item := range items {
		iv, ok := item.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: bytes must be integers, not '%s'", item.Type().Name)
		}
		n, fits := iv.Int64()
		if !fits || n < 0 || n > 255 {
			return nil, fmt.Errorf("ValueError: bytes must be in range(0, 256)")
		}
		buf[i] = byte(n)
	}
	return objects.NewBytes(buf), nil
}

// ByteArrayCtor ports bytearray_new.
// Same construction shapes as bytes, but returns a mutable bytearray.
//
// CPython: Objects/bytearrayobject.c bytearray_new_impl
func ByteArrayCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	b, err := BytesCtor(args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewByteArray(b.(*objects.Bytes).Bytes()), nil
}

func mergeFromPairs(dst *objects.Dict, iterable objects.Object) error {
	it, err := abstract.Iter(iterable)
	if err != nil {
		return err
	}
	i := 0
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return nil
		}
		if err != nil {
			return err
		}
		pair, err := drainIterable(v)
		if err != nil {
			return err
		}
		if len(pair) != 2 {
			return fmt.Errorf("ValueError: dictionary update sequence element #%d has length %d; 2 is required", i, len(pair))
		}
		if err := dst.SetItem(pair[0], pair[1]); err != nil {
			return err
		}
		i++
	}
}

// ComplexCtor ports complex(). Accepts complex(real=0, imag=0) or
// complex(x) where x is a number or string.
//
// CPython: Objects/complexobject.c:739 complex_new_impl
func ComplexCtor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	// Pick up keyword args.
	var realObj, imagObj objects.Object
	if v, ok := kwargs["real"]; ok {
		realObj = v
	}
	if v, ok := kwargs["imag"]; ok {
		imagObj = v
	}
	for k := range kwargs {
		if k != "real" && k != "imag" {
			return nil, fmt.Errorf("TypeError: complex() got unexpected keyword argument '%s'", k)
		}
	}
	switch len(args) {
	case 0:
		// complex() or complex(real=x, imag=y)
	case 1:
		if realObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'real'")
		}
		realObj = args[0]
	case 2:
		if realObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'real'")
		}
		if imagObj != nil {
			return nil, fmt.Errorf("TypeError: complex() got multiple values for argument 'imag'")
		}
		realObj = args[0]
		imagObj = args[1]
	default:
		return nil, fmt.Errorf("TypeError: complex() takes at most 2 arguments (%d given)", len(args))
	}
	toFloat := func(o objects.Object) (float64, error) {
		if o == nil {
			return 0, nil
		}
		switch v := o.(type) {
		case *objects.Float:
			return v.Float64(), nil
		case *objects.Int:
			f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
			return f, nil
		case *objects.Complex:
			return real(v.Complex128()), nil
		}
		if n := o.Type().Number; n != nil && n.Float != nil {
			fv, err := n.Float(o)
			if err != nil {
				return 0, err
			}
			return fv.(*objects.Float).Float64(), nil
		}
		return 0, fmt.Errorf("TypeError: complex() argument must be a string or a number, not '%s'", o.Type().Name)
	}
	// String form: complex("1+2j")
	if realObj != nil && imagObj == nil {
		if realObj.Type() == objects.StrType() {
			s, _ := objects.Str(realObj)
			// strip whitespace
			s = trimSpace(s)
			c, err := parseComplexString(s)
			if err != nil {
				return nil, fmt.Errorf("ValueError: complex() arg is a malformed string")
			}
			return objects.NewComplex(real(c), imag(c)), nil
		}
		// If it's a complex, return it (imagObj ignored).
		if cv, ok := realObj.(*objects.Complex); ok {
			return cv, nil
		}
	}
	re, err := toFloat(realObj)
	if err != nil {
		return nil, err
	}
	im, err := toFloat(imagObj)
	if err != nil {
		return nil, err
	}
	// If real was complex, add its imag to im.
	if realObj != nil {
		if cv, ok := realObj.(*objects.Complex); ok {
			re = real(cv.Complex128())
			im += imag(cv.Complex128())
		}
	}
	return objects.NewComplex(re, im), nil
}

// parseComplexString parses a Python-style complex literal like "1+2j".
// Minimal port of CPython Objects/complexobject.c:392 complex_from_string_inner.
func parseComplexString(s string) (complex128, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	// Remove surrounding parens.
	if s[0] == '(' && s[len(s)-1] == ')' {
		s = trimSpace(s[1 : len(s)-1])
	}
	// Try parsing as a single float (real only or pure imaginary).
	if s[len(s)-1] == 'j' || s[len(s)-1] == 'J' {
		im, err := pystrconv.ParseFloat(s[:len(s)-1])
		if err == nil {
			return complex(0, im), nil
		}
		// Try "a+bj" or "a-bj" form.
		for i := len(s) - 2; i > 0; i-- {
			if s[i] == '+' || s[i] == '-' {
				re, err1 := pystrconv.ParseFloat(s[:i])
				im2, err2 := pystrconv.ParseFloat(s[i : len(s)-1])
				if err1 == nil && err2 == nil {
					return complex(re, im2), nil
				}
				break
			}
		}
		return 0, fmt.Errorf("malformed")
	}
	re, err := pystrconv.ParseFloat(s)
	if err != nil {
		return 0, err
	}
	return complex(re, 0), nil
}

// memoryViewCtor ports memoryview(). Accepts a single bytes-like object
// and returns a MemoryView wrapping it.
//
// CPython: Objects/memoryobject.c:930 memoryview_new_impl
func memoryViewCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: memoryview() takes exactly one argument (%d given)", len(args))
	}
	return objects.NewMemoryView(args[0])
}
