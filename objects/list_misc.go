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
// CPython: Objects/listobject.c:1502 list_count. The loop reads
// Py_SIZE live so an __eq__ that clears or grows the list does not
// drive Count past the current end (bpo-38610 hardening).
func (l *List) Count(value Object) (int, error) {
	n := 0
	for i := 0; i < len(l.items); i++ {
		eq, err := RichCmpBool(l.items[i], value, CompareEQ)
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
// CPython: Objects/listobject.c:1430 list_index_impl. The loop reads
// Py_SIZE(self) live each iteration so __eq__ side effects (clearing
// the list, growing it) shorten or extend the walk in step with the
// observed state, matching the patch from bpo-1005778.
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
	for i := start; i < stop && i < len(l.items); i++ {
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
	// The list takes a counted reference on the inserted value; listDealloc
	// releases one per item on teardown.
	// CPython: Objects/listobject.c:1138 list_insert_impl (ins1 -> Py_INCREF)
	if value != nil {
		Incref(value)
	}
	l.items = append(l.items, nil)
	copy(l.items[where+1:], l.items[where:])
	l.items[where] = value
	l.size = int64(len(l.items))
}

// Remove deletes the first occurrence equal to value. The loop reads
// len(l.items) live so an __eq__ that mutates the list mid-search
// cannot drive Remove past the current end (bpo-38610 hardening).
//
// CPython: Objects/listobject.c:1408 list_remove
func (l *List) Remove(value Object) error {
	for i := 0; i < len(l.items); i++ {
		eq, err := RichCmpBool(l.items[i], value, CompareEQ)
		if err != nil {
			return err
		}
		if eq {
			if i >= len(l.items) {
				break
			}
			// Drop the list's reference to the removed element.
			// CPython: Objects/listobject.c:1408 list_remove (Py_DECREF)
			old := l.items[i]
			l.items = append(l.items[:i], l.items[i+1:]...)
			l.size = int64(len(l.items))
			if old != nil {
				Decref(old)
			}
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

// Clear empties the list, dropping the list's reference to every item.
// The item vector is detached before any Decref runs, mirroring
// list_clear which saves ob_item to a local, nulls the list, then
// decrefs from the saved copy. A re-entrant reader (a RichCompareBool
// that clears the list mid-iteration, bpo-39453) keeps walking the
// detached array rather than a half-niled live one.
//
// CPython: Objects/listobject.c:1228 list_clear (Py_DECREF over the saved vector)
func (l *List) Clear() {
	old := l.items
	l.items = nil
	l.size = 0
	for _, it := range old {
		if it != nil {
			Decref(it)
		}
	}
}

// Copy returns a shallow copy.
//
// CPython: Objects/listobject.c:1248 list_copy
func (l *List) Copy() *List {
	return NewList(l.items)
}

// listRichCmp implements the rich-compare slot for lists. The loop
// reads len(al.items) and len(bl.items) live on every iteration so
// that side effects from element __eq__ (e.g., clearing the operand
// list) shorten the walk immediately, and the post-loop size
// comparison observes the resulting sizes. This is the exact shape
// bpo-38588 fixed in CPython.
//
// CPython: Objects/listobject.c:3396 list_richcompare_impl
//
//nolint:gocyclo // mirrors list_richcompare_impl: per-op dispatch plus live-size element walk
func listRichCmp(a, b Object, op CompareOp) (Object, error) {
	al, ok := a.(*List)
	if !ok {
		return notImplemented(), nil
	}
	bl, ok := b.(*List)
	if !ok {
		return notImplemented(), nil
	}
	if (op == CompareEQ || op == CompareNE) && len(al.items) != len(bl.items) {
		return NewBool(op == CompareNE), nil
	}
	// Guard against self-referential lists (e.g. x = []; x.append(x)).
	// CPython: Objects/listobject.c:3396 list_richcompare_impl (Py_ENTER_RECURSIVE_CALL)
	if err := enterRecursiveCall(" in comparison"); err != nil {
		return nil, err
	}
	defer leaveRecursiveCall()
	var i int
	for i = 0; i < len(al.items) && i < len(bl.items); i++ {
		vitem := al.items[i]
		witem := bl.items[i]
		if vitem == witem {
			continue
		}
		eq, err := RichCmpBool(vitem, witem, CompareEQ)
		if err != nil {
			return nil, err
		}
		if !eq {
			break
		}
	}
	if i >= len(al.items) || i >= len(bl.items) {
		la, lb := len(al.items), len(bl.items)
		var r bool
		switch op {
		case CompareLT:
			r = la < lb
		case CompareLE:
			r = la <= lb
		case CompareEQ:
			r = la == lb
		case CompareNE:
			r = la != lb
		case CompareGT:
			r = la > lb
		case CompareGE:
			r = la >= lb
		}
		return NewBool(r), nil
	}
	if op == CompareEQ {
		return False(), nil
	}
	if op == CompareNE {
		return True(), nil
	}
	return RichCmp(al.items[i], bl.items[i], op)
}
