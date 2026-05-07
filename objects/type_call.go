// type.__call__: this is the slot CPython invokes when source like
// `MyClass(args)` runs. The `type` singleton's call splits two ways:
// type(x) returns the type of x, type(name, bases, dict) builds a new
// class. Calls on any other type allocate an instance and run
// __init__.
//
// The v0.10.1 cut covers the user-class path (NewInstance + __init__);
// built-in types still go through their dedicated constructor wrappers
// in builtins/, so wiring this slot does not change their behavior.
//
// CPython: Objects/typeobject.c:1748 type_call

package objects

import "fmt"

func init() {
	typeType.Call = typeCall
}

// typeCall is the tp_call slot for `type` itself (and, by inheritance,
// every class). Behavior:
//
//   - When the receiver is the `type` singleton:
//
//   - type(x)               -> Py_TYPE(x)
//
//   - type(name, bases, ns) -> NewUserType(name, bases, ns)
//
//   - When the receiver is a user-defined class (IsUser==true): call
//     __new__ if present, otherwise allocate an Instance, then run
//     __init__ with the args.
//
//   - Built-in types reach the same slot but are not callable through
//     it: they are constructed via their named builtin (int(), list(),
//     ...) which routes through builtins/ctor.go.
//
// CPython: Objects/typeobject.c:1748 type_call
func typeCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	cls, ok := callable.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: type.__call__ on non-type object")
	}

	if cls == typeType {
		return typeMetaCall(args, kwargs)
	}

	// Built-in types that publish their own constructor (super, ...) get
	// dispatched through that slot directly. Mirrors the way CPython's
	// PyType_Type tp_call routes through the subtype's tp_new/tp_init.
	if cls.Call != nil {
		return cls.Call(callable, args, kwargs)
	}

	if !cls.IsUser {
		return nil, fmt.Errorf("TypeError: cannot create '%s' instances directly", cls.Name)
	}

	inst := NewInstance(cls)
	if init, _ := LookupDescriptor(cls, "__init__"); init != nil {
		bound := bindDescr(init, inst, cls)
		if _, err := callBound(bound, args, kwargs); err != nil {
			return nil, err
		}
	}
	return inst, nil
}

// typeMetaCall handles the two `type(...)` forms.
//
// CPython: Objects/typeobject.c:1817 type_call (the type==&PyType_Type branch)
func typeMetaCall(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) == 1 && len(kwargs) == 0 {
		return args[0].Type(), nil
	}
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: type() takes 1 or 3 arguments")
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 1 must be str, not %s", typeNameOf(args[0]))
	}
	basesT, ok := args[1].(*Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 2 must be tuple, not %s", typeNameOf(args[1]))
	}
	ns, ok := args[2].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: type() argument 3 must be dict, not %s", typeNameOf(args[2]))
	}
	bases := make([]*Type, 0, basesT.Len())
	for i := 0; i < basesT.Len(); i++ {
		t, ok := basesT.Item(i).(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: type() bases must contain types, got %s", typeNameOf(basesT.Item(i)))
		}
		bases = append(bases, t)
	}
	return NewUserType(nameObj.v, bases, ns), nil
}

// bindDescr applies the descriptor protocol to descr if its type
// implements DescrGet, otherwise returns descr unchanged.
func bindDescr(descr Object, owner Object, ownerType *Type) Object {
	dt := descr.Type()
	if dt.DescrGet == nil {
		return descr
	}
	v, err := dt.DescrGet(descr, owner, ownerType)
	if err != nil {
		return descr
	}
	return v
}

// callBound invokes a bound callable with positional args and kwargs.
func callBound(fn Object, args []Object, kwargs map[string]Object) (Object, error) {
	var kwd *Dict
	if len(kwargs) > 0 {
		kwd = NewDict()
		for k, v := range kwargs {
			if err := kwd.SetItem(NewStr(k), v); err != nil {
				return nil, err
			}
		}
	}
	return Call(fn, NewTuple(args), kwd)
}
