// Container savers. Phase 3 of P14.1 covers the proto-5 binary
// write path for list / tuple / dict / set / frozenset. Each writer
// is a 1:1 port of the matching CPython save_* function so that
// `pickle.dumps(value, protocol=5)` returns byte-identical output.
//
// CPython: Modules/_pickle.c:2847 save_tuple
// CPython: Modules/_pickle.c:3135 save_list
// CPython: Modules/_pickle.c:3428 save_dict
// CPython: Modules/_pickle.c:3495 save_set
// CPython: Modules/_pickle.c:3650 save_frozenset

package _pickle

import (
	"errors"
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// saveList emits EMPTY_LIST + MEMOIZE + (optional APPENDS batch).
// The empty-list short form is `5d 94` (EMPTY_LIST, MEMOIZE). Non-
// empty lists batch items into MARK ... APPENDS chunks of BATCHSIZE
// so very long lists do not blow the unpickler stack.
//
// CPython: Modules/_pickle.c:3135 save_list
func (p *pickler) saveList(l *objects.List) error {
	p.writeByte(opEmptyList)
	if err := p.memoPut(l); err != nil {
		return err
	}
	n := l.Len()
	if n == 0 {
		return nil
	}
	return p.batchListExact(l)
}

// batchListExact writes MARK + items + APPENDS in BATCHSIZE chunks
// for a built-in list. The single-item special case skips MARK and
// just emits item + APPEND.
//
// CPython: Modules/_pickle.c:3080 batch_list_exact
func (p *pickler) batchListExact(l *objects.List) error {
	n := l.Len()
	if n == 1 {
		if err := p.save(l.Item(0)); err != nil {
			return err
		}
		p.writeByte(opAppend)
		return nil
	}
	total := 0
	for total < n {
		p.writeByte(opMark)
		batch := 0
		for total < n {
			if err := p.save(l.Item(total)); err != nil {
				return err
			}
			total++
			batch++
			if batch == batchSize {
				break
			}
		}
		p.writeByte(opAppends)
	}
	return nil
}

// saveTuple picks the narrowest tuple opcode. Empty -> EMPTY_TUPLE
// (and no MEMOIZE, matching CPython's behavior: a zero-length tuple
// is a singleton in CPython, so memoizing it would waste a slot).
// Sizes 1..3 -> save items then TUPLE1 / TUPLE2 / TUPLE3 then MEMOIZE.
// Sizes >3 -> MARK + items + TUPLE + MEMOIZE.
//
// CPython's save_tuple also re-checks the memo after saving the items
// to detect recursive tuples. Self-referential tuples are vanishingly
// rare and never happen for atoms, so we omit that pop-and-refetch
// branch for now (it would require POP / POP_MARK + memoGet) and
// document the gap. The byte-equality gate covers everything except
// recursive tuples.
//
// CPython: Modules/_pickle.c:2847 save_tuple
func (p *pickler) saveTuple(t *objects.Tuple) error {
	n := t.Len()
	if n == 0 {
		p.writeByte(opEmptyTuple)
		return nil
	}
	if n <= 3 {
		for i := 0; i < n; i++ {
			if err := p.save(t.Item(i)); err != nil {
				return err
			}
		}
		switch n {
		case 1:
			p.writeByte(opTuple1)
		case 2:
			p.writeByte(opTuple2)
		case 3:
			p.writeByte(opTuple3)
		}
		return p.memoPut(t)
	}
	p.writeByte(opMark)
	for i := 0; i < n; i++ {
		if err := p.save(t.Item(i)); err != nil {
			return err
		}
	}
	p.writeByte(opTuple)
	return p.memoPut(t)
}

// saveDict emits EMPTY_DICT + MEMOIZE + (optional SETITEMS batch).
// Iteration follows insertion order so the wire stream matches what
// CPython's PyDict_Next walks.
//
// CPython: Modules/_pickle.c:3428 save_dict
func (p *pickler) saveDict(d *objects.Dict) error {
	p.writeByte(opEmptyDict)
	if err := p.memoPut(d); err != nil {
		return err
	}
	if d.Len() == 0 {
		return nil
	}
	return p.batchDictExact(d)
}

// batchDictExact writes MARK + (key value)... + SETITEMS in
// BATCHSIZE chunks. The single-item special case skips MARK and
// emits key + value + SETITEM.
//
// CPython: Modules/_pickle.c:3356 batch_dict_exact
func (p *pickler) batchDictExact(d *objects.Dict) error {
	keys := d.Keys()
	if len(keys) == 1 {
		k := keys[0]
		v, err := d.GetItem(k)
		if err != nil {
			return err
		}
		if err := p.save(k); err != nil {
			return err
		}
		if err := p.save(v); err != nil {
			return err
		}
		p.writeByte(opSetitem)
		return nil
	}
	i := 0
	for i < len(keys) {
		p.writeByte(opMark)
		batch := 0
		for i < len(keys) {
			k := keys[i]
			v, err := d.GetItem(k)
			if err != nil {
				return err
			}
			if err := p.save(k); err != nil {
				return err
			}
			if err := p.save(v); err != nil {
				return err
			}
			i++
			batch++
			if batch == batchSize {
				break
			}
		}
		p.writeByte(opSetitems)
	}
	return nil
}

// batchListIter consumes a Python iterator of items and emits them as
// MARK ... APPENDS chunks (or a lone APPEND for a single item), exactly
// like CPython's batch_list. This drives the listitems slot (4th
// element) of a reduce value so a list subclass reconstructed via
// __newobj__ regains its contents.
//
// CPython: Modules/_pickle.c:2967 batch_list
func (p *pickler) batchListIter(it objects.Object) error {
	return p.batchIter(it, p.save, opAppend, opAppends)
}

// batchDictIter consumes a Python iterator of (key, value) pairs and
// emits them as MARK key value ... SETITEMS chunks (or a lone SETITEM
// for a single pair), like CPython's batch_dict. This drives the
// dictitems slot (5th element) of a reduce value.
//
// CPython: Modules/_pickle.c:3208 batch_dict
func (p *pickler) batchDictIter(it objects.Object) error {
	return p.batchIter(it, p.saveDictPair, opSetitem, opSetitems)
}

// saveDictPair saves a single (key, value) pair from the dictitems
// iterator. CPython requires each element to be a 2-tuple.
//
// CPython: Modules/_pickle.c:3208 batch_dict
func (p *pickler) saveDictPair(o objects.Object) error {
	pair, ok := o.(*objects.Tuple)
	if !ok || pair.Len() != 2 {
		return fmt.Errorf("PicklingError: dictitems iterator must return 2-tuples")
	}
	if err := p.save(pair.Item(0)); err != nil {
		return err
	}
	return p.save(pair.Item(1))
}

// batchIter is the shared engine behind batch_list and batch_dict. It
// pulls elements from a Python iterator and writes them with saveOne,
// emitting a lone singleOp for a single trailing element or MARK ...
// batchOp chunks of BATCHSIZE otherwise.
//
// CPython: Modules/_pickle.c:2967 batch_list / 3208 batch_dict
func (p *pickler) batchIter(it objects.Object, saveOne func(objects.Object) error, singleOp, batchOp byte) error {
	for {
		first, err := nextOrStop(it)
		if err != nil {
			return err
		}
		if first == nil {
			return nil // exhausted
		}
		second, err := nextOrStop(it)
		if err != nil {
			return err
		}
		if second == nil {
			// Exactly one element left.
			if err := saveOne(first); err != nil {
				return err
			}
			p.writeByte(singleOp)
			return nil
		}
		n, err := p.batchChunk(it, saveOne, batchOp, first, second)
		if err != nil {
			return err
		}
		if n < batchSize {
			return nil
		}
	}
}

// batchChunk writes one MARK ... batchOp chunk of up to BATCHSIZE
// elements, beginning with the two already-pulled elements, and returns
// the count written.
func (p *pickler) batchChunk(it objects.Object, saveOne func(objects.Object) error, batchOp byte, first, second objects.Object) (int, error) {
	p.writeByte(opMark)
	if err := saveOne(first); err != nil {
		return 0, err
	}
	n := 1
	cur := second
	for cur != nil {
		if err := saveOne(cur); err != nil {
			return 0, err
		}
		n++
		if n == batchSize {
			break
		}
		var err error
		cur, err = nextOrStop(it)
		if err != nil {
			return 0, err
		}
	}
	p.writeByte(batchOp)
	return n, nil
}

// nextOrStop pulls the next value from a Python iterator, returning
// (nil, nil) once the iterator is exhausted so callers can loop without
// matching on the StopIteration sentinel themselves.
func nextOrStop(it objects.Object) (objects.Object, error) {
	v, err := objects.IterNext(it)
	if err != nil {
		if errors.Is(err, objects.ErrStopIteration) {
			return nil, nil
		}
		return nil, err
	}
	return v, nil
}

// callReduce calls __reduce__ on obj and returns the resulting tuple.
func (p *pickler) callReduce(obj objects.Object) (*objects.Tuple, error) {
	fn, err := objects.GetAttr(obj, objects.NewStr("__reduce__"))
	if err != nil {
		return nil, fmt.Errorf("PicklingError: cannot find __reduce__: %w", err)
	}
	result, err := objects.CallNoArgs(fn)
	if err != nil {
		return nil, fmt.Errorf("PicklingError: __reduce__ failed: %w", err)
	}
	rv, ok := result.(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("PicklingError: __reduce__ must return a tuple, not %s", result.Type().Name)
	}
	return rv, nil
}

// saveSet emits EMPTY_SET + MEMOIZE + MARK + items + ADDITEMS for
// proto >= 4. For proto < 4, falls through to save_reduce using the
// set.__reduce__ 3-tuple (set, ([list],), state).
//
// CPython: Modules/_pickle.c:3495 save_set
func (p *pickler) saveSet(s *objects.Set) error {
	if p.proto < 4 {
		rv, err := p.callReduce(s)
		if err != nil {
			return err
		}
		return p.saveReduceTuple(rv, s)
	}
	p.writeByte(opEmptySet)
	if err := p.memoPut(s); err != nil {
		return err
	}
	items := s.Items()
	if len(items) == 0 {
		return nil
	}
	i := 0
	for i < len(items) {
		p.writeByte(opMark)
		batch := 0
		for i < len(items) {
			if err := p.save(items[i]); err != nil {
				return err
			}
			i++
			batch++
			if batch == batchSize {
				break
			}
		}
		p.writeByte(opAdditems)
	}
	return nil
}

// saveFrozenset emits MARK + items + FROZENSET + MEMOIZE for proto >= 4.
// For proto < 4, falls through to save_reduce using frozenset.__reduce__.
//
// CPython: Modules/_pickle.c:3650 save_frozenset
func (p *pickler) saveFrozenset(s *objects.Set) error {
	if p.proto < 4 {
		rv, err := p.callReduce(s)
		if err != nil {
			return err
		}
		return p.saveReduceTuple(rv, s)
	}
	p.writeByte(opMark)
	for _, item := range s.Items() {
		if err := p.save(item); err != nil {
			return err
		}
	}
	p.writeByte(opFrozenset)
	return p.memoPut(s)
}
