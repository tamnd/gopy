// gc built-in module. Exposes the cycle collector surface that the
// CPython gcmodule.c file publishes: collect, enable, disable,
// isenabled, get_threshold, set_threshold, get_count, is_tracked.
// v0.10 wires the surface against the existing skeleton; the
// observable behavior for the cycle-collecting entries lands as the
// 1613 checklist progresses.
//
// CPython: Modules/gcmodule.c:1995 gcmodule_methods

package gc

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("gc", buildModule)
}

// buildModule constructs the gc module dict. Mirrors gcmodule_exec
// on the C side: the public callables get registered as builtin
// functions on the module object.
//
// CPython: Modules/gcmodule.c:2044 gcmodule_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("gc")
	d := m.Dict()
	entries := []struct {
		name string
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"collect", gcCollect},
		{"enable", gcEnable},
		{"disable", gcDisable},
		{"isenabled", gcIsEnabled},
		{"get_threshold", gcGetThreshold},
		{"set_threshold", gcSetThreshold},
		{"get_count", gcGetCount},
		{"is_tracked", gcIsTracked},
		{"get_objects", gcGetObjects},
		{"get_referrers", gcGetReferrers},
		{"get_referents", gcGetReferents},
		{"freeze", gcFreeze},
		{"unfreeze", gcUnfreeze},
		{"get_freeze_count", gcGetFreezeCount},
	}
	if err := d.SetItem(objects.NewStr("garbage"), objects.NewList(nil)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("callbacks"), objects.NewList(nil)); err != nil {
		return nil, err
	}
	for _, e := range entries {
		bf := objects.NewBuiltinFunction(e.name, e.fn)
		if err := d.SetItem(objects.NewStr(e.name), bf); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// gcCollect implements gc.collect([generation=NumGenerations-1]).
// Defaults to a full collection (oldest generation) when no argument
// is supplied, matching CPython's gc.collect() default.
//
// CPython: Modules/gcmodule.c:1822 gc_collect_impl
func gcCollect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: collect() takes at most 1 argument (%d given)", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: collect() takes no keyword arguments")
	}
	gen := NumGenerations - 1
	if len(args) == 1 {
		x, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: collect() argument must be int, not %s", args[0].Type().Name)
		}
		v, fits := x.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: collect() argument out of range")
		}
		gen = int(v)
	}
	return objects.NewInt(int64(Collect(gen))), nil
}

// gcEnable / gcDisable / gcIsEnabled wrap the package-level toggles.
//
// CPython: Modules/gcmodule.c gc_enable_impl / gc_disable_impl / gc_isenabled_impl
func gcEnable(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: enable() takes no arguments")
	}
	Enable()
	return objects.None(), nil
}

func gcDisable(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: disable() takes no arguments")
	}
	Disable()
	return objects.None(), nil
}

func gcIsEnabled(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: isenabled() takes no arguments")
	}
	return objects.NewBool(IsEnabled()), nil
}

// gcGetThreshold reports the per-generation thresholds as a 3-tuple.
//
// CPython: Modules/gcmodule.c gc_get_threshold_impl
func gcGetThreshold(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_threshold() takes no arguments")
	}
	a, b, c := GetThreshold()
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(a)),
		objects.NewInt(int64(b)),
		objects.NewInt(int64(c)),
	}), nil
}

// gcSetThreshold accepts up to three positional ints. Missing
// trailing arguments retain the existing thresholds, matching
// CPython's signature gc.set_threshold(threshold0[, threshold1[, threshold2]]).
//
// CPython: Modules/gcmodule.c gc_set_threshold
func gcSetThreshold(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: set_threshold() takes no keyword arguments")
	}
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: set_threshold() takes 1 to 3 arguments (%d given)", len(args))
	}
	cur0, cur1, cur2 := GetThreshold()
	vals := []int{cur0, cur1, cur2}
	for i, a := range args {
		x, ok := a.(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: set_threshold() argument must be int, not %s", a.Type().Name)
		}
		v, fits := x.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: set_threshold() argument out of range")
		}
		vals[i] = int(v)
	}
	SetThreshold(vals[0], vals[1], vals[2])
	return objects.None(), nil
}

// gcGetCount reports the per-generation live counts.
//
// CPython: Modules/gcmodule.c gc_get_count_impl
func gcGetCount(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_count() takes no arguments")
	}
	a, b, c := GetCount()
	return objects.NewTuple([]objects.Object{
		objects.NewInt(int64(a)),
		objects.NewInt(int64(b)),
		objects.NewInt(int64(c)),
	}), nil
}

// gcIsTracked answers gc.is_tracked(obj).
//
// CPython: Modules/gcmodule.c gc_is_tracked
func gcIsTracked(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: is_tracked() takes exactly 1 argument (%d given)", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: is_tracked() takes no keyword arguments")
	}
	return objects.NewBool(IsTracked(args[0])), nil
}

// gcGetObjects implements gc.get_objects([generation]).
//
// CPython: Modules/gcmodule.c gc_get_objects_impl
func gcGetObjects(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_objects() takes no keyword arguments")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: get_objects() takes at most 1 argument (%d given)", len(args))
	}
	gen := -1
	if len(args) == 1 {
		x, ok := args[0].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: get_objects() argument must be int, not %s", args[0].Type().Name)
		}
		v, fits := x.Int64()
		if !fits {
			return nil, fmt.Errorf("OverflowError: get_objects() argument out of range")
		}
		gen = int(v)
		if gen < 0 || gen >= NumGenerations {
			return nil, fmt.Errorf("ValueError: generation must be in range(%d)", NumGenerations)
		}
	}
	return objects.NewList(GetObjects(gen)), nil
}

// gcGetReferrers implements gc.get_referrers(*objs).
//
// CPython: Modules/gcmodule.c gc_get_referrers
func gcGetReferrers(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_referrers() takes no keyword arguments")
	}
	return objects.NewList(GetReferrers(args...)), nil
}

// gcGetReferents implements gc.get_referents(*objs).
//
// CPython: Modules/gcmodule.c gc_get_referents
func gcGetReferents(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_referents() takes no keyword arguments")
	}
	return objects.NewList(GetReferents(args...)), nil
}

// gcFreeze / gcUnfreeze / gcGetFreezeCount expose the permanent
// generation surface.
//
// CPython: Modules/gcmodule.c gc_freeze / gc_unfreeze / gc_get_freeze_count
func gcFreeze(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: freeze() takes no arguments")
	}
	Freeze()
	return objects.None(), nil
}

func gcUnfreeze(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: unfreeze() takes no arguments")
	}
	Unfreeze()
	return objects.None(), nil
}

func gcGetFreezeCount(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: get_freeze_count() takes no arguments")
	}
	return objects.NewInt(int64(GetFreezeCount())), nil
}
