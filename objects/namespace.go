// SimpleNamespace mirrors types.SimpleNamespace: a writable object
// whose attributes live in a plain dict and whose repr is the
// k=repr(v), ... form sorted by insertion order. The Go side keeps
// the dict directly; attribute access is a thin wrapper around it.
//
// CPython: Objects/namespaceobject.c

package objects

import (
	"fmt"
	"sort"
	"strings"
)

// Namespace mirrors _PyNamespaceObject.
//
// CPython: Objects/namespaceobject.c:11 _PyNamespaceObject
type Namespace struct {
	Header
	dict *Dict
}

// NamespaceType is the type singleton for SimpleNamespace.
//
// CPython: Objects/namespaceobject.c:275 _PyNamespace_Type
var NamespaceType = NewType("types.SimpleNamespace", []*Type{objectType})

func init() {
	NamespaceType.Repr = namespaceRepr
	NamespaceType.Str = namespaceRepr
	NamespaceType.Getattro = namespaceGetattr
	NamespaceType.Setattro = namespaceSetattr
	NamespaceType.RichCmp = namespaceCompare
}

// NewNamespace creates an empty SimpleNamespace.
//
// CPython: Objects/namespaceobject.c:27 namespace_new
func NewNamespace() *Namespace {
	n := &Namespace{dict: NewDict()}
	n.init(NamespaceType)
	return n
}

// Dict returns the namespace's underlying attribute dict.
func (n *Namespace) Dict() *Dict { return n.dict }

// CPython: Objects/namespaceobject.c:93 namespace_repr
func namespaceRepr(o Object) (string, error) {
	n := o.(*Namespace)
	keys := make([]string, 0, n.dict.Len())
	for _, k := range n.dict.Keys() {
		s, ok := k.(*strStub)
		if !ok {
			continue
		}
		keys = append(keys, s.v)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v, err := n.dict.GetItem(NewStr(k))
		if err != nil {
			continue
		}
		vr, err := Repr(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, vr))
	}
	return fmt.Sprintf("namespace(%s)", strings.Join(parts, ", ")), nil
}

// CPython: Objects/namespaceobject.c module_getattro fallback path
func namespaceGetattr(o Object, name Object) (Object, error) {
	n := o.(*Namespace)
	v, err := n.dict.GetItem(name)
	if err == nil {
		return v, nil
	}
	return nil, fmt.Errorf("AttributeError: %q", attrNameStr(name))
}

func namespaceSetattr(o Object, name, value Object) error {
	n := o.(*Namespace)
	if value == nil {
		return n.dict.DelItem(name)
	}
	return n.dict.SetItem(name, value)
}

// CPython: Objects/namespaceobject.c:201 namespace_richcompare
func namespaceCompare(a, b Object, op CompareOp) (Object, error) {
	an, ok := a.(*Namespace)
	if !ok {
		return NotImplemented(), nil
	}
	bn, ok := b.(*Namespace)
	if !ok {
		return NotImplemented(), nil
	}
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	eq := dictsEqual(an.dict, bn.dict)
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

func dictsEqual(a, b *Dict) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, k := range a.Keys() {
		av, err := a.GetItem(k)
		if err != nil {
			return false
		}
		bv, err := b.GetItem(k)
		if err != nil {
			return false
		}
		eq, err := RichCmpBool(av, bv, CompareEQ)
		if err != nil || !eq {
			return false
		}
	}
	return true
}
