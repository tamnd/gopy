// Iterator object types backing the iter / reversed / enumerate /
// zip builtins. Each is a thin wrapper that holds the cursor state
// and implements tp_iter (returns self) plus tp_iternext.

package builtins

import (
	"errors"
	"fmt"
	"strings"

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
	objects.SetTypeDescr(seqIterType, "__reduce__", objects.NewMethodDescrConv(seqIterType, "__reduce__", objects.MethNoArgs,
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
	objects.SetTypeDescr(seqIterType, "__setstate__", objects.NewMethodDescrConv(seqIterType, "__setstate__", objects.MethO,
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
	objects.SetTypeDescr(callIterType, "__reduce__", objects.NewMethodDescrConv(callIterType, "__reduce__", objects.MethNoArgs,
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
	reversedIterType.IterNext = reversedIterNext
	objects.SetTypeDescr(reversedIterType, "__reduce__", objects.NewMethodDescrConv(reversedIterType, "__reduce__", objects.MethNoArgs, reversedIterReduce))
	objects.SetTypeDescr(reversedIterType, "__setstate__", objects.NewMethodDescrConv(reversedIterType, "__setstate__", objects.MethO, reversedIterSetState))
	objects.SetTypeDescr(reversedIterType, "__length_hint__", objects.NewMethodDescrConv(reversedIterType, "__length_hint__", objects.MethNoArgs, reversedIterLengthHint))
	objects.AddIterSlotWrappers(reversedIterType)
}

// CPython: Objects/enumobject.c:1135 reversed_next
func reversedIterNext(o objects.Object) (objects.Object, error) {
	it := o.(*reversedIter)
	if it.idx < 0 || it.o == nil {
		it.idx = -1
		it.o = nil
		return nil, objects.ErrStopIteration
	}
	v, err := it.o.Type().Sequence.GetItem(it.o, it.idx)
	if err != nil {
		msg := err.Error()
		if strings.HasPrefix(msg, "IndexError") || errors.Is(err, objects.ErrStopIteration) {
			it.idx = -1
			it.o = nil
			return nil, objects.ErrStopIteration
		}
		return nil, err
	}
	it.idx--
	return v, nil
}

// CPython: Objects/enumobject.c:757 reversed_reduce
func reversedIterReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
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
}

// CPython: Objects/enumobject.c:779 reversed_setstate
func reversedIterSetState(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
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
}

// CPython: Objects/enumobject.c:744 reversed_len
func reversedIterLengthHint(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __length_hint__ takes no arguments")
	}
	it := args[0].(*reversedIter)
	if it.idx < 0 || it.o == nil {
		return objects.NewInt(0), nil
	}
	position := int64(it.idx + 1)
	if it.o.Type().Sequence != nil && it.o.Type().Sequence.Length != nil {
		cur, err := it.o.Type().Sequence.Length(it.o)
		if err != nil {
			return nil, err
		}
		if int64(cur) < position {
			return objects.NewInt(0), nil
		}
	}
	return objects.NewInt(position), nil
}

func newReversedIter(o objects.Object, n int) *reversedIter {
	it := &reversedIter{o: o, idx: n - 1}
	it.Init(reversedIterType)
	return it
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
	ZipType.IterNext = zipIterNext
	objects.SetTypeDescr(ZipType, "__reduce__", objects.NewMethodDescrConv(ZipType, "__reduce__", objects.MethNoArgs, zipReduce))
	objects.SetTypeDescr(ZipType, "__setstate__", objects.NewMethodDescrConv(ZipType, "__setstate__", objects.MethO, zipSetState))
}

// CPython: Python/bltinmodule.c:3222 zip_next
func zipIterNext(o objects.Object) (objects.Object, error) {
	z, ok := o.(*zipIter)
	if !ok {
		return nil, objects.ErrStopIteration
	}
	if z.done || len(z.iters) == 0 {
		return nil, objects.ErrStopIteration
	}
	out := make([]objects.Object, len(z.iters))
	for i, it := range z.iters {
		v, err := objects.IterNext(it)
		if err != nil || v == nil {
			if err != nil && !errors.Is(err, objects.ErrStopIteration) {
				return nil, err
			}
			z.done = true
			if z.strict {
				if serr := zipStrictCheck(z.iters, i); serr != nil {
					return nil, serr
				}
			}
			return nil, objects.ErrStopIteration
		}
		out[i] = v
	}
	return objects.NewTuple(out), nil
}

// zipReduce returns (type(self), (iter1, iter2, ...)) when strict is
// off, or the 3-tuple form (type(self), iters, True) when strict is
// on. pickle calls __setstate__(True) after constructing the zip so
// the unpickled instance still raises on length mismatch.
//
// CPython: Python/bltinmodule.c:3262 zip_reduce
func zipReduce(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	z, ok := args[0].(*zipIter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'zip' objects doesn't apply to a '%s' object", args[0].Type().Name)
	}
	if z.strict {
		return objects.NewTuple([]objects.Object{ZipType, objects.NewTuple(z.iters), objects.True()}), nil
	}
	return objects.NewTuple([]objects.Object{ZipType, objects.NewTuple(z.iters)}), nil
}

// zipSetState rebinds the strict flag from the pickle state. CPython
// coerces state to bool through PyObject_IsTrue so any truthy value
// turns strict on.
//
// CPython: Python/bltinmodule.c:3273 zip_setstate
func zipSetState(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __setstate__() takes exactly one argument (%d given)", len(args)-1)
	}
	z, ok := args[0].(*zipIter)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__setstate__' for 'zip' objects doesn't apply to a '%s' object", args[0].Type().Name)
	}
	strict, err := objects.IsTruthy(args[1])
	if err != nil {
		return nil, err
	}
	z.strict = strict
	return objects.None(), nil
}

func newZip(iters []objects.Object, strict bool) *zipIter {
	z := &zipIter{iters: iters, strict: strict}
	z.Init(ZipType)
	return z
}

// zipStrictCheck mirrors CPython's zip_next strict branch. When iter
// shortIdx raises StopIteration in the middle of building a result
// tuple, two shapes of mismatch are possible:
//
//  1. shortIdx > 0. Every iter before it already produced a value at
//     this position, so iter shortIdx is the short one. Raise
//     ValueError immediately.
//  2. shortIdx == 0. The first iter stopped before any of the others
//     was advanced. Walk iters[1..N-1] once each: if any still has a
//     value, that iter is longer. Otherwise every iter was exhausted
//     together and the strict pass succeeds.
//
// CPython: Python/bltinmodule.c:3222 zip_next (check: label)
func zipStrictCheck(iters []objects.Object, shortIdx int) error {
	if shortIdx > 0 {
		plural := " "
		if shortIdx > 1 {
			plural = "s 1-"
		}
		return fmt.Errorf("ValueError: zip() argument %d is shorter than argument%s%d",
			shortIdx+1, plural, shortIdx)
	}
	for j := 1; j < len(iters); j++ {
		it := iters[j]
		v, err := objects.IterNext(it)
		if errors.Is(err, objects.ErrStopIteration) {
			continue
		}
		if err != nil {
			return err
		}
		if v == nil {
			continue
		}
		plural := " "
		if j > 1 {
			plural = "s 1-"
		}
		return fmt.Errorf("ValueError: zip() argument %d is longer than argument%s%d",
			j+1, plural, j)
	}
	return nil
}
