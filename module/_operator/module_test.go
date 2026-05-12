// Tests for the _operator accelerator. We exercise the binary,
// unary, comparison, sequence, and identity helpers, plus the three
// callable types (itemgetter, attrgetter, methodcaller) and the
// timing-safe compare_digest primitive.

package _operator

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

// callBuiltin invokes a builtin function exposed by the module dict
// with positional args.
func callBuiltin(t *testing.T, name string, args ...objects.Object) objects.Object {
	t.Helper()
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	fn, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	out, err := objects.Call(fn, objects.NewTuple(args), nil)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return out
}

func intOf(t *testing.T, o objects.Object) int64 {
	t.Helper()
	i, ok := o.(*objects.Int)
	if !ok {
		t.Fatalf("want *Int, got %T", o)
	}
	v, _ := i.Int64()
	return v
}

func boolOf(t *testing.T, o objects.Object) bool {
	t.Helper()
	return objects.IsTrue(o)
}

func TestArithmetic(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
	}{
		{"add", 3, 4, 7},
		{"sub", 10, 4, 6},
		{"mul", 6, 7, 42},
		{"floordiv", 17, 5, 3},
		{"mod", 17, 5, 2},
		{"pow", 2, 10, 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := callBuiltin(t, c.name, objects.NewInt(c.a), objects.NewInt(c.b))
			if got := intOf(t, out); got != c.want {
				t.Fatalf("%s(%d, %d) = %d, want %d", c.name, c.a, c.b, got, c.want)
			}
		})
	}
}

func TestTrueDiv(t *testing.T) {
	out := callBuiltin(t, "truediv", objects.NewInt(7), objects.NewInt(2))
	f, ok := out.(*objects.Float)
	if !ok {
		t.Fatalf("want *Float, got %T", out)
	}
	if f.Float64() != 3.5 {
		t.Fatalf("truediv(7, 2) = %v, want 3.5", f.Float64())
	}
}

func TestComparisons(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want bool
	}{
		{"lt", 1, 2, true},
		{"lt", 2, 2, false},
		{"le", 2, 2, true},
		{"eq", 3, 3, true},
		{"eq", 3, 4, false},
		{"ne", 3, 4, true},
		{"gt", 5, 4, true},
		{"ge", 5, 5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := callBuiltin(t, c.name, objects.NewInt(c.a), objects.NewInt(c.b))
			if got := boolOf(t, out); got != c.want {
				t.Fatalf("%s(%d, %d) = %v, want %v", c.name, c.a, c.b, got, c.want)
			}
		})
	}
}

func TestBitwise(t *testing.T) {
	if got := intOf(t, callBuiltin(t, "and_", objects.NewInt(0xF0), objects.NewInt(0x3C))); got != 0x30 {
		t.Fatalf("and_ wrong: %d", got)
	}
	if got := intOf(t, callBuiltin(t, "or_", objects.NewInt(0x0F), objects.NewInt(0xF0))); got != 0xFF {
		t.Fatalf("or_ wrong: %d", got)
	}
	if got := intOf(t, callBuiltin(t, "xor", objects.NewInt(0xFF), objects.NewInt(0x0F))); got != 0xF0 {
		t.Fatalf("xor wrong: %d", got)
	}
	if got := intOf(t, callBuiltin(t, "invert", objects.NewInt(0))); got != -1 {
		t.Fatalf("invert(0) = %d, want -1", got)
	}
	if got := intOf(t, callBuiltin(t, "lshift", objects.NewInt(1), objects.NewInt(4))); got != 16 {
		t.Fatalf("lshift wrong: %d", got)
	}
	if got := intOf(t, callBuiltin(t, "rshift", objects.NewInt(64), objects.NewInt(2))); got != 16 {
		t.Fatalf("rshift wrong: %d", got)
	}
}

func TestGetSetDelItem(t *testing.T) {
	d := objects.NewDict()
	if err := d.SetItem(objects.NewStr("k"), objects.NewInt(42)); err != nil {
		t.Fatal(err)
	}
	got := callBuiltin(t, "getitem", d, objects.NewStr("k"))
	if intOf(t, got) != 42 {
		t.Fatalf("getitem failed")
	}
	// setitem: 3-arg.
	mod, _ := buildModule()
	fn, _ := mod.Dict().GetItem(objects.NewStr("setitem"))
	if _, err := objects.Call(fn, objects.NewTuple([]objects.Object{d, objects.NewStr("k"), objects.NewInt(99)}), nil); err != nil {
		t.Fatalf("setitem: %v", err)
	}
	got = callBuiltin(t, "getitem", d, objects.NewStr("k"))
	if intOf(t, got) != 99 {
		t.Fatalf("setitem did not update value")
	}
	if _, err := objects.Call(mustGet(t, mod, "delitem"), objects.NewTuple([]objects.Object{d, objects.NewStr("k")}), nil); err != nil {
		t.Fatalf("delitem: %v", err)
	}
	if _, err := d.GetItem(objects.NewStr("k")); err == nil {
		t.Fatalf("key should be gone")
	}
}

func TestContains(t *testing.T) {
	l := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2), objects.NewInt(3)})
	if !boolOf(t, callBuiltin(t, "contains", l, objects.NewInt(2))) {
		t.Fatalf("contains true case failed")
	}
	if boolOf(t, callBuiltin(t, "contains", l, objects.NewInt(99))) {
		t.Fatalf("contains false case failed")
	}
}

