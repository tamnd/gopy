// Sequence protocol entry points. PySequence_* functions hand off to
// the type's SequenceMethods slot bundle, falling through to
// nb_add / nb_multiply / IterNext where CPython does. The Length /
// GetItem / SetItem / Contains / Iter helpers in protocol_object.go
// remain the public entry points for those four operations; this file
// adds the rest.
//
// CPython: Objects/abstract.c:1672 PySequence_Check

package objects

import (
	"errors"
	"fmt"
)

// SequenceCheck reports whether s implements the sequence protocol.
// Dict is excluded because it advertises tp_as_sequence indirectly via
// the mapping protocol.
//
// CPython: Objects/abstract.c:1672 PySequence_Check
func SequenceCheck(s Object) bool {
	if s == nil {
		return false
	}
	if _, ok := s.(*Dict); ok {
		return false
	}
	sq := s.Type().Sequence
	return sq != nil && sq.GetItem != nil
}

// SequenceSize returns len(s) when s implements the sequence protocol.
// Returns a TypeError when s is a mapping or has no length slot, which
// is the same shape PySequence_Size reports.
//
// CPython: Objects/abstract.c:1681 PySequence_Size
func SequenceSize(s Object) (int, error) {
	if s == nil {
		return 0, errors.New("TypeError: NoneType has no len()")
	}
	tp := s.Type()
	if sq := tp.Sequence; sq != nil && sq.Length != nil {
		return sq.Length(s)
	}
	if mp := tp.Mapping; mp != nil && mp.Length != nil {
		return 0, fmt.Errorf("TypeError: %s is not a sequence", tp.Name)
	}
	return 0, fmt.Errorf("TypeError: object of type '%s' has no len()", tp.Name)
}

