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
	}
	for _, e := range entries {
		bf := objects.NewBuiltinFunction(e.name, e.fn)
		if err := d.SetItem(objects.NewStr(e.name), bf); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// gcCollect implements gc.collect([generation]). v0.10's collector is
// still a no-op so the int return is always zero; the cycle path
// arrives with 1613-K.
//
// CPython: Modules/gcmodule.c:1822 gc_collect_impl
func gcCollect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: collect() takes at most 1 argument (%d given)", len(args))
	}
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: collect() takes no keyword arguments")
	}
	return objects.NewInt(int64(Collect())), nil
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
