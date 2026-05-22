package errors_test

import (
	"testing"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/traceback"
)

// TestExceptionWithTraceback covers the with_traceback method: writes
// tb into self.__traceback__ and returns self.
//
// CPython: Objects/exceptions.c:279 BaseException_with_traceback_impl
func TestExceptionWithTraceback(t *testing.T) {
	exc := errors.New(errors.PyExc_ValueError, objects.NewTuple([]objects.Object{objects.NewStr("boom")}))
	tb := &traceback.Traceback{}
	method, err := objects.GetAttr(exc, objects.NewStr("with_traceback"))
	if err != nil {
		t.Fatalf("getattr with_traceback: %v", err)
	}
	out, err := objects.Call(method, objects.NewTuple([]objects.Object{tb}), nil)
	if err != nil {
		t.Fatalf("call with_traceback: %v", err)
	}
	if out != exc {
		t.Fatalf("with_traceback returned %v, want self", out)
	}
	if exc.TB != tb {
		t.Fatalf("self.__traceback__ = %v, want tb", exc.TB)
	}
}

// TestExceptionSetState iterates a dict and stores every entry on the
// exception via setattr. None is a no-op.
//
// CPython: Objects/exceptions.c:243 BaseException___setstate___impl
func TestExceptionSetState(t *testing.T) {
	exc := errors.New(errors.PyExc_RuntimeError, objects.NewTuple([]objects.Object{objects.NewStr("x")}))
	state := objects.NewDict()
	_ = state.SetItem(objects.NewStr("foo"), objects.NewInt(7))
	_ = state.SetItem(objects.NewStr("bar"), objects.NewStr("baz"))
	method, err := objects.GetAttr(exc, objects.NewStr("__setstate__"))
	if err != nil {
		t.Fatalf("getattr __setstate__: %v", err)
	}
	if _, err := objects.Call(method, objects.NewTuple([]objects.Object{state}), nil); err != nil {
		t.Fatalf("call __setstate__: %v", err)
	}
	for k, want := range map[string]string{"foo": "7", "bar": "baz"} {
		got, err := objects.GetAttr(exc, objects.NewStr(k))
		if err != nil {
			t.Fatalf("getattr %s: %v", k, err)
		}
		gs, _ := objects.Str(got)
		if gs != want {
			t.Fatalf("%s = %q, want %q", k, gs, want)
		}
	}
	// None payload is a no-op (no attributes touched).
	if _, err := objects.Call(method, objects.NewTuple([]objects.Object{objects.None()}), nil); err != nil {
		t.Fatalf("call __setstate__(None): %v", err)
	}
}
