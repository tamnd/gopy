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

// GenericClassGetitemHook is called by the Generic.__class_getitem__ built-in
// when the hook is set. typing.py wires _generic_class_getitem here via
// _set_generic_class_getitem so that string arguments flow through
// _type_convert -> _make_forward_ref -> eager SyntaxError validation.
//
// CPython: Objects/typevarobject.c:2459 generic_class_getitem
var GenericClassGetitemHook func(cls Object, args Object) (Object, error)

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
	// Note: `type` itself is NOT given a __class_getitem__. CPython's `type`
	// has no such method (`'__class_getitem__' in type.__dict__` is False);
	// `type[int]` is served by type's mp_subscript slot, mirrored by the
	// `cls == TypeType()` fast path in vm/eval_simple.go typeSubscript.
	// Wiring it here would make every class inherit it through the metatype,
	// so int[x] would wrongly succeed instead of raising "not subscriptable".
	//
	// CPython: Objects/typeobject.c type_subscript (mp_subscript, not a
	// __class_getitem__ method)
	// Further built-ins whose CPython tp_methods carry a
	// "__class_getitem__" entry pointing at Py_GenericAlias.
	//
	// CPython: Objects/enumobject.c enum_methods
	bindClassGetitem(EnumerateType)
	// CPython: Objects/memoryobject.c memory_methods
	bindClassGetitem(MemoryViewType)
	// CPython: Objects/descrobject.c mappingproxy_methods
	bindClassGetitem(MappingProxyType)
	// GeneratorType/CoroutineType/AsyncGeneratorType are assigned inside
	// their own init() (not a top-level var), so they may be nil when this
	// init() runs. They wire __class_getitem__ themselves, right after
	// NewType. CPython: Objects/genobject.c gen_methods / coro_methods /
	// async_gen_methods.
	// CPython: Objects/weakrefobject.c weakref_methods
	bindClassGetitem(WeakrefType)
	// CPython: Objects/templateobject.c template_methods
	bindClassGetitem(TemplateStrType)
	// CPython: Objects/interpolationobject.c interpolation_methods
	bindClassGetitem(InterpolationType)
	// typing.Generic[T] subscription. Uses a hookable wrapper so that
	// typing.py can redirect through _generic_class_getitem (which runs
	// _type_convert -> _make_forward_ref -> SyntaxError validation on
	// string arguments) once the hook is wired.
	//
	// CPython: Objects/typevarobject.c:2459 generic_class_getitem
	bindGenericClassGetitem(GenericType)

	// `int | str` reaches the binary-op machinery, which walks each
	// operand's NumberMethods.Or. Wiring it on typeType lets every
	// class (since classes are instances of typeType) participate.
	//
	// CPython: Objects/typeobject.c:7045 type_or
	typeType.Number = &NumberMethods{
		Or: unionTypeOr,
	}
}

// bindGenericClassGetitem installs a hookable `__class_getitem__` classmethod
// on t. When GenericClassGetitemHook is set (after typing.py wires it via
// _set_generic_class_getitem) the hook is called instead of the bare
// NewGenericAlias path, so string type arguments raise SyntaxError.
//
// CPython: Objects/typevarobject.c:2459 generic_class_getitem
func bindGenericClassGetitem(t *Type) {
	fn := NewBuiltinFunction("__class_getitem__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __class_getitem__() takes exactly one argument (%d given)", len(args)-1)
		}
		cls := args[0]
		arg := args[1]
		if hook := GenericClassGetitemHook; hook != nil {
			return hook(cls, arg)
		}
		tp, ok := cls.(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: __class_getitem__() requires a type, got %s", typeNameOf(cls))
		}
		return NewGenericAlias(tp, arg), nil
	})
	SetTypeDescr(t, "__class_getitem__", NewClassMethod(fn))
}

// BindClassGetitem is the exported entry point so module packages
// (_collections deque, itertools chain, _sre SRE_Pattern/SRE_Match,
// contextvars ContextVar/Token, os.DirEntry) can wire the same
// __class_getitem__ classmethod their CPython tp_methods tables carry.
//
// CPython: Objects/genericaliasobject.c:1040 Py_GenericAlias
func BindClassGetitem(t *Type) {
	bindClassGetitem(t)
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