func TestIdentity(t *testing.T) {
	// Use a fresh dict so we avoid small-int interning when comparing
	// distinct objects.
	a := objects.NewDict()
	if !boolOf(t, callBuiltin(t, "is_", a, a)) {
		t.Fatalf("is_ same object failed")
	}
	b := objects.NewDict()
	if boolOf(t, callBuiltin(t, "is_", a, b)) {
		t.Fatalf("is_ distinct objects should be False")
	}
	if !boolOf(t, callBuiltin(t, "is_not", a, b)) {
		t.Fatalf("is_not failed")
	}
	if !boolOf(t, callBuiltin(t, "is_none", objects.None())) {
		t.Fatalf("is_none failed")
	}
	if !boolOf(t, callBuiltin(t, "is_not_none", objects.NewInt(1))) {
		t.Fatalf("is_not_none failed")
	}
}

func TestTruthAndNot(t *testing.T) {
	if !boolOf(t, callBuiltin(t, "truth", objects.NewInt(5))) {
		t.Fatalf("truth(5) should be True")
	}
	if boolOf(t, callBuiltin(t, "truth", objects.NewInt(0))) {
		t.Fatalf("truth(0) should be False")
	}
	if !boolOf(t, callBuiltin(t, "not_", objects.NewInt(0))) {
		t.Fatalf("not_(0) should be True")
	}
}

func TestItemgetter(t *testing.T) {
	ig1, err := ItemgetterType.TpNew(ItemgetterType, []objects.Object{objects.NewInt(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	l := objects.NewList([]objects.Object{objects.NewInt(10), objects.NewInt(20), objects.NewInt(30)})
	out, err := objects.Call(ig1, objects.NewTuple([]objects.Object{l}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intOf(t, out) != 20 {
		t.Fatalf("itemgetter(1)(l) = %d, want 20", intOf(t, out))
	}
	// Multi-item getter.
	ig2, err := ItemgetterType.TpNew(ItemgetterType, []objects.Object{objects.NewInt(0), objects.NewInt(2)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err = objects.Call(ig2, objects.NewTuple([]objects.Object{l}), nil)
	if err != nil {
		t.Fatal(err)
	}
	tup, ok := out.(*objects.Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("multi itemgetter want 2-tuple, got %v", out)
	}
	if intOf(t, tup.Item(0)) != 10 || intOf(t, tup.Item(1)) != 30 {
		t.Fatalf("multi itemgetter wrong values")
	}
}

func TestAttrgetter(t *testing.T) {
	// Build an instance of a user-defined class so attribute lookup
	// falls through to the per-instance __dict__.
	tp := objects.NewUserType("AttrFoo", nil, objects.NewDict())
	inst := objects.NewInstance(tp)
	d := inst.Dict()
	_ = d.SetItem(objects.NewStr("x"), objects.NewInt(11))
	_ = d.SetItem(objects.NewStr("y"), objects.NewInt(22))

	ag, err := AttrgetterType.TpNew(AttrgetterType, []objects.Object{objects.NewStr("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := objects.Call(ag, objects.NewTuple([]objects.Object{inst}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intOf(t, out) != 11 {
		t.Fatalf("attrgetter('x') = %d, want 11", intOf(t, out))
	}
	// Two attrs.
	ag2, err := AttrgetterType.TpNew(AttrgetterType, []objects.Object{objects.NewStr("x"), objects.NewStr("y")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err = objects.Call(ag2, objects.NewTuple([]objects.Object{inst}), nil)
	if err != nil {
		t.Fatal(err)
	}
	tup, ok := out.(*objects.Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("attrgetter(x, y) want 2-tuple, got %v", out)
	}
	if intOf(t, tup.Item(0)) != 11 || intOf(t, tup.Item(1)) != 22 {
		t.Fatalf("attrgetter values wrong: %d, %d", intOf(t, tup.Item(0)), intOf(t, tup.Item(1)))
	}
}

func TestMethodcaller(t *testing.T) {
	// Build a type with a callable attribute (a builtin method that
	// returns the bound instance's stored value plus an offset).
	tp := objects.NewUserType("MCFoo", nil, objects.NewDict())
	inst := objects.NewInstance(tp)
	addOffset := objects.NewBuiltinFunction("add_offset", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		offset, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(100 + offset), nil
	})
	_ = inst.Dict().SetItem(objects.NewStr("bump"), addOffset)

	mc, err := MethodcallerType.TpNew(MethodcallerType, []objects.Object{objects.NewStr("bump"), objects.NewInt(5)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := objects.Call(mc, objects.NewTuple([]objects.Object{inst}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if intOf(t, out) != 105 {
		t.Fatalf("methodcaller bump(5) = %d, want 105", intOf(t, out))
	}
}

func TestCompareDigestBytes(t *testing.T) {
	out := callBuiltin(t, "_compare_digest", objects.NewBytesFromString("hello"), objects.NewBytesFromString("hello"))
	if !boolOf(t, out) {
		t.Fatalf("_compare_digest equal failed")
	}
	out = callBuiltin(t, "_compare_digest", objects.NewBytesFromString("hello"), objects.NewBytesFromString("world"))
	if boolOf(t, out) {
		t.Fatalf("_compare_digest different should be False")
	}
	out = callBuiltin(t, "_compare_digest", objects.NewBytesFromString("hello"), objects.NewBytesFromString("hellothere"))
	if boolOf(t, out) {
		t.Fatalf("_compare_digest different lengths should be False")
	}
}

func TestCompareDigestStr(t *testing.T) {
	a := objects.NewStr("abc")
	b := objects.NewStr("abc")
	out := callBuiltin(t, "_compare_digest", a, b)
	if !boolOf(t, out) {
		t.Fatalf("_compare_digest equal strings failed")
	}
}

// mustGet looks up a member of mod and fails the test on miss.
func mustGet(t *testing.T, mod *objects.Module, name string) objects.Object {
	t.Helper()
	v, err := mod.Dict().GetItem(objects.NewStr(name))
	if err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	return v
}
