// Package typingmod is the gopy port of CPython's _typing builtin
// (Modules/_typingmodule.c). The module is small: it re-exports the
// TypeVar / ParamSpec / TypeVarTuple / Generic / Union / TypeAliasType
// types living in interp->cached_objects so Lib/typing.py can
// `from _typing import (...)` them, plus the _idfunc identity
// accelerator and the NoDefault sentinel.
//
// CPython: Modules/_typingmodule.c:1 _typing module
package typingmod

import (
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_typing", buildModule)
}

// idfunc is the _typing._idfunc accelerator: Py_NewRef(x). typing.py
// uses it as the default __call__ for NewType instances so subscripting
// stays cheap.
//
// CPython: Modules/_typingmodule.c:30 _typing__idfunc
func idfunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _idfunc() takes exactly 1 argument (%d given)", len(args))
	}
	return args[0], nil
}

// setUnionTypeCheck wires typing._type_check into the union builder so
// that non-directly-unionable objects (ForwardRef, strings, TypeVar,
// _GenericAlias instances) are accepted by types.UnionType[...].
// typing.py calls this after defining _type_check.
//
// CPython: Objects/unionobject.c:478 call_typing_func_object("_type_check")
func setUnionTypeCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _set_union_type_check() takes exactly 1 argument (%d given)", len(args))
	}
	fn := args[0]
	objects.UnionTypeCheckHook = func(arg objects.Object, msg string) (objects.Object, error) {
		result, err := objects.Call(fn, objects.NewTuple([]objects.Object{arg, objects.NewStr(msg)}), nil)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return objects.None(), nil
}

// setGenericClassGetitem wires a Python callable as the hook for
// Generic.__class_getitem__ so that string arguments go through
// _type_convert -> _make_forward_ref -> eager SyntaxError validation.
// typing.py calls this after defining _generic_class_getitem.
//
// CPython: Objects/typevarobject.c:2459 generic_class_getitem
func setGenericClassGetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: _set_generic_class_getitem() takes exactly 1 argument (%d given)", len(args))
	}
	fn := args[0]
	objects.GenericClassGetitemHook = func(cls objects.Object, arg objects.Object) (objects.Object, error) {
		return objects.Call(fn, objects.NewTuple([]objects.Object{cls, arg}), nil)
	}
	return objects.None(), nil
}

// buildModule materializes the _typing module dict. Mirrors
// _typing_exec.
//
// CPython: Modules/_typingmodule.c:46 _typing_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_typing")
	dd := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"TypeVar", objects.TypeVarType},
		{"TypeVarTuple", objects.TypeVarTupleType},
		{"ParamSpec", objects.ParamSpecType},
		{"ParamSpecArgs", objects.ParamSpecArgsType},
		{"ParamSpecKwargs", objects.ParamSpecKwargsType},
		{"Generic", objects.GenericType},
		{"TypeAliasType", objects.TypeAliasObjType},
		{"Union", objects.UnionTypeType},
		{"NoDefault", objects.NoDefault()},
	}
	for _, e := range entries {
		if err := dd.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}

	if err := dd.SetItem(objects.NewStr("_idfunc"), objects.NewBuiltinFunction("_idfunc", idfunc)); err != nil {
		return nil, err
	}
	if err := dd.SetItem(objects.NewStr("_set_union_type_check"), objects.NewBuiltinFunction("_set_union_type_check", setUnionTypeCheck)); err != nil {
		return nil, err
	}
	if err := dd.SetItem(objects.NewStr("_set_generic_class_getitem"), objects.NewBuiltinFunction("_set_generic_class_getitem", setGenericClassGetitem)); err != nil {
		return nil, err
	}
	return m, nil
}
