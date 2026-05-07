package builtins

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestGlobalsRoutesThroughHook(t *testing.T) {
	g := objects.NewDict()
	prev := currentScope
	defer SetCurrentScope(prev)
	SetCurrentScope(func() (objects.Object, objects.Object) {
		return g, nil
	})

	out, err := Globals(nil, nil)
	if err != nil {
		t.Fatalf("globals(): %v", err)
	}
	if out != g {
		t.Fatalf("globals() = %v, want %v", out, g)
	}
}

func TestLocalsRoutesThroughHook(t *testing.T) {
	l := objects.NewDict()
	prev := currentScope
	defer SetCurrentScope(prev)
	SetCurrentScope(func() (objects.Object, objects.Object) {
		return nil, l
	})

	out, err := Locals(nil, nil)
	if err != nil {
		t.Fatalf("locals(): %v", err)
	}
	if out != l {
		t.Fatalf("locals() = %v, want %v", out, l)
	}
}

func TestGlobalsReturnsNoneWithoutHook(t *testing.T) {
	prev := currentScope
	defer SetCurrentScope(prev)
	SetCurrentScope(nil)
	out, err := Globals(nil, nil)
	if err != nil {
		t.Fatalf("globals(): %v", err)
	}
	if !objects.IsNone(out) {
		t.Fatalf("globals() = %v, want None", out)
	}
}

func TestLocalsReturnsNoneWithoutHook(t *testing.T) {
	prev := currentScope
	defer SetCurrentScope(prev)
	SetCurrentScope(nil)
	out, err := Locals(nil, nil)
	if err != nil {
		t.Fatalf("locals(): %v", err)
	}
	if !objects.IsNone(out) {
		t.Fatalf("locals() = %v, want None", out)
	}
}

func TestGlobalsRejectsArguments(t *testing.T) {
	if _, err := Globals([]objects.Object{objects.NewInt(1)}, nil); err == nil {
		t.Fatal("globals(1): want TypeError, got nil")
	}
}

func TestLocalsRejectsArguments(t *testing.T) {
	if _, err := Locals([]objects.Object{objects.NewInt(1)}, nil); err == nil {
		t.Fatal("locals(1): want TypeError, got nil")
	}
}
