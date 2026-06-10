// Package _heapq is the gopy port of CPython's Modules/_heapqmodule.c.
// It provides the heap queue algorithm (priority queue) functions that
// back Lib/heapq.py.
//
// CPython: Modules/_heapqmodule.c

package _heapq

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_heapq", buildModule)
}

// buildModule materializes the _heapq module dict.
//
// CPython: Modules/_heapqmodule.c:540 heapq_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_heapq")
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"heappush", objects.NewBuiltinFunction("heappush", heappush)},
		{"heappop", objects.NewBuiltinFunction("heappop", heappop)},
		{"heappushpop", objects.NewBuiltinFunction("heappushpop", heappushpop)},
		{"heapreplace", objects.NewBuiltinFunction("heapreplace", heapreplace)},
		{"heapify", objects.NewBuiltinFunction("heapify", heapify)},
		{"nlargest", objects.NewBuiltinFunction("nlargest", nlargest)},
		{"nsmallest", objects.NewBuiltinFunction("nsmallest", nsmallest)},
		{"__about__", objects.NewStr("Heap queues\n\n[explanation by Francois Pinard]\n")},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Comparison helper.
// ---------------------------------------------------------------------------

// cmpLT returns true if a < b using Python's rich comparison.
//
// CPython: Modules/_heapqmodule.c:34 cmp_lt
func cmpLT(a, b objects.Object) (bool, error) {
	return objects.RichCmpBool(a, b, objects.CompareLT)
}

// listSet sets heap[i] = v using the sequence SetItem slot.
func listSet(heap *objects.List, i int, v objects.Object) error {
	return objects.SetItem(heap, objects.NewInt(int64(i)), v)
}

// ---------------------------------------------------------------------------
// Sift helpers.
// ---------------------------------------------------------------------------

// siftdown restores heap invariant by bubbling the item at pos upward.
// Called after appending an element.
//
// CPython: Modules/_heapqmodule.c:108 _siftdown
func siftdown(heap *objects.List, startpos, pos int) error {
	newitem := heap.Item(pos)
	for pos > startpos {
		parentpos := (pos - 1) >> 1
		parent := heap.Item(parentpos)
		lt, err := cmpLT(newitem, parent)
		if err != nil {
			return err
		}
		if !lt {
			break
		}
		if err := listSet(heap, pos, parent); err != nil {
			return err
		}
		pos = parentpos
	}
	return listSet(heap, pos, newitem)
}

// siftup restores heap invariant by pushing the item at pos downward.
// Called after replacing the root element.
//
// CPython: Modules/_heapqmodule.c:133 _siftup
func siftup(heap *objects.List, pos int) error {
	endpos := heap.Len()
	startpos := pos
	newitem := heap.Item(pos)
	childpos := 2*pos + 1
	for childpos < endpos {
		rightpos := childpos + 1
		if rightpos < endpos {
			lt, err := cmpLT(heap.Item(childpos), heap.Item(rightpos))
			if err != nil {
				return err
			}
			// CPython: Modules/_heapqmodule.c:54 size-change guard
			if heap.Len() != endpos {
				return fmt.Errorf("RuntimeError: list changed size during iteration")
			}
			if !lt {
				childpos = rightpos
			}
		}
		if childpos >= heap.Len() {
			return fmt.Errorf("RuntimeError: list changed size during iteration")
		}
		if err := listSet(heap, pos, heap.Item(childpos)); err != nil {
			return err
		}
		pos = childpos
		childpos = 2*pos + 1
	}
	if err := listSet(heap, pos, newitem); err != nil {
		return err
	}
	return siftdown(heap, startpos, pos)
}

// ---------------------------------------------------------------------------
// Module functions.
// ---------------------------------------------------------------------------

// heappush pushes item onto the heap, maintaining the heap invariant.
//
// CPython: Modules/_heapqmodule.c:165 _heapq_heappush_impl
func heappush(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: heappush() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: heappush() expected 2 arguments, got %d", len(args))
	}
	heap, ok := args[0].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: heap argument must be a list")
	}
	heap.Append(args[1])
	if err := siftdown(heap, 0, heap.Len()-1); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// heappop pops and returns the smallest item from the heap.
