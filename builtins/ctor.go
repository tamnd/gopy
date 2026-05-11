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
	"math/big"

	"github.com/tamnd/gopy/abstract"
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
		if args[0].Type() != objects.StrType() {
			return nil, fmt.Errorf("TypeError: int() can't convert non-string with explicit base")
		}
		s, _ := objects.Str(args[0])
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
	case *objects.Int:
		return v, nil
	case *objects.Float:
		out, _ := new(big.Float).SetFloat64(v.Float64()).Int(nil)
		return objects.NewIntFromBig(out), nil
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
func FloatCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.NewFloat(0), nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: float expected at most 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case *objects.Float:
		return v, nil
	case *objects.Int:
		f, _ := new(big.Float).SetInt(v.BigInt()).Float64()
		return objects.NewFloat(f), nil
	}
	if args[0].Type() == objects.StrType() {
		s, _ := objects.Str(args[0])
		f, err := pystrconv.ParseFloat(trimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("ValueError: could not convert string to float: %q", s)
		}
		return objects.NewFloat(f), nil
	}
	if n := args[0].Type().Number; n != nil && n.Float != nil {
		return n.Float(args[0])
	}
	return nil, fmt.Errorf("TypeError: float() argument must be a string or a number, not '%s'", args[0].Type().Name)
}

// BoolCtor ports bool_new. 0 args returns False; one positional runs
// through PyObject_IsTrue.
//
// CPython: Objects/boolobject.c bool_new
func BoolCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
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

// SetCtor ports set_init. 0 args returns an empty set; one positional
// drains the iterable into a fresh set.
//
// CPython: Objects/setobject.c:2284 set_init
func SetCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: set expected at most 1 argument, got %d", len(args))
	}
	out := objects.NewSet()
	if len(args) == 0 {
		return out, nil
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := out.Add(item); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FrozensetCtor ports frozenset_new. 0 args returns the singleton
// empty frozenset (gopy materializes a fresh one each call); one
// positional drains the iterable into a frozenset.
//
// CPython: Objects/setobject.c:2362 frozenset_new
func FrozensetCtor(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: frozenset expected at most 1 argument, got %d", len(args))
	}
	if len(args) == 0 {
		return objects.NewFrozenset(nil)
	}
	items, err := drainIterable(args[0])
	if err != nil {
		return nil, err
	}
	return objects.NewFrozenset(items)
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
