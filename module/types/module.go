// _types built-in module. Re-exports the canonical built-in type
// singletons (function, mappingproxy, simplenamespace, ...) so the
// vendored Lib/types.py can run its `try: from _types import *`
// fast path instead of falling through to the introspection-based
// fallback. The fallback path depends on `_f.__code__`, `type.__dict__`,
// and `sys.implementation` working end-to-end, which is more runtime
// surface than gopy ships today; this module keeps types.py upstream-
// byte-equal by feeding it the type objects directly.
//
// CPython: Modules/_typesmodule.c:9 _types_exec
// CPython: Modules/Setup.bootstrap.in:28 _types _typesmodule.c

package types

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/traceback"
)

func init() {
	_ = imp.AppendInittab("_types", buildModule)
}

// buildModule lays out the export table in the same order as
// _typesmodule.c so a future audit can diff them line-by-line.
//
// CPython: Modules/_typesmodule.c:20 EXPORT_STATIC_TYPE list
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_types")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		// CPython: Modules/_typesmodule.c:20 AsyncGeneratorType
		{"AsyncGeneratorType", objects.AsyncGeneratorType},
		// CPython: Modules/_typesmodule.c:21 BuiltinFunctionType
		{"BuiltinFunctionType", objects.BuiltinFunctionType},
		// CPython: Modules/_typesmodule.c:23 BuiltinMethodType (aliased)
		{"BuiltinMethodType", objects.BuiltinFunctionType},
		// CPython: Modules/_typesmodule.c:24 CapsuleType
		{"CapsuleType", objects.CapsuleType},
		// CPython: Modules/_typesmodule.c:25 CellType
		{"CellType", objects.CellType},
		// CPython: Modules/_typesmodule.c:27 CodeType
		{"CodeType", objects.CodeType},
		// CPython: Modules/_typesmodule.c:28 CoroutineType
		{"CoroutineType", objects.CoroutineType},
		// CPython: Modules/_typesmodule.c:29 EllipsisType
		{"EllipsisType", objects.Ellipsis().Type()},
		// CPython: Modules/_typesmodule.c:30 FrameType
		{"FrameType", objects.FrameType()},
		// CPython: Modules/_typesmodule.c:31 FunctionType
		{"FunctionType", objects.FunctionType},
		// CPython: Modules/_typesmodule.c:32 GeneratorType
		{"GeneratorType", objects.GeneratorType},
		// CPython: Modules/_typesmodule.c:34 GetSetDescriptorType
		{"GetSetDescriptorType", objects.GetSetDescrType},
		// CPython: Modules/_typesmodule.c:36 LambdaType (aliased)
		{"LambdaType", objects.FunctionType},
		// CPython: Modules/_typesmodule.c:37 MappingProxyType
		{"MappingProxyType", objects.MappingProxyType},
		// CPython: Modules/_typesmodule.c:38 MemberDescriptorType
		{"MemberDescriptorType", objects.MemberDescrType},
		// CPython: Modules/_typesmodule.c:39 MethodDescriptorType
		{"MethodDescriptorType", objects.MethodDescrType},
		// CPython: Modules/_typesmodule.c:40 MethodType
		{"MethodType", objects.BoundMethodType},
		// gopy collapses wrapper_descriptor into method_descriptor (no
		// separate slot-wrapper descriptor type yet), so the alias here
		// keeps `isinstance(object.__init__, types.WrapperDescriptorType)`
		// observable for inspect.py and friends.
		//
		// CPython: Modules/_typesmodule.c WrapperDescriptorType (PyWrapperDescr_Type)
		{"WrapperDescriptorType", objects.MethodDescrType},
		// Same collapse for method-wrapper: bound slot wrappers fall
		// through to BoundMethodType in gopy.
		//
		// CPython: Modules/_typesmodule.c MethodWrapperType (_PyMethodWrapper_Type)
		{"MethodWrapperType", objects.BoundMethodType},
		// classmethod_descriptor is the type of dict.__dict__['fromkeys'],
		// distinct from the classmethod wrapper produced by @classmethod.
		//
		// CPython: Modules/_typesmodule.c ClassMethodDescriptorType (PyClassMethodDescr_Type)
		{"ClassMethodDescriptorType", objects.ClassMethodDescrType},
		// CPython: Modules/_typesmodule.c:42 ModuleType
		{"ModuleType", objects.ModuleType},
		// CPython: Modules/_typesmodule.c:43 NoneType
		{"NoneType", objects.None().Type()},
		// CPython: Modules/_typesmodule.c:44 NotImplementedType
		{"NotImplementedType", objects.NotImplemented().Type()},
		// CPython: Modules/_typesmodule.c:45 SimpleNamespace
		{"SimpleNamespace", objects.NamespaceType},
		// CPython: Modules/_typesmodule.c:46 TracebackType
		{"TracebackType", traceback.Type},
		// CPython: Modules/_typesmodule.c:33 GenericAlias
		{"GenericAlias", objects.GenericAliasType},
		// CPython: Modules/_typesmodule.c:48 UnionType
		{"UnionType", objects.UnionTypeType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}
