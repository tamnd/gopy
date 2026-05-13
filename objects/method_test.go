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

// method.__doc__ proxies to the wrapped function so introspection
// sees the target's docstring rather than the method type's.
//
// CPython: Objects/classobject.c:127 method_get_doc
func TestBoundMethodDocProxies(t *testing.T) {
	code := NewCode()
	code.Name = "echo"
	fn := NewFunction("echo", code, NewDict())
	if err := SetAttr(fn, NewStr("__doc__"), NewStr("the docstring")); err != nil {
		t.Fatal(err)
	}
	bm := NewBoundMethod(fn, NewInt(1))
	got, err := GetAttr(bm, NewStr("__doc__"))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := got.(*Unicode); !ok || s.Value() != "the docstring" {
		t.Errorf("__doc__ = %v, want 'the docstring'", got)
	}
}

// method.__reduce__ returns (getattr, (self, funcname)) so pickle can
// reconstitute the binding off the instance.
//
// CPython: Objects/classobject.c:90 method___reduce___impl
func TestBoundMethodReduce(t *testing.T) {
	code := NewCode()
	code.Name = "speak"
	fn := NewFunction("speak", code, NewDict())
	self := NewStr("S")
	bm := NewBoundMethod(fn, self)

	got, err := GetAttr(bm, NewStr("__reduce__"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Call(got, NewTuple(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	tup, ok := out.(*Tuple)
	if !ok || tup.Len() != 2 {
		t.Fatalf("reduce result = %v, want 2-tuple", out)
	}
	if tup.Item(0) != builtinGetattrCallable {
		t.Errorf("reduce[0] = %v, want getattr callable", tup.Item(0))
	}
	args, ok := tup.Item(1).(*Tuple)
	if !ok || args.Len() != 2 {
		t.Fatalf("reduce[1] = %v, want (self, name)", tup.Item(1))
	}
	if args.Item(0) != self {
		t.Errorf("reduce[1][0] = %v, want %v", args.Item(0), self)
	}
	if s, ok := args.Item(1).(*Unicode); !ok || s.Value() != "speak" {
		t.Errorf("reduce[1][1] = %v, want 'speak'", args.Item(1))
	}
}

// method.__get__(obj, cls) returns the bound method as-is. Looking up
// an already-bound method on a class attribute does not re-wrap it.
//
// CPython: Objects/classobject.c:292 method_descr_get
func TestBoundMethodDescrGetReturnsSelf(t *testing.T) {
	fn := NewBuiltinFunction("noop", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	bm := NewBoundMethod(fn, NewInt(1))
	got, err := boundMethodDescrGet(bm, NewInt(2), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != bm {
		t.Errorf("descr_get returned %v, want self", got)
	}
}

// method() constructor rejects non-callables and None instances.
//
// CPython: Objects/classobject.c:180 method_new_impl
func TestBoundMethodTpNewValidates(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	if _, err := boundMethodTpNew(BoundMethodType, []Object{NewInt(1), NewInt(2)}, nil); err == nil {
		t.Error("expected TypeError for non-callable")
	}
	if _, err := boundMethodTpNew(BoundMethodType, []Object{fn, None()}, nil); err == nil {
		t.Error("expected TypeError for None instance")
	}
	out, err := boundMethodTpNew(BoundMethodType, []Object{fn, NewInt(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*BoundMethod); !ok {
		t.Errorf("got %T, want *BoundMethod", out)
	}
}

// instancemethod wraps a callable and binds it through __get__.
//
// CPython: Objects/classobject.c:418 instancemethod_descr_get
func TestInstanceMethodDescrGetBinds(t *testing.T) {
	fn := NewBuiltinFunction("noop", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	im := NewInstanceMethod(fn)
	// Without an instance, __get__ returns the wrapped callable.
	got, err := instanceMethodDescrGet(im, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != fn {
		t.Errorf("descr_get(nil) = %v, want wrapped callable", got)
	}
	// With an instance, __get__ binds to it.
	got, err = instanceMethodDescrGet(im, NewInt(42), nil)
	if err != nil {
		t.Fatal(err)
	}
	bm, ok := got.(*BoundMethod)
	if !ok {
		t.Fatalf("descr_get(obj) = %T, want *BoundMethod", got)
	}
	self, _ := bm.Self().(*Int).Int64()
	if bm.Func() != fn || self != 42 {
		t.Errorf("bound method has wrong func/self: %v", bm)
	}
}

// Calling an instancemethod directly forwards to the wrapped callable.
//
// CPython: Objects/classobject.c:412 instancemethod_call
func TestInstanceMethodCallForwards(t *testing.T) {
	fn := NewBuiltinFunction("echo", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	im := NewInstanceMethod(fn)
	got, err := CallOneArg(im, NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	tup := got.(*Tuple)
	if tup.Len() != 1 {
		t.Fatalf("len = %d, want 1", tup.Len())
	}
	v, _ := tup.Item(0).(*Int).Int64()
	if v != 7 {
		t.Errorf("arg = %d, want 7", v)
	}
}

// instancemethod compares by wrapped callable; identity holds and
// distinct wrappers around the same callable compare equal.
//
// CPython: Objects/classobject.c:428 instancemethod_richcompare
func TestInstanceMethodEqByFunc(t *testing.T) {
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	other := NewBuiltinFunction("g", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	a := NewInstanceMethod(fn)
	b := NewInstanceMethod(fn)
	c := NewInstanceMethod(other)
	eq, err := RichCmpBool(a, b, CompareEQ)
	if err != nil || !eq {
		t.Errorf("a == b should be True, got %v / %v", eq, err)
	}
	ne, err := RichCmpBool(a, c, CompareNE)
	if err != nil || !ne {
		t.Errorf("a != c should be True, got %v / %v", ne, err)
	}
}

// instancemethod() __new__ rejects a non-callable first argument.
//
// CPython: Objects/classobject.c:488 instancemethod_new_impl
func TestInstanceMethodTpNewValidates(t *testing.T) {
	if _, err := instanceMethodTpNew(InstanceMethodType, []Object{NewInt(1)}, nil); err == nil {
		t.Error("expected TypeError for non-callable")
	}
	fn := NewBuiltinFunction("f", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	})
	out, err := instanceMethodTpNew(InstanceMethodType, []Object{fn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*InstanceMethod); !ok {
		t.Errorf("got %T, want *InstanceMethod", out)
	}
}
