package objects

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
}

// ObjectType returns the root `object` type singleton. Mirrors
// PyBaseObject_Type.
//
// CPython: Objects/typeobject.c:L6299 PyBaseObject_Type
func ObjectType() *Type {
	return objectType
}
