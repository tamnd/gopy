// Gate panel for the _contextvars built-in module registration.
// Mirrors the test_contextvars boundary: import _contextvars; check
// the four exposed names; check the type-as-constructor shape.

package contextvar

import (
	"strings"
	"testing"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func TestInittabRegistration(t *testing.T) {
	if imp.FindInitFunc("_contextvars") == nil {
		t.Fatal("_contextvars not registered in inittab")
	}
}

func TestBuildModuleExports(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	want := []string{"Context", "ContextVar", "Token", "copy_context"}
	for _, name := range want {
		v, err := mod.Dict().GetItem(objects.NewStr(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v == nil {
			t.Fatalf("%s: nil entry", name)
		}
	}
}

func TestModuleNameAttr(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	v, err := mod.Dict().GetItem(objects.NewStr("__name__"))
	if err != nil {
		t.Fatalf("__name__: %v", err)
	}
	got, err := objects.Str(v)
	if err != nil {
		t.Fatalf("Str(__name__): %v", err)
	}
	if got != "_contextvars" {
		t.Fatalf("__name__ = %q, want %q", got, "_contextvars")
	}
}

func TestContextConstructor(t *testing.T) {
	out, err := ContextType.Call(ContextType, nil, nil)
	if err != nil {
		t.Fatalf("Context(): %v", err)
	}
	if _, ok := out.(*Context); !ok {
		t.Fatalf("Context() returned %T, want *Context", out)
	}
}

func TestContextConstructorRejectsArgs(t *testing.T) {
	_, err := ContextType.Call(ContextType, []objects.Object{objects.NewStr("x")}, nil)
	if err == nil {
		t.Fatal("Context('x'): want TypeError, got nil")
	}
	if !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("Context('x'): err=%v, want TypeError", err)
	}
}

func TestContextVarConstructor(t *testing.T) {
	out, err := ContextVarType.Call(ContextVarType,
		[]objects.Object{objects.NewStr("v1")}, nil)
	if err != nil {
		t.Fatalf("ContextVar('v1'): %v", err)
	}
	cv, ok := out.(*ContextVar)
	if !ok {
		t.Fatalf("ContextVar('v1') returned %T, want *ContextVar", out)
	}
	if cv.Name() != "v1" {
		t.Fatalf("Name() = %q, want %q", cv.Name(), "v1")
	}
	if _, hasDef := cv.Default(); hasDef {
		t.Fatal("Default(): hasDef true, want false")
	}
}

func TestContextVarConstructorWithDefault(t *testing.T) {
	def := objects.NewStr("fallback")
	out, err := ContextVarType.Call(ContextVarType,
		[]objects.Object{objects.NewStr("v2")},
		map[string]objects.Object{"default": def})
	if err != nil {
		t.Fatalf("ContextVar('v2', default=...): %v", err)
	}
	cv := out.(*ContextVar)
	got, hasDef := cv.Default()
	if !hasDef {
		t.Fatal("Default(): hasDef false, want true")
	}
	if got != def {
		t.Fatalf("Default() value: got %v, want %v", got, def)
	}
}

func TestContextVarConstructorRejectsBadKwarg(t *testing.T) {
	_, err := ContextVarType.Call(ContextVarType,
		[]objects.Object{objects.NewStr("v3")},
		map[string]objects.Object{"junk": objects.NewStr("x")})
	if err == nil {
		t.Fatal("ContextVar(..., junk=...): want TypeError, got nil")
	}
	if !strings.Contains(err.Error(), "junk") {
		t.Fatalf("err=%v, want mention of 'junk'", err)
	}
}

func TestTokenConstructorIsForbidden(t *testing.T) {
	_, err := TokenType.Call(TokenType, nil, nil)
	if err == nil {
		t.Fatal("Token(): want RuntimeError, got nil")
	}
	if !strings.Contains(err.Error(), "RuntimeError") {
		t.Fatalf("err=%v, want RuntimeError", err)
	}
}
