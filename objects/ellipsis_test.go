package objects

import "testing"

func TestEllipsisSingleton(t *testing.T) {
	a := Ellipsis()
	b := Ellipsis()
	if a != b {
		t.Fatalf("Ellipsis() returned distinct values")
	}
	if !IsEllipsis(a) {
		t.Fatalf("IsEllipsis(Ellipsis()) = false")
	}
	if IsEllipsis(None()) {
		t.Fatalf("IsEllipsis(None) = true")
	}
}

func TestEllipsisRepr(t *testing.T) {
	got, err := Repr(Ellipsis())
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if got != "Ellipsis" {
		t.Fatalf("Repr = %q, want %q", got, "Ellipsis")
	}
}

func TestEllipsisType(t *testing.T) {
	if Ellipsis().Type().Name != "ellipsis" {
		t.Fatalf("type name = %q, want %q", Ellipsis().Type().Name, "ellipsis")
	}
}
