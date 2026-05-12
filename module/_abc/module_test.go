// Tests for the _abc builtin module. Mirror the public API contract
// codified by Lib/abc.py: _abc_init populates the abstract method set
// and stores a fresh _abc_data; _abc_register bumps the cache token and
// makes the registered class participate in subclass checks; the
// six-step subclass check honors __subclasshook__, the registry, and
// the recursive walk.

package abcmod

import (
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func loadModule(t *testing.T) *objects.Module {
	t.Helper()
	fn := imp.FindInitFunc("_abc")
	if fn == nil {
		t.Fatal("imp.FindInitFunc(\"_abc\") = nil")
	}
	m, err := fn()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	return m
}

func mustCall(t *testing.T, fn objects.Object, args ...objects.Object) objects.Object {
	t.Helper()
	v, err := fn.Type().Call(fn, args, nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	return v
}

func getFn(t *testing.T, m *objects.Module, name string) objects.Object {
	t.Helper()
	v, err := m.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("missing %s: %v", name, err)
	}
	return v
}

// newClass mints a fresh user class with name and (optionally) one
// attribute. Each call produces a distinct *Type so per-test ABC state
// stays isolated.
func newClass(name string, attrs map[string]objects.Object, bases ...*objects.Type) *objects.Type {
	if len(bases) == 0 {
		bases = []*objects.Type{objects.ObjectType()}
	}
	ns := objects.NewDict()
	for k, v := range attrs {
		_ = ns.SetItem(objects.NewStr(k), v)
	}
	return objects.NewUserType(name, bases, ns)
}

// TestAbcInitInstallsAbstractMethods pins the basic _abc_init shape:
// the class gains a frozenset __abstractmethods__ derived from members
// whose __isabstractmethod__ is truthy, plus an _abc_impl carrier.
func TestAbcInitInstallsAbstractMethods(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")

	// Build a tiny user class whose instance carries
	// __isabstractmethod__=True; the instance acts as the abstract
	// marker on the surrounding class's attribute.
	markerNS := objects.NewDict()
	_ = markerNS.SetItem(objects.NewStr("__isabstractmethod__"), objects.True())
	markerCls := objects.NewUserType("Marker", []*objects.Type{objects.ObjectType()}, markerNS)
	marker := objects.NewInstance(markerCls)
	cls := newClass("Abs", map[string]objects.Object{"foo": marker})

	if _, err := init.Type().Call(init, []objects.Object{cls}, nil); err != nil {
		t.Fatalf("_abc_init: %v", err)
	}

	abs, _ := objects.LookupDescriptor(cls, "__abstractmethods__")
	if abs == nil {
		t.Fatal("__abstractmethods__ not installed")
	}
	set, ok := abs.(*objects.Set)
	if !ok {
		t.Fatalf("__abstractmethods__ is %T, want *Set", abs)
	}
	has, _ := set.Contains(objects.NewStr("foo"))
	if !has {
		t.Errorf("foo not in __abstractmethods__")
	}
	if impl, _ := objects.LookupDescriptor(cls, "_abc_impl"); impl == nil {
		t.Error("_abc_impl missing")
	}
}

// TestAbcRegisterReturnsSubclassAndBumpsToken pins the public surface
// of register(): the call returns subclass and get_cache_token reports
// a higher value afterward.
func TestAbcRegisterReturnsSubclassAndBumpsToken(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	reg := getFn(t, m, "_abc_register")
	tok := getFn(t, m, "get_cache_token")

	cls := newClass("A", nil)
	sub := newClass("B", nil)
	mustCall(t, init, cls)

	before, _ := mustCall(t, tok).(*objects.Int).Int64()
	got := mustCall(t, reg, cls, sub)
	if got != objects.Object(sub) {
		t.Errorf("_abc_register returned %v, want subclass", got)
	}
	after, _ := mustCall(t, tok).(*objects.Int).Int64()
	if after <= before {
		t.Errorf("cache token did not advance: %d -> %d", before, after)
	}
}

// TestAbcSubclasscheckDirectSubclass pins step 4 of the algorithm.
func TestAbcSubclasscheckDirectSubclass(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	sc := getFn(t, m, "_abc_subclasscheck")
	parent := newClass("P", nil)
	child := newClass("C", nil, parent)
	mustCall(t, init, parent)
	r := mustCall(t, sc, parent, child)
	if !objects.IsTrue(r) {
		t.Error("direct subclass not reported true")
	}
}

// TestAbcSubclasscheckRegistered pins step 5.
func TestAbcSubclasscheckRegistered(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	reg := getFn(t, m, "_abc_register")
	sc := getFn(t, m, "_abc_subclasscheck")
	abc := newClass("ABC", nil)
	virt := newClass("Virt", nil)
	mustCall(t, init, abc)
	mustCall(t, reg, abc, virt)
	r := mustCall(t, sc, abc, virt)
	if !objects.IsTrue(r) {
		t.Error("registered subclass not reported true")
	}
	// Subclass of a registered class also counts.
	deeper := newClass("Deeper", nil, virt)
	r2 := mustCall(t, sc, abc, deeper)
	if !objects.IsTrue(r2) {
		t.Error("subclass of registered class not reported true")
	}
}

// TestAbcSubclasscheckHook pins step 3.
func TestAbcSubclasscheckHook(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	sc := getFn(t, m, "_abc_subclasscheck")

	hook := objects.NewBuiltinFunction("__subclasshook__", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		// args = (cls, subclass); always say True.
		return objects.True(), nil
	})
	cls := newClass("H", map[string]objects.Object{"__subclasshook__": objects.NewClassMethod(hook)})
	other := newClass("Other", nil)
	mustCall(t, init, cls)
	r := mustCall(t, sc, cls, other)
	if !objects.IsTrue(r) {
		t.Error("__subclasshook__ True ignored")
	}
}

