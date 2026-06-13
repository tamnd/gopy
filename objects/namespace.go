// namespace object implementation: types.SimpleNamespace, a writable
// object whose attributes live in a plain dict reachable as __dict__.
// Attribute access routes through the generic getattr/setattr machinery
// (tp_dictoffset points at ns_dict); repr renders k=repr(v) pairs in
// insertion order guarded against recursion.
//
// CPython: Objects/namespaceobject.c
package objects

import (
	"fmt"
	"strings"
)

// Namespace mirrors _PyNamespaceObject. ns_dict is allocated eagerly by
// namespace_new and is the same dict that backs both __dict__ and the
// instance's attribute storage.
//
// CPython: Objects/namespaceobject.c:10 _PyNamespaceObject
type Namespace struct {
	Header
	dict *Dict
}

// NamespaceType is the type singleton for SimpleNamespace. It is a base
// type (Py_TPFLAGS_BASETYPE), so user code may subclass it.
//
// CPython: Objects/namespaceobject.c:275 _PyNamespace_Type
var NamespaceType = NewType("types.SimpleNamespace", []*Type{objectType})

func init() {
	NamespaceType.Repr = namespaceRepr
	NamespaceType.Str = namespaceRepr
	// tp_getattro/tp_setattro are PyObject_GenericGetAttr/SetAttr; the
	// __dict__ member at offsetof(ns_dict) and tp_dictoffset wire the
	// instance dict into the generic machinery.
	//
	// CPython: Objects/namespaceobject.c:300 _PyNamespace_Type
	NamespaceType.Getattro = GenericGetAttr
	NamespaceType.Setattro = GenericSetAttr
	NamespaceType.HasDict = true
	NamespaceType.RichCmp = namespaceCompare
	NamespaceType.TpNew = namespaceNew
	NamespaceType.TpNewWithDict = namespaceNewWithDict

	// namespace_members exposes __dict__ as a read-only member at
	// offsetof(ns_dict). gopy models it as a getset whose getter returns
	// ns_dict and whose absent setter makes it read-only.
	//
	// CPython: Objects/namespaceobject.c:19 namespace_members
	SetTypeDescr(NamespaceType, "__dict__", NewGetSetDescr("__dict__",
		func(o Object) (Object, error) {
			n, ok := o.(*Namespace)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__dict__' for 'types.SimpleNamespace' objects doesn't apply to a '%s' object", o.Type().Name)
			}
			Incref(n.dict)
			return n.dict, nil
		}, nil))

	// __reduce__ returns (type, (), ns_dict) so pickle reconstructs the
	// namespace by calling the type with no args then restoring state
	// through the dict.
	//
	// CPython: Objects/namespaceobject.c:230 namespace_reduce
	SetTypeDescr(NamespaceType, "__reduce__",
		NewMethodDescrConv(NamespaceType, "__reduce__", MethNoArgs, namespaceReduce))

	// Expose __new__ so super().__new__(cls) from a SimpleNamespace
	// subclass routes here and returns a *Namespace, rather than falling
	// back to object.__new__ (which would build a plain instance lacking
	// ns_dict). namespace_new allocates only; population is namespace_init's
	// job, so the keyword arguments are ignored at this stage.
	//
	// CPython: Objects/namespaceobject.c:25 namespace_new
	SetTypeDescr(NamespaceType, "__new__", NewBuiltinFunction("types.SimpleNamespace.__new__",
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("TypeError: SimpleNamespace.__new__(): not enough arguments")
			}
			cls, ok := args[0].(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: SimpleNamespace.__new__(X): X is not a type object")
			}
			return allocNamespace(cls), nil
		}))

	// __replace__ backs copy.replace(): a fresh namespace of the same
	// type, its dict updated from self then from the keyword arguments.
	//
	// CPython: Objects/namespaceobject.c:246 namespace_replace
	SetTypeDescr(NamespaceType, "__replace__",
		NewMethodDescr(NamespaceType, "__replace__", namespaceReplace))
}

