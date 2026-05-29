// Iterator object types backing the iter / reversed / enumerate /
// zip builtins. Each is a thin wrapper that holds the cursor state
// and implements tp_iter (returns self) plus tp_iternext.

package builtins

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// seqIter is the gopy port of PySeqIter_Type. It walks an object
// that lacks tp_iter but implements tp_as_sequence: the cursor
// starts at zero and steps through Sequence.GetItem until it
// raises IndexError (or beyond the reported length).
//
// CPython: Objects/iterobject.c:33 PySeqIter_Type
type seqIter struct {
	objects.Header
	o   objects.Object
	idx int
}

var seqIterType = objects.NewType("iterator", []*objects.Type{objects.ObjectType()})

func init() {
	seqIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	seqIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*seqIter)
		if it.o == nil {
			return nil, objects.ErrStopIteration
		}
		// CPython: Objects/iterobject.c:62 iter_iternext — never uses
		// sq_length; always calls sq_item and stops on IndexError or
		// StopIteration. Length is not a bound; objects may lie about it.
		v, err := it.o.Type().Sequence.GetItem(it.o, it.idx)
		if err != nil {
			if errors.Is(err, objects.ErrStopIteration) ||
				errors.Is(err, objects.ErrIndexOutOfRange) ||
				(objects.IsStopIterationHook != nil && objects.IsStopIterationHook(err)) ||
				(objects.IsIndexErrorHook != nil && objects.IsIndexErrorHook(err)) {
				it.o = nil
				if objects.ClearCurrentExceptionHook != nil {
					objects.ClearCurrentExceptionHook()
				}
				return nil, objects.ErrStopIteration
			}
			return nil, err
		}
		it.idx++
		return v, nil
	}
	// CPython: Objects/iterobject.c:133 iter_reduce
	objects.SetTypeDescr(seqIterType, "__reduce__", objects.NewMethodDescr(seqIterType, "__reduce__",
		func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*seqIter)
			if objects.BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := objects.BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.o == nil {
				// CPython: Objects/iterobject.c:133 iter_reduce exhausted branch
				// uses "N(())" which gives iter(()) — an empty tuple, not list.
				return objects.NewTuple([]objects.Object{iterFn, objects.NewTuple([]objects.Object{objects.NewTuple(nil)})}), nil
			}
			return objects.NewTuple([]objects.Object{
				iterFn,
				objects.NewTuple([]objects.Object{it.o}),
				objects.NewInt(int64(it.idx)),
			}), nil
		},
	))
	// CPython: Objects/iterobject.c:152 iter_setstate
	objects.SetTypeDescr(seqIterType, "__setstate__", objects.NewMethodDescr(seqIterType, "__setstate__",
		func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*seqIter)
			idx, ok := args[1].(*objects.Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__ requires int argument")
			}
			v, _ := idx.Int64()
			if v < 0 {
				v = 0
			}
			it.idx = int(v)
			return objects.None(), nil
		},
	))
	objects.AddIterSlotWrappers(seqIterType)
}

func newSeqIter(o objects.Object) *seqIter {
	it := &seqIter{o: o}
	it.Init(seqIterType)
	return it
}

// callIter is the gopy port of PyCallIter_Type: drive a callable
// until it returns the sentinel object.
//
// CPython: Objects/iterobject.c:159 PyCallIter_Type
type callIter struct {
	objects.Header
	callable objects.Object
	sentinel objects.Object
	done     bool
}

var callIterType = objects.NewType("callable_iterator", []*objects.Type{objects.ObjectType()})