//
// CPython: Modules/_heapqmodule.c:192 _heapq_heappop_impl
func heappop(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: heappop() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: heappop() expected 1 argument, got %d", len(args))
	}
	heap, ok := args[0].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: heap argument must be a list")
	}
	if heap.Len() == 0 {
		return nil, errors.New("IndexError: index out of range")
	}
	// Swap root with last, pop last, sift root down.
	lastelt, err := heap.Pop(-1)
	if err != nil {
		return nil, err
	}
	if heap.Len() == 0 {
		return lastelt, nil
	}
	returnitem := heap.Item(0)
	if err := listSet(heap, 0, lastelt); err != nil {
		return nil, err
	}
	if err := siftup(heap, 0); err != nil {
		return nil, err
	}
	return returnitem, nil
}

// heapreplace pops and returns the current smallest value, then pushes
// the new item. The heap size does not change.
//
// CPython: Modules/_heapqmodule.c:233 _heapq_heapreplace_impl
func heapreplace(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: heapreplace() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: heapreplace() expected 2 arguments, got %d", len(args))
	}
	heap, ok := args[0].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: heap argument must be a list")
	}
	if heap.Len() == 0 {
		return nil, errors.New("IndexError: index out of range")
	}
	returnitem := heap.Item(0)
	if err := listSet(heap, 0, args[1]); err != nil {
		return nil, err
	}
	if err := siftup(heap, 0); err != nil {
		return nil, err
	}
	return returnitem, nil
}

// heappushpop pushes item on the heap, then pops and returns the
// smallest item. Faster than heappush followed by heappop.
//
// CPython: Modules/_heapqmodule.c:263 _heapq_heappushpop_impl
func heappushpop(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: heappushpop() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: heappushpop() expected 2 arguments, got %d", len(args))
	}
	heap, ok := args[0].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: heap argument must be a list")
	}
	item := args[1]
	if heap.Len() > 0 {
		top := heap.Item(0)
		lt, err := cmpLT(top, item)
		if err != nil {
			return nil, err
		}
		if lt {
			if err := listSet(heap, 0, item); err != nil {
				return nil, err
			}
			item = top
			if err := siftup(heap, 0); err != nil {
				return nil, err
			}
		}
	}
	return item, nil
}

// heapify transforms list x into a heap, in-place, in O(len(x)) time.
//
// CPython: Modules/_heapqmodule.c:300 _heapq_heapify_impl
func heapify(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("TypeError: heapify() takes no keyword arguments")
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: heapify() expected 1 argument, got %d", len(args))
	}
	heap, ok := args[0].(*objects.List)
	if !ok {
		return nil, errors.New("TypeError: heap argument must be a list")
	}
	n := heap.Len()
	for i := n/2 - 1; i >= 0; i-- {
		if err := siftup(heap, i); err != nil {
			return nil, err
		}
	}
	return objects.None(), nil
}

