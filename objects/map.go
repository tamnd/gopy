// Map is the iterator type behind the map() builtin: map(f, *iters)
// yields f(*tuple(next(it) for it in iters)) until the shortest
// iterable is exhausted, or until any iterable is exhausted when
// strict=True (in which case the next non-exhausted source raises
// ValueError).
//
// CPython: Python/bltinmodule.c:1361 map_new
// CPython: Python/bltinmodule.c:1482 map_next
// CPython: Python/bltinmodule.c:1624 PyMap_Type

package objects

import (
	"errors"
	"fmt"
)

// Map is mapobject.
//
// CPython: Python/bltinmodule.c:1351 mapobject
type Map struct {
	Header
	Func   Object
	Iters  []Object
	Strict bool
}

// MapType is the type singleton for map.
//
// CPython: Python/bltinmodule.c:1624 PyMap_Type
var MapType = NewType("map", []*Type{objectType})

func init() {
	MapType.Iter = SelfIter
	MapType.IterNext = mapNext
	MapType.Dealloc = mapDealloc
	MapType.TpTraverse = mapTraverse
	AddIterSlotWrappers(MapType)
	SetTypeDescr(MapType, "__reduce__", NewMethodDescrConv(MapType, "__reduce__", MethNoArgs, mapReduce))
	SetTypeDescr(MapType, "__setstate__", NewMethodDescrConv(MapType, "__setstate__", MethO, mapSetstate))
}

// NewMap mirrors map_new: convert each iterable to its iterator up
// front so we fail eagerly on a non-iterable argument.
//
// CPython: Python/bltinmodule.c:1361 map_new
func NewMap(fn Object, iterables []Object, strict bool) (*Map, error) {
	if len(iterables) < 1 {
		return nil, errors.New("TypeError: map() must have at least two arguments")
	}
	iters := make([]Object, len(iterables))
	for i, src := range iterables {
		it, err := Iter(src)
		if err != nil {
			return nil, err
		}
		iters[i] = it
	}
	// The map owns a counted reference to its function so a bound method
	// or lambda handed straight into map() survives until the map is
	// exhausted, even after the caller's stack slot is released. Each
	// entry of iters already carries the owned reference Iter() returned,
	// so only the function needs an explicit incref here. Without this the
	// function (often a bound method whose imSelf is the only live handle
	// on an instance) drops to refcount zero when map() returns, its
	// dealloc decrefs that instance, and a mid-iteration collection then
	// reaps an object the loop is still walking.
	//
	// CPython: Python/bltinmodule.c:1361 map_new (Py_INCREF(func);
	//          the iters PyTuple owns its entries)
	Incref(fn)
	m := &Map{Func: fn, Iters: iters, Strict: strict}
	m.init(MapType)
	return m, nil
}

// mapDealloc releases the owned references on the function and every
// source iterator. Mirrors map_dealloc's Py_XDECREF(lz->func) plus the
// iters-tuple release.
//
// CPython: Python/bltinmodule.c:1404 map_dealloc
func mapDealloc(o Object) {
	m, ok := o.(*Map)
	if !ok {
		return
	}
	if m.Func != nil {
		Decref(m.Func)
	}
	for _, it := range m.Iters {
		if it != nil {
			Decref(it)
		}
	}
}

// mapTraverse lets the cyclic collector trace through the function and
// the source iterators. Mirrors map_traverse.
//
// CPython: Python/bltinmodule.c:1416 map_traverse
func mapTraverse(o Object, visit Visitor) error {
	m, ok := o.(*Map)
	if !ok {
		return nil
	}
	if m.Func != nil {
		if err := visit(m.Func); err != nil {
			return err
		}
	}
	for _, it := range m.Iters {
		if it == nil {
			continue
		}
		if err := visit(it); err != nil {
			return err
		}
	}
	return nil
}

// mapNext pulls one value from each source iterator and forwards
// them to the function as a flat positional call. In strict mode an
// uneven set of source lengths is reported as ValueError naming the
// position of the offending iterable.
//
// CPython: Python/bltinmodule.c:1482 map_next
func mapNext(o Object) (Object, error) {
	m, ok := o.(*Map)
	if !ok {
		return nil, ErrStopIteration
	}
	stack := make([]Object, 0, len(m.Iters))
	for i, it := range m.Iters {
		val, err := IterNext(it)
		if err != nil {
			if !errors.Is(err, ErrStopIteration) {
				return nil, err
			}
			if !m.Strict {
				return nil, ErrStopIteration
			}
			return nil, mapStrictMismatch(m, i)
		}
		stack = append(stack, val)
	}
	args := NewTuple(stack)
	return Call(m.Func, args, nil)
}

// mapReduce returns (type(self), (func, iter1, iter2, ...)) so pickle
// can reconstruct the map at the right iterator position. In strict
// mode it appends Py_True as the pickle state so __setstate__ can
// restore the strict flag, matching map_reduce's "ONO" build.
//
// CPython: Python/bltinmodule.c:1573 map_reduce
func mapReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	m, ok := args[0].(*Map)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'map' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	elems := make([]Object, 0, 1+len(m.Iters))
	elems = append(elems, m.Func)
	elems = append(elems, m.Iters...)
	if m.Strict {
		return NewTuple([]Object{MapType, NewTuple(elems), True()}), nil
	}
	return NewTuple([]Object{MapType, NewTuple(elems)}), nil
}

// mapSetstate restores the strict flag from the pickle state. map_setstate
// takes truthiness of the state object, so any truthy value re-enables
// strict mode after unpickling.
//
// CPython: Python/bltinmodule.c:1596 map_setstate
func mapSetstate(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument (%d given)", len(args)-1)
	}
	m, ok := args[0].(*Map)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__setstate__' for 'map' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	m.Strict = IsTrue(args[1])
	return None(), nil
}

// mapStrictMismatch is the strict-mode error that map_next emits when
// argument i has run dry while earlier arguments still had values.
// When i==0, the convention is to keep pulling from the remaining
// iterators so we can name the *first* longer iterable in the
// "argument N is longer than..." form.
//
// CPython: Python/bltinmodule.c:1525 map_next check label
func mapStrictMismatch(m *Map, i int) error {
	if i > 0 {
		plural := " "
		if i > 1 {
			plural = "s 1-"
		}
		return fmt.Errorf("ValueError: map() argument %d is shorter than argument%s%d",
			i+1, plural, i)
	}
	for j := 1; j < len(m.Iters); j++ {
		val, err := IterNext(m.Iters[j])
		if err == nil {
			_ = val
			plural := " "
			if j > 1 {
				plural = "s 1-"
			}
			return fmt.Errorf("ValueError: map() argument %d is longer than argument%s%d",
				j+1, plural, j)
		}
		if !errors.Is(err, ErrStopIteration) {
			return err
		}
	}
	return ErrStopIteration
}
