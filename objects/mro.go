package objects

import (
	"fmt"
	"slices"
	"strings"
)

// c3Linearize computes the C3 linearization of t. The algorithm is
// the one Python uses for new-style classes, ported from
// pmerge/mro_implementation in cpython/Objects/typeobject.c.
//
// For a built-in type with bases [B1, B2, ...], the linearization is:
//
//	L[T] = T :: merge(L[B1], L[B2], ..., [B1, B2, ...])
//
// where `merge` repeatedly takes the head of the first list whose
// head appears only as a head (never in the tails) of the remaining
// lists.
//
// CPython: Objects/typeobject.c:2349 mro_implementation
func c3Linearize(t *Type) ([]*Type, error) {
	if len(t.Bases) == 0 {
		return []*Type{t}, nil
	}
	if err := checkCompleteBases(t); err != nil {
		return nil, err
	}
	lists := make([][]*Type, 0, len(t.Bases)+1)
	for _, b := range t.Bases {
		lists = append(lists, append([]*Type(nil), b.MRO...))
	}
	lists = append(lists, append([]*Type(nil), t.Bases...))

	out := []*Type{t}
	for {
		nonEmpty := false
		for _, l := range lists {
			if len(l) > 0 {
				nonEmpty = true
				break
			}
		}
		if !nonEmpty {
			return out, nil
		}

		var head *Type
		for _, l := range lists {
			if len(l) == 0 {
				continue
			}
			cand := l[0]
			if isGoodHead(cand, lists) {
				head = cand
				break
			}
		}
		if head == nil {
			names := make([]string, len(t.Bases))
			for i, b := range t.Bases {
				names[i] = tailName(b.Name)
			}
			return nil, fmt.Errorf("TypeError: Cannot create a consistent method resolution order (MRO) for bases %s", joinNames(names))
		}
		out = append(out, head)
		for i := range lists {
			if len(lists[i]) > 0 && lists[i][0] == head {
				lists[i] = lists[i][1:]
			}
		}
	}
}

// checkCompleteBases rejects a base whose tp_mro is still NULL, i.e. one
// that is mid-creation (a custom metaclass mro() that reaches back to
// subclass an uninitialized type). CPython raises a clean TypeError rather
// than dereferencing the missing linearization.
//
// CPython: Objects/typeobject.c:3342 mro_implementation_unlocked
func checkCompleteBases(t *Type) error {
	for _, b := range t.Bases {
		if b != nil && b != objectType && b.MRO == nil {
			return fmt.Errorf("TypeError: Cannot extend an incomplete type '%s'", tailName(b.Name))
		}
	}
	return nil
}

// joinNames joins base names with ", " for MRO error messages.
func joinNames(names []string) string {
	return strings.Join(names, ", ")
}

// mroInvoke computes a fresh MRO for t, honoring a metaclass mro()
// override, and returns it WITHOUT installing it onto t.MRO. When the
// metaclass is exactly `type` (or only inherits type.mro) it falls back to
// the C3 default. A custom mro() may reassign t's __bases__ reentrantly,
// which installs a new t.MRO deeper in the call stack; the caller detects
// that by slice identity and discards this result. Unlike the
// initial-creation path (applyMetaclassMRO), t.MRO keeps its existing value
// across the call: set_tp_mro on a complete type does not null tp_mro
// before invoking mro().
//
// CPython: Objects/typeobject.c:3492 mro_invoke
func mroInvoke(t *Type) ([]*Type, error) {
	meta := t.Type()
	if meta == nil || meta == typeType {
		return c3Linearize(t)
	}
	descr, _ := LookupDescriptor(meta, "mro")
	if descr == nil {
		return c3Linearize(t)
	}
	if owner, ok := mroDescrOwner(descr); ok && owner == typeType {
		return c3Linearize(t)
	}
	bound := bindDescr(descr, t, meta)
	res, err := callBound(bound, nil, nil)
	if err != nil {
		return nil, err
	}
	return mroResultToTypes(res)
}

