// Port of the aggregation panel from Python/bltinmodule.c: sum, min,
// max, any, all, sorted.

package builtins

import (
	"errors"
	"fmt"
	"math"
	"math/big"
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
	// CPython rejects str, bytes, and bytearray start values up front so
	// that sum() never silently builds the wrong kind of concatenation.
	//
	// CPython: Python/bltinmodule.c:2686 builtin_sum_impl
	if start.Type() == objects.StrType() {
		return nil, fmt.Errorf("TypeError: sum() can't sum strings [use ''.join(seq) instead]")
	}
	if _, ok := start.(*objects.Bytes); ok {
		return nil, fmt.Errorf("TypeError: sum() can't sum bytes [use b''.join(seq) instead]")
	}
	if _, ok := start.(*objects.ByteArray); ok {
		return nil, fmt.Errorf("TypeError: sum() can't sum bytearray [use b''.join(seq) instead]")
	}

	it, err := abstract.Iter(args[0])
	if err != nil {
		return nil, err
	}
	acc := start

	// Fast paths keep the running sum in C-level scalars instead of
	// allocating a new Python object per item, and assume the inputs are
	// homogeneous. On the first item that breaks the assumption each path
	// converts its scalar back to a Python object and falls through to the
	// next path (or the general loop), exactly as CPython does.
	//
	// CPython: Python/bltinmodule.c:2825 builtin_sum_impl (SLOW_SUM guard)
	if acc != nil && acc.Type() == objects.IntType {
		acc, err = sumIntFast(it, acc.(*objects.Int).BigInt())
		if err != nil {
			return nil, err
		}
	}
	if acc != nil && acc.Type() == objects.FloatType {
		res, err := sumFloatFast(it, csFromDouble(acc.(*objects.Float).Float64()))
		if err != nil {
			return nil, err
		}
		acc = res
	}
	if acc != nil && acc.Type() == objects.ComplexType {
		z := acc.(*objects.Complex)
		res, err := sumComplexFast(it, csFromDouble(z.Real()), csFromDouble(z.Imag()))
		if err != nil {
			return nil, err
		}
		acc = res
	}

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

// sumIntFast is the PyLong_CheckExact fast path. It accumulates exact ints
// and bools into total. On the first non-int item it converts total back to
// an int, adds the item with PyNumber_Add, and returns that object so the
// caller can hand it to the next fast path. When the iterator is exhausted
// with an all-int sum it returns the int total directly.
//
// CPython: Python/bltinmodule.c:2831 builtin_sum_impl (PyLong_CheckExact path)
func sumIntFast(it objects.Object, total *big.Int) (objects.Object, error) {
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return objects.NewIntFromBig(total), nil
		}
		if err != nil {
			return nil, err
		}
		// PyLong_CheckExact(item) || PyBool_Check(item): exact int or
		// bool. int subclasses fall through to the general add, matching
		// CPython's exact-type guard.
		if v.Type() == objects.IntType || v.Type() == objects.BoolType {
			b, _ := v.(interface{ BigInt() *big.Int })
			total.Add(total, b.BigInt())
			continue
		}
		return abstract.Add(objects.NewIntFromBig(total), v)
	}
}

// sumFloatFast is the PyFloat_CheckExact fast path using Neumaier
// compensated summation. Exact floats add their double directly; any int
// (PyLong_Check, subclass-inclusive) converts via PyLong_AsDouble. The first
// other item converts the running sum back to a float and adds normally.
//
// CPython: Python/bltinmodule.c:2879 builtin_sum_impl (PyFloat_CheckExact path)
func sumFloatFast(it objects.Object, reSum compensatedSum) (objects.Object, error) {
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return objects.NewFloat(csToDouble(reSum)), nil
		}
		if err != nil {
			return nil, err
		}
		if v.Type() == objects.FloatType {
			reSum = csAdd(reSum, v.(*objects.Float).Float64())
			continue
		}
		if d, ok, err := objects.LongAsDouble(v); err != nil {
			return nil, err
		} else if ok {
			reSum = csAdd(reSum, d)
			continue
		}
		return abstract.Add(objects.NewFloat(csToDouble(reSum)), v)
	}
}

// sumComplexFast is the PyComplex_CheckExact fast path: it keeps a
// compensated sum for each component. Exact complex adds both, any int or
// float adds the real component, anything else falls back to PyNumber_Add.
//
// CPython: Python/bltinmodule.c:2924 builtin_sum_impl (PyComplex_CheckExact path)
func sumComplexFast(it objects.Object, reSum, imSum compensatedSum) (objects.Object, error) {
	for {
		v, err := abstract.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			return objects.NewComplex(csToDouble(reSum), csToDouble(imSum)), nil
		}
		if err != nil {
			return nil, err
		}
		if v.Type() == objects.ComplexType {
			c := v.(*objects.Complex)
			reSum = csAdd(reSum, c.Real())
			imSum = csAdd(imSum, c.Imag())
			continue
		}
		if d, ok, err := objects.LongAsDouble(v); err != nil {
			return nil, err
		} else if ok {
			reSum = csAdd(reSum, d)
			continue
		}
		if v.Type() == objects.FloatType {
			reSum = csAdd(reSum, v.(*objects.Float).Float64())
			continue
		}
		return abstract.Add(objects.NewComplex(csToDouble(reSum), csToDouble(imSum)), v)
	}
}

// compensatedSum is the running (hi, lo) pair of the improved
// Kahan-Babuska (Neumaier) summation algorithm: hi holds the high-order
// bits of the sum, lo accumulates the lost low-order bits.
//
// CPython: Python/bltinmodule.c:2731 CompensatedSum
type compensatedSum struct {
	hi float64
	lo float64
}

// csFromDouble seeds a compensated sum from a single double.
//
// CPython: Python/bltinmodule.c:2737 cs_from_double
func csFromDouble(x float64) compensatedSum { return compensatedSum{hi: x} }

// csAdd folds x into the compensated sum, routing the rounding error of
// hi+x into lo depending on which operand is larger in magnitude.
//
// CPython: Python/bltinmodule.c:2743 cs_add
func csAdd(total compensatedSum, x float64) compensatedSum {
	t := total.hi + x
	if math.Abs(total.hi) >= math.Abs(x) {
		total.lo += (total.hi - t) + x
	} else {
		total.lo += (x - t) + total.hi
	}
	return compensatedSum{hi: t, lo: total.lo}
}

// csToDouble collapses the compensated sum to a single double, guarding
// against losing the sign on a negative result and against an infinite or
// overflowed hi turning into a NaN when the compensation is added.
//
// CPython: Python/bltinmodule.c:2756 cs_to_double
func csToDouble(total compensatedSum) float64 {
	if total.lo != 0 && !math.IsInf(total.lo, 0) && total.lo == total.lo {
		return total.hi + total.lo
	}
	return total.hi
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
	// min/max accept only the "key" and "default" keywords; anything
	// else is a TypeError, matching the PyArg_ParseTupleAndKeywords
	// keyword list min_max uses.
	//
	// CPython: Python/bltinmodule.c:1845 min_max
	//
	// min_max forwards its keyword arguments to _PyArg_ParseStack-
	// AndKeywords with zero positional arguments (the iterable is read
	// separately), so the over-supply check fires on the keyword count
	// alone before any individual name is validated.
	if err := objects.CheckKeywordCount(name, 0, len(kwargs), 2); err != nil {
		return nil, err
	}
	for k := range kwargs {
		if k != "key" && k != "default" {
			return nil, fmt.Errorf("TypeError: %s() got an unexpected keyword argument '%s'", name, k)
		}
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
