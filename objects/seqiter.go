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
	SeqIterType.Iter = SelfIter
	SeqIterType.IterNext = seqIterNext
	AddIterSlotWrappers(SeqIterType)
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

// KeyErrorFactory builds a proper Python KeyError wrapping the given key
// object so e.args[0] is the key itself. Installed from errors/init to
// break the import cycle between objects and errors.
//
// CPython: Objects/exceptions.c KeyError stores the key in args[0]
var KeyErrorFactory func(key Object) error

// IsIndexErrorHook lets the errors package teach seqIterNext to
// recognize a Python-level IndexError. Installed from errors/init so
// objects does not depend on errors.
//
// CPython: Objects/iterobject.c:78 iter_iternext PyErr_ExceptionMatches(PyExc_IndexError)
var IsIndexErrorHook func(error) bool

// IsStopIterationHook lets the errors package teach IterNext to
// recognize a Python-level StopIteration. Installed from errors/init.
// Mirrors PyIter_Next's _PyErr_ExceptionMatches(PyExc_StopIteration) check.
//
// CPython: Objects/abstract.c:2852 PyIter_Next
var IsStopIterationHook func(error) bool

// ClearCurrentExceptionHook drops the thread-state's current exception.
// Installed from errors/init so seqIterNext can match PyErr_Clear after
// PyErr_ExceptionMatches(PyExc_IndexError) without importing errors.
//
// CPython: Python/errors.c:488 _PyErr_Clear
var ClearCurrentExceptionHook func()

// ClearCurrentExceptionIfIterStopHook drops the thread-state's current
// exception only when it is an IndexError or StopIteration, the two
// exception types iteration legitimately swallows: iter_iternext clears
// after PyErr_ExceptionMatches(PyExc_IndexError) and PyIter_Next clears
// after PyErr_ExceptionMatches(PyExc_StopIteration). The gopy iterators
// signal exhaustion with a Go-level ErrStopIteration / errIndexOutOfRange
// and never install that exception on the thread state, so an
// unconditional clear would wipe an unrelated exception that happens to
// be pending (for example one being raised while its repr walks a
// contained sequence). Guarding on the exception type mirrors CPython's
// ExceptionMatches gate.
//
// CPython: Objects/iterobject.c:78 iter_iternext (PyErr_ExceptionMatches + PyErr_Clear)
// CPython: Objects/abstract.c:2852 PyIter_Next (PyErr_ExceptionMatches + PyErr_Clear)
var ClearCurrentExceptionIfIterStopHook func()

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
	if s != nil && s.GetItem != nil {
		v, err := s.GetItem(it.seq, it.index)
		if err != nil {
			if errors.Is(err, errIndexOutOfRange) || errors.Is(err, ErrStopIteration) ||
				(IsIndexErrorHook != nil && IsIndexErrorHook(err)) {
				it.seq = nil
				if ClearCurrentExceptionIfIterStopHook != nil {
					ClearCurrentExceptionIfIterStopHook()
				}
				return nil, ErrStopIteration
			}
			return nil, err
		}
		it.index++
		return v, nil
	}
	// Python-level __getitem__ fallback: call obj.__getitem__(index) and stop
	// on IndexError or StopIteration.
	//
	// CPython: Objects/iterobject.c:73 iter_iternext — same stop conditions
	descr, _ := LookupDescriptor(it.seq.Type(), "__getitem__")
	if descr == nil {
		it.seq = nil
		return nil, ErrStopIteration
	}
	getItem, err := bindDescriptor(descr, it.seq)
	if err != nil {
		it.seq = nil
		return nil, ErrStopIteration
	}
	idx := NewInt(int64(it.index))
	v, err := CallObject(getItem, NewTuple([]Object{idx}))
	if err != nil {
		if errors.Is(err, errIndexOutOfRange) || errors.Is(err, ErrStopIteration) ||
			(IsIndexErrorHook != nil && IsIndexErrorHook(err)) {
			it.seq = nil
			if ClearCurrentExceptionIfIterStopHook != nil {
				ClearCurrentExceptionIfIterStopHook()
			}
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
	CallIterType.Iter = SelfIter
	CallIterType.IterNext = callIterNext
	AddIterSlotWrappers(CallIterType)
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
	// CPython: Objects/iterobject.c:198 calliter_iternext — re-check
	// after call because a reentrant call may have exhausted us.
	if it.callable == nil {
		return nil, ErrStopIteration
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
