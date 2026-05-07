// User-facing list methods (count, index, insert, remove, pop,
// reverse, clear, copy) plus the rich-compare slot. The slot
// dispatch table itself lives in list.go; this file is just the
// implementations that aren't core sequence slots.
//
// CPython: Objects/listobject.c:1502 list_count

package objects

import (
	"fmt"
)

// Count returns the number of elements equal to value.
//
// CPython: Objects/listobject.c:1502 list_count
func (l *List) Count(value Object) (int, error) {
	n := 0
	for _, it := range l.items {
		eq, err := RichCmpBool(it, value, CompareEQ)
		if err != nil {
			return 0, err
		}
		if eq {
			n++
		}
	}
	return n, nil
}

// Index returns the position of the first item equal to value inside
// [start, stop). Negative bounds are normalised the same way
// list_index_impl does, and a missing match raises ValueError.
//
// CPython: Objects/listobject.c:1430 list_index_impl
func (l *List) Index(value Object, start, stop int) (int, error) {
	n := len(l.items)
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop += n
		if stop < 0 {
			stop = 0
		}
	}
	if stop > n {
		stop = n
	}
	for i := start; i < stop; i++ {
		eq, err := RichCmpBool(l.items[i], value, CompareEQ)
		if err != nil {
			return 0, err
		}
		if eq {
			return i, nil
		}
	}
	return 0, fmt.Errorf("ValueError: %v is not in list", value)
}

// Insert puts value at position where, shifting later items right.
// Negative where is clamped to 0; where past the end appends.
//
// CPython: Objects/listobject.c:1138 list_insert_impl
func (l *List) Insert(where int, value Object) {
	n := len(l.items)
	if where < 0 {
		where += n
		if where < 0 {
			where = 0
		}
	}
	if where > n {
		where = n
	}
	l.items = append(l.items, nil)
	copy(l.items[where+1:], l.items[where:])
	l.items[where] = value
	l.size = int64(len(l.items))
}

// Remove deletes the first occurrence equal to value.
//
// CPython: Objects/listobject.c:1408 list_remove
func (l *List) Remove(value Object) error {
	for i, it := range l.items {
		eq, err := RichCmpBool(it, value, CompareEQ)
		if err != nil {
			return err
		}
		if eq {
			l.items = append(l.items[:i], l.items[i+1:]...)
			l.size = int64(len(l.items))
			return nil
		}
	}
	return fmt.Errorf("ValueError: list.remove(x): x not in list")
}

// Pop removes and returns items[i]. i==-1 (the default at the call
// site) pops the tail. Out-of-range indices raise IndexError.
//
// CPython: Objects/listobject.c:1365 list_pop_impl
func (l *List) Pop(i int) (Object, error) {
	n := len(l.items)
	if n == 0 {
		return nil, fmt.Errorf("IndexError: pop from empty list")
	}
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("IndexError: pop index out of range")
	}
	v := l.items[i]
	l.items = append(l.items[:i], l.items[i+1:]...)
	l.size = int64(len(l.items))
	return v, nil
}

// Reverse reverses items in place.
//
// CPython: Objects/listobject.c:1262 list_reverse
func (l *List) Reverse() {
	for i, j := 0, len(l.items)-1; i < j; i, j = i+1, j-1 {
		l.items[i], l.items[j] = l.items[j], l.items[i]
	}
}

// Clear empties the list.
//
// CPython: Objects/listobject.c:1228 list_clear
func (l *List) Clear() {
	l.items = l.items[:0]
	l.size = 0
}

// Copy returns a shallow copy.
//
// CPython: Objects/listobject.c:1248 list_copy
func (l *List) Copy() *List {
	return NewList(l.items)
}

// listRichCmp implements the rich-compare slot for lists. Only EQ/NE
// are wired today; the abstract layer asks the other side for the
// ordered operators by returning NotImplemented.
//
// CPython: Objects/listobject.c:2999 list_richcompare
func listRichCmp(a, b Object, op CompareOp) (Object, error) {
	al, ok := a.(*List)
	if !ok {
		return notImplemented(), nil
	}
	bl, ok := b.(*List)
	if !ok {
		return notImplemented(), nil
	}
	switch op {
	case CompareEQ, CompareNE:
		eq := len(al.items) == len(bl.items)
		if eq {
			for i := range al.items {
				ok, err := RichCmpBool(al.items[i], bl.items[i], CompareEQ)
				if err != nil {
					return nil, err
				}
				if !ok {
					eq = false
					break
				}
			}
		}
		if op == CompareNE {
			eq = !eq
		}
		return NewBool(eq), nil
	}
	return notImplemented(), nil
}