func init() {
	callIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	callIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*callIter)
		if it.done {
			return nil, objects.ErrStopIteration
		}
		v, err := objects.Vectorcall(it.callable, nil, 0, nil)
		if err != nil {
			// CPython: Objects/iterobject.c:208 calliter_iternext — if the
			// callable raises StopIteration, exhaust the iterator silently.
			if errors.Is(err, objects.ErrStopIteration) ||
				(objects.IsStopIterationHook != nil && objects.IsStopIterationHook(err)) {
				it.done = true
				if objects.ClearCurrentExceptionHook != nil {
					objects.ClearCurrentExceptionHook()
				}
				return nil, objects.ErrStopIteration
			}
			return nil, err
		}
		// CPython: Objects/iterobject.c:215 calliter_iternext — re-check
		// after the call because a reentrant call may have set done=true.
		if it.done {
			return nil, objects.ErrStopIteration
		}
		eq, err := objects.RichCmpBool(v, it.sentinel, objects.CompareEQ)
		if err != nil {
			return nil, err
		}
		if eq {
			it.done = true
			return nil, objects.ErrStopIteration
		}
		return v, nil
	}
	// CPython: Objects/iterobject.c:237 calliter_reduce
	objects.SetTypeDescr(callIterType, "__reduce__", objects.NewMethodDescr(callIterType, "__reduce__",
		func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*callIter)
			if objects.BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := objects.BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			if it.done {
				// CPython: Objects/iterobject.c:237 calliter_reduce uses "N(())" — empty tuple.
				return objects.NewTuple([]objects.Object{
					iterFn,
					objects.NewTuple([]objects.Object{objects.NewTuple(nil)}),
				}), nil
			}
			return objects.NewTuple([]objects.Object{
				iterFn,
				objects.NewTuple([]objects.Object{it.callable, it.sentinel}),
			}), nil
		},
	))
	objects.AddIterSlotWrappers(callIterType)
}

func newCallIter(callable, sentinel objects.Object) *callIter {
	it := &callIter{callable: callable, sentinel: sentinel}
	it.Init(callIterType)
	return it
}

// reversedIter is the gopy port of PyReversedIter_Type. It uses the
// sequence's GetItem with a descending index.
//
// CPython: Objects/enumobject.c:reversed_iter
type reversedIter struct {
	objects.Header
	o   objects.Object
	idx int
}

var reversedIterType = objects.NewType("reversed", []*objects.Type{objects.ObjectType()})

func init() {
	reversedIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	reversedIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*reversedIter)
		if it.idx < 0 {
			return nil, objects.ErrStopIteration
		}
		v, err := it.o.Type().Sequence.GetItem(it.o, it.idx)
		if err != nil {
			return nil, err
		}
		it.idx--
		return v, nil
	}
	// CPython: Objects/enumobject.c:757 reversed_reduce
	objects.SetTypeDescr(reversedIterType, "__reduce__", objects.NewMethodDescr(reversedIterType, "__reduce__",
		func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			it := args[0].(*reversedIter)
			if objects.BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			revFn, err := objects.BuiltinLookup("reversed")
			if err != nil {
				return nil, err
			}
			if it.idx < 0 {
				return objects.NewTuple([]objects.Object{
					revFn,
					objects.NewTuple([]objects.Object{objects.NewList(nil)}),
				}), nil
			}
			return objects.NewTuple([]objects.Object{
				revFn,
				objects.NewTuple([]objects.Object{it.o}),
				objects.NewInt(int64(it.idx)),
			}), nil
		},
	))
	// CPython: Objects/enumobject.c:779 reversed_setstate
	objects.SetTypeDescr(reversedIterType, "__setstate__", objects.NewMethodDescr(reversedIterType, "__setstate__",
		func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument")
			}
			it := args[0].(*reversedIter)
			idx, ok := args[1].(*objects.Int)
			if !ok {
				return nil, fmt.Errorf("TypeError: __setstate__ requires int argument")
			}
			v, _ := idx.Int64()
			n := int64(-1)
			if it.o != nil && it.o.Type().Sequence != nil && it.o.Type().Sequence.Length != nil {
				if ln, lerr := it.o.Type().Sequence.Length(it.o); lerr == nil {
					n = int64(ln) - 1
				}
			}
			if v < -1 {
				v = -1
			} else if v > n {
				v = n
			}
			it.idx = int(v)
			return objects.None(), nil
		},
	))
	objects.AddIterSlotWrappers(reversedIterType)
}

