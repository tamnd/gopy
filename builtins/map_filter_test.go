package builtins

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestMapBuiltinSingleIterable(t *testing.T) {
	src := objects.NewList([]objects.Object{objects.NewInt(1), objects.NewInt(2)})
	double := objects.NewBuiltinFunction("dbl", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		v, _ := args[0].(*objects.Int).Int64()
		return objects.NewInt(v * 2), nil
	})
	out, err := Map([]objects.Object{double, src}, nil)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if out.Type() != objects.MapType {
		t.Fatalf("out.Type = %s, want map", out.Type().Name)
	}
}

func TestMapBuiltinNeedsTwoArgs(t *testing.T) {
	if _, err := Map([]objects.Object{objects.None()}, nil); err == nil {
		t.Fatal("Map(None): want TypeError, got nil")
	}
}

func TestMapBuiltinUnknownKwarg(t *testing.T) {
	src := objects.NewList(nil)
	_, err := Map(
		[]objects.Object{objects.None(), src},
		map[string]objects.Object{"unknown": objects.True()},
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected keyword") {
		t.Fatalf("err = %v, want unexpected keyword", err)
	}
}

func TestMapBuiltinStrictKwarg(t *testing.T) {
	a := objects.NewList([]objects.Object{objects.NewInt(1)})
	b := objects.NewList([]objects.Object{objects.NewInt(10), objects.NewInt(20)})
	add := objects.NewBuiltinFunction("add", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		return objects.NewInt(0), nil
	})
	out, err := Map(
		[]objects.Object{add, a, b},
		map[string]objects.Object{"strict": objects.True()},
	)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	m := out
	if _, err := objects.IterNext(m); err != nil {
		t.Fatalf("first next: %v", err)
	}
	_, err = objects.IterNext(m)
	if err == nil || !strings.Contains(err.Error(), "longer") {
		t.Fatalf("err = %v, want strict-mode longer error", err)
	}
}

func TestFilterBuiltin(t *testing.T) {
	src := objects.NewList([]objects.Object{objects.NewInt(0), objects.NewInt(1), objects.NewInt(0)})
	out, err := Filter([]objects.Object{objects.None(), src}, nil)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if out.Type() != objects.FilterType {
		t.Fatalf("out.Type = %s, want filter", out.Type().Name)
	}
	v, err := objects.IterNext(out)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if iv, _ := v.(*objects.Int).Int64(); iv != 1 {
		t.Fatalf("first value = %d, want 1", iv)
	}
	if _, err := objects.IterNext(out); !errors.Is(err, objects.ErrStopIteration) {
		t.Fatalf("next = %v, want StopIteration", err)
	}
}

func TestFilterBuiltinArity(t *testing.T) {
	if _, err := Filter([]objects.Object{objects.None()}, nil); err == nil {
		t.Fatal("Filter(None): want TypeError, got nil")
	}
	if _, err := Filter(
		[]objects.Object{objects.None(), objects.NewList(nil)},
		map[string]objects.Object{"x": objects.True()},
	); err == nil {
		t.Fatal("Filter(None, [], x=True): want TypeError, got nil")
	}
}
