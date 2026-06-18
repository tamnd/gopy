// Filter is the iterator type behind the filter() builtin: filter(f,
// iterable) yields the elements x of iterable for which f(x) is true.
// When f is None (or the bool type), x is taken at face value through
// the truth protocol, matching CPython's `checktrue` shortcut.
//
// CPython: Python/bltinmodule.c:501 filter_new
// CPython: Python/bltinmodule.c:584 filter_next
// CPython: Python/bltinmodule.c:640 PyFilter_Type

package objects

import "fmt"

// Filter is filterobject.
//
// CPython: Python/bltinmodule.c:493 filterobject
type Filter struct {
	Header
	Func Object
	It   Object
}

// FilterType is the type singleton for filter.
//
// CPython: Python/bltinmodule.c:640 PyFilter_Type
var FilterType = NewType("filter", []*Type{objectType})

func init() {
	FilterType.Iter = SelfIter
	FilterType.IterNext = filterNext
	FilterType.Dealloc = filterDealloc
	FilterType.TpTraverse = filterTraverse
	AddIterSlotWrappers(FilterType)
	SetTypeDescr(FilterType, "__reduce__", NewMethodDescrConv(FilterType, "__reduce__", MethNoArgs, filterReduce))
}

// NewFilter mirrors filter_new: turn iterable into an iterator and
// pair it with the predicate. The predicate may be None to mean "use
// the truth protocol on each element directly".
//
// CPython: Python/bltinmodule.c:501 filter_new
func NewFilter(fn, iterable Object) (*Filter, error) {
	it, err := Iter(iterable)
	if err != nil {
		return nil, err
	}
	// The filter owns a counted reference to its predicate. it already
	// carries the owned reference Iter() returned. Without owning fn, a
	// bound-method predicate handed straight into filter() drops to
	// refcount zero when the call returns, its dealloc decrefs imSelf, and
	// a mid-iteration collection reaps an instance the loop still walks.
	//
	// CPython: Python/bltinmodule.c:501 filter_new (Py_INCREF(func))
	Incref(fn)
	f := &Filter{Func: fn, It: it}
	f.init(FilterType)
	return f, nil
}

// filterDealloc releases the owned predicate and source-iterator
// references. Mirrors filter_dealloc's Py_XDECREF pair.
//
// CPython: Python/bltinmodule.c:540 filter_dealloc
func filterDealloc(o Object) {
	f, ok := o.(*Filter)
	if !ok {
		return
	}
	if f.Func != nil {
		Decref(f.Func)
	}
	if f.It != nil {
		Decref(f.It)
	}
}

// filterTraverse lets the cyclic collector trace through the predicate
// and the source iterator. Mirrors filter_traverse.
//
// CPython: Python/bltinmodule.c:552 filter_traverse
func filterTraverse(o Object, visit Visitor) error {
	f, ok := o.(*Filter)
	if !ok {
		return nil
	}
	if f.Func != nil {
		if err := visit(f.Func); err != nil {
			return err
		}
	}
	if f.It != nil {
		return visit(f.It)
	}
	return nil
}

// filterNext advances the iterator until the predicate accepts a
// value or the source iterator is exhausted.
//
// CPython: Python/bltinmodule.c:584 filter_next
func filterNext(o Object) (Object, error) {
	f, ok := o.(*Filter)
	if !ok {
		return nil, ErrStopIteration
	}
	checktrue := IsNone(f.Func) || f.Func == BoolType
	for {
		item, err := IterNext(f.It)
		if err != nil {
			return nil, err
		}
		var ok bool
		if checktrue {
			ok, err = IsTruthy(item)
			if err != nil {
				return nil, err
			}
		} else {
			good, err := CallOneArg(f.Func, item)
			if err != nil {
				return nil, err
			}
			ok, err = IsTruthy(good)
			if err != nil {
				return nil, err
			}
		}
		if ok {
			return item, nil
		}
	}
}

// filterReduce returns (type(self), (func, it)) so pickle can reconstruct
// the filter at the right iterator position.
//
// CPython: Python/bltinmodule.c:640 filter_reduce
func filterReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	f, ok := args[0].(*Filter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'filter' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	return NewTuple([]Object{FilterType, NewTuple([]Object{f.Func, f.It})}), nil
}
