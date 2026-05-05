package objects

import "strings"

// dictEntry is one slot in the dict's open-addressed table. CPython
// uses a parallel index array + entry array for compact dicts; v0.2
// ships the simpler two-array form and the compact layout arrives
// with the full dictobject.c port in v0.4.
//
// CPython: Objects/dictobject.c:L353 PyDictKeyEntry
type dictEntry struct {
	hash  int64
	key   Object
	value Object
	used  bool
}

// Dict is the Python dict.
//
// CPython: Include/cpython/dictobject.h:L19 PyDictObject
type Dict struct {
	Header
	entries []dictEntry
	used    int
}

// DictType is the type singleton for dict. Mirrors PyDict_Type.
//
// CPython: Objects/dictobject.c:L4023 PyDict_Type
var DictType = NewType("dict", []*Type{objectType})

const dictMinSize = 8

func init() {
	DictType.Repr = dictRepr
	DictType.Str = dictRepr
	DictType.Iter = dictIter
	DictType.Mapping = &MappingMethods{
		Length:  dictLen,
		GetItem: dictMappingGet,
		SetItem: dictMappingSet,
		DelItem: dictMappingDel,
	}
}

// NewDict builds an empty dict.
//
// CPython: Objects/dictobject.c:L765 PyDict_New
func NewDict() *Dict {
	d := &Dict{entries: make([]dictEntry, dictMinSize)}
	d.init(DictType)
	return d
}

// Len returns the number of items.
//
// CPython: Objects/dictobject.c:L3138 PyDict_Size
func (d *Dict) Len() int { return d.used }

// Keys returns a snapshot of the live keys in insertion order. The
// returned slice is owned by the caller.
//
// CPython: Objects/dictobject.c PyDict_Keys
func (d *Dict) Keys() []Object {
	out := make([]Object, 0, d.used)
	for _, e := range d.entries {
		if e.used {
			out = append(out, e.key)
		}
	}
	return out
}

// SetItem inserts or replaces key. Mirrors PyDict_SetItem.
//
// CPython: Objects/dictobject.c:L1985 PyDict_SetItem
func (d *Dict) SetItem(key, value Object) error {
	h, err := Hash(key)
	if err != nil {
		return err
	}
	if d.used*3 >= len(d.entries)*2 {
		d.grow()
	}
	return d.insert(h, key, value)
}

// GetItem looks up key. Returns errKeyNotFound when absent.
//
// CPython: Objects/dictobject.c:L1925 PyDict_GetItemWithError
func (d *Dict) GetItem(key Object) (Object, error) {
	h, err := Hash(key)
	if err != nil {
		return nil, err
	}
	idx, ok, err := d.lookup(h, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errKeyNotFound
	}
	return d.entries[idx].value, nil
}

// DelItem removes key. Mirrors PyDict_DelItem.
//
// CPython: Objects/dictobject.c:L2154 PyDict_DelItem
func (d *Dict) DelItem(key Object) error {
	h, err := Hash(key)
	if err != nil {
		return err
	}
	idx, ok, err := d.lookup(h, key)
	if err != nil {
		return err
	}
	if !ok {
		return errKeyNotFound
	}
	d.entries[idx] = dictEntry{}
	d.used--
	return nil
}

// Contains reports whether key is in the dict.
//
// CPython: Objects/dictobject.c:L2495 PyDict_Contains
func (d *Dict) Contains(key Object) (bool, error) {
	h, err := Hash(key)
	if err != nil {
		return false, err
	}
	_, ok, err := d.lookup(h, key)
	return ok, err
}

func (d *Dict) lookup(h int64, key Object) (idx int, found bool, err error) {
	mask := uint64(len(d.entries) - 1)
	i := uint64(h) & mask
	perturb := uint64(h)
	for {
		e := &d.entries[i]
		if !e.used {
			return int(i), false, nil
		}
		if e.hash == h {
			eq, err := RichCmpBool(e.key, key, CompareEQ)
			if err != nil {
				return 0, false, err
			}
			if eq {
				return int(i), true, nil
			}
		}
		// CPython probing: i = (5*i + 1 + perturb) & mask; perturb >>= 5.
		// CPython: Objects/dictobject.c:L171 PERTURB_SHIFT.
		perturb >>= 5
		i = (5*i + 1 + perturb) & mask
	}
}

func (d *Dict) insert(h int64, key, value Object) error {
	idx, ok, err := d.lookup(h, key)
	if err != nil {
		return err
	}
	d.entries[idx] = dictEntry{hash: h, key: key, value: value, used: true}
	if !ok {
		d.used++
	}
	return nil
}

func (d *Dict) grow() {
	old := d.entries
	d.entries = make([]dictEntry, len(old)*2)
	d.used = 0
	for _, e := range old {
		if !e.used {
			continue
		}
		_ = d.insert(e.hash, e.key, e.value)
	}
}

func dictLen(o Object) (int, error) { return o.(*Dict).Len(), nil }

func dictMappingGet(o, key Object) (Object, error) { return o.(*Dict).GetItem(key) }

func dictMappingSet(o, key, value Object) error { return o.(*Dict).SetItem(key, value) }

func dictMappingDel(o, key Object) error { return o.(*Dict).DelItem(key) }

func dictRepr(o Object) (string, error) {
	d := o.(*Dict)
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, e := range d.entries {
		if !e.used {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		ks, err := Repr(e.key)
		if err != nil {
			return "", err
		}
		vs, err := Repr(e.value)
		if err != nil {
			return "", err
		}
		b.WriteString(ks)
		b.WriteString(": ")
		b.WriteString(vs)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// dictIterator iterates the dict's keys in insertion order.
//
// CPython: Objects/dictobject.c:L4983 PyDictIterKey_Type
type dictIterator struct {
	Header
	src  *Dict
	keys []Object
	pos  int
}

var dictIterType = NewType("dict_keyiterator", []*Type{objectType})

func init() {
	dictIterType.Iter = func(o Object) (Object, error) { return o, nil }
	dictIterType.IterNext = func(o Object) (Object, error) {
		it := o.(*dictIterator)
		if it.pos >= len(it.keys) {
			return nil, ErrStopIteration
		}
		v := it.keys[it.pos]
		it.pos++
		return v, nil
	}
}

func dictIter(o Object) (Object, error) {
	d := o.(*Dict)
	keys := make([]Object, 0, d.used)
	for _, e := range d.entries {
		if e.used {
			keys = append(keys, e.key)
		}
	}
	it := &dictIterator{src: d, keys: keys}
	it.init(dictIterType)
	return it, nil
}