func newReversedIter(o objects.Object, n int) *reversedIter {
	it := &reversedIter{o: o, idx: n - 1}
	it.Init(reversedIterType)
	return it
}

// enumerateIter is the gopy port of PyEnum_Type. The counter is the
// CPython en_index field; the wrapped iterator drives the values.
//
// CPython: Objects/enumobject.c:46 PyEnum_Type
type enumerateIter struct {
	objects.Header
	it    objects.Object
	index int64
}

var enumerateType = objects.NewType("enumerate", []*objects.Type{objects.ObjectType()})

func init() {
	enumerateType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	enumerateType.IterNext = func(o objects.Object) (objects.Object, error) {
		e := o.(*enumerateIter)
		v, err := e.it.Type().IterNext(e.it)
		if err != nil {
			return nil, err
		}
		out := objects.NewTuple([]objects.Object{objects.NewInt(e.index), v})
		e.index++
		return out, nil
	}
}

func newEnumerate(it objects.Object, start int64) *enumerateIter {
	e := &enumerateIter{it: it, index: start}
	e.Init(enumerateType)
	return e
}

// zipIter is the gopy port of PyZip_Type. Holds the underlying
// iterators and the strict flag. When any iterator stops, the zip
// stops; in strict mode a remaining-items check kicks in afterwards.
//
// CPython: Objects/enumobject.c:1142 PyZip_Type
type zipIter struct {
	objects.Header
	iters  []objects.Object
	strict bool
	done   bool
}

// ZipType is the type singleton for zip, exported so init.go can
// expose it as a builtin name.
//
// CPython: Objects/enumobject.c:1142 PyZip_Type
var ZipType = objects.NewType("zip", []*objects.Type{objects.ObjectType()})

func init() {
	ZipType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	ZipType.IterNext = func(o objects.Object) (objects.Object, error) {
		z, ok := o.(*zipIter)
		if !ok {
			return nil, objects.ErrStopIteration
		}
		if z.done || len(z.iters) == 0 {
			return nil, objects.ErrStopIteration
		}
		out := make([]objects.Object, len(z.iters))
		for i, it := range z.iters {
			v, err := it.Type().IterNext(it)
			if errors.Is(err, objects.ErrStopIteration) {
				z.done = true
				if z.strict {
					if serr := zipStrictCheck(z.iters, i); serr != nil {
						return nil, serr
					}
				}
				return nil, objects.ErrStopIteration
			}
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return objects.NewTuple(out), nil
	}
	objects.SetTypeDescr(ZipType, "__reduce__", objects.NewMethodDescr(ZipType, "__reduce__", zipReduce))
}

// zipReduce returns (type(self), (iter1, iter2, ...)) so pickle can
// reconstruct the zip at the right iterator position.
//
// CPython: Objects/enumobject.c:1142 zip_reduce
func zipReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	z, ok := args[0].(*zipIter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'zip' objects doesn't apply to a '%s' object", args[0].Type().Name)
	}
	return objects.NewTuple([]objects.Object{ZipType, objects.NewTuple(z.iters)}), nil
}

func newZip(iters []objects.Object, strict bool) *zipIter {
	z := &zipIter{iters: iters, strict: strict}
	z.Init(ZipType)
	return z
}

// zipStrictCheck mirrors CPython's zip strict handling: when iter i
// raises StopIteration, the iterators before it must also be done,
// and the iterators after must already have been advanced. Any
// mismatch is a ValueError.
//
// CPython: Objects/enumobject.c:1241 zip_next strict branch
func zipStrictCheck(iters []objects.Object, shortIdx int) error {
	for j, it := range iters {
		if j == shortIdx {
			continue
		}
		_, err := it.Type().IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			continue
		}
		if err != nil {
			return err
		}
		if j < shortIdx {
			return fmt.Errorf("ValueError: zip() argument %d is longer than argument %d", j+1, shortIdx+1)
		}
		return fmt.Errorf("ValueError: zip() argument %d is shorter than argument %d", shortIdx+1, j+1)
	}
	return nil
}