// namespaceNew is the tp_new slot. It allocates an empty namespace of the
// requested (possibly subclass) type; population happens in namespace_init,
// which gopy folds into the with-dict variant so keyword order survives.
//
// CPython: Objects/namespaceobject.c:25 namespace_new
func namespaceNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	n := allocNamespace(cls)
	if err := namespaceInit(n, args, mapToDict(kwargs)); err != nil {
		return nil, err
	}
	return n, nil
}

// namespaceNewWithDict is the order-preserving tp_new path. typeCall routes
// SimpleNamespace(mapping, **kwds) through here so the keyword dict keeps
// its call-site order, which namespace_repr later renders verbatim.
//
// CPython: Objects/namespaceobject.c:25 namespace_new + :46 namespace_init
func namespaceNewWithDict(cls *Type, args []Object, kwargs *Dict) (Object, error) {
	n := allocNamespace(cls)
	if err := namespaceInit(n, args, kwargs); err != nil {
		return nil, err
	}
	return n, nil
}

// allocNamespace mirrors type->tp_alloc + PyDict_New: a zeroed namespace of
// the requested type with an empty ns_dict. A subclass instance is still a
// *Namespace whose ob_type is the subclass, matching CPython's single C
// struct shared down the SimpleNamespace hierarchy.
//
// CPython: Objects/namespaceobject.c:31 namespace_new (tp_alloc + PyDict_New)
func allocNamespace(cls *Type) *Namespace {
	if cls == nil {
		cls = NamespaceType
	}
	n := &Namespace{dict: NewDict()}
	n.init(cls)
	return n
}

// NewNamespace creates an empty SimpleNamespace.
//
// CPython: Objects/namespaceobject.c:25 namespace_new
func NewNamespace() *Namespace { return allocNamespace(NamespaceType) }

// namespaceInit is namespace_init: an optional single positional mapping is
// converted to a dict and merged, then the keyword arguments are merged.
// Both merges validate that every key is a string.
//
// CPython: Objects/namespaceobject.c:46 namespace_init
func namespaceInit(n *Namespace, args []Object, kwds *Dict) error {
	if len(args) > 1 {
		return fmt.Errorf("TypeError: %s expected at most 1 argument, got %d", n.Type().Name, len(args))
	}
	if len(args) == 1 {
		arg := args[0]
		// PyDict_CheckExact takes the fast path; otherwise dict(arg)
		// materializes a plain dict from the mapping before merging.
		d, ok := arg.(*Dict)
		if !ok {
			converted := NewDict()
			if err := dictMergeFromArg(converted, arg); err != nil {
				return err
			}
			d = converted
		}
		if err := validateKeywordArguments(d); err != nil {
			return err
		}
		if err := dictMergeFromArg(n.dict, d); err != nil {
			return err
		}
	}
	if kwds == nil || kwds.Len() == 0 {
		return nil
	}
	if err := validateKeywordArguments(kwds); err != nil {
		return err
	}
	return dictMergeFromArg(n.dict, kwds)
}

// validateKeywordArguments mirrors PyArg_ValidateKeywordArguments: every key
// must be a str instance, else the caller raises TypeError.
//
// CPython: Objects/dictobject.c PyArg_ValidateKeywordArguments
func validateKeywordArguments(d *Dict) error {
	for _, k := range d.Keys() {
		if !IsSubtype(k.Type(), strType) {
			return fmt.Errorf("TypeError: keywords must be strings")
		}
	}
	return nil
}

// AttrDict and EnsureAttrDict expose ns_dict to the generic attribute
// machinery: __dict__ is the attribute store, so both return the eagerly
// allocated dict.
//
// CPython: Objects/namespaceobject.c tp_dictoffset = offsetof(ns_dict)
func (n *Namespace) AttrDict() *Dict       { return n.dict }
func (n *Namespace) EnsureAttrDict() *Dict { return n.dict }

// Dict returns the namespace's underlying attribute dict.
func (n *Namespace) Dict() *Dict { return n.dict }

