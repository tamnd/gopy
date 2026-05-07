package objects

import "testing"

func TestNamespaceSetGetAttr(t *testing.T) {
	n := NewNamespace()
	if err := n.Type().Setattro(n, NewStr("x"), NewInt(7)); err != nil {
		t.Fatalf("Setattro: %v", err)
	}
	v, err := n.Type().Getattro(n, NewStr("x"))
	if err != nil {
		t.Fatalf("Getattro: %v", err)
	}
	i, ok := v.(*Int)
	if !ok {
		t.Fatalf("Getattro = %v, want *Int", v)
	}
	n64, ok := i.Int64()
	if !ok || n64 != 7 {
		t.Fatalf("Getattro = %d, want 7", n64)
	}
}

func TestNamespaceRepr(t *testing.T) {
	n := NewNamespace()
	_ = n.Type().Setattro(n, NewStr("a"), NewInt(1))
	_ = n.Type().Setattro(n, NewStr("b"), NewInt(2))
	got, err := Repr(n)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	want := "namespace(a=1, b=2)"
	if got != want {
		t.Fatalf("Repr = %q, want %q", got, want)
	}
}

func TestNamespaceEqual(t *testing.T) {
	a := NewNamespace()
	_ = a.Type().Setattro(a, NewStr("x"), NewInt(1))
	b := NewNamespace()
	_ = b.Type().Setattro(b, NewStr("x"), NewInt(1))
	res, err := a.Type().RichCmp(a, b, CompareEQ)
	if err != nil {
		t.Fatalf("RichCmp: %v", err)
	}
	if res != True() {
		t.Fatalf("a == b: got %v, want True", res)
	}

	_ = b.Type().Setattro(b, NewStr("x"), NewInt(2))
	res, err = a.Type().RichCmp(a, b, CompareEQ)
	if err != nil {
		t.Fatalf("RichCmp: %v", err)
	}
	if res != False() {
		t.Fatalf("a != b after mutation: got %v, want False", res)
	}
}

func TestNamespaceTypeName(t *testing.T) {
	if NewNamespace().Type().Name != "types.SimpleNamespace" {
		t.Fatalf("type name = %q", NewNamespace().Type().Name)
	}
}
