package builtins

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestAIterReturnsAsyncGenerator(t *testing.T) {
	g := objects.NewAsyncGenerator("g")
	out, err := AIter([]objects.Object{g}, nil)
	if err != nil {
		t.Fatalf("aiter(g): %v", err)
	}
	if out != g {
		t.Fatalf("aiter(g) = %v, want %v", out, g)
	}
}

func TestAIterRejectsNonAsync(t *testing.T) {
	if _, err := AIter([]objects.Object{objects.NewInt(1)}, nil); err == nil {
		t.Fatal("aiter(1): want TypeError, got nil")
	}
}

func TestANextReturnsAwaitable(t *testing.T) {
	g := objects.NewAsyncGenerator("g")
	out, err := ANext([]objects.Object{g}, nil)
	if err != nil {
		t.Fatalf("anext(g): %v", err)
	}
	if out == nil {
		t.Fatal("anext(g) = nil, want awaitable")
	}
	if out.Type() != objects.AsyncGenASendType {
		t.Fatalf("anext(g) type = %s, want async_generator_asend", out.Type().Name)
	}
}

func TestANextDefaultUnsupported(t *testing.T) {
	g := objects.NewAsyncGenerator("g")
	_, err := ANext([]objects.Object{g, objects.NewInt(0)}, nil)
	if err == nil {
		t.Fatal("anext(g, default): want NotImplementedError, got nil")
	}
}

func TestANextRejectsNonAsync(t *testing.T) {
	if _, err := ANext([]objects.Object{objects.NewInt(1)}, nil); err == nil {
		t.Fatal("anext(1): want TypeError, got nil")
	}
}

func TestAIterRejectsKwargs(t *testing.T) {
	g := objects.NewAsyncGenerator("g")
	_, err := AIter([]objects.Object{g}, map[string]objects.Object{"x": objects.NewInt(1)})
	if err == nil {
		t.Fatal("aiter(g, x=1): want TypeError, got nil")
	}
}

func TestAIterArgRange(t *testing.T) {
	if _, err := AIter(nil, nil); err == nil {
		t.Fatal("aiter(): want TypeError, got nil")
	}
}

func TestANextArgRange(t *testing.T) {
	if _, err := ANext(nil, nil); err == nil {
		t.Fatal("anext(): want TypeError, got nil")
	}
	g := objects.NewAsyncGenerator("g")
	too := []objects.Object{g, objects.NewInt(0), objects.NewInt(1)}
	if _, err := ANext(too, nil); err == nil {
		t.Fatal("anext(g, 0, 1): want TypeError, got nil")
	}
}
