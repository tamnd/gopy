// Port of the aggregation panel from Python/bltinmodule.c: sum, min,
// max, any, all, sorted.

package builtins

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tamnd/gopy/abstract"
	"github.com/tamnd/gopy/objects"
)

// Sum ports builtin_sum_impl. Iterates iterable accumulating with
// PyNumber_Add starting from start (default 0). Strings are rejected
// to match CPython's "use ”.join() instead" guard.
//
// CPython: Python/bltinmodule.c:2658 builtin_sum_impl
func Sum(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: sum expected 1 or 2 arguments, got %d", len(args))
	}
	var start objects.Object = objects.NewInt(0)
	if len(args) == 2 {
		start = args[1]
	}
	if v, ok := kwargs["start"]; ok {
		start = v
	}
	if start.Type() == objects.StrType() {
		return nil, fmt.Errorf("TypeError: sum() can't sum strings [use ''.join(seq) instead]")
	}

	it, err := abstract.Iter(args[0])
	if err != nil {
		return nil, err
	}
	acc := start
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return acc, nil
		}
		if err != nil {
			return nil, err
		}
		acc, err = abstract.Add(acc, v)
		if err != nil {
			return nil, err
		}
	}
}

// MinOf ports builtin_min. With one positional, iterate; with two or
// more, treat args as the values directly. key= applies a callable
// per element to derive the comparison key. default= returns when
// the iterable is empty (only legal with one positional).
//
// CPython: Python/bltinmodule.c:1919 min_max
func MinOf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return minMax(args, kwargs, objects.CompareLT, "min")
}

// MaxOf ports builtin_max with the same dispatch shape as min.
//
// CPython: Python/bltinmodule.c:1948 builtin_max
func MaxOf(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return minMax(args, kwargs, objects.CompareGT, "max")
}

func minMax(args []objects.Object, kwargs map[string]objects.Object, op objects.CompareOp, name string) (objects.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: %s expected at least 1 argument, got 0", name)
	}
	keyFn, ok := kwargs["key"]
	if ok && keyFn == objects.None() {
		keyFn = nil
	}
	defVal, hasDefault := kwargs["default"]
	if hasDefault && len(args) > 1 {
		return nil, fmt.Errorf("TypeError: Cannot specify a default for %s() with multiple positional arguments", name)
	}

	var values objects.Object
	if len(args) == 1 {
		values = args[0]
	} else {
		values = objects.NewTuple(args)
	}
	it, err := abstract.Iter(values)
	if err != nil {
		return nil, err
	}

	var bestKey, bestVal objects.Object
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			break
		}
		if err != nil {
			return nil, err
		}
		k := v
		if keyFn != nil {
			k, err = applyKey(keyFn, v)
			if err != nil {
				return nil, err
			}
		}
		if bestVal == nil {
			bestKey, bestVal = k, v
			continue
		}
		better, err := objects.RichCmpBool(k, bestKey, op)
		if err != nil {
			return nil, err
		}
		if better {
			bestKey, bestVal = k, v
		}
	}
	if bestVal == nil {
		if hasDefault {
			return defVal, nil
		}
		return nil, fmt.Errorf("ValueError: %s() iterable argument is empty", name)
	}
	return bestVal, nil
}

// Any ports builtin_any: True iff any element is truthy. Short-circuits.
//
// CPython: Python/bltinmodule.c:467 builtin_any
func Any(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: any() takes exactly one argument (%d given)", len(args))
	}
	it, err := abstract.Iter(args[0])
	if err != nil {
		return nil, err
	}
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return objects.False(), nil
		}
		if err != nil {
			return nil, err
		}
		t, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		if t {
			return objects.True(), nil
		}
	}
}

// All ports builtin_all: False iff any element is falsy.
//
// CPython: Python/bltinmodule.c:445 builtin_all
func All(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: all() takes exactly one argument (%d given)", len(args))
	}
	it, err := abstract.Iter(args[0])
	if err != nil {
		return nil, err
	}
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return objects.True(), nil
		}
		if err != nil {
			return nil, err
		}
		t, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		if !t {
			return objects.False(), nil
		}
	}
}

// Sorted ports builtin_sorted. Materializes the iterable into a list
// and stable-sorts it via RichCmp(<). key= applies a callable per
// element to derive the comparison key; reverse= flips the order.
//
// CPython: Python/bltinmodule.c:2419 builtin_sorted
func Sorted(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: sorted expected 1 argument, got %d", len(args))
	}
	keyFn, hasKey := kwargs["key"]
	if hasKey && keyFn == objects.None() {
		keyFn = nil
	}
	reverse := false
	if v, ok := kwargs["reverse"]; ok {
		b, err := objects.IsTruthy(v)
		if err != nil {
			return nil, err
		}
		reverse = b
	}

	it, err := abstract.Iter(args[0])
	if err != nil {
		return nil, err
	}
	type pair struct{ key, val objects.Object }
	var pairs []pair
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			break
		}
		if err != nil {
			return nil, err
		}
		k := v
		if keyFn != nil {
			k, err = applyKey(keyFn, v)
			if err != nil {
				return nil, err
			}
		}
		pairs = append(pairs, pair{key: k, val: v})
	}

	op := objects.CompareLT
	if reverse {
		op = objects.CompareGT
	}
	var sortErr error
	sort.SliceStable(pairs, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		less, err := objects.RichCmpBool(pairs[i].key, pairs[j].key, op)
		if err != nil {
			sortErr = err
			return false
		}
		return less
	})
	if sortErr != nil {
		return nil, sortErr
	}
	out := make([]objects.Object, len(pairs))
	for i, p := range pairs {
		out[i] = p.val
	}
	return objects.NewList(out), nil
}

// applyKey routes through the call protocol so any callable shape
// (BuiltinFunction, Function, lambda, type) works.
func applyKey(fn, v objects.Object) (objects.Object, error) {
	tup := objects.NewTuple([]objects.Object{v})
	return objects.Call(fn, tup, nil)
}
