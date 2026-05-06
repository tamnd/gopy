package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func TestWarningHierarchy(t *testing.T) {
	cases := []struct {
		name string
		typ  *objects.Type
	}{
		{"UserWarning", errors.PyExc_UserWarning},
		{"DeprecationWarning", errors.PyExc_DeprecationWarning},
		{"PendingDeprecationWarning", errors.PyExc_PendingDeprecationWarning},
		{"SyntaxWarning", errors.PyExc_SyntaxWarning},
		{"RuntimeWarning", errors.PyExc_RuntimeWarning},
		{"FutureWarning", errors.PyExc_FutureWarning},
		{"ImportWarning", errors.PyExc_ImportWarning},
		{"UnicodeWarning", errors.PyExc_UnicodeWarning},
		{"BytesWarning", errors.PyExc_BytesWarning},
		{"ResourceWarning", errors.PyExc_ResourceWarning},
		{"EncodingWarning", errors.PyExc_EncodingWarning},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.typ.Name != c.name {
				t.Fatalf("name = %q, want %q", c.typ.Name, c.name)
			}
			if !errors.IsSubtype(c.typ, errors.PyExc_Warning) {
				t.Fatalf("%s is not a Warning subclass", c.name)
			}
			if !errors.IsSubtype(c.typ, errors.PyExc_Exception) {
				t.Fatalf("%s is not an Exception subclass", c.name)
			}
		})
	}
	if !errors.IsSubtype(errors.PyExc_Warning, errors.PyExc_Exception) {
		t.Fatal("Warning must inherit from Exception")
	}
}
