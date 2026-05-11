package errors

import (
	"errors"

	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/traceback"
)

// Hook BaseException up to GenericGetAttr / GenericSetAttr so the
// descriptors registered below get picked up on attribute lookup.
// Subclass types inherit the descriptors through the MRO walk done
// inside LookupDescriptor.
//
// CPython: Objects/exceptions.c:508 BaseException_getset
// CPython: Objects/exceptions.c:605 BaseException_members
func installBaseExceptionAttrs() {
	t := PyExc_BaseException
	t.Getattro = objects.GenericGetAttr
	t.Setattro = objects.GenericSetAttr

	objects.SetTypeDescr(t, "args", objects.NewGetSetDescr("args", argsGet, argsSet))
	objects.SetTypeDescr(t, "__traceback__", objects.NewGetSetDescr("__traceback__", tracebackGet, tracebackSet))
	objects.SetTypeDescr(t, "__context__", objects.NewGetSetDescr("__context__", contextGet, contextSet))
	objects.SetTypeDescr(t, "__cause__", objects.NewGetSetDescr("__cause__", causeGet, causeSet))
	objects.SetTypeDescr(t, "__suppress_context__", objects.NewGetSetDescr("__suppress_context__", suppressGet, suppressSet))
}

// argsGet returns self->args, or None when args is unset. CPython
// stores an empty tuple at construction time so the None branch is
// dead in practice; mirror the upstream check anyway.
//
// CPython: Objects/exceptions.c:343 BaseException_args_get_impl
func argsGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	if e.Args == nil {
		return objects.None(), nil
	}
	return e.Args, nil
}

// argsSet runs PySequence_Tuple(value) and stores the result.
//
// CPython: Objects/exceptions.c:359 BaseException_args_set_impl
func argsSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		return errors.New("TypeError: args may not be deleted")
	}
	if t, ok := value.(*objects.Tuple); ok {
		e.Args = t
		return nil
	}
	iter, err := objects.Iter(value)
	if err != nil {
		return err
	}
	var items []objects.Object
	for {
		next, err := objects.IterNext(iter)
		if err != nil {
			return err
		}
		if next == nil {
			break
		}
		items = append(items, next)
	}
	e.Args = objects.NewTuple(items)
	return nil
}

// tracebackGet returns self->traceback, or None when unset.
//
// CPython: Objects/exceptions.c:381 BaseException___traceback___get_impl
func tracebackGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	if e.TB == nil {
		return objects.None(), nil
	}
	return e.TB, nil
}

// tracebackSet accepts a Traceback instance or None. Rejects delete
// and any other type.
//
// CPython: Objects/exceptions.c:397 BaseException___traceback___set_impl
func tracebackSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		return errors.New("TypeError: __traceback__ may not be deleted")
	}
	if objects.IsNone(value) {
		e.TB = nil
		return nil
	}
	tb, ok := value.(*traceback.Traceback)
	if !ok {
		return errors.New("TypeError: __traceback__ must be a traceback or None")
	}
	e.TB = tb
	return nil
}

// contextGet returns self->context, or None when unset.
//
// CPython: Objects/exceptions.c:427 BaseException___context___get_impl
func contextGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	if e.Context == nil {
		return objects.None(), nil
	}
	return e.Context, nil
}

// contextSet accepts an exception instance or None.
//
// CPython: Objects/exceptions.c:443 BaseException___context___set_impl
func contextSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		return errors.New("TypeError: __context__ may not be deleted")
	}
	if objects.IsNone(value) {
		e.Context = nil
		return nil
	}
	exc, ok := value.(*Exception)
	if !ok {
		return errors.New("TypeError: exception context must be None or derive from BaseException")
	}
	e.Context = exc
	return nil
}

// causeGet returns self->cause, or None when unset.
//
// CPython: Objects/exceptions.c:470 BaseException___cause___get_impl
func causeGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	if e.Cause == nil {
		return objects.None(), nil
	}
	return e.Cause, nil
}

// causeSet accepts an exception instance or None. Setting cause also
// flips suppress_context to True, matching PyException_SetCause.
//
// CPython: Objects/exceptions.c:486 BaseException___cause___set_impl
// CPython: Objects/exceptions.c:555 PyException_SetCause
func causeSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		return errors.New("TypeError: __cause__ may not be deleted")
	}
	if objects.IsNone(value) {
		e.Cause = nil
	} else {
		exc, ok := value.(*Exception)
		if !ok {
			return errors.New("TypeError: exception cause must be None or derive from BaseException")
		}
		e.Cause = exc
	}
	e.Suppress = true
	return nil
}

// suppressGet exposes the __suppress_context__ bool member.
//
// CPython: Objects/exceptions.c:605 BaseException_members
func suppressGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	return objects.NewBool(e.Suppress), nil
}

// suppressSet accepts any truthy/falsy value, matching Py_T_BOOL
// semantics (CPython converts via PyObject_IsTrue when writing).
//
// CPython: Objects/exceptions.c:605 BaseException_members
func suppressSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		return errors.New("TypeError: __suppress_context__ may not be deleted")
	}
	e.Suppress = objects.IsTrue(value)
	return nil
}
