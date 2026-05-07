package objects

import (
	"strings"
	"testing"
)

// makeClassWithMember builds a user type whose namespace contains a
// single (name, value) attribute. It's a thin shortcut that keeps the
// super tests readable without pulling in __build_class__.
func makeClassWithMember(t *testing.T, name string, bases []*Type, member, value string) *Type {
	t.Helper()
	ns := NewDict()
	if err := ns.SetItem(NewStr(member), NewStr(value)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	return NewUserType(name, bases, ns)
}

func TestSuperWalksMROAfterType(t *testing.T) {
	a := makeClassWithMember(t, "A", nil, "tag", "from_A")
	b := NewUserType("B", []*Type{a}, NewDict())
	c := NewUserType("C", []*Type{b}, NewDict())
	inst := NewInstance(c)

	su, err := NewSuper(b, inst)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	v, err := superGetAttr(su, NewStr("tag"))
	if err != nil {
		t.Fatalf("superGetAttr: %v", err)
	}
	got, ok := v.(*Unicode)
	if !ok {
		t.Fatalf("got %T, want *Unicode", v)
	}
	if got.v != "from_A" {
		t.Fatalf("got %q, want from_A", got.v)
	}
}

func TestSuperSkipsAttrOnNamedType(t *testing.T) {
	a := makeClassWithMember(t, "A", nil, "tag", "from_A")
	bNS := NewDict()
	if err := bNS.SetItem(NewStr("tag"), NewStr("from_B")); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	b := NewUserType("B", []*Type{a}, bNS)
	c := NewUserType("C", []*Type{b}, NewDict())
	inst := NewInstance(c)

	// super(B, inst) skips B and lands on A's tag.
	su, err := NewSuper(b, inst)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	v, err := superGetAttr(su, NewStr("tag"))
	if err != nil {
		t.Fatalf("superGetAttr: %v", err)
	}
	if v.(*Unicode).v != "from_A" {
		t.Fatalf("got %q, want from_A", v.(*Unicode).v)
	}

	// super(C, inst) lands on B's tag (the next class after C).
	su2, err := NewSuper(c, inst)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	v2, err := superGetAttr(su2, NewStr("tag"))
	if err != nil {
		t.Fatalf("superGetAttr: %v", err)
	}
	if v2.(*Unicode).v != "from_B" {
		t.Fatalf("got %q, want from_B", v2.(*Unicode).v)
	}
}

func TestSuperRejectsNonSubtype(t *testing.T) {
	a := NewUserType("A", nil, NewDict())
	b := NewUserType("B", nil, NewDict()) // unrelated
	inst := NewInstance(b)
	if _, err := NewSuper(a, inst); err == nil {
		t.Fatal("expected TypeError, got nil")
	} else if !strings.Contains(err.Error(), "is not an instance or subtype") {
		t.Fatalf("error = %v, want containing supercheck message", err)
	}
}

func TestSuperWithClassReceiver(t *testing.T) {
	a := makeClassWithMember(t, "A", nil, "tag", "from_A")
	b := NewUserType("B", []*Type{a}, NewDict())

	// super(B, B) is the classmethod path: obj is the class itself.
	su, err := NewSuper(b, b)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	v, err := superGetAttr(su, NewStr("tag"))
	if err != nil {
		t.Fatalf("superGetAttr: %v", err)
	}
	if v.(*Unicode).v != "from_A" {
		t.Fatalf("got %q, want from_A", v.(*Unicode).v)
	}
}

func TestSuperUnboundFormReturnsSelfOnClassAccess(t *testing.T) {
	a := NewUserType("A", nil, NewDict())
	b := NewUserType("B", []*Type{a}, NewDict())
	su, err := NewSuper(b, nil)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	got, err := superDescrGet(su, nil, b)
	if err != nil {
		t.Fatalf("superDescrGet: %v", err)
	}
	if got != su {
		t.Fatalf("got %p, want self %p", got, su)
	}
}

func TestSuperUnboundDescrGetRebindsToInstance(t *testing.T) {
	a := makeClassWithMember(t, "A", nil, "tag", "from_A")
	b := NewUserType("B", []*Type{a}, NewDict())
	su, err := NewSuper(b, nil)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	inst := NewInstance(b)
	bound, err := superDescrGet(su, inst, b)
	if err != nil {
		t.Fatalf("superDescrGet: %v", err)
	}
	bs, ok := bound.(*Super)
	if !ok {
		t.Fatalf("got %T, want *Super", bound)
	}
	if bs == su {
		t.Fatal("expected a fresh Super, got the same unbound one")
	}
	if bs.obj != inst || bs.objType != b {
		t.Fatalf("rebind missed: obj=%v objType=%v", bs.obj, bs.objType)
	}
}

func TestSuperReprBound(t *testing.T) {
	a := NewUserType("A", nil, NewDict())
	b := NewUserType("B", []*Type{a}, NewDict())
	inst := NewInstance(b)
	su, err := NewSuper(b, inst)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	r, err := superRepr(su)
	if err != nil {
		t.Fatalf("superRepr: %v", err)
	}
	want := "<super: <class 'B'>, <B object>>"
	if r != want {
		t.Fatalf("repr = %q, want %q", r, want)
	}
}

func TestSuperReprUnbound(t *testing.T) {
	b := NewUserType("B", nil, NewDict())
	su, err := NewSuper(b, nil)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	r, err := superRepr(su)
	if err != nil {
		t.Fatalf("superRepr: %v", err)
	}
	want := "<super: <class 'B'>, NULL>"
	if r != want {
		t.Fatalf("repr = %q, want %q", r, want)
	}
}

func TestSuperCallTwoArgsViaTypeSlot(t *testing.T) {
	a := makeClassWithMember(t, "A", nil, "tag", "from_A")
	b := NewUserType("B", []*Type{a}, NewDict())
	inst := NewInstance(b)

	got, err := Call(SuperType, NewTuple([]Object{b, inst}), nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	su, ok := got.(*Super)
	if !ok {
		t.Fatalf("got %T, want *Super", got)
	}
	if su.typ != b || su.obj != inst || su.objType != b {
		t.Fatalf("super fields wrong: typ=%v obj=%v objType=%v", su.typ, su.obj, su.objType)
	}
}

func TestSuperCallRejectsKwargs(t *testing.T) {
	b := NewUserType("B", nil, NewDict())
	kw := NewDict()
	if err := kw.SetItem(NewStr("nope"), NewInt(1)); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	_, err := Call(SuperType, NewTuple([]Object{b}), kw)
	if err == nil {
		t.Fatal("expected TypeError, got nil")
	}
	if !strings.Contains(err.Error(), "no keyword arguments") {
		t.Fatalf("error = %v, want kwargs rejection", err)
	}
}

func TestSuperBoundMethodResolutionThroughDescriptor(t *testing.T) {
	// Functions are descriptors; their DescrGet returns a BoundMethod.
	// Make sure super hands the descriptor an instance bind target so
	// `super(C, c).method()` resolves to A.method bound to c.
	fn := NewBuiltinFunction("ping", func(args []Object, kwargs map[string]Object) (Object, error) {
		return args[0], nil
	})

	ns := NewDict()
	if err := ns.SetItem(NewStr("ping"), fn); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	a := NewUserType("A", nil, ns)
	c := NewUserType("C", []*Type{a}, NewDict())
	inst := NewInstance(c)

	su, err := NewSuper(c, inst)
	if err != nil {
		t.Fatalf("NewSuper: %v", err)
	}
	got, err := superGetAttr(su, NewStr("ping"))
	if err != nil {
		t.Fatalf("superGetAttr: %v", err)
	}
	// Built-in functions don't bind, so we get the raw function back.
	if got != fn {
		t.Fatalf("got %v, want %v", got, fn)
	}
}
