package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
)

func TestSyntaxErrorHierarchy(t *testing.T) {
	if !errors.IsSubtype(errors.PyExc_SyntaxError, errors.PyExc_Exception) {
		t.Fatal("SyntaxError must inherit from Exception")
	}
	if !errors.IsSubtype(errors.PyExc_IndentationError, errors.PyExc_SyntaxError) {
		t.Fatal("IndentationError must inherit from SyntaxError")
	}
	if !errors.IsSubtype(errors.PyExc_TabError, errors.PyExc_IndentationError) {
		t.Fatal("TabError must inherit from IndentationError")
	}
	if !errors.IsSubtype(errors.PyExc_TabError, errors.PyExc_SyntaxError) {
		t.Fatal("TabError must transitively inherit from SyntaxError")
	}
	if !errors.IsSubtype(errors.PyExc_IncompleteInputError, errors.PyExc_SyntaxError) {
		t.Fatal("_IncompleteInputError must inherit from SyntaxError")
	}
	if errors.PyExc_TabError.Name != "TabError" {
		t.Fatalf("name = %q, want TabError", errors.PyExc_TabError.Name)
	}
}
