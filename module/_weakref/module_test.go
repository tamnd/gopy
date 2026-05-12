// Tests for the _weakref builtin module. Mirrors the shape of the
// public-API cases in cpython/Lib/test/test_weakref.py: ref()
// roundtrip, getweakrefcount accounting, callback delivery on
// clear, and proxy attribute forwarding.

package weakrefmod

import (
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// loadModule builds a fresh _weakref module via the registered
// init function. Mirrors how the import system would materialize it.
func loadModule(t *testing.T) *objects.Module {
	t.Helper()
	fn := imp.FindInitFunc("_weakref")
	if fn == nil {
		t.Fatal("imp.FindInitFunc(\"_weakref\") = nil; inittab registration missing")
	}
	m, err := fn()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	return m
}

func getAttr(t *testing.T, m *objects.Module, name string) objects.Object {
	t.Helper()
	v, err := m.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("module attr %q missing: %v", name, err)
	}
	return v
}

func TestModuleExportsRefAndTypeAliases(t *testing.T) {
	m := loadModule(t)
	if getAttr(t, m, "ref") != objects.WeakrefType {
		t.Error("ref is not WeakrefType")
	}
	if getAttr(t, m, "ReferenceType") != objects.WeakrefType {
		t.Error("ReferenceType is not WeakrefType")
	}
	if getAttr(t, m, "ProxyType") != objects.WeakProxyType {
		t.Error("ProxyType is not WeakProxyType")
	}
	if getAttr(t, m, "CallableProxyType") != objects.WeakCallableProxyType {
		t.Error("CallableProxyType is not WeakCallableProxyType")
	}
}

// TestRefCallReturnsReferent pins the cardinal CPython behavior:
// r = ref(obj); r() is obj.
//
// CPython: Lib/test/test_weakref.py test_basic_ref
func TestRefCallReturnsReferent(t *testing.T) {
	obj := objects.NewList(nil)
	r := objects.NewWeakref(obj, nil)
	got, err := objects.WeakrefType.Call(r, nil, nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != obj {
		t.Fatalf("r() = %v, want %v", got, obj)
	}
}

// TestRefAfterClearIsNone pins the dead-ref contract: once the
// strong reference is gone, the weakref reports None.
//
// CPython: Lib/test/test_weakref.py test_ref_reuse + test_basic_ref
func TestRefAfterClearIsNone(t *testing.T) {
	obj := objects.NewList(nil)
	r := objects.NewWeakref(obj, nil)
	r.Clear()
	got, _ := objects.WeakrefType.Call(r, nil, nil)
	if got != objects.None() {
		t.Fatalf("dead r() = %v, want None", got)
	}
}

// TestGetweakrefcountAccountsForRefsAndProxies pins the count
// arithmetic getweakrefcount publishes.
//
// CPython: Modules/_weakref.c:25 _weakref_getweakrefcount_impl
func TestGetweakrefcountAccountsForRefsAndProxies(t *testing.T) {
	m := loadModule(t)
	fn := getAttr(t, m, "getweakrefcount")
	target := objects.NewList(nil)

	call := func() int64 {
		v, err := fn.Type().Call(fn, []objects.Object{target}, nil)
		if err != nil {
			t.Fatalf("getweakrefcount: %v", err)
		}
		n, _ := v.(*objects.Int).Int64()
		return n
	}

	if call() != 0 {
		t.Fatalf("initial count = %d, want 0", call())
	}
	r := objects.NewWeakref(target, nil)
	_ = r
	if call() != 1 {
		t.Fatalf("after ref: count = %d, want 1", call())
	}
	cb := objects.NewBuiltinFunction("cb", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.None(), nil
	})
	r2 := objects.NewWeakref(target, cb)
	_ = r2
	if call() != 2 {
		t.Fatalf("after ref+cb: count = %d, want 2", call())
	}
	p := objects.NewWeakProxy(target, nil)
	_ = p
	if call() != 3 {
		t.Fatalf("after proxy: count = %d, want 3", call())
	}
	// Reusing a basic ref must NOT add a new entry.
	r3 := objects.NewWeakref(target, nil)
	if r3 != r {
		t.Errorf("ref(x) called twice returned distinct refs")
	}
	if call() != 3 {
		t.Fatalf("after duplicate basic ref: count = %d, want 3", call())
	}
}

// TestCallbackFiresOnClear pins that ClearWeakRefs invokes registered
// callbacks with the weakref as the sole argument.
//
// CPython: Objects/weakrefobject.c:986 handle_callback
func TestCallbackFiresOnClear(t *testing.T) {
	target := objects.NewList(nil)
	var calls int
	var seen objects.Object
	cb := objects.NewBuiltinFunction("cb", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		if len(args) == 1 {
			seen = args[0]
		}
		return objects.None(), nil
	})
	r := objects.NewWeakref(target, cb)
	objects.ClearWeakRefs(target)
	if calls != 1 {
		t.Fatalf("callback fired %d times, want 1", calls)
	}
	if seen != objects.Object(r) {
		t.Fatalf("callback arg = %v, want the weakref %v", seen, r)
	}
	if !r.IsDead() {
		t.Fatal("ref still live after ClearWeakRefs")
	}
}