// nlargest returns the n largest elements from the iterable.
//
// CPython: Modules/_heapqmodule.c:370 _heapq_nlargest_impl
func nlargest(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var key objects.Object
	for k, v := range kwargs {
		if k == "key" {
			key = v
		} else {
			return nil, fmt.Errorf("TypeError: nlargest() got an unexpected keyword argument '%s'", k)
		}
	}
	// Accept key as optional third positional argument.
	switch len(args) {
	case 3:
		if key != nil {
			return nil, errors.New("TypeError: nlargest() got multiple values for argument 'key'")
		}
		key = args[2]
		args = args[:2]
	case 2:
		// ok
	default:
		return nil, fmt.Errorf("TypeError: nlargest() expected 2 arguments, got %d", len(args))
	}
	if key == objects.None() {
		key = nil
	}
	nInt, ok := args[0].(*objects.Int)
	if !ok {
		return nil, errors.New("TypeError: n must be an integer")
	}
	nVal, ok2 := nInt.Int64()
	if !ok2 {
		return nil, errors.New("OverflowError: n is too large")
	}
	n := int(nVal)
	if n < 0 {
		n = 0
	}
	// When a key function is provided, collect all items, sort by key, return top n.
	// CPython: Modules/_heapqmodule.c:370 _heapq_nlargest_impl (key path via heapq.py)
	if key != nil {
		it, err := objects.Iter(args[1])
		if err != nil {
			return nil, err
		}
		allItems := objects.NewList(nil)
		for {
			v, iterErr := objects.IterNext(it)
			if iterErr != nil {
				if errors.Is(iterErr, objects.ErrStopIteration) {
					break
				}
				return nil, iterErr
			}
			allItems.Append(v)
		}
		if err := allItems.Sort(key, true); err != nil {
			return nil, err
		}
		count := allItems.Len()
		if n > count {
			n = count
		}
		out := make([]objects.Object, n)
		for i := 0; i < n; i++ {
			out[i] = allItems.Item(i)
		}
		return objects.NewList(out), nil
	}
	it, err := objects.Iter(args[1])
	if err != nil {
		return nil, err
	}
	result := make([]objects.Object, 0, n)
	for len(result) < n {
		v, iterErr := objects.IterNext(it)
		if iterErr != nil {
			if errors.Is(iterErr, objects.ErrStopIteration) {
				break
			}
			return nil, iterErr
		}
		result = append(result, v)
	}
	if len(result) == 0 {
		return objects.NewList(nil), nil
	}
	heap := objects.NewList(result)
	if _, err := heapify([]objects.Object{heap}, nil); err != nil {
		return nil, err
	}
	for {
		v, iterErr := objects.IterNext(it)
		if iterErr != nil {
			if errors.Is(iterErr, objects.ErrStopIteration) {
				break
			}
			return nil, iterErr
		}
		top := heap.Item(0)
		lt, cmpErr := cmpLT(top, v)
		if cmpErr != nil {
			return nil, cmpErr
		}
		if lt {
			if _, err := heapreplace([]objects.Object{heap, v}, nil); err != nil {
				return nil, err
			}
		}
	}
	if err := heap.Sort(nil, true); err != nil {
		return nil, err
	}
	return heap, nil
}

// nsmallest returns the n smallest elements from the iterable.
//
// CPython: Modules/_heapqmodule.c:440 _heapq_nsmallest_impl
func nsmallest(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var key objects.Object
	for k, v := range kwargs {
		if k == "key" {
			key = v
		} else {
			return nil, fmt.Errorf("TypeError: nsmallest() got an unexpected keyword argument '%s'", k)
		}
	}
	// Accept key as optional third positional argument.
	switch len(args) {
	case 3:
		if key != nil {
			return nil, errors.New("TypeError: nsmallest() got multiple values for argument 'key'")
		}
		key = args[2]
		args = args[:2]
	case 2:
		// ok
	default:
		return nil, fmt.Errorf("TypeError: nsmallest() expected 2 arguments, got %d", len(args))
	}
	if key == objects.None() {
		key = nil
	}
	nInt, ok := args[0].(*objects.Int)
	if !ok {
		return nil, errors.New("TypeError: n must be an integer")
	}
	nVal, ok2 := nInt.Int64()
	if !ok2 {
		return nil, errors.New("OverflowError: n is too large")
	}
	n := int(nVal)
	if n < 0 {
		n = 0
	}
	it, err := objects.Iter(args[1])
	if err != nil {
		return nil, err
	}
	allItems := objects.NewList(nil)
	for {
		v, iterErr := objects.IterNext(it)
		if iterErr != nil {
			if errors.Is(iterErr, objects.ErrStopIteration) {
				break
			}
			return nil, iterErr
		}
		allItems.Append(v)
	}
	if err := allItems.Sort(key, false); err != nil {
		return nil, err
	}
	count := allItems.Len()
	if n > count {
		n = count
	}
	out := make([]objects.Object, n)
	for i := 0; i < n; i++ {
		out[i] = allItems.Item(i)
	}
	return objects.NewList(out), nil
}
