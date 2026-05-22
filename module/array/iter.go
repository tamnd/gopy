// arrayiterator is the iterator type backing array.__iter__.
// CPython lays out an arrayiterobject as (ao, index, getitem); gopy
// keeps the same triple and rebinds getitem to the descriptor's
// current pack/unpack hook so type-conversion of items goes through
// the same path the indexed accessors use.
//
// CPython: Modules/arraymodule.c:3014 arrayiter_type / arrayiterobject

package array

import (
	"github.com/tamnd/gopy/objects"
)

type arrayIterator struct {
	objects.Header
	ao      *arrayObject
	index   int
	getitem func(ap *arrayObject, i int) (objects.Object, error)
}

// arrayIterType is the Python-visible type for array_iterator.
//
// CPython: Modules/arraymodule.c:3026 PyArrayIter_Type
var arrayIterType = objects.NewType("arrayiterator", []*objects.Type{objects.ObjectType()})

func init() {
	arrayIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	arrayIterType.IterNext = arrayIterNext
	objects.AddIterSlotWrappers(arrayIterType)
}

// arrayIter is the tp_iter slot installed on ArrayType.
//
// CPython: Modules/arraymodule.c:2987 array_iter
func arrayIter(o objects.Object) (objects.Object, error) {
	a := o.(*arrayObject)
	it := &arrayIterator{ao: a, getitem: a.descr.getitem}
	it.Init(arrayIterType)
	return it, nil
}

// arrayIterNext mirrors arrayiter_next: when index reaches count the
// iterator drops its reference to the source array and raises
// StopIteration.
//
// CPython: Modules/arraymodule.c:3001 arrayiter_next
func arrayIterNext(o objects.Object) (objects.Object, error) {
	it := o.(*arrayIterator)
	if it.ao == nil || it.index >= it.ao.count {
		it.ao = nil
		return nil, objects.ErrStopIteration
	}
	v, err := it.getitem(it.ao, it.index)
	if err != nil {
		return nil, err
	}
	it.index++
	return v, nil
}
