// Enumerate and Reversed back the builtin enumerate() and reversed()
// constructors. They are iterators in their own right rather than
// generators, which is why they live next to the other iterator
// adapters.
//
// CPython: Python/bltinmodule.c:1455 enum_type
// CPython: Python/bltinmodule.c:2724 PyReversed_Type

package objects

import (
	"errors"
	"math/big"
)

// Enumerate backs enumerate(iterable, start=0). Index uses big.Int so
// it stays correct past 2**63 the way CPython's enumobject does.
//
// CPython: Python/bltinmodule.c:1390 enumobject
type Enumerate struct {
	Header
	iter  Object
	index *big.Int
}

// EnumerateType is the type singleton for enumerate.
//
// CPython: Python/bltinmodule.c:1455 PyEnum_Type
var EnumerateType = NewType("enumerate", []*Type{objectType})

func init() {
	EnumerateType.Iter = SelfIter
	EnumerateType.IterNext = enumerateNext
	AddIterSlotWrappers(EnumerateType)
}

// NewEnumerate wraps an iterable as enumerate(iterable, start). The
// iterable must expose a tp_iter slot.
//
// CPython: Python/bltinmodule.c:1404 enum_new_impl
func NewEnumerate(iterable Object, start *Int) (*Enumerate, error) {
	if iterable.Type().Iter == nil {
		return nil, errors.New("TypeError: 'enumerate' requires an iterable")
	}
	it, err := iterable.Type().Iter(iterable)
	if err != nil {
		return nil, err
	}
	idx := new(big.Int)
	if start != nil {
		idx.Set(&start.v)
	}
	e := &Enumerate{iter: it, index: idx}
	e.init(EnumerateType)
	return e, nil
}

// enumerateNext mirrors enum_next: fetch next item from the inner
// iterator, return (index, item) tuple, bump index. Stops when the
// inner iterator stops.
//
// CPython: Python/bltinmodule.c:1438 enum_next
func enumerateNext(o Object) (Object, error) {
	e := o.(*Enumerate)
	if e.iter == nil {
		return nil, ErrStopIteration
	}
	next := e.iter.Type().IterNext
	if next == nil {
		e.iter = nil
		return nil, ErrStopIteration
	}
	v, err := next(e.iter)
	if err != nil {
		if errors.Is(err, ErrStopIteration) {
			e.iter = nil
		}
		return nil, err
	}
	idx := NewIntFromBig(e.index)
	e.index = new(big.Int).Add(e.index, big.NewInt(1))
	return NewTuple([]Object{idx, v}), nil
}

// Reversed backs reversed(seq). It walks the sequence backward via
// the SequenceMethods.GetItem slot, decrementing index from len-1.
//
// CPython: Python/bltinmodule.c:2627 reversedobject
type Reversed struct {
	Header
	seq   Object
	index int
}

// ReversedType is the type singleton for reversed.
//
// CPython: Python/bltinmodule.c:2724 PyReversed_Type
var ReversedType = NewType("reversed", []*Type{objectType})

func init() {
	ReversedType.Iter = SelfIter
	ReversedType.IterNext = reversedNext
	AddIterSlotWrappers(ReversedType)
}

// NewReversed wraps a sequence as reversed(seq). The sequence must
// expose Sequence.Length and Sequence.GetItem; otherwise an error is
// returned. CPython also routes through __reversed__ when present;
// that hook isn't wired in gopy yet.
//
// CPython: Python/bltinmodule.c:2654 reversed_new_impl
func NewReversed(seq Object) (*Reversed, error) {
	s := seq.Type().Sequence
	if s == nil || s.Length == nil || s.GetItem == nil {
		return nil, errors.New("TypeError: argument to reversed() must be a sequence")
	}
	n, err := s.Length(seq)
	if err != nil {
		return nil, err
	}
	r := &Reversed{seq: seq, index: n - 1}
	r.init(ReversedType)
	return r, nil
}

// reversedNext mirrors reversed_next: return seq[index], decrement;
// stop when index falls below 0.
//
// CPython: Python/bltinmodule.c:2696 reversed_next
func reversedNext(o Object) (Object, error) {
	r := o.(*Reversed)
	if r.index < 0 || r.seq == nil {
		return nil, ErrStopIteration
	}
	s := r.seq.Type().Sequence
	v, err := s.GetItem(r.seq, r.index)
	if err != nil {
		if errors.Is(err, errIndexOutOfRange) || errors.Is(err, ErrStopIteration) {
			r.seq = nil
			return nil, ErrStopIteration
		}
		return nil, err
	}
	r.index--
	return v, nil
}
