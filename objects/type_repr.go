// type_repr formats a type the way Python prints it: <class 'name'>
// for built-ins or types without a __module__, or
// <class 'module.name'> for user-defined types where the module is
// known. The formatter also short-circuits the "builtins" module
// because CPython hides it from the qualified form.
//
// CPython: Objects/typeobject.c:1268 type_repr

package objects

import "fmt"

func init() {
	typeType.Repr = typeRepr
	// type does not define its own tp_str: it inherits object_str, which
	// dispatches to the object's own tp_repr. That fallback is what lets a
	// metaclass override take effect for str(cls) too: when EnumType only
	// defines __repr__, str(SomeEnum) routes object.__str__ -> __repr__ and
	// prints "<enum 'Name'>" rather than the default "<class 'Name'>".
	// Giving typeType its own __str__ here would shadow that fallback.
	//
	// CPython: Objects/typeobject.c:6735 PyType_Type tp_str = 0 (inherits object_str)
	//
	// Register __repr__ as a descriptor so ABCMeta subclasses (whose
	// metaclass is a user-level Python class) inherit it via the
	// callDunderNoArgObject MRO walk.
	//
	// CPython: Objects/typeobject.c:8230 slotdefs TPSLOT __repr__
	SetTypeDescr(typeType, "__repr__", NewMethodDescr(typeType, "__repr__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'type' argument")
			}
			s, err := typeRepr(args[0])
			if err != nil {
				return nil, err
			}
			return NewStr(s), nil
		},
	))
}

// typeRepr is the tp_repr slot for Type. Mirrors type_repr's two
// branches: types with a module other than "builtins" report
// "<class 'module.qualname'>"; everything else falls back to the
// bare name.
//
// CPython: Objects/typeobject.c:1268 type_repr
func typeRepr(o Object) (string, error) {
	t, ok := o.(*Type)
	if !ok {
		return "", fmt.Errorf("TypeError: type_repr expects a type, got %s", typeNameOf(o))
	}
	mod := t.Module
	if mod == "" || mod == "builtins" {
		return "<class '" + t.Name + "'>", nil
	}
	// CPython formats the qualified branch with tp_qualname, not tp_name,
	// so nested/local classes render their full dotted __qualname__.
	qual := t.Name
	if t.IsUser && t.Qualname != "" {
		qual = t.Qualname
	}
	return "<class '" + mod + "." + qual + "'>", nil
}
