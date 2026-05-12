// Package _bisect is the gopy port of CPython's Modules/_bisectmodule.c.
// It provides binary search functions that back Lib/bisect.py.
//
// CPython: Modules/_bisectmodule.c

package _bisect

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_bisect", buildModule)
}

// buildModule materializes the _bisect module dict.
//
// CPython: Modules/_bisectmodule.c:260 bisect_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_bisect")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"bisect_left", objects.NewBuiltinFunction("bisect_left", bisectLeft)},
		{"bisect_right", objects.NewBuiltinFunction("bisect_right", bisectRight)},
		{"bisect", objects.NewBuiltinFunction("bisect", bisectRight)},
		{"insort_left", objects.NewBuiltinFunction("insort_left", insortLeft)},
		{"insort_right", objects.NewBuiltinFunction("insort_right", insortRight)},
		{"insort", objects.NewBuiltinFunction("insort", insortRight)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// parseArgs extracts the common (a, x, lo, hi, key) arguments used by
// all four bisect/insort functions. lo defaults to 0, hi defaults to
// len(a), key defaults to None.
func parseArgs(name string, args []objects.Object, kwargs map[string]objects.Object) (
	a *objects.List, x objects.Object, lo, hi int, key objects.Object, err error,
) {
	key = objects.None()

	// Pull keyword arguments.
	loObj, hasLo := kwargs["lo"]
	hiObj, hasHi := kwargs["hi"]
	keyObj, hasKey := kwargs["key"]
	for k := range kwargs {
		if k != "lo" && k != "hi" && k != "key" {
			err = fmt.Errorf("TypeError: %s() got an unexpected keyword argument '%s'", name, k)
			return
		}
	}

	if len(args) < 2 {
		err = fmt.Errorf("TypeError: %s() requires at least 2 positional arguments", name)
		return
	}

	var ok bool
	a, ok = args[0].(*objects.List)
	if !ok {
		err = fmt.Errorf("TypeError: %s() first argument must be a list", name)
		return
	}
	x = args[1]

	lo = 0
	hi = a.Len()

	if len(args) >= 3 && !hasLo {
		loObj = args[2]
		hasLo = true
	}
	if len(args) >= 4 && !hasHi {
		hiObj = args[3]
		hasHi = true
	}

	if hasLo {
		li, isInt := loObj.(*objects.Int)
		if !isInt {
			err = fmt.Errorf("TypeError: %s() lo must be an integer", name)
			return
		}
		v, _ := li.Int64()
		lo = int(v)
	}
	if hasHi {
		hi2, isInt := hiObj.(*objects.Int)
		if !isInt {
			err = fmt.Errorf("TypeError: %s() hi must be an integer", name)
			return
		}
		v, _ := hi2.Int64()
		hi = int(v)
	}
	if hasKey {
		key = keyObj
	}

	if lo < 0 {
		err = errors.New("ValueError: lo must be non-negative")
		return
	}
	return
}

// applyKey applies the key function to value x if key is not None.
//
// CPython: Modules/_bisectmodule.c:31 (key application inline in internal_bisect_left)
func applyKey(key, x objects.Object) (objects.Object, error) {
	if objects.IsNone(key) {
		return x, nil
	}
	return objects.CallOneArg(key, x)
}

// ---------------------------------------------------------------------------
// Internal bisect implementations.
// ---------------------------------------------------------------------------

// internalBisectLeft returns the leftmost index in a[lo:hi] where x
// would be inserted to keep a sorted.
//
// CPython: Modules/_bisectmodule.c:31 internal_bisect_left
func internalBisectLeft(a *objects.List, x objects.Object, lo, hi int, key objects.Object) (int, error) {
	kx, err := applyKey(key, x)
	if err != nil {
		return 0, err
	}
	for lo < hi {
		mid := (lo + hi) >> 1
		item := a.Item(mid)
		kitem, err := applyKey(key, item)
		if err != nil {
			return 0, err
		}
		// if kitem < kx: lo = mid + 1 else: hi = mid
		lt, err := objects.RichCmpBool(kitem, kx, objects.CompareLT)
		if err != nil {
			return 0, err
		}
		if lt {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, nil
}

// internalBisectRight returns the rightmost index in a[lo:hi] where x
// would be inserted to keep a sorted.
//
// CPython: Modules/_bisectmodule.c:75 internal_bisect_right
func internalBisectRight(a *objects.List, x objects.Object, lo, hi int, key objects.Object) (int, error) {
	kx, err := applyKey(key, x)
	if err != nil {
		return 0, err
	}
	for lo < hi {
		mid := (lo + hi) >> 1
		item := a.Item(mid)
		kitem, err := applyKey(key, item)
		if err != nil {
			return 0, err
		}
		// if kx < kitem: hi = mid else: lo = mid + 1
		lt, err := objects.RichCmpBool(kx, kitem, objects.CompareLT)
		if err != nil {
			return 0, err
		}
		if lt {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

// ---------------------------------------------------------------------------
// Module functions.
// ---------------------------------------------------------------------------

// bisectLeft returns the leftmost insertion point for x in sorted list a.
//
// CPython: Modules/_bisectmodule.c:119 _bisect_bisect_left_impl
func bisectLeft(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, x, lo, hi, key, err := parseArgs("bisect_left", args, kwargs)
	if err != nil {
		return nil, err
	}
	idx, err := internalBisectLeft(a, x, lo, hi, key)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(idx)), nil
}

// bisectRight returns the rightmost insertion point for x in sorted list a.
//
// CPython: Modules/_bisectmodule.c:154 _bisect_bisect_right_impl
func bisectRight(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, x, lo, hi, key, err := parseArgs("bisect_right", args, kwargs)
	if err != nil {
		return nil, err
	}
	idx, err := internalBisectRight(a, x, lo, hi, key)
	if err != nil {
		return nil, err
	}
	return objects.NewInt(int64(idx)), nil
}

// insortLeft inserts x into sorted list a, keeping a sorted (leftmost).
//
// CPython: Modules/_bisectmodule.c:189 _bisect_insort_left_impl
func insortLeft(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, x, lo, hi, key, err := parseArgs("insort_left", args, kwargs)
	if err != nil {
		return nil, err
	}
	idx, err := internalBisectLeft(a, x, lo, hi, key)
	if err != nil {
		return nil, err
	}
	a.Insert(idx, x)
	return objects.None(), nil
}

// insortRight inserts x into sorted list a, keeping a sorted (rightmost).
//
// CPython: Modules/_bisectmodule.c:224 _bisect_insort_right_impl
func insortRight(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	a, x, lo, hi, key, err := parseArgs("insort_right", args, kwargs)
	if err != nil {
		return nil, err
	}
	idx, err := internalBisectRight(a, x, lo, hi, key)
	if err != nil {
		return nil, err
	}
	a.Insert(idx, x)
	return objects.None(), nil
}
