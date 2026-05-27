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

// saveSet emits EMPTY_SET + MEMOIZE + MARK + items + ADDITEMS for
// proto >= 4. Earlier protocols would route through save_reduce
// with the set() constructor and a list of items, which Phase 3
// does not need yet (the gate fixes proto=5).
//
// CPython: Modules/_pickle.c:3495 save_set
func (p *pickler) saveSet(s *objects.Set) error {
	if p.proto < 4 {
		return errors.New("PicklingError: set pickling for proto < 4 not implemented")
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

// saveFrozenset emits MARK + items + FROZENSET + MEMOIZE.
//
// CPython's save_frozenset also re-checks the memo after items go in
// to detect recursive frozensets. Frozensets are immutable so a
// self-reference would require an enclosing mutable container that
// later gets memoized as a parent, that path is exotic and not on
// the proto-5 byte-equality gate, so we defer the POP_MARK + memoGet
// branch.
//
// CPython: Modules/_pickle.c:3650 save_frozenset
func (p *pickler) saveFrozenset(s *objects.Set) error {
	if p.proto < 4 {
		return errors.New("PicklingError: frozenset pickling for proto < 4 not implemented")
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