// mroResultToTypes converts the object a metaclass mro() returns (a tuple
// or list of types) into a []*Type, mirroring PySequence_Tuple plus the
// per-element type check mro_invoke performs.
//
// CPython: Objects/typeobject.c:3510 mro_invoke (PySequence_Tuple + mro_check)
func mroResultToTypes(res Object) ([]*Type, error) {
	tup, ok := res.(*Tuple)
	if !ok {
		lst, ok := res.(*List)
		if !ok {
			return nil, fmt.Errorf("TypeError: mro() returned a non-tuple: %s", typeNameOf(res))
		}
		items := make([]Object, lst.Len())
		for i := 0; i < lst.Len(); i++ {
			items[i] = lst.Item(i)
		}
		tup = NewTuple(items)
	}
	out := make([]*Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		entry, ok := tup.Item(i).(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: mro() returned a non-type at index %d: %s", i, typeNameOf(tup.Item(i)))
		}
		out = append(out, entry)
	}
	return out, nil
}

// sameSliceIdentity reports whether a and b are the same slice value (same
// backing array and length). It stands in for CPython's tp_mro
// pointer-identity reentrancy check: mro_invoke never touches tp_mro
// itself, so a changed slice header means a reentrant __bases__ assignment
// reinstalled the MRO deeper in the stack.
func sameSliceIdentity(a, b []*Type) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

// isGoodHead reports whether cand appears only as a head (never in
// the tail) of every list. The C code calls this `tail_contains`
// inverted.
//
// CPython: Objects/typeobject.c:L2308 tail_contains
func isGoodHead(cand *Type, lists [][]*Type) bool {
	for _, l := range lists {
		for i := 1; i < len(l); i++ {
			if l[i] == cand {
				return false
			}
		}
	}
	return true
}

// IsSubtype reports whether sub is sub or one of its bases. Mirrors
// PyType_IsSubtype, which delegates to is_subtype_with_mro on tp_mro.
//
// CPython: Objects/typeobject.c:2556 PyType_IsSubtype
func IsSubtype(sub, super *Type) bool {
	return isSubtypeWithMRO(sub, super)
}

// isSubtypeWithMRO answers IsSubtype by scanning a's MRO, or, when a is
// still mid-creation with a NULL tp_mro, by walking its tp_base chain.
// Without the fallback, super(cls, cls) inside a custom mro() (where
// cls.tp_mro is NULL) would wrongly reject cls as not a subtype of itself
// and raise TypeError instead of letting the attribute lookup fail with
// AttributeError.
//
// CPython: Objects/typeobject.c:2804 is_subtype_with_mro
func isSubtypeWithMRO(a, b *Type) bool {
	if a.MRO != nil {
		return slices.Contains(a.MRO, b)
	}
	return typeIsSubtypeBaseChain(a, b)
}

// typeIsSubtypeBaseChain follows the primary tp_base link (gopy's
// Bases[0]) up to object, matching solidBase's walk. CPython follows the
// single tp_base pointer; gopy uses the first declared base as that
// primary link.
//
// CPython: Objects/typeobject.c:2792 type_is_subtype_base_chain
func typeIsSubtypeBaseChain(a, b *Type) bool {
	for a != nil {
		if a == b {
			return true
		}
		if len(a.Bases) == 0 {
			a = nil
		} else {
			a = a.Bases[0]
		}
	}
	return b == objectType
}

// basesCauseCycle reports whether making base an ancestor of t would close
// an inheritance cycle. It mirrors the two-pronged check in
// type_set_bases_unlocked: first via base's MRO (or its tp_base chain when
// the MRO is not yet set), then, when the MRO exists, also via the tp_base
// chain. The second walk is essential during reentrance, where a custom
// mro() has assigned base's primary base before its MRO is refreshed; the
// MRO scan alone would miss the freshly-formed cycle and a later solidBase
// walk would loop forever.
//
// CPython: Objects/typeobject.c:1823 type_set_bases_unlocked
func basesCauseCycle(base, t *Type) bool {
	if isSubtypeWithMRO(base, t) {
		return true
	}
	return base.MRO != nil && typeIsSubtypeBaseChain(base, t)
}
