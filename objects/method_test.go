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
