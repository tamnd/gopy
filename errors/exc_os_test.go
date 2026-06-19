package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
)

func TestOSErrorHierarchy(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  *objects.Type
	}{
		{"BlockingIOError", errors.PyExc_BlockingIOError},
		{"ConnectionError", errors.PyExc_ConnectionError},
		{"ChildProcessError", errors.PyExc_ChildProcessError},
		{"BrokenPipeError", errors.PyExc_BrokenPipeError},
		{"ConnectionAbortedError", errors.PyExc_ConnectionAbortedError},
		{"ConnectionRefusedError", errors.PyExc_ConnectionRefusedError},
		{"ConnectionResetError", errors.PyExc_ConnectionResetError},
		{"FileExistsError", errors.PyExc_FileExistsError},
		{"FileNotFoundError", errors.PyExc_FileNotFoundError},
		{"IsADirectoryError", errors.PyExc_IsADirectoryError},
		{"NotADirectoryError", errors.PyExc_NotADirectoryError},
		{"InterruptedError", errors.PyExc_InterruptedError},
		{"PermissionError", errors.PyExc_PermissionError},
		{"ProcessLookupError", errors.PyExc_ProcessLookupError},
		{"TimeoutError", errors.PyExc_TimeoutError},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !errors.IsSubtype(c.typ, errors.PyExc_OSError) {
				t.Fatalf("%s must inherit from OSError", c.name)
			}
		})
	}
	if !errors.IsSubtype(errors.PyExc_BrokenPipeError, errors.PyExc_ConnectionError) {
		t.Fatal("BrokenPipeError must inherit from ConnectionError")
	}
}
