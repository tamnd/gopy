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
func init() {
	t := PyExc_BaseException

	objects.SetTypeDescr(t, "args", objects.NewGetSetDescr("args", argsGet, argsSet))
	objects.SetTypeDescr(t, "__traceback__", objects.NewGetSetDescr("__traceback__", tracebackGet, tracebackSet))
	objects.SetTypeDescr(t, "__context__", objects.NewGetSetDescr("__context__", contextGet, contextSet))
	objects.SetTypeDescr(t, "__cause__", objects.NewGetSetDescr("__cause__", causeGet, causeSet))
	objects.SetTypeDescr(t, "__suppress_context__", objects.NewGetSetDescr("__suppress_context__", suppressGet, suppressSet))
	objects.SetTypeDescr(t, "__init__", objects.NewMethodDescr(t, "__init__", baseExceptionInit))
	objects.SetTypeDescr(t, "add_note", objects.NewMethodDescr(t, "add_note", baseExceptionAddNote))
	objects.SetTypeDescr(t, "__notes__", objects.NewGetSetDescr("__notes__", notesGet, notesSet))
	objects.SetTypeDescr(t, "with_traceback", objects.NewMethodDescr(t, "with_traceback", baseExceptionWithTraceback))
	objects.SetTypeDescr(t, "__setstate__", objects.NewMethodDescr(t, "__setstate__", baseExceptionSetState))
	objects.SetTypeDescr(t, "__reduce__", objects.NewMethodDescr(t, "__reduce__", baseExceptionReduce))
}

// baseExceptionWithTraceback writes tb into self.__traceback__ and
// returns self. CPython's clinic wrapper calls
// BaseException___traceback___set_impl, which is exactly tracebackSet
// in gopy.
//
// CPython: Objects/exceptions.c:279 BaseException_with_traceback_impl
func baseExceptionWithTraceback(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: with_traceback() takes exactly one argument")
	}
	self := args[0]
	if _, ok := self.(*Exception); !ok {
		return nil, errors.New("TypeError: with_traceback() requires a BaseException")
	}
	if err := tracebackSet(self, args[1]); err != nil {
		return nil, err
	}
	return self, nil
}

// baseExceptionSetState restores per-instance attributes from a pickled
// state dict. CPython iterates state as a dict and calls
// PyObject_SetAttr(self, key, value) for every entry. None is a no-op.
//
// CPython: Objects/exceptions.c:243 BaseException___setstate___impl
func baseExceptionSetState(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: __setstate__() takes exactly one argument")
	}
	self := args[0]
	state := args[1]
	if objects.IsNone(state) {
		return objects.None(), nil
	}
	d, ok := state.(*objects.Dict)
	if !ok {
		return nil, errors.New("TypeError: state is not a dictionary")
	}
	for _, k := range d.Keys() {
		v, err := d.GetItem(k)
		if err != nil {
			return nil, err
		}
		if err := objects.SetAttr(self, k, v); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// baseExceptionReduce implements BaseException.__reduce__: returns
// (type, args) or (type, args, dict) when an instance dict is present.
// This ensures pickle.loads calls cls(*args) which re-runs __init__
// and restores Python-level attributes like .message on subclasses.
//
// CPython: Objects/exceptions.c:220 BaseException___reduce___impl
func baseExceptionReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return nil, errors.New("TypeError: __reduce__() requires a self argument")
	}
	e, ok := args[0].(*Exception)
	if !ok {
		return nil, errors.New("TypeError: __reduce__() requires a BaseException")
	}
	tp := objects.Object(e.Type())
	eArgs := objects.Object(objects.NewTuple(nil))
	if e.Args != nil {
		eArgs = e.Args
	}
	d := e.AttrDict()
	if d != nil && d.Len() > 0 {
		return objects.NewTuple([]objects.Object{tp, eArgs, d}), nil
	}
	return objects.NewTuple([]objects.Object{tp, eArgs}), nil
}

// baseExceptionAddNote appends a string note to self.__notes__,
// allocating the list on first call. Mirrors BaseException.add_note.
//
// CPython: Objects/exceptions.c:298 BaseException_add_note_impl
func baseExceptionAddNote(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: add_note() takes exactly one argument")
	}
	e, ok := args[0].(*Exception)
	if !ok {
		return nil, errors.New("TypeError: add_note() requires a BaseException")
	}
	if args[1] == nil || args[1].Type() != objects.StrType() {
		return nil, errors.New("TypeError: note must be a str")
	}
	if e.Notes == nil {
		e.Notes = objects.NewList(nil)
	}
	e.Notes.Append(args[1])
	return objects.None(), nil
}

// notesGet returns self.__notes__, or raises AttributeError when unset
// (matching CPython's PyObject_GetOptionalAttr semantics through the
// generic getattr path).
//
// CPython: Objects/exceptions.c:298 BaseException_add_note_impl (read path)
func notesGet(owner objects.Object) (objects.Object, error) {
	e := owner.(*Exception)
	if e.Notes == nil {
		return nil, errors.New("AttributeError: __notes__")
	}
	return e.Notes, nil
}

// notesSet stores __notes__. CPython lets users assign any object; the
// list-only check fires only inside add_note when reading back.
//
// CPython: Objects/exceptions.c:298 BaseException_add_note_impl (write path)
func notesSet(owner objects.Object, value objects.Object) error {
	e := owner.(*Exception)
	if value == nil {
		e.Notes = nil
		return nil
	}
	lst, ok := value.(*objects.List)
	if !ok {
		return errors.New("TypeError: __notes__ must be a list")
	}
	e.Notes = lst
	return nil
}

// baseExceptionInit is the tp_init slot for BaseException. CPython
// stores positional args on self.args and ignores kwargs. The slot
// matters when user subclasses call super().__init__(*args): without
// it the MRO walk falls through to object.__init__, which rejects
// extra args.
//
// CPython: Objects/exceptions.c:L84 BaseException_init
func baseExceptionInit(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) == 0 {
		return objects.None(), nil
	}
	e, ok := args[0].(*Exception)
	if !ok {
		return objects.None(), nil
	}
	e.Args = objects.NewTuple(args[1:])
	return objects.None(), nil
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
		e.ContextSet = true // user explicitly set to None; block implicit chaining
		return nil
	}
	exc, ok := value.(*Exception)
	if !ok {
		return errors.New("TypeError: exception context must be None or derive from BaseException")
	}
	e.Context = exc
	e.ContextSet = true
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
