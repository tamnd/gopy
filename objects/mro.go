package objects

import "slices"

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
// CPython: Objects/typeobject.c:L2349 mro_implementation
func c3Linearize(t *Type) []*Type {
	if len(t.Bases) == 0 {
		return []*Type{t}
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
			return out
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
			panic("objects: cannot linearize type hierarchy (inconsistent MRO)")
		}
		out = append(out, head)
		for i := range lists {
			if len(lists[i]) > 0 && lists[i][0] == head {
				lists[i] = lists[i][1:]
			}
		}
	}
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
// PyType_IsSubtype.
//
// CPython: Objects/typeobject.c:L2556 PyType_IsSubtype
func IsSubtype(sub, super *Type) bool {
	return slices.Contains(sub.MRO, super)
}
