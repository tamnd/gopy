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

var seqIterType = objects.NewType("iterator", []*objects.Type{objects.TypeType()})

func init() {
	seqIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	seqIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*seqIter)
		seq := it.o.Type().Sequence
		n, err := seq.Length(it.o)
		if err != nil {
			return nil, err
		}
		if it.idx >= n {
			return nil, objects.ErrStopIteration
		}
		v, err := seq.GetItem(it.o, it.idx)
		if err != nil {
			return nil, err
		}
		it.idx++
		return v, nil
	}
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

var callIterType = objects.NewType("callable_iterator", []*objects.Type{objects.TypeType()})

func init() {
	callIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	callIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*callIter)
		if it.done {
			return nil, objects.ErrStopIteration
		}
		v, err := objects.Vectorcall(it.callable, nil, 0, nil)
		if err != nil {
			return nil, err
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

var reversedIterType = objects.NewType("reversed", []*objects.Type{objects.TypeType()})

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

var enumerateType = objects.NewType("enumerate", []*objects.Type{objects.TypeType()})

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
		z := o.(*zipIter)
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
