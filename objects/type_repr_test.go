package objects

import "testing"

func TestTypeReprBuiltin(t *testing.T) {
	r, err := Repr(IntType)
	if err != nil {
		t.Fatalf("Repr(IntType): %v", err)
	}
	if r != "<class 'int'>" {
		t.Fatalf("got %q, want <class 'int'>", r)
	}
}

func TestTypeReprWithModule(t *testing.T) {
	tp := NewType("Widget", []*Type{objectType})
	tp.Module = "ui"
	r, _ := Repr(tp)
	if r != "<class 'ui.Widget'>" {
		t.Fatalf("got %q, want <class 'ui.Widget'>", r)
	}
}

func TestTypeReprBuiltinsModuleHidden(t *testing.T) {
	tp := NewType("Stub", []*Type{objectType})
	tp.Module = "builtins"
	r, _ := Repr(tp)
	if r != "<class 'Stub'>" {
		t.Fatalf("got %q, want <class 'Stub'>", r)
	}
}
