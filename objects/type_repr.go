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
	typeType.Str = typeRepr
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
	return "<class '" + mod + "." + t.Name + "'>", nil
}
