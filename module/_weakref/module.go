// Package weakrefmod is the gopy port of CPython's _weakref builtin
// module (Modules/_weakref.c). It re-exports the weakref.ref /
// proxy / callable-proxy types defined in objects/weakref.go and
// objects/weakref_proxy.go, plus the four module functions
// CPython publishes: getweakrefcount, _remove_dead_weakref,
// getweakrefs, proxy.
//
// CPython: Modules/_weakref.c:171 weakrefmodule
package weakrefmod

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_weakref", buildModule)
}

// buildModule constructs the _weakref module dict. Mirrors
// weakref_exec on the C side: register the four methods, then add
// the ref / ReferenceType / ProxyType / CallableProxyType type
// objects.
//
// CPython: Modules/_weakref.c:143 weakref_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_weakref")
	d := m.Dict()

	for _, e := range []struct {
		name string
		fn   func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)
	}{
		{"getweakrefcount", getweakrefcount},
		{"_remove_dead_weakref", removeDeadWeakref},
		{"getweakrefs", getweakrefs},
		{"proxy", proxy},
	} {
		bf := objects.NewBuiltinFunction(e.name, e.fn)
		if err := d.SetItem(objects.NewStr(e.name), bf); err != nil {
			return nil, err
		}
	}

	for _, e := range []struct {
		name string
		typ  *objects.Type
	}{
		{"ref", objects.WeakrefType},
		{"ReferenceType", objects.WeakrefType},
		{"ProxyType", objects.WeakProxyType},
		{"CallableProxyType", objects.WeakCallableProxyType},
	} {
		if err := d.SetItem(objects.NewStr(e.name), e.typ); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// getweakrefcount implements _weakref.getweakrefcount(object).
// Returns the number of live weak references pointing at object.
//
// CPython: Modules/_weakref.c:25 _weakref_getweakrefcount_impl
func getweakrefcount(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: getweakrefcount() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: getweakrefcount() takes exactly 1 argument (%d given)", len(args))
	}
	return objects.NewInt(int64(objects.PyWeakref_GetWeakrefCount(args[0]))), nil
}

// removeDeadWeakref implements _weakref._remove_dead_weakref(dct, key).
// Atomically removes key from dct only when dct[key] is a dead
// weakref. CPython uses _PyDict_DelItemIf with is_dead_weakref as
// the predicate; gopy mirrors the contract.
//
// CPython: Modules/_weakref.c:54 _weakref__remove_dead_weakref_impl
func removeDeadWeakref(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: _remove_dead_weakref() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: _remove_dead_weakref() takes exactly 2 arguments (%d given)", len(args))
	}
	dct, ok := args[0].(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: _remove_dead_weakref() argument 1 must be dict, not %s", args[0].Type().Name)
	}
	key := args[1]
	v, getErr := dct.GetItem(key)
	if getErr != nil {
		// Key absent: silently ignore, matching _PyDict_DelItemIf.
		return objects.None(), nil //nolint:nilerr // CPython swallows the lookup miss
	}
	w, ok := v.(*objects.Weakref)
	if !ok {
		return nil, fmt.Errorf("TypeError: not a weakref")
	}
	if w.IsDead() {
		if err := dct.DelItem(key); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// getweakrefs implements _weakref.getweakrefs(object). Returns a
// list of every live weak reference pointing at object.
//
// CPython: Modules/_weakref.c:75 _weakref_getweakrefs
func getweakrefs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: getweakrefs() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: getweakrefs() takes exactly 1 argument (%d given)", len(args))
	}
	return objects.NewList(objects.GetWeakrefs(args[0])), nil
}

// proxy implements _weakref.proxy(object, callback=None). Returns
// CallableProxyType when object is callable, ProxyType otherwise.
//
// CPython: Modules/_weakref.c:125 _weakref_proxy_impl
func proxy(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: proxy() takes no keyword arguments")
	}
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: proxy() takes 1 to 2 arguments (%d given)", len(args))
	}
	var cb objects.Object
	if len(args) == 2 {
		cb = args[1]
	}
	p, err := objects.PyWeakref_NewProxy(args[0], cb)
	if err != nil {
		return nil, err
	}
	return p, nil
}