// TestAbcInstanceCheckCacheHit pins the inline fast path of
// _abc_instancecheck: when instance.__class__ already appears in the
// positive cache, the check returns True without going back through
// __subclasscheck__. ABCMeta's full instancecheck/subclasscheck pair is
// out of scope for the C module port; the cache-hit branch is the bit
// _abc.c actually owns.
func TestAbcInstanceCheckCacheHit(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	ic := getFn(t, m, "_abc_instancecheck")

	abc := newClass("ABC3", nil)
	concrete := newClass("Concrete3", nil)
	mustCall(t, init, abc)

	// Manually prime _abc_cache. CPython does this from the cache-update
	// branches inside _abc_subclasscheck; doing it directly here keeps
	// the test focused on the cache-hit path that lives in _abc.c.
	d, err := getImpl(abc)
	if err != nil {
		t.Fatal(err)
	}
	if err := addToWeakSet(d, &d.cache, concrete); err != nil {
		t.Fatal(err)
	}

	inst := objects.NewInstance(concrete)
	r := mustCall(t, ic, abc, inst)
	if !objects.IsTrue(r) {
		t.Error("instancecheck cache hit returned False")
	}
}

// TestGetDumpShape pins the 4-tuple shape.
func TestGetDumpShape(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	dump := getFn(t, m, "_get_dump")
	cls := newClass("D", nil)
	mustCall(t, init, cls)
	r := mustCall(t, dump, cls)
	tup, ok := r.(*objects.Tuple)
	if !ok || tup.Len() != 4 {
		t.Fatalf("_get_dump returned %v (%T)", r, r)
	}
	if _, ok := tup.Item(3).(*objects.Int); !ok {
		t.Errorf("4th element is not Int: %T", tup.Item(3))
	}
}

// TestResetRegistryAndCaches pins that both helpers clear state.
func TestResetRegistryAndCaches(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	reg := getFn(t, m, "_abc_register")
	sc := getFn(t, m, "_abc_subclasscheck")
	rr := getFn(t, m, "_reset_registry")
	rc := getFn(t, m, "_reset_caches")
	dump := getFn(t, m, "_get_dump")

	cls := newClass("R", nil)
	sub := newClass("RSub", nil)
	mustCall(t, init, cls)
	mustCall(t, reg, cls, sub)
	mustCall(t, sc, cls, sub) // populates cache

	mustCall(t, rr, cls)
	mustCall(t, rc, cls)
	r := mustCall(t, dump, cls)
	tup := r.(*objects.Tuple)
	for i := 0; i < 3; i++ {
		s := tup.Item(i).(*objects.Set)
		if s.Len() != 0 {
			t.Errorf("set %d not empty after reset: %d", i, s.Len())
		}
	}
}

// TestRegisterCycleRaises pins that X.register(Y) where Y is a parent
// of X yields RuntimeError.
func TestRegisterCycleRaises(t *testing.T) {
	m := loadModule(t)
	init := getFn(t, m, "_abc_init")
	reg := getFn(t, m, "_abc_register")
	parent := newClass("Par", nil)
	child := newClass("Chi", nil, parent)
	mustCall(t, init, child)
	_, err := reg.Type().Call(reg, []objects.Object{child, parent}, nil)
	if err == nil {
		t.Fatal("expected RuntimeError for inheritance cycle")
	}
}
