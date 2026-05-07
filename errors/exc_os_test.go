package errors_test

import (
	"syscall"
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

func TestErrnoSubclass(t *testing.T) {
	cases := []struct {
		errno int
		want  *objects.Type
	}{
		{int(syscall.ENOENT), errors.PyExc_FileNotFoundError},
		{int(syscall.EEXIST), errors.PyExc_FileExistsError},
		{int(syscall.EACCES), errors.PyExc_PermissionError},
		{int(syscall.EPERM), errors.PyExc_PermissionError},
		{int(syscall.EINTR), errors.PyExc_InterruptedError},
		{int(syscall.EPIPE), errors.PyExc_BrokenPipeError},
		{int(syscall.ECHILD), errors.PyExc_ChildProcessError},
		{int(syscall.EISDIR), errors.PyExc_IsADirectoryError},
		{int(syscall.ENOTDIR), errors.PyExc_NotADirectoryError},
		{int(syscall.ECONNREFUSED), errors.PyExc_ConnectionRefusedError},
		{int(syscall.ECONNRESET), errors.PyExc_ConnectionResetError},
		{int(syscall.ECONNABORTED), errors.PyExc_ConnectionAbortedError},
		{int(syscall.ESRCH), errors.PyExc_ProcessLookupError},
		{int(syscall.ETIMEDOUT), errors.PyExc_TimeoutError},
		{0, errors.PyExc_OSError},
		{99999, errors.PyExc_OSError},
	}
	for _, c := range cases {
		if got := errors.ErrnoSubclass(c.errno); got != c.want {
			t.Errorf("ErrnoSubclass(%d) = %v, want %v", c.errno, got, c.want)
		}
	}
}