// TestProxyDelegatesAttributeAccess pins the proxy forwarding
// contract: attribute access on the proxy reaches the referent.
//
// CPython: Objects/weakrefobject.c:599 proxy_getattr
func TestProxyDelegatesAttributeAccess(t *testing.T) {
	m := loadModule(t)
	fn := getAttr(t, m, "proxy")
	target := objects.NewList(nil)
	target.Append(objects.NewInt(11))
	v, err := fn.Type().Call(fn, []objects.Object{target}, nil)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	p, ok := v.(*objects.WeakProxy)
	if !ok {
		t.Fatalf("proxy() returned %T", v)
	}
	n, err := objects.Length(p)
	if err != nil {
		t.Fatalf("Length(proxy): %v", err)
	}
	if n != 1 {
		t.Fatalf("len(proxy) = %d, want 1", n)
	}
	got, err := objects.GetItem(p, objects.NewInt(0))
	if err != nil {
		t.Fatalf("GetItem(proxy, 0): %v", err)
	}
	if i, _ := got.(*objects.Int).Int64(); i != 11 {
		t.Errorf("proxy[0] = %d, want 11", i)
	}
}

// TestProxyOfCallableReturnsCallableProxy pins type selection: a
// callable referent yields CallableProxyType.
//
// CPython: Objects/weakrefobject.c:929 PyWeakref_NewProxy (callable
// branch)
func TestProxyOfCallableReturnsCallableProxy(t *testing.T) {
	m := loadModule(t)
	fn := getAttr(t, m, "proxy")
	callee := objects.NewBuiltinFunction("callee", func(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(42), nil
	})
	v, err := fn.Type().Call(fn, []objects.Object{callee}, nil)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if v.Type() != objects.WeakCallableProxyType {
		t.Errorf("proxy of callable: type = %v, want WeakCallableProxyType", v.Type().Name)
	}
}

// TestGetweakrefsReturnsLiveRefs pins getweakrefs() ordering and
// membership: every NewWeakref / NewWeakProxy must appear.
//
// CPython: Modules/_weakref.c:75 _weakref_getweakrefs
func TestGetweakrefsReturnsLiveRefs(t *testing.T) {
	m := loadModule(t)
	fn := getAttr(t, m, "getweakrefs")
	target := objects.NewList(nil)

	r := objects.NewWeakref(target, nil)
	p := objects.NewWeakProxy(target, nil)
	got, err := fn.Type().Call(fn, []objects.Object{target}, nil)
	if err != nil {
		t.Fatalf("getweakrefs: %v", err)
	}
	lst, ok := got.(*objects.List)
	if !ok {
		t.Fatalf("getweakrefs returned %T", got)
	}
	items := []objects.Object{}
	for i := 0; i < lst.Len(); i++ {
		items = append(items, lst.Item(i))
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	saw := map[objects.Object]bool{}
	for _, x := range items {
		saw[x] = true
	}
	if !saw[r] || !saw[p] {
		t.Errorf("missing ref or proxy in getweakrefs(): %v", items)
	}
}

// TestRemoveDeadWeakrefDeletesOnlyDead pins the _remove_dead_weakref
// contract: live entries stay, dead entries are dropped.
//
// CPython: Modules/_weakref.c:54 _weakref__remove_dead_weakref_impl
func TestRemoveDeadWeakrefDeletesOnlyDead(t *testing.T) {
	m := loadModule(t)
	fn := getAttr(t, m, "_remove_dead_weakref")

	target := objects.NewList(nil)
	live := objects.NewWeakref(target, nil)
	dead := objects.NewWeakref(objects.NewList(nil), nil)
	dead.Clear()

	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("live"), live); err != nil {
		t.Fatal(err)
	}
	if err := d.SetItem(objects.NewStr("dead"), dead); err != nil {
		t.Fatal(err)
	}

	if _, err := fn.Type().Call(fn, []objects.Object{d, objects.NewStr("live")}, nil); err != nil {
		t.Fatalf("_remove_dead_weakref(live): %v", err)
	}
	if _, err := d.GetItem(objects.NewStr("live")); err != nil {
		t.Error("live entry was removed by _remove_dead_weakref")
	}

	if _, err := fn.Type().Call(fn, []objects.Object{d, objects.NewStr("dead")}, nil); err != nil {
		t.Fatalf("_remove_dead_weakref(dead): %v", err)
	}
	if _, err := d.GetItem(objects.NewStr("dead")); err == nil {
		t.Error("dead entry survived _remove_dead_weakref")
	}
}
