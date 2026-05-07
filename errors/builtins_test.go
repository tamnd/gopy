package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
)

func TestFloatingPointErrorIsArithmeticError(t *testing.T) {
	if !errors.IsSubtype(errors.PyExc_FloatingPointError, errors.PyExc_ArithmeticError) {
		t.Fatal("FloatingPointError must inherit from ArithmeticError")
	}
	if !errors.IsSubtype(errors.PyExc_FloatingPointError, errors.PyExc_Exception) {
		t.Fatal("FloatingPointError must inherit from Exception")
	}
	if errors.PyExc_FloatingPointError.Name != "FloatingPointError" {
		t.Fatalf("name = %q, want FloatingPointError", errors.PyExc_FloatingPointError.Name)
	}
}
