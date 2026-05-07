package objects

import (
	"strings"
	"testing"
)

func TestCapsuleRoundTrip(t *testing.T) {
	target := struct{ x int }{x: 42}
	c, err := NewCapsule(&target, "mod.target", nil)
	if err != nil {
		t.Fatalf("NewCapsule: %v", err)
	}
	if !c.IsValid("mod.target") {
		t.Fatal("capsule should be valid for its name")
	}
	if c.IsValid("mod.other") {
		t.Fatal("capsule must reject mismatched name")
	}
	got, err := c.GetPointer("mod.target")
	if err != nil {
		t.Fatalf("GetPointer: %v", err)
	}
	if got.(*struct{ x int }).x != 42 {
		t.Fatal("pointer payload corrupted")
	}
}

func TestCapsuleNullPointerRejected(t *testing.T) {
	if _, err := NewCapsule(nil, "x", nil); err == nil {
		t.Fatal("NewCapsule(nil) should fail")
	}
}

func TestCapsuleGetPointerWrongName(t *testing.T) {
	c, _ := NewCapsule(&struct{}{}, "real", nil)
	if _, err := c.GetPointer("fake"); err == nil {
		t.Fatal("GetPointer with wrong name should fail")
	}
}

func TestCapsuleRepr(t *testing.T) {
	c, _ := NewCapsule(&struct{}{}, "mymod.thing", nil)
	got, err := Repr(c)
	if err != nil {
		t.Fatalf("Repr: %v", err)
	}
	if !strings.Contains(got, `"mymod.thing"`) {
		t.Fatalf("Repr = %q, want name", got)
	}
}

func TestCapsuleContext(t *testing.T) {
	c, _ := NewCapsule(&struct{}{}, "x", nil)
	if c.Context() != nil {
		t.Fatal("fresh capsule should have nil context")
	}
	c.SetContext("ctx")
	if c.Context() != "ctx" {
		t.Fatalf("Context = %v, want ctx", c.Context())
	}
}
