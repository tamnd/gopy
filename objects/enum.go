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
	"fmt"
	"math/big"
)

// Enumerate backs enumerate(iterable, start=0). Index uses big.Int so
// it stays correct past 2**63 the way CPython's enumobject does.
//
// CPython: Objects/enumobject.c:18 enumobject
type Enumerate struct {
	Header
	iter  Object
	index *big.Int
}

// EnumerateType is the type singleton for enumerate.
//
// CPython: Objects/enumobject.c:319 PyEnum_Type
var EnumerateType = NewType("enumerate", []*Type{objectType})

func init() {
	EnumerateType.Iter = SelfIter
	EnumerateType.IterNext = enumerateNext
	AddIterSlotWrappers(EnumerateType)
	SetTypeDescr(EnumerateType, "__reduce__", NewMethodDescrConv(EnumerateType, "__reduce__", MethNoArgs, enumerateReduce))
}

// NewEnumerateOfType is the gopy port of enum_new_impl. It builds an
// enumerate of the given (sub)type around an already-constructed
// iterator and a starting index. CPython allocates with type->tp_alloc
// so a subclass instance keeps its own type, which is why cls is
// threaded through here rather than hardwired to EnumerateType.
//
// CPython: Objects/enumobject.c:48 enum_new_impl
func NewEnumerateOfType(cls *Type, iter Object, index *big.Int) *Enumerate {
	idx := new(big.Int)
	if index != nil {
		idx.Set(index)
	}
	e := &Enumerate{iter: iter, index: idx}
	e.init(cls)
	return e
}

// NewEnumerate wraps an iterable as enumerate(iterable, start). The
// iterable must expose a tp_iter slot.
//
// CPython: Objects/enumobject.c:48 enum_new_impl
func NewEnumerate(iterable Object, start *Int) (*Enumerate, error) {
	if iterable.Type().Iter == nil {
		return nil, errors.New("TypeError: 'enumerate' requires an iterable")
	}
	it, err := iterable.Type().Iter(iterable)
	if err != nil {
		return nil, err
	}
	var idx *big.Int
	if start != nil {
		idx = &start.v
	}
	return NewEnumerateOfType(EnumerateType, it, idx), nil
}

// Iter exposes the wrapped secondary iterator (en_sit) so the builtin
// pickle path can rebuild the enumerate.
func (e *Enumerate) Iter() Object { return e.iter }

// Index exposes the current enumeration counter (en_index) for pickling.
func (e *Enumerate) Index() *big.Int { return e.index }

// enumerateReduce ports enum_reduce: pickle an enumerate as
// (type(self), (en_sit, en_index)) so unpickling re-runs the
// constructor with the live sub-iterator and resumes counting.
//
// CPython: Objects/enumobject.c:288 enum_reduce
func enumerateReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments (%d given)", len(args)-1)
	}
	e, ok := args[0].(*Enumerate)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__reduce__' for 'enumerate' objects doesn't apply to a '%s' object", args[0].Type().Name)
	}
	inner := NewTuple([]Object{e.iter, NewIntFromBig(e.index)})
	return NewTuple([]Object{e.Type(), inner}), nil
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
	ReversedType.Dealloc = reversedDealloc
	ReversedType.TpTraverse = reversedTraverse
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
	// reversed() holds a counted reference to the sequence, so a freshly
	// built temporary (cls.__mro__ passed straight into reversed()) survives
	// until the iterator is exhausted instead of being reaped after the call.
	// CPython: Python/bltinmodule.c:2654 reversed_new_impl (Py_NewRef(seq))
	Incref(seq)
	r := &Reversed{seq: seq, index: n - 1}
	r.init(ReversedType)
	return r, nil
}

// reversedDealloc drops the held sequence reference.
//
// CPython: Python/bltinmodule.c:2638 reversed_dealloc
func reversedDealloc(o Object) {
	r, ok := o.(*Reversed)
	if !ok {
		return
	}
	if r.seq != nil {
		Decref(r.seq)
		r.seq = nil
	}
}

// reversedTraverse visits the held sequence so the cyclic collector can
// trace cycles that run through it.
//
// CPython: Python/bltinmodule.c:2647 reversed_traverse
func reversedTraverse(o Object, visit Visitor) error {
	r := o.(*Reversed)
	if r.seq == nil {
		return nil
	}
	return visit(r.seq)
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
			Decref(r.seq)
			r.seq = nil
			return nil, ErrStopIteration
		}
		return nil, err
	}
	r.index--
	if r.index < 0 {
		// Exhausted: drop the sequence reference eagerly, matching
		// reversed_next which clears it_seq once the index goes negative.
		// CPython: Python/bltinmodule.c:2696 reversed_next
		Decref(r.seq)
		r.seq = nil
	}
	return v, nil
}
