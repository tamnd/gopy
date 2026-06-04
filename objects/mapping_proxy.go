// mappingproxy: a read-only view over any mapping. types.MappingProxyType
// is the public alias; type.__dict__ returns an instance of this so user
// code cannot mutate a class dict through the published handle.
//
// CPython: Objects/descrobject.c:1034 mappingproxy

package objects

import "fmt"

// MappingProxy wraps an underlying mapping and forwards reads while
// rejecting writes with TypeError. The wrapped mapping is any object
// implementing the mapping protocol; CPython rejects list/tuple
// explicitly via mappingproxy_check_mapping so we mirror that.
//
// CPython: Objects/descrobject.c:1039 mappingproxyobject
type MappingProxy struct {
	Header
	mapping Object
}

// MappingProxyType is the type singleton for mappingproxy.
//
// CPython: Objects/descrobject.c:1193 PyDictProxy_Type
var MappingProxyType = NewType("mappingproxy", []*Type{objectType})

func init() {
	MappingProxyType.Repr = mappingProxyRepr
	MappingProxyType.Str = mappingProxyRepr
	MappingProxyType.Iter = mappingProxyIter
	MappingProxyType.RichCmp = mappingProxyRichCompare
	MappingProxyType.Hash = mappingProxyHash
	MappingProxyType.Mapping = &MappingMethods{
		Length:  mappingProxyLen,
		GetItem: mappingProxyGetItem,
		SetItem: mappingProxySetItem,
		DelItem: mappingProxyDelItem,
	}
	MappingProxyType.Sequence = &SequenceMethods{
		Contains: mappingProxyContains,
	}
	MappingProxyType.Number = &NumberMethods{
		Or:        mappingProxyOr,
		InPlaceOr: mappingProxyIOr,
	}
	MappingProxyType.TpNew = mappingProxyNew

	SetTypeDescr(MappingProxyType, "get", NewMethodDescr(MappingProxyType, "get", mappingProxyGetMethod))
	SetTypeDescr(MappingProxyType, "keys", NewMethodDescr(MappingProxyType, "keys", mappingProxyForwardNoArgs("keys")))
	SetTypeDescr(MappingProxyType, "values", NewMethodDescr(MappingProxyType, "values", mappingProxyForwardNoArgs("values")))
	SetTypeDescr(MappingProxyType, "items", NewMethodDescr(MappingProxyType, "items", mappingProxyForwardNoArgs("items")))
	SetTypeDescr(MappingProxyType, "copy", NewMethodDescr(MappingProxyType, "copy", mappingProxyForwardNoArgs("copy")))
	SetTypeDescr(MappingProxyType, "__reversed__", NewMethodDescr(MappingProxyType, "__reversed__", mappingProxyForwardNoArgs("__reversed__")))
}

// mappingProxyForwardNoArgs returns a closure that forwards a no-arg
// method call to the wrapped mapping. CPython's mappingproxy_keys /
// values / items / copy / __reversed__ all share this shape.
//
// CPython: Objects/descrobject.c:1131 mappingproxy_keys (and friends)
func mappingProxyForwardNoArgs(method string) func(args []Object, _ map[string]Object) (Object, error) {
	return func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: %s() takes no arguments (%d given)", method, len(args)-1)
		}
		mp, ok := args[0].(*MappingProxy)
		if !ok {
			return nil, fmt.Errorf("TypeError: descriptor '%s' requires a 'mappingproxy' object but received a '%s'",
				method, args[0].Type().Name)
		}
		fn, err := GetAttr(mp.mapping, NewStr(method))
		if err != nil {
			return nil, err
		}
		return Call(fn, NewTuple(nil), nil)
	}
}

// mappingProxyGetMethod backs mappingproxy.get(key, default=None).
//
// CPython: Objects/descrobject.c:1112 mappingproxy_get
func mappingProxyGetMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: get expected 1 to 2 arguments, got %d", len(args)-1)
	}
	mp, ok := args[0].(*MappingProxy)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'get' requires a 'mappingproxy' object but received a '%s'",
			args[0].Type().Name)
	}
	fn, err := GetAttr(mp.mapping, NewStr("get"))
	if err != nil {
		return nil, err
	}
	callArgs := []Object{args[1]}
	if len(args) == 3 {
		callArgs = append(callArgs, args[2])
	} else {
		callArgs = append(callArgs, None())
	}
	return Call(fn, NewTuple(callArgs), nil)
}

// NewMappingProxy wraps mapping in a read-only proxy. CPython's
// PyDictProxy_New is the C-level constructor that the import machinery
// and Modules/_typesmodule.c reach for; the public Python class is
// MappingProxyType.
//
// CPython: Objects/descrobject.c:1279 PyDictProxy_New
func NewMappingProxy(mapping Object) (*MappingProxy, error) {
	if err := mappingProxyCheckMapping(mapping); err != nil {
		return nil, err
	}
	mp := &MappingProxy{mapping: mapping}
	mp.init(MappingProxyType)
	return mp, nil
}

