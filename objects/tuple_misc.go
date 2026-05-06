// Tuple methods that aren't slot routines: count() and index().
// Both walk the items and dispatch through PyObject_RichCompareBool
// the same way CPython's tupleobject implementations do.
//
// CPython: Objects/tupleobject.c

package objects

import "fmt"

// Count returns the number of elements equal to value. Mirrors
// tuple.count.
//
// CPython: Objects/tupleobject.c:822 tuple_count
func (t *Tuple) Count(value Object) (int, error) {
	n := 0
	for _, it := range t.items {
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

// Index returns the position of the first item equal to value
// inside [start, stop). Negative bounds are normalised the same way
// CPython's tuple_index does, and a missing match raises ValueError.
//
// CPython: Objects/tupleobject.c:765 tuple_index
func (t *Tuple) Index(value Object, start, stop int) (int, error) {
	n := len(t.items)
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
		eq, err := RichCmpBool(t.items[i], value, CompareEQ)
		if err != nil {
			return 0, err
		}
		if eq {
			return i, nil
		}
	}
	return 0, fmt.Errorf("ValueError: tuple.index(x): x not in tuple")
}