// SequenceConcat is s + o restricted to the sequence protocol. Falls
// back to nb_add when both operands look like sequences but the LHS
// has no sq_concat, mirroring the user-class shim in CPython.
//
// CPython: Objects/abstract.c:1712 PySequence_Concat
func SequenceConcat(s, o Object) (Object, error) {
	if sq := s.Type().Sequence; sq != nil && sq.Concat != nil {
		return sq.Concat(s, o)
	}
	if SequenceCheck(s) && SequenceCheck(o) {
		out, err := numberBinaryNoErr(s, o, func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Add })
		if err != nil {
			return nil, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: '%s' object can't be concatenated", s.Type().Name)
}

// SequenceRepeat is s * count. Falls back to nb_multiply against a
// fresh int when sq_repeat is missing, the way CPython's
// PySequence_Repeat does for user classes that defined __mul__.
//
// CPython: Objects/abstract.c:1738 PySequence_Repeat
func SequenceRepeat(s Object, count int) (Object, error) {
	if sq := s.Type().Sequence; sq != nil && sq.Repeat != nil {
		return sq.Repeat(s, count)
	}
	if SequenceCheck(s) {
		out, err := numberBinaryNoErr(s, NewInt(int64(count)), func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Multiply })
		if err != nil {
			return nil, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: '%s' object can't be repeated", s.Type().Name)
}

// SequenceInPlaceConcat is s += o on the sequence protocol. Tries
// sq_inplace_concat, then sq_concat, then nb_inplace_add /
// nb_add, matching PySequence_InPlaceConcat.
//
// CPython: Objects/abstract.c:1769 PySequence_InPlaceConcat
func SequenceInPlaceConcat(s, o Object) (Object, error) {
	if sq := s.Type().Sequence; sq != nil {
		if sq.InPlaceConcat != nil {
			return sq.InPlaceConcat(s, o)
		}
		if sq.Concat != nil {
			return sq.Concat(s, o)
		}
	}
	if SequenceCheck(s) && SequenceCheck(o) {
		out, err := inPlaceBinaryNoErr(s, o,
			func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceAdd },
			func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Add })
		if err != nil {
			return nil, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: '%s' object can't be concatenated", s.Type().Name)
}

// SequenceInPlaceRepeat is s *= count on the sequence protocol. Tries
// sq_inplace_repeat, then sq_repeat, then nb_inplace_multiply /
// nb_multiply, matching PySequence_InPlaceRepeat.
//
// CPython: Objects/abstract.c:1798 PySequence_InPlaceRepeat
func SequenceInPlaceRepeat(s Object, count int) (Object, error) {
	if sq := s.Type().Sequence; sq != nil {
		if sq.InPlaceRepeat != nil {
			return sq.InPlaceRepeat(s, count)
		}
		if sq.Repeat != nil {
			return sq.Repeat(s, count)
		}
	}
	if SequenceCheck(s) {
		out, err := inPlaceBinaryNoErr(s, NewInt(int64(count)),
			func(n *NumberMethods) func(a, b Object) (Object, error) { return n.InPlaceMultiply },
			func(n *NumberMethods) func(a, b Object) (Object, error) { return n.Multiply })
		if err != nil {
			return nil, err
		}
		if out != nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("TypeError: '%s' object can't be repeated", s.Type().Name)
}

// SequenceGetItem is s[i] on the sequence protocol. Negative indices
// are normalized via sq_length the way PySequence_GetItem does;
// callers that already have a non-negative index can skip that work
// and call the slot directly.
//
// CPython: Objects/abstract.c:1832 PySequence_GetItem
func SequenceGetItem(s Object, i int) (Object, error) {
	tp := s.Type()
	sq := tp.Sequence
	if sq != nil && sq.GetItem != nil {
		if i < 0 && sq.Length != nil {
			n, err := sq.Length(s)
			if err != nil {
				return nil, err
			}
			i += n
		}
		return sq.GetItem(s, i)
	}
	if mp := tp.Mapping; mp != nil && mp.GetItem != nil {
		return nil, fmt.Errorf("TypeError: %s is not a sequence", tp.Name)
	}
	return nil, fmt.Errorf("TypeError: '%s' object does not support indexing", tp.Name)
}

// SequenceSetItem is s[i] = v on the sequence protocol.
//
// CPython: Objects/abstract.c:1884 PySequence_SetItem
func SequenceSetItem(s Object, i int, v Object) error {
	tp := s.Type()
	sq := tp.Sequence
	if sq != nil && sq.SetItem != nil {
		if i < 0 && sq.Length != nil {
			n, err := sq.Length(s)
			if err != nil {
				return err
			}
			i += n
		}
		return sq.SetItem(s, i, v)
	}
	if mp := tp.Mapping; mp != nil && mp.SetItem != nil {
		return fmt.Errorf("TypeError: %s is not a sequence", tp.Name)
	}
	return fmt.Errorf("TypeError: '%s' object does not support item assignment", tp.Name)
}

// SequenceDelItem is del s[i] on the sequence protocol; forwards to
// SequenceSetItem with v==nil.
//
// CPython: Objects/abstract.c:1917 PySequence_DelItem
func SequenceDelItem(s Object, i int) error {
	return SequenceSetItem(s, i, nil)
}

// SequenceCount returns the number of times o equals an element of s.
// Walks the iterator and compares with RichCmpBool, matching
// _PySequence_IterSearch in PY_ITERSEARCH_COUNT mode.
//
// CPython: Objects/abstract.c:2222 PySequence_Count
func SequenceCount(s, o Object) (int, error) {
	return iterSearch(s, o, iterSearchCount)
}

// SequenceIndex returns the position of the first element of s equal
// to o. Raises ValueError when o is absent.
//
// CPython: Objects/abstract.c:2252 PySequence_Index
func SequenceIndex(s, o Object) (int, error) {
	return iterSearch(s, o, iterSearchIndex)
}

// SequenceContains reports whether o appears in s. Uses sq_contains
// when available, otherwise iterates.
//
// CPython: Objects/abstract.c:2231 PySequence_Contains
func SequenceContains(s, o Object) (bool, error) {
	if sq := s.Type().Sequence; sq != nil && sq.Contains != nil {
		return sq.Contains(s, o)
	}
	n, err := iterSearch(s, o, iterSearchContains)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// iterSearch is the shared walk behind SequenceCount / SequenceIndex /
// SequenceContains. Mirrors _PySequence_IterSearch.
//
// CPython: Objects/abstract.c:2129 _PySequence_IterSearch
func iterSearch(s, o Object, op iterSearchOp) (int, error) {
	it, err := Iter(s)
	if err != nil {
		return 0, err
	}
	count := 0
	pos := 0
	for {
		x, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				break
			}
			return 0, err
		}
		eq, err := RichCmpBool(x, o, CompareEQ)
		if err != nil {
			return 0, err
		}
		if eq {
			switch op {
			case iterSearchCount:
				count++
			case iterSearchIndex:
				return pos, nil
			case iterSearchContains:
				return 1, nil
			}
		}
		pos++
	}
	if op == iterSearchIndex {
		return 0, errors.New("ValueError: sequence.index(x): x not in sequence")
	}
	return count, nil
}

type iterSearchOp int

const (
	iterSearchCount iterSearchOp = iota
	iterSearchIndex
	iterSearchContains
)

// SequenceList consumes v's iterator and returns a fresh list. The
// special-case PyList_CheckExact branch in CPython makes a copy of an
// existing list; gopy follows suit so callers don't accidentally
// alias the input.
//
// CPython: Objects/abstract.c:2072 PySequence_List
func SequenceList(v Object) (*List, error) {
	if l, ok := v.(*List); ok {
		items := make([]Object, l.Len())
		for i := range l.Len() {
			items[i] = l.Item(i)
		}
		return NewList(items), nil
	}
	out := NewList(nil)
	it, err := Iter(v)
	if err != nil {
		return nil, err
	}
	for {
		x, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return out, nil
			}
			return nil, err
		}
		out.Append(x)
	}
}

// SequenceTuple consumes v's iterator and returns a fresh tuple.
// PyTuple_CheckExact returns the input unchanged in CPython; gopy
// matches that since Tuple is immutable.
//
// CPython: Objects/abstract.c:1996 PySequence_Tuple
func SequenceTuple(v Object) (*Tuple, error) {
	if t, ok := v.(*Tuple); ok {
		return t, nil
	}
	l, err := SequenceList(v)
	if err != nil {
		return nil, err
	}
	items := make([]Object, l.Len())
	for i := range l.Len() {
		items[i] = l.Item(i)
	}
	return NewTuple(items), nil
}

// SequenceFast returns v as-is when it is already a list or tuple,
// otherwise materializes it into a fresh list. m is the TypeError
// message used when v is not iterable.
//
// CPython: Objects/abstract.c:2095 PySequence_Fast
func SequenceFast(v Object, m string) (Object, error) {
	switch v.(type) {
	case *List, *Tuple:
		return v, nil
	}
	out, err := SequenceList(v)
	if err != nil {
		if errors.Is(err, ErrStopIteration) {
			return nil, errors.New(m)
		}
		return nil, errors.New("TypeError: " + m)
	}
	return out, nil
}
