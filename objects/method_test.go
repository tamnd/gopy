package objects

import "testing"

func TestBoundMethodPrependsSelf(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	self := NewInt(99)
	bm := NewBoundMethod(echo, self)
	out, err := CallNoArgs(bm)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if tup.Len() != 1 {
		t.Fatalf("len=%d, want 1", tup.Len())
	}
	if tup.Item(0) != self {
		t.Errorf("first arg = %v, want self", tup.Item(0))
	}
}

func TestBoundMethodForwardsArgs(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	self := NewInt(1)
	bm := NewBoundMethod(echo, self)
	out, err := CallOneArg(bm, NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if tup.Len() != 2 {
		t.Fatalf("len=%d, want 2", tup.Len())
	}
	a, _ := tup.Item(0).(*Int).Int64()
	b, _ := tup.Item(1).(*Int).Int64()
	if a != 1 || b != 2 {
		t.Errorf("got (%d, %d), want (1, 2)", a, b)
	}
}

func TestClassMethodBindsType(t *testing.T) {
	fn := NewBuiltinFunction("noop", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	cm := NewClassMethod(fn)
	tp := NewType("X", []*Type{objectType})
	bound, err := classMethodDescrGet(cm, NewInt(0), tp)
	if err != nil {
		t.Fatal(err)
	}
	bm := bound.(*BoundMethod)
	if bm.Self() != tp {
		t.Errorf("classmethod bound to %v, want %v", bm.Self(), tp)
	}
}

// Phase 2 gates for spec 1704: classmethod must expose __func__,
// __wrapped__, and __isabstractmethod__ so abstractmethod / wraps /
// inspect can introspect a decorated classmethod the way CPython does.
//
// CPython: Objects/funcobject.c:1504 cm_memberlist
// CPython: Objects/funcobject.c:1551 cm_getsetlist
func TestClassMethodGetSetGates(t *testing.T) {
	fn := NewBuiltinFunction("f", func(args []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	cm := NewClassMethod(fn)

	got, err := GetAttr(cm, NewStr("__func__"))
	if err != nil || got != fn {
		t.Fatalf("__func__ = %v, %v; want %v, nil", got, err, fn)
	}
	got, err = GetAttr(cm, NewStr("__wrapped__"))
	if err != nil || got != fn {
		t.Fatalf("__wrapped__ = %v, %v; want %v, nil", got, err, fn)
	}
	isAbs, err := GetAttr(cm, NewStr("__isabstractmethod__"))
	if err != nil {
		t.Fatalf("__isabstractmethod__ error: %v", err)
	}
	if isAbs != False() {
		t.Errorf("__isabstractmethod__ = %v, want False", isAbs)
	}
}

// classmethod's repr matches CPython's <classmethod(REPR)> shape so
// debugging a decorated callable shows the wrapped function, not just
// "<classmethod object>".
//
// CPython: Objects/funcobject.c:1565 cm_repr
func TestClassMethodReprShowsCallable(t *testing.T) {
	fn := NewBuiltinFunction("inner", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	cm := NewClassMethod(fn)
	r, err := Repr(cm)
	if err != nil {
		t.Fatal(err)
	}
	want := "<classmethod(<built-in function inner>)>"
	if r != want {
		t.Errorf("repr = %q, want %q", r, want)
	}
}

// gate 5.1: same bound method compares equal to itself.
//
// CPython: Objects/classobject.c:206 method_richcompare
func TestBoundMethodEqSelf(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	self := NewInt(1)
	a := NewBoundMethod(fn, self)
	b := NewBoundMethod(fn, self)
	eq, err := RichCmpBool(a, b, CompareEQ)
	if err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Error("bound methods over same (func, self) should compare equal")
	}
}

// gate 5.2: distinct self => not equal.
func TestBoundMethodNeqDifferentSelf(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	a := NewBoundMethod(fn, NewInt(1))
	b := NewBoundMethod(fn, NewInt(2))
	ne, err := RichCmpBool(a, b, CompareNE)
	if err != nil {
		t.Fatal(err)
	}
	if !ne {
		t.Error("bound methods over distinct self should compare not-equal")
	}
}

// gate 5.3: two bound methods sharing only func differ by hash. The
// identity-hash contribution from im_self is what keeps them in
// distinct buckets.
//
// CPython: Objects/classobject.c:230 method_hash
func TestBoundMethodHashDistinctPerSelf(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	a := NewBoundMethod(fn, NewList(nil))
	b := NewBoundMethod(fn, NewList(nil))
	ha, err := Hash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := Hash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Errorf("hashes should differ for different self bindings, got %d", ha)
	}
}

// gate 5.5: repr matches `<bound method QUALNAME of REPR>` with the
// function's qualname and the self repr. CPython prefers the wrapped
// function's __qualname__ so nested-class methods print as
// `Outer.Inner.f` instead of just `f`.
//
// CPython: Objects/classobject.c:280 method_repr
func TestBoundMethodReprFormat(t *testing.T) {
	code := NewCode()
	code.Name = "speak"
	fn := NewFunction("speak", code, NewDict())
	fn.Qualname = "C.speak"
	self := NewStr("the-self")
	m := NewBoundMethod(fn, self)
	got, err := Repr(m)
	if err != nil {
		t.Fatal(err)
	}
	want := "<bound method C.speak of 'the-self'>"
	if got != want {
		t.Errorf("Repr = %q, want %q", got, want)
	}
}

// boundMethodFuncName falls back to "?" when the wrapped callable
// exposes neither __qualname__ nor __name__ (e.g. a raw built-in
// closure we wrap for tests).
//
// CPython: Objects/classobject.c:280 method_repr (defname fallback)
func TestBoundMethodReprFallsBackOnUnnamedFunc(t *testing.T) {
	fn := NewBuiltinFunction("unused", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	m := NewBoundMethod(fn, NewStr("self"))
	got, err := Repr(m)
	if err != nil {
		t.Fatal(err)
	}
	// BuiltinFunction has neither __qualname__ nor __name__ as
	// attributes yet, so the "?" fallback applies.
	if got != "<bound method ? of 'self'>" {
		t.Errorf("Repr = %q, want fallback", got)
	}
}

// Non-eq/ne comparisons short-circuit to NotImplemented so the
// protocol fallback raises the expected TypeError.
func TestBoundMethodOrderingNotImplemented(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	a := NewBoundMethod(fn, NewInt(1))
	b := NewBoundMethod(fn, NewInt(2))
	got, err := boundMethodRichCompare(a, b, CompareLT)
	if err != nil {
		t.Fatal(err)
	}
	if got != NotImplemented() {
		t.Errorf("LT should be NotImplemented, got %v", got)
	}
}

func TestStaticMethodReturnsCallable(t *testing.T) {
	fn := NewBuiltinFunction("noop", func(args []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	sm := NewStaticMethod(fn)
	got, err := staticMethodDescrGet(sm, NewInt(0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != fn {
		t.Errorf("staticmethod returned %v, want wrapped callable", got)
	}
}
