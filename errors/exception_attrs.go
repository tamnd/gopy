package errors

import (
	"errors"

	"github.com/tamnd/gopy/objects"
)

// Hook BaseException up to GenericGetAttr / GenericSetAttr so the
// descriptors registered below get picked up on attribute lookup.
// Subclass types inherit the descriptors through the MRO walk done
// inside LookupDescriptor.
//
// CPython: Objects/exceptions.c:508 BaseException_getset
// CPython: Objects/exceptions.c:605 BaseException_members
func init() {
	t := PyExc_BaseException
	t.Getattro = objects.GenericGetAttr
	t.Setattro = objects.GenericSetAttr

	objects.SetTypeDescr(t, "args", objects.NewGetSetDescr("args", argsGet, argsSet))
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
