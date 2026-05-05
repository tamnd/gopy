package objects

import "testing"

func TestBuiltinFunctionCall(t *testing.T) {
	add := NewBuiltinFunction("add", func(args []Object, _ map[string]Object) (Object, error) {
		a, _ := args[0].(*Int).Int64()
		b, _ := args[1].(*Int).Int64()
		return NewInt(a + b), nil
	})
	v, err := Call(add, []Object{NewInt(2), NewInt(3)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.(*Int).Int64()
	if got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestCallNonCallable(t *testing.T) {
	_, err := Call(NewInt(0), nil, nil)
	if err == nil {
		t.Fatal("expected error for non-callable int")
	}
}
