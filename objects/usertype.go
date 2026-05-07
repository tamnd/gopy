// NewUserType builds a Type from the (name, bases, namespace) triple
// __build_class__ produces. Each entry in ns becomes a type-level
// descriptor reachable through LookupDescriptor; instance attribute
// access is wired to instanceGetAttr/instanceSetAttr so the dict is
// the per-instance backing store.
//
// CPython lays the same path through type.__call__ -> type_new ->
// type_init, which copies the body dict into tp_dict and stamps
// tp_getattro / tp_setattro to the generic slots. gopy's v0.10.1 cut
// keeps the sub-pieces small: the type call slot dispatches here and
// here we install the namespace.
//
// CPython: Objects/typeobject.c:4153 type_new

package objects

// NewUserType builds a Python-defined class. bases default to
// [object] when empty; ns must be non-nil and is iterated for type
// members. Special dunder slots beyond plain attribute storage are
// left to the broader 1672 typeobject port.
//
// CPython: Objects/typeobject.c:4153 type_new
func NewUserType(name string, bases []*Type, ns *Dict) *Type {
	if len(bases) == 0 {
		bases = []*Type{objectType}
	}
	t := NewType(name, bases)
	t.IsUser = true
	t.Getattro = instanceGetAttr
	t.Setattro = instanceSetAttr
	if ns != nil {
		// __classcell__ is the cell __build_class__ left in the
		// namespace so we can patch it with the new class. It is not a
		// real attribute, so install it before walking the rest of the
		// namespace and skip it during the descriptor copy.
		classCellKey := NewStr("__classcell__")
		if cellObj, err := ns.GetItem(classCellKey); err == nil {
			if cell, ok := cellObj.(*Cell); ok {
				cell.Contents = t
			}
			_ = ns.DelItem(classCellKey)
		}
		for _, k := range ns.Keys() {
			s, ok := k.(*Unicode)
			if !ok {
				continue
			}
			v, err := ns.GetItem(k)
			if err != nil {
				continue
			}
			SetTypeDescr(t, s.v, v)
		}
	}
	return t
}
