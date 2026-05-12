package objects

import "fmt"

// objectType is the root type that every other type inherits from.
// Mirrors PyBaseObject_Type.
//
// CPython: Objects/typeobject.c:L6299 PyBaseObject_Type
var objectType = func() *Type {
	t := &Type{Name: "object"}
	t.init(typeType)
	t.Bases = []*Type{}
	t.MRO = []*Type{t}
	return t
}()

// init wires object_new: bare object() returns a fresh instance.
// Mirrors CPython's tp_new on PyBaseObject_Type. Wired via init() to
// break the objectType -> NewInstance -> NewDict -> DictType cycle.
//
// CPython: Objects/typeobject.c:5021 object_new
func init() {
	objectType.TpNew = func(cls *Type, _ []Object, _ map[string]Object) (Object, error) {
		return NewInstance(cls), nil
	}
	// object.__init__ is a no-op accepting any args. Subclasses that
	// inherit __init__ from object reach this via super().__init__().
	//
	// CPython: Objects/typeobject.c:5097 object_init
	SetTypeDescr(objectType, "__init__", NewBuiltinFunction("__init__", func(_ []Object, _ map[string]Object) (Object, error) {
		return None(), nil
	}))
	// object.__class__ exposes the runtime type of self. Mirrors the
	// object_get_class entry in object_getsets.
	//
	// CPython: Objects/typeobject.c:5260 object_getsets
	SetTypeDescr(objectType, "__class__", NewGetSetDescr("__class__",
		func(o Object) (Object, error) { return o.Type(), nil },
		nil))
	// object.__dict__ exposes the per-instance __dict__ when the
	// subclass has one (HasDict). User classes without __slots__ pick
	// this up unchanged; __slots__ classes without __dict__ raise
	// AttributeError, matching CPython.
	//
	// CPython: Objects/typeobject.c:5260 object_getsets (subtype_dict)
	SetTypeDescr(objectType, "__dict__", NewGetSetDescr("__dict__",
		func(o Object) (Object, error) {
			if inst, ok := o.(*Instance); ok {
				if inst.dict == nil {
					return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
				}
				return inst.dict, nil
			}
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '__dict__'", o.Type().Name)
		},
		nil))

	// object.__getattribute__ / __setattr__ / __delattr__ are the slot
	// wrappers that expose tp_getattro / tp_setattro. dataclasses uses
	// `object.__setattr__(self, ...)` to bypass overridden __setattr__
	// on frozen instances, so they must be reachable via the type.
	//
	// CPython: Objects/typeobject.c:5260 object_methods +
	// Objects/typeobject.c slot_tp_getattr_hook / slot_tp_setattro
	SetTypeDescr(objectType, "__getattribute__", NewMethodDescr(objectType, "__getattribute__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
			}
			return GenericGetAttr(args[0], args[1])
		}))
	SetTypeDescr(objectType, "__setattr__", NewMethodDescr(objectType, "__setattr__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 3 {
				return nil, fmt.Errorf("TypeError: expected 3 arguments, got %d", len(args))
			}
			if err := GenericSetAttr(args[0], args[1], args[2]); err != nil {
				return nil, err
			}
			return None(), nil
		}))
	SetTypeDescr(objectType, "__delattr__", NewMethodDescr(objectType, "__delattr__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: expected 2 arguments, got %d", len(args))
			}
			if err := GenericSetAttr(args[0], args[1], nil); err != nil {
				return nil, err
			}
			return None(), nil
		}))
	// object comparison methods. __eq__ uses identity; __ne__ negates
	// __eq__; ordering ops return NotImplemented so subclasses can
	// define their own. Mirrors object_richcompare in CPython.
	//
	// CPython: Objects/typeobject.c:5161 object_richcompare
	SetTypeDescr(objectType, "__eq__", NewMethodDescr(objectType, "__eq__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return NotImplemented(), nil
			}
			return NewBool(args[0] == args[1]), nil
		}))
	SetTypeDescr(objectType, "__ne__", NewMethodDescr(objectType, "__ne__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 2 {
				return NotImplemented(), nil
			}
			return NewBool(args[0] != args[1]), nil
		}))
	SetTypeDescr(objectType, "__lt__", NewMethodDescr(objectType, "__lt__",
		func(args []Object, _ map[string]Object) (Object, error) {
			return NotImplemented(), nil
		}))
	SetTypeDescr(objectType, "__le__", NewMethodDescr(objectType, "__le__",
		func(args []Object, _ map[string]Object) (Object, error) {
			return NotImplemented(), nil
		}))
	SetTypeDescr(objectType, "__gt__", NewMethodDescr(objectType, "__gt__",
		func(args []Object, _ map[string]Object) (Object, error) {
			return NotImplemented(), nil
		}))
	SetTypeDescr(objectType, "__ge__", NewMethodDescr(objectType, "__ge__",
		func(args []Object, _ map[string]Object) (Object, error) {
			return NotImplemented(), nil
		}))
	// object.__init_subclass__ is the PEP 487 hook every subclass walks
	// up to via super(). The base implementation accepts no extra args
	// and returns None; subclasses that override it can opt-in to
	// keyword arguments. Marked as a classmethod via NewClassMethod so
	// `cls` is bound at lookup time.
	//
	// CPython: Objects/typeobject.c:5097 object___init_subclass__
	SetTypeDescr(objectType, "__init_subclass__", NewClassMethod(
		NewBuiltinFunction("__init_subclass__", func(args []Object, kwargs map[string]Object) (Object, error) {
			if len(args) > 1 {
				return nil, fmt.Errorf("TypeError: object.__init_subclass__() takes no arguments (%d given)", len(args)-1)
			}
			if len(kwargs) != 0 {
				return nil, fmt.Errorf("TypeError: object.__init_subclass__() takes no keyword arguments")
			}
			return None(), nil
		})))
}

// ObjectType returns the root `object` type singleton. Mirrors
// PyBaseObject_Type.
//
// CPython: Objects/typeobject.c:L6299 PyBaseObject_Type
func ObjectType() *Type {
	return objectType
}
