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
