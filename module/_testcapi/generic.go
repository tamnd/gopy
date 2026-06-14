package testcapi

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// genericAliasObject backs _testcapi.GenericAlias: a minimal C-level type
// holding one item, whose __mro_entries__ resolves to a 1-tuple of that
// item. test_genericclass.CAPITest drives it to check that a C type's
// __class_getitem__ plus __mro_entries__ feed class-statement bases.
//
// CPython: Modules/_testcapimodule.c:2960 PyGenericAliasObject
type genericAliasObject struct {
	objects.Header
	item objects.Object
}

// genericAliasType / genericType are the type singletons.
//
// CPython: Modules/_testcapimodule.c:2984 GenericAlias_Type,
//
//	Modules/_testcapimodule.c:3020 Generic_Type
var (
	genericAliasType *objects.Type
	genericType      *objects.Type
)

// genericAliasNew constructs a GenericAlias holding item, mirroring
// generic_alias_new.
//
// CPython: Modules/_testcapimodule.c:2994 generic_alias_new
func genericAliasNew(item objects.Object) objects.Object {
	objects.Incref(item)
	o := &genericAliasObject{item: item}
	o.Init(genericAliasType)
	return o
}

// genericAliasMroEntries ports generic_alias_mro_entries: PyTuple_Pack(1,
// item).
//
// CPython: Modules/_testcapimodule.c:2972 generic_alias_mro_entries
func genericAliasMroEntries(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __mro_entries__() missing self argument")
	}
	self, ok := args[0].(*genericAliasObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: __mro_entries__ requires a GenericAlias, not %s", args[0].Type().Name)
	}
	return objects.NewTuple([]objects.Object{self.item}), nil
}

// genericAliasTraverse keeps the held item reachable for the collector.
//
// CPython: Modules/_testcapimodule.c:2960 PyGenericAliasObject (item)
func genericAliasTraverse(o objects.Object, visit objects.Visitor) error {
	self, ok := o.(*genericAliasObject)
	if !ok || self.item == nil {
		return nil
	}
	return visit(self.item)
}

// genericClassGetitem ports generic_class_getitem: a METH_O|METH_CLASS
// method returning a fresh GenericAlias wrapping the subscript.
//
// CPython: Modules/_testcapimodule.c:3011 generic_class_getitem
func genericClassGetitem(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __class_getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	return genericAliasNew(args[1]), nil
}

func init() {
	genericAliasType = objects.NewType("GenericAlias", []*objects.Type{objects.ObjectType()})
	genericAliasType.TpFlags |= objects.TpFlagBasetype
	genericAliasType.TpTraverse = genericAliasTraverse
	objects.SetTypeDescr(genericAliasType, "__mro_entries__",
		objects.NewMethodDescr(genericAliasType, "__mro_entries__", genericAliasMroEntries))

	genericType = objects.NewType("Generic", []*objects.Type{objects.ObjectType()})
	genericType.TpFlags |= objects.TpFlagBasetype
	genericType.TpNew = objects.TypeGenericNew
	cg := objects.NewBuiltinFunction("__class_getitem__", genericClassGetitem)
	objects.SetTypeDescr(genericType, "__class_getitem__", objects.NewClassMethod(cg))
}
