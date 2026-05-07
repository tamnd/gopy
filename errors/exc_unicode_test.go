package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func TestUnicodeErrorHierarchy(t *testing.T) {
	if !errors.IsSubtype(errors.PyExc_UnicodeError, errors.PyExc_ValueError) {
		t.Fatal("UnicodeError must inherit from ValueError")
	}
	for _, sub := range []struct {
		name string
		typ  *objects.Type
	}{
		{"UnicodeEncodeError", errors.PyExc_UnicodeEncodeError},
		{"UnicodeDecodeError", errors.PyExc_UnicodeDecodeError},
		{"UnicodeTranslateError", errors.PyExc_UnicodeTranslateError},
	} {
		t.Run(sub.name, func(t *testing.T) {
			if !errors.IsSubtype(sub.typ, errors.PyExc_UnicodeError) {
				t.Fatalf("%s must inherit from UnicodeError", sub.name)
			}
			if !errors.IsSubtype(sub.typ, errors.PyExc_ValueError) {
				t.Fatalf("%s must transitively inherit from ValueError", sub.name)
			}
			if sub.typ.Name != sub.name {
				t.Fatalf("name = %q, want %q", sub.typ.Name, sub.name)
			}
		})
	}
}