// namespaceRepr renders name(k=repr(v), ...) in the dict's insertion order,
// using "namespace" for an exact SimpleNamespace and the subclass tp_name
// otherwise. Py_ReprEnter guards a namespace that (transitively) contains
// itself, collapsing the inner visit to name(...).
//
// CPython: Objects/namespaceobject.c:91 namespace_repr
func namespaceRepr(o Object) (string, error) {
	n := o.(*Namespace)
	name := "namespace"
	if n.Type() != NamespaceType {
		name = n.Type().Name
	}
	if ReprEnter(o) {
		return fmt.Sprintf("%s(...)", name), nil
	}
	defer ReprLeave(o)

	parts := make([]string, 0, n.dict.Len())
	for _, k := range n.dict.Keys() {
		s, ok := k.(*Unicode)
		if !ok || len(s.v) == 0 {
			continue
		}
		v, err := n.dict.GetItem(k)
		if err != nil {
			continue
		}
		vr, err := Repr(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s=%s", s.v, vr))
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", ")), nil
}

// namespaceCompare delegates to the underlying dicts' richcompare when both
// operands are namespaces, returning NotImplemented otherwise. dict only
// implements EQ/NE, so other operators fall through to NotImplemented there.
//
// CPython: Objects/namespaceobject.c:199 namespace_richcompare
func namespaceCompare(a, b Object, op CompareOp) (Object, error) {
	an, ok := a.(*Namespace)
	if !ok {
		return NotImplemented(), nil
	}
	bn, ok := b.(*Namespace)
	if !ok {
		return NotImplemented(), nil
	}
	return RichCmp(an.dict, bn.dict, op)
}

// namespaceReduce returns (type, (), ns_dict) for pickling.
//
// CPython: Objects/namespaceobject.c:230 namespace_reduce
func namespaceReduce(args []Object, _ map[string]Object) (Object, error) {
	n, ok := selfNamespace(args)
	if !ok {
		return nil, fmt.Errorf("TypeError: __reduce__() requires a types.SimpleNamespace object")
	}
	Incref(n.Type())
	Incref(n.dict)
	return NewTuple([]Object{n.Type(), NewTuple(nil), n.dict}), nil
}

// namespaceReplace backs copy.replace(): it builds a fresh namespace of the
// same type, copies self's dict into it, then applies the keyword overrides.
// Positional arguments are rejected.
//
// CPython: Objects/namespaceobject.c:246 namespace_replace
func namespaceReplace(args []Object, kwargs map[string]Object) (Object, error) {
	n, ok := selfNamespace(args)
	if !ok {
		return nil, fmt.Errorf("TypeError: __replace__() requires a types.SimpleNamespace object")
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("TypeError: __replace__() takes no positional arguments")
	}
	// Build the replacement by calling the (possibly overridden) type with
	// no arguments, exactly as namespace_replace does. A subclass whose
	// __new__ returns a non-namespace is rejected here rather than silently
	// producing a broken object.
	//
	// CPython: Objects/namespaceobject.c:251 namespace_replace
	resObj, err := CallNoArgs(n.Type())
	if err != nil {
		return nil, err
	}
	result, ok := resObj.(*Namespace)
	if !ok {
		return nil, fmt.Errorf("TypeError: expect types.SimpleNamespace type, but %s() returned '%s' object",
			typeFullNameOf(n), typeFullNameOf(resObj))
	}
	if err := dictMergeFromArg(result.dict, n.dict); err != nil {
		return nil, err
	}
	if len(kwargs) > 0 {
		if err := dictMergeFromArg(result.dict, mapToDict(kwargs)); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// selfNamespace pulls the bound self out of a method-descriptor arg list.
func selfNamespace(args []Object) (*Namespace, bool) {
	if len(args) == 0 {
		return nil, false
	}
	n, ok := args[0].(*Namespace)
	return n, ok
}

// mapToDict rebuilds a *Dict from a kwargs map. The map has already lost
// call-site order, so this is the fallback path; the order-preserving
// constructors thread the original *Dict instead.
func mapToDict(m map[string]Object) *Dict {
	if len(m) == 0 {
		return nil
	}
	d := NewDict()
	for k, v := range m {
		_ = d.SetItem(NewStr(k), v)
	}
	return d
}
