// Set algebra: union, intersection, difference, symmetric difference,
// plus the subset/superset/disjoint predicates. The result type
// follows CPython: union(set, frozenset) returns a set when self is a
// set, a frozenset when self is a frozenset.
//
// CPython: Objects/setobject.c

package objects

import "fmt"

// Union returns self ∪ others. The result borrows self's mutability
// (set if self is a set, frozenset if self is a frozenset).
//
// CPython: Objects/setobject.c:1448 set_union
func (s *Set) Union(others ...*Set) (*Set, error) {
	out, err := s.copyShape()
	if err != nil {
		return nil, err
	}
	for _, other := range others {
		for _, e := range other.entries {
			if !e.used {
				continue
			}
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// Intersection returns self ∩ others. Empty result when any operand is
// empty. Order of evaluation follows CPython: walk the smallest
// operand and probe the others for each candidate.
//
// CPython: Objects/setobject.c:1611 set_intersection
func (s *Set) Intersection(others ...*Set) (*Set, error) {
	if len(others) == 0 {
		return s.copyShape()
	}
	out := s.emptyShape()
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		keep := true
		for _, other := range others {
			ok, err := other.Contains(e.key)
			if err != nil {
				return nil, err
			}
			if !ok {
				keep = false
				break
			}
		}
		if keep {
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// Difference returns self \ ⋃others.
//
// CPython: Objects/setobject.c:1750 set_difference_multi
func (s *Set) Difference(others ...*Set) (*Set, error) {
	out := s.emptyShape()
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		drop := false
		for _, other := range others {
			ok, err := other.Contains(e.key)
			if err != nil {
				return nil, err
			}
			if ok {
				drop = true
				break
			}
		}
		if !drop {
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// SymmetricDifference returns self △ other: items in exactly one of
// the two operands.
//
// CPython: Objects/setobject.c:1881 set_symmetric_difference
func (s *Set) SymmetricDifference(other *Set) (*Set, error) {
	out := s.emptyShape()
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		ok, err := other.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range other.entries {
		if !e.used {
			continue
		}
		ok, err := s.Contains(e.key)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// IsSubset reports whether every element of self is in other.
//
// CPython: Objects/setobject.c:1986 set_issubset
func (s *Set) IsSubset(other *Set) (bool, error) {
	if s.used > other.used {
		return false, nil
	}
	for _, e := range s.entries {
		if !e.used {
			continue
		}
		ok, err := other.Contains(e.key)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// IsSuperset reports whether every element of other is in self.
//
// CPython: Objects/setobject.c:2014 set_issuperset
func (s *Set) IsSuperset(other *Set) (bool, error) {
	return other.IsSubset(s)
}

// IsDisjoint reports whether self and other share no elements.
//
// CPython: Objects/setobject.c:2185 set_isdisjoint
func (s *Set) IsDisjoint(other *Set) (bool, error) {
	small, big := s, other
	if small.used > big.used {
		small, big = big, small
	}
	for _, e := range small.entries {
		if !e.used {
			continue
		}
		ok, err := big.Contains(e.key)
		if err != nil {
			return false, err
		}
		if ok {
			return false, nil
		}
	}
	return true, nil
}

// copyShape returns a copy of self with the same mutability (set vs
// frozenset). Used by ops that need to seed the result with self's
// items.
func (s *Set) copyShape() (*Set, error) {
	out := s.emptyShape()
	for _, e := range s.entries {
		if e.used {
			if err := out.add(e.key); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// emptyShape returns an empty set or frozenset matching self's type.
func (s *Set) emptyShape() *Set {
	if !s.frozen {
		return NewSet()
	}
	out := &Set{entries: make([]setEntry, setMinSize), frozen: true}
	out.init(FrozensetType)
	return out
}

// Update folds others into self in place. Errors when self is frozen.
//
// CPython: Objects/setobject.c:1330 set_update_internal
func (s *Set) Update(others ...*Set) error {
	if s.frozen {
		return fmt.Errorf("AttributeError: 'frozenset' object has no attribute 'update'")
	}
	for _, other := range others {
		for _, e := range other.entries {
			if !e.used {
				continue
			}
			if err := s.add(e.key); err != nil {
				return err
			}
		}
	}
	return nil
}
