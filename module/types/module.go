// types module: minimal Go-backed surface. Lib/types.py is mostly
// `type(x) for x in some_canonical_sample` — a runtime-introspection
// table of the canonical Python types. unittest's loader and case
// poke at FunctionType, ModuleType, MethodType, GenericAlias, and
// MappingProxyType. Until the real Lib/types port lands, expose the
// existing gopy type singletons under the same names.
//
// CPython: Lib/types.py the public surface

package types

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("types", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("types")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"FunctionType", objects.FunctionType},
		{"BuiltinFunctionType", objects.BuiltinFunctionType},
		{"BuiltinMethodType", objects.BuiltinFunctionType},
		{"MethodType", objects.BoundMethodType},
		{"ModuleType", objects.ModuleType},
		{"GenericAlias", genericAliasType},
		{"MappingProxyType", mappingProxyType},
		{"SimpleNamespace", simpleNamespaceType},
		{"NoneType", objects.None().Type()},
		{"NotImplementedType", objects.NotImplemented().Type()},
		{"EllipsisType", objects.Ellipsis().Type()},
		{"TracebackType", tracebackTypeStub},
		{"FrameType", frameTypeStub},
		{"CodeType", objects.CodeType},
		{"CellType", cellTypeStub},
		{"GeneratorType", generatorTypeStub},
		{"CoroutineType", coroutineTypeStub},
		{"AsyncGeneratorType", asyncGeneratorTypeStub},
		{"new_class", objects.NewBuiltinFunction("new_class", newClass)},
		{"prepare_class", objects.NewBuiltinFunction("prepare_class", prepareClass)},
		{"resolve_bases", objects.NewBuiltinFunction("resolve_bases", resolveBases)},
		{"DynamicClassAttribute", dynamicClassAttrType},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	genericAliasType       = objects.NewType("GenericAlias", []*objects.Type{objects.ObjectType()})
	mappingProxyType       = objects.NewType("mappingproxy", []*objects.Type{objects.ObjectType()})
	simpleNamespaceType    = newSimpleNamespaceType()
	tracebackTypeStub      = objects.NewType("traceback", []*objects.Type{objects.ObjectType()})
	frameTypeStub          = objects.NewType("frame", []*objects.Type{objects.ObjectType()})
	cellTypeStub           = objects.NewType("cell", []*objects.Type{objects.ObjectType()})
	generatorTypeStub      = objects.NewType("generator", []*objects.Type{objects.ObjectType()})
	coroutineTypeStub      = objects.NewType("coroutine", []*objects.Type{objects.ObjectType()})
	asyncGeneratorTypeStub = objects.NewType("async_generator", []*objects.Type{objects.ObjectType()})
	dynamicClassAttrType   = objects.NewType("DynamicClassAttribute", []*objects.Type{objects.ObjectType()})
)

func newSimpleNamespaceType() *objects.Type {
	t := objects.NewType("SimpleNamespace", []*objects.Type{objects.ObjectType()})
	t.HasDict = true
	t.Getattro = objects.GenericGetAttr
	t.TpNew = func(cls *objects.Type, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		_ = args
		inst := objects.NewInstance(cls)
		for k, v := range kwargs {
			_ = inst.Dict().SetItem(objects.NewStr(k), v)
		}
		return inst, nil
	}
	return t
}

func newClass(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.None(), nil
}

func prepareClass(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewTuple([]objects.Object{
		objects.TypeType(),
		objects.NewDict(),
		objects.NewDict(),
	}), nil
}

func resolveBases(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) >= 1 {
		return args[0], nil
	}
	return objects.NewTuple(nil), nil
}
