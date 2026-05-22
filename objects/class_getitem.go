// Class-getitem wiring for built-in generic-capable types. CPython
// generates these slots from each type's tp_methods table (e.g.,
// list_methods has "__class_getitem__" pointing at the generic
// classmethod helper in genericaliasobject.c). gopy wires them
// explicitly so list[int], dict[str, int], tuple[int, ...], set[int],
// frozenset[int], and type[int] all produce a GenericAlias.
//
// `type | None` and friends are wired via NumberMethods.Or on typeType
// so the BINARY_OP NB_OR dispatcher reaches unionTypeOr.
//
// CPython: Objects/genericaliasobject.c:1040 Py_GenericAlias
// CPython: Objects/unionobject.c:257 _Py_union_type_or

package objects

import "fmt"

func init() {
	// __class_getitem__ on the canonical "generic-capable" builtins.
	// Each call wraps a one-arg builtin in a ClassMethod so attribute
	// access binds the class as the implicit first argument; the
	// typeSubscript fast path in vm/eval_simple.go calls these
	// directly, but Python-level `list.__class_getitem__(int)` still
	// works through the descriptor.
	//
	// CPython: Objects/listobject.c:3308 list_methods (entry for
	// "__class_getitem__")
	bindClassGetitem(ListType)
	bindClassGetitem(DictType)
	bindClassGetitem(TupleType)
	bindClassGetitem(SetType)
	bindClassGetitem(FrozensetType)
	bindClassGetitem(typeType)
	// typing.Generic[T] subscription. Same shape as the others: a
	// classmethod that builds a GenericAlias whose origin is the
	// Generic singleton.
	//
	// CPython: Objects/typevarobject.c:2459 generic_class_getitem
	bindClassGetitem(GenericType)

	// `int | str` reaches the binary-op machinery, which walks each
	// operand's NumberMethods.Or. Wiring it on typeType lets every
	// class (since classes are instances of typeType) participate.
	//
	// CPython: Objects/typeobject.c:7045 type_or
	typeType.Number = &NumberMethods{
		Or: unionTypeOr,
	}
}

// bindClassGetitem installs a `__class_getitem__` classmethod on t
// that wraps a one-arg call into NewGenericAlias(t, arg).
//
// CPython: Objects/genericaliasobject.c:1040 Py_GenericAlias
func bindClassGetitem(t *Type) {
	fn := NewBuiltinFunction("__class_getitem__", func(args []Object, _ map[string]Object) (Object, error) {
		// classmethod binding prepends the class as args[0]; the
		// user-visible call is __class_getitem__(item), so we expect
		// exactly two positional values.
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __class_getitem__() takes exactly one argument (%d given)", len(args)-1)
		}
		cls, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: __class_getitem__() requires a type, got %s", typeNameOf(args[0]))
		}
		return NewGenericAlias(cls, args[1]), nil
	})
	SetTypeDescr(t, "__class_getitem__", NewClassMethod(fn))
}
