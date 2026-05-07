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

func TestVectorcallDictUnpacksKwargs(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 1 || len(kwargs) != 2 {
			t.Fatalf("got %d positional %d kwargs, want 1/2", len(args), len(kwargs))
		}
		return NewTuple([]Object{args[0], kwargs["a"], kwargs["b"]}), nil
	})
	d := NewDict()
	_ = d.SetItem(NewStr("a"), NewInt(2))
	_ = d.SetItem(NewStr("b"), NewInt(3))
	out, err := VectorcallDict(echo, []Object{NewInt(1)}, 1, d)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if v, _ := tup.Item(0).(*Int).Int64(); v != 1 {
		t.Errorf("pos = %d, want 1", v)
	}
	if v, _ := tup.Item(1).(*Int).Int64(); v != 2 {
		t.Errorf("a = %d, want 2", v)
	}
	if v, _ := tup.Item(2).(*Int).Int64(); v != 3 {
		t.Errorf("b = %d, want 3", v)
	}
}

func TestVectorcallDictEmptyDictSkipsUnpack(t *testing.T) {
	id := NewBuiltinFunction("id", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(kwargs) != 0 {
			t.Fatalf("unexpected kwargs: %v", kwargs)
		}
		return args[0], nil
	})
	v, err := VectorcallDict(id, []Object{NewInt(99)}, 1, NewDict())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*Int).Int64(); got != 99 {
		t.Errorf("got %d, want 99", got)
	}
}

func TestVectorcallDictNilDict(t *testing.T) {
	id := NewBuiltinFunction("id", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(kwargs) != 0 {
			t.Fatalf("unexpected kwargs: %v", kwargs)
		}
		return args[0], nil
	})
	v, err := VectorcallDict(id, []Object{NewInt(5)}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.(*Int).Int64(); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestVectorcallPrependPlacesArgFirst(t *testing.T) {
	first := NewBuiltinFunction("first", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 3 {
			t.Fatalf("got %d args, want 3", len(args))
		}
		return NewTuple(args), nil
	})
	out, err := VectorcallPrepend(first, NewInt(0), []Object{NewInt(1), NewInt(2)}, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	for i, want := range []int64{0, 1, 2} {
		if got, _ := tup.Item(i).(*Int).Int64(); got != want {
			t.Errorf("arg[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestVectorcallPrependCarriesKwnames(t *testing.T) {
	echo := NewBuiltinFunction("echo", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 2 {
			t.Fatalf("got %d args, want 2", len(args))
		}
		v, ok := kwargs["k"]
		if !ok {
			t.Fatalf("missing kwarg k")
		}
		return NewTuple([]Object{args[0], args[1], v}), nil
	})
	// Caller's vectorcall stack: 1 positional + 1 kwarg value.
	stack := []Object{NewInt(7), NewInt(42)}
	out, err := VectorcallPrepend(echo, NewInt(0), stack, 1, NewTuple([]Object{NewStr("k")}))
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	for i, want := range []int64{0, 7, 42} {
		if got, _ := tup.Item(i).(*Int).Int64(); got != want {
			t.Errorf("slot[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestVectorcallPrependHonorsOffsetBit(t *testing.T) {
	first := NewBuiltinFunction("first", func(args []Object, _ map[string]Object) (Object, error) {
		return NewTuple(args), nil
	})
	// Even with the OFFSET bit set, the prepended arg must land at index 0.
	out, err := VectorcallPrepend(first, NewInt(99), []Object{NewInt(1)}, 1|VectorcallArgumentsOffset, nil)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	if v, _ := tup.Item(0).(*Int).Int64(); v != 99 {
		t.Errorf("slot[0] = %d, want 99", v)
	}
	if v, _ := tup.Item(1).(*Int).Int64(); v != 1 {
		t.Errorf("slot[1] = %d, want 1", v)
	}
}

func TestCallPrependBindsObjAsSelf(t *testing.T) {
	bound := NewBuiltinFunction("bound", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 3 {
			t.Fatalf("got %d args, want 3", len(args))
		}
		v := kwargs["k"]
		return NewTuple([]Object{args[0], args[1], args[2], v}), nil
	})
	kw := NewDict()
	_ = kw.SetItem(NewStr("k"), NewInt(9))
	out, err := CallPrepend(bound, NewInt(0), NewTuple([]Object{NewInt(1), NewInt(2)}), kw)
	if err != nil {
		t.Fatal(err)
	}
	tup := out.(*Tuple)
	for i, want := range []int64{0, 1, 2, 9} {
		if got, _ := tup.Item(i).(*Int).Int64(); got != want {
			t.Errorf("slot[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestCallPrependNilArgs(t *testing.T) {
	id := NewBuiltinFunction("id", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			t.Fatalf("got %d args, want 1", len(args))
		}
		return args[0], nil
	})
	out, err := CallPrepend(id, NewInt(11), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := out.(*Int).Int64(); got != 11 {
		t.Errorf("got %d, want 11", got)
	}
}
