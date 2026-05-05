package objects

import "testing"

// TestVectorcallKwnamesRoutesThroughBuiltin verifies that a kwnames
// tuple plus inline values get unpacked into the (positional, kwargs)
// shape a BuiltinFunction expects. Mirrors what the eval loop's
// CALL_KW handler hands us.
func TestVectorcallKwnamesRoutesThroughBuiltin(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 1 {
			t.Fatalf("got %d positional, want 1", len(args))
		}
		v, ok := kwargs["k"]
		if !ok {
			t.Fatalf("kwarg %q missing", "k")
		}
		return NewTuple([]Object{args[0], v}), nil
	})
	stack := []Object{NewInt(1), NewInt(2)}
	out, err := Vectorcall(echo, stack, 1, NewTuple([]Object{NewStr("k")}))
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if got, _ := tup.Item(0).(*Int).Int64(); got != 1 {
		t.Errorf("positional = %d, want 1", got)
	}
	if got, _ := tup.Item(1).(*Int).Int64(); got != 2 {
		t.Errorf("kwarg = %d, want 2", got)
	}
}

// TestCallTupleDictRoutesThroughBuiltin covers PyObject_Call: a tuple
// of positional args and an optional dict of kwargs. The vectorcall
// fast path applies because BuiltinFunction has a Vectorcall slot.
func TestCallTupleDictRoutesThroughBuiltin(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, kwargs map[string]Object) (Object, error) {
		v := kwargs["k"]
		return NewTuple([]Object{args[0], v}), nil
	})
	d := NewDict()
	if err := d.SetItem(NewStr("k"), NewInt(20)); err != nil {
		t.Fatal(err)
	}
	out, err := Call(echo, NewTuple([]Object{NewInt(10)}), d)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if got, _ := tup.Item(0).(*Int).Int64(); got != 10 {
		t.Errorf("positional = %d, want 10", got)
	}
	if got, _ := tup.Item(1).(*Int).Int64(); got != 20 {
		t.Errorf("kwarg = %d, want 20", got)
	}
}

// TestVectorcallNargsStripsFlag exercises the bit-stripping helper.
func TestVectorcallNargsStripsFlag(t *testing.T) {
	if got := VectorcallNargs(3 | VectorcallArgumentsOffset); got != 3 {
		t.Errorf("VectorcallNargs(3|OFFSET) = %d, want 3", got)
	}
	if got := VectorcallNargs(0); got != 0 {
		t.Errorf("VectorcallNargs(0) = %d, want 0", got)
	}
}

// TestCallNoArgs hits the zero-arg path.
func TestCallNoArgs(t *testing.T) {
	noargs := NewBuiltinFunction("noargs", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 0 {
			t.Fatalf("got %d positional, want 0", len(args))
		}
		return NewInt(42), nil
	})
	v, err := CallNoArgs(noargs)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*Int).Int64(); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

// TestCallOneArg covers the single-positional shortcut.
func TestCallOneArg(t *testing.T) {
	id := NewBuiltinFunction("id", func(args []Object, _ map[string]Object) (Object, error) {
		return args[0], nil
	})
	v, err := CallOneArg(id, NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*Int).Int64(); got != 7 {
		t.Errorf("got %d, want 7", got)
	}
}
