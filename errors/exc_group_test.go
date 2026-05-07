package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
)

func TestExceptionGroupHierarchy(t *testing.T) {
	if !errors.IsSubtype(errors.PyExc_BaseExceptionGroup, errors.PyExc_BaseException) {
		t.Fatal("BaseExceptionGroup must inherit from BaseException")
	}
	if errors.IsSubtype(errors.PyExc_BaseExceptionGroup, errors.PyExc_Exception) {
		t.Fatal("BaseExceptionGroup must NOT inherit from Exception")
	}
	if !errors.IsSubtype(errors.PyExc_ExceptionGroup, errors.PyExc_BaseExceptionGroup) {
		t.Fatal("ExceptionGroup must inherit from BaseExceptionGroup")
	}
	if !errors.IsSubtype(errors.PyExc_ExceptionGroup, errors.PyExc_Exception) {
		t.Fatal("ExceptionGroup must inherit from Exception (PEP 654 multi-base)")
	}
	if errors.PyExc_ExceptionGroup.Name != "ExceptionGroup" {
		t.Fatalf("name = %q, want ExceptionGroup", errors.PyExc_ExceptionGroup.Name)
	}
}