// Mapping returns the wrapped mapping. Used by callers that already
// know they have a proxy and want the underlying object (the | / |=
// dispatch and reprs lean on this).
//
// CPython: Objects/descrobject.c:1039 mappingproxyobject->mapping
func (mp *MappingProxy) Mapping() Object { return mp.mapping }

// CPython: Objects/descrobject.c:1240 mappingproxy_check_mapping
func mappingProxyCheckMapping(mapping Object) error {
	if mapping == nil {
		return fmt.Errorf("TypeError: mappingproxy() argument must be a mapping, not NoneType")
	}
	if _, ok := mapping.(*List); ok {
		return fmt.Errorf("TypeError: mappingproxy() argument must be a mapping, not list")
	}
	if _, ok := mapping.(*Tuple); ok {
		return fmt.Errorf("TypeError: mappingproxy() argument must be a mapping, not tuple")
	}
	if !MappingCheck(mapping) {
		return fmt.Errorf("TypeError: mappingproxy() argument must be a mapping, not %s", mapping.Type().Name)
	}
	return nil
}

// CPython: Objects/descrobject.c:1262 mappingproxy_new_impl
func mappingProxyNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: mappingproxy() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: mappingproxy expected 1 argument, got %d", len(args))
	}
	return NewMappingProxy(args[0])
}

// CPython: Objects/descrobject.c:1044 mappingproxy_len
func mappingProxyLen(o Object) (int, error) {
	mp := o.(*MappingProxy)
	return Length(mp.mapping)
}

// CPython: Objects/descrobject.c:1051 mappingproxy_getitem
func mappingProxyGetItem(o, key Object) (Object, error) {
	mp := o.(*MappingProxy)
	return GetItem(mp.mapping, key)
}

// CPython: Objects/descrobject.c:1058 mappingproxy_as_mapping
// (mp_ass_subscript == NULL: any assignment is a TypeError).
func mappingProxySetItem(o, _, _ Object) error {
	_ = o
	return fmt.Errorf("TypeError: 'mappingproxy' object does not support item assignment")
}

func mappingProxyDelItem(o, _ Object) error {
	_ = o
	return fmt.Errorf("TypeError: 'mappingproxy' object does not support item deletion")
}

// CPython: Objects/descrobject.c:1088 mappingproxy_contains
func mappingProxyContains(o, key Object) (bool, error) {
	mp := o.(*MappingProxy)
	return Contains(mp.mapping, key)
}

// mappingProxyOr is the nb_or slot: a | b. Either operand that is a
// proxy is unwrapped to its underlying mapping, then the result is
// PyNumber_Or of the unwrapped operands. So `proxy | dict` and
// `userdict | proxy` both behave as if the proxy were its mapping.
//
// CPython: Objects/descrobject.c:1065 mappingproxy_or
func mappingProxyOr(a, b Object) (Object, error) {
	if mp, ok := a.(*MappingProxy); ok {
		a = mp.mapping
	}
	if mp, ok := b.(*MappingProxy); ok {
		b = mp.mapping
	}
	return NumberOr(a, b)
}

// mappingProxyIOr is the nb_inplace_or slot. A proxy is read-only, so
// |= is always rejected, pointing the caller at | instead.
//
// CPython: Objects/descrobject.c:1077 mappingproxy_ior
func mappingProxyIOr(a, _ Object) (Object, error) {
	return nil, fmt.Errorf("TypeError: '|=' is not supported by %s; use '|' instead", a.Type().Name)
}

// CPython: Objects/descrobject.c:1196 mappingproxy_getiter
func mappingProxyIter(o Object) (Object, error) {
	mp := o.(*MappingProxy)
	return Iter(mp.mapping)
}

// CPython: Objects/descrobject.c:1203 mappingproxy_hash
func mappingProxyHash(o Object) (int64, error) {
	mp := o.(*MappingProxy)
	return Hash(mp.mapping)
}

// CPython: Objects/descrobject.c:1217 mappingproxy_repr
func mappingProxyRepr(o Object) (string, error) {
	mp := o.(*MappingProxy)
	r, err := Repr(mp.mapping)
	if err != nil {
		return "", err
	}
	return "mappingproxy(" + r + ")", nil
}

// CPython: Objects/descrobject.c:1232 mappingproxy_richcompare
func mappingProxyRichCompare(a, b Object, op CompareOp) (Object, error) {
	ap, ok := a.(*MappingProxy)
	if !ok {
		return NotImplemented(), nil
	}
	// Compare against the wrapped mapping on the right; if the right is
	// also a proxy, unwrap it for symmetry.
	right := b
	if bp, ok := b.(*MappingProxy); ok {
		right = bp.mapping
	}
	return RichCmp(ap.mapping, right, op)
}
