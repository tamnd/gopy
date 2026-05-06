// SeqIter and CallIter back the two non-iterable iter() forms:
// iter(seq) for an object that exposes __getitem__ but no __iter__,
// and iter(callable, sentinel) which polls callable() until the
// returned value compares equal to sentinel.
//
// CPython: Objects/iterobject.c:21 PySeqIter_Type
// CPython: Objects/iterobject.c:171 PyCallIter_Type

package objects

import "errors"

// SeqIter is the iterator returned by iter(seq) when seq lacks
// tp_iter but has a sequence GetItem slot.
//
// CPython: Objects/iterobject.c:8 seqiterobject
type SeqIter struct {
	Header
	seq   Object
	index int
}

// SeqIterType is the type singleton for seqiterator.
//
// CPython: Objects/iterobject.c:21 PySeqIter_Type
var SeqIterType = NewType("iterator", []*Type{objectType})

func init() {
	SeqIterType.Iter = func(o Object) (Object, error) { return o, nil }
	SeqIterType.IterNext = seqIterNext
}

// NewSeqIter wraps a sequence as iter(seq). The sequence must have a
// SequenceMethods.GetItem slot; otherwise the iterator stops on the
// first call to next().
//
// CPython: Objects/iterobject.c:32 PySeqIter_New
func NewSeqIter(seq Object) *SeqIter {
	it := &SeqIter{seq: seq}
	it.init(SeqIterType)
	return it
}

// seqIterNext mirrors iter_iternext: it_seq[it_index++], stopping on
// IndexError or StopIteration. Other errors propagate.
//
// CPython: Objects/iterobject.c:62 iter_iternext
func seqIterNext(o Object) (Object, error) {
	it := o.(*SeqIter)
	if it.seq == nil {
		return nil, ErrStopIteration
	}
	s := it.seq.Type().Sequence
	if s == nil || s.GetItem == nil {
		it.seq = nil
		return nil, ErrStopIteration
	}
	v, err := s.GetItem(it.seq, it.index)
	if err != nil {
		if errors.Is(err, errIndexOutOfRange) || errors.Is(err, ErrStopIteration) {
			it.seq = nil
			return nil, ErrStopIteration
		}
		return nil, err
	}
	it.index++
	return v, nil
}

// CallIter backs iter(callable, sentinel): it calls callable() and
// stops when the result equals sentinel.
//
// CPython: Objects/iterobject.c:155 calliterobject
type CallIter struct {
	Header
	callable Object
	sentinel Object
}

// CallIterType is the type singleton for callable_iterator.
//
// CPython: Objects/iterobject.c:171 PyCallIter_Type
var CallIterType = NewType("callable_iterator", []*Type{objectType})

func init() {
	CallIterType.Iter = func(o Object) (Object, error) { return o, nil }
	CallIterType.IterNext = callIterNext
}

// NewCallIter wraps callable+sentinel as iter(callable, sentinel).
//
// CPython: Objects/iterobject.c:139 PyCallIter_New
func NewCallIter(callable, sentinel Object) *CallIter {
	it := &CallIter{callable: callable, sentinel: sentinel}
	it.init(CallIterType)
	return it
}

// callIterNext mirrors calliter_iternext: call the callable, compare
// against the sentinel, end iteration when they match.
//
// CPython: Objects/iterobject.c:198 calliter_iternext
func callIterNext(o Object) (Object, error) {
	it := o.(*CallIter)
	if it.callable == nil {
		return nil, ErrStopIteration
	}
	v, err := CallNoArgs(it.callable)
	if err != nil {
		if errors.Is(err, ErrStopIteration) {
			it.callable = nil
		}
		return nil, err
	}
	eq, err := RichCmpBool(v, it.sentinel, CompareEQ)
	if err != nil {
		return nil, err
	}
	if eq {
		it.callable = nil
		return nil, ErrStopIteration
	}
	return v, nil
}
