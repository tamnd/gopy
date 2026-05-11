package objects

import (
	"errors"
	"fmt"
	"strings"
)

// dictEntry is one slot in the dict's open-addressed table. The slot
// is one of three states: empty (used=false, dummy=false) the probe
// stops here; active (used=true) the key/value/hash fields are live;
// dummy (dummy=true) the slot was deleted but stays in the chain so
// later probes for keys hashed earlier in the chain still find them.
// The dummy goes away on the next resize. CPython tracks the same
// three states via DKIX_EMPTY/DKIX_DUMMY/ix>=0 in its parallel index
// array.
//
// CPython: Objects/dictobject.c:353 PyDictKeyEntry
type dictEntry struct {
	hash  int64
	key   Object
	value Object
	used  bool
	dummy bool
}

// Dict is the Python dict.
//
// CPython: Include/cpython/dictobject.h:19 PyDictObject
type Dict struct {
	Header
	entries    []dictEntry
	order      []int       // slot indices in insertion order; one entry per live key
	used       int         // active entries
	fill       int         // active entries + dummies; only resets on resize
	kind       dictKind    // DictKeysKind: gates the four lookup variants
	sharedKeys *SharedKeys // non-nil while in split-keys mode (1680-D)
	// keysVersion is the dk_version stamped into inline caches by the
	// adaptive specializer. 0 means "not yet allocated or invalidated";
	// GetKeysVersion lazily allocates a fresh value from the global
	// counter on first read. Insert/delete clears it back to 0 so the
	// next read forces a re-allocation and prior cache hits invalidate.
	//
	// CPython: Include/internal/pycore_dict.h dk_version
	keysVersion uint32

	// mutationCount tallies how many times the watcher callback has
	// fired on this dict. The Tier-2 globals/builtins folder reads it
	// to decide whether the dict is too volatile to specialize. CPython
	// stores the same counter in the high bits of _ma_watcher_tag
	// (DICT_WATCHED_MUTATION_BITS); gopy keeps it in its own field.
	//
	// CPython: Include/internal/pycore_dict.h DICT_WATCHED_MUTATION_BITS
	mutationCount uint32
}

// DictType is the type singleton for dict. Mirrors PyDict_Type.
//
// CPython: Objects/dictobject.c:L4023 PyDict_Type
var DictType = NewType("dict", []*Type{objectType})

const dictMinSize = 8

func init() {
	DictType.TpFlags = TpFlagMapping
	DictType.Repr = dictRepr
	DictType.Str = dictRepr
	DictType.Iter = dictIter
	DictType.Mapping = &MappingMethods{
		Length:  dictLen,
		GetItem: dictMappingGet,
		SetItem: dictMappingSet,
		DelItem: dictMappingDel,
	}
	DictType.TpTraverse = dictTraverse
	DictType.Getattro = GenericGetAttr
	SetTypeDescr(DictType, "keys", NewMethodDescr(DictType, "keys", dictKeysMethod))
	SetTypeDescr(DictType, "values", NewMethodDescr(DictType, "values", dictValuesMethod))
	SetTypeDescr(DictType, "items", NewMethodDescr(DictType, "items", dictItemsMethod))
	SetTypeDescr(DictType, "get", NewMethodDescr(DictType, "get", dictGetMethod))
	SetTypeDescr(DictType, "__contains__", NewMethodDescr(DictType, "__contains__", dictContainsMethod))
}

func dictKeysMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: keys() takes no arguments (%d given)", len(args)-1)
	}
	return args[0].(*Dict).KeysView(), nil
}

func dictValuesMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: values() takes no arguments (%d given)", len(args)-1)
	}
	return args[0].(*Dict).ValuesView(), nil
}

func dictItemsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: items() takes no arguments (%d given)", len(args)-1)
	}
	return args[0].(*Dict).ItemsView(), nil
}

// dictGetMethod ports dict_get_impl: dict.get(key, default=None).
//
// CPython: Objects/dictobject.c:3823 dict_get_impl
func dictGetMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: get expected 1 to 2 arguments, got %d", len(args)-1)
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err == nil && v != nil {
		return v, nil
	}
	if len(args) == 3 {
		return args[2], nil
	}
	return None(), nil
}

// dictContainsMethod backs dict.__contains__.
//
// CPython: Objects/dictobject.c:3735 dict_contains
func dictContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err != nil {
		return nil, err
	}
	return NewBool(v != nil), nil
}

// dictTraverse visits every key and every value.
//
// CPython: Objects/dictobject.c:4022 dict_traverse
func dictTraverse(o Object, visit Visitor) error {
	d := o.(*Dict)
	for _, slot := range d.order {
		e := &d.entries[slot]
		if e.key != nil {
			if err := visit(e.key); err != nil {
				return err
			}
		}
		if e.value != nil {
			if err := visit(e.value); err != nil {
				return err
			}
		}
	}
	return nil
}

// NewDict builds an empty dict.
//
// CPython: Objects/dictobject.c:L765 PyDict_New
func NewDict() *Dict {
	d := &Dict{entries: make([]dictEntry, dictMinSize), kind: dictKindUnicode}
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
	for _, slot := range d.order {
		out = append(out, d.entries[slot].key)
	}
	return out
}

// SetItem inserts or replaces key. Mirrors PyDict_SetItem.
//
// CPython: Objects/dictobject.c:1985 PyDict_SetItem
func (d *Dict) SetItem(key, value Object) error {
	h, err := Hash(key)
	if err != nil {
		return err
	}
	return dictInsert(d, h, key, value)
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
// CPython: Objects/dictobject.c:2834 PyDict_DelItem
func (d *Dict) DelItem(key Object) error {
	return dictDelete(d, key)
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

// lookup is the dispatcher all dict ops route through. Picks one of
// the four CPython lookdict variants based on the dict's key-kind
// flag and the lookup key's type. See dict_lookup.go.
//
// CPython: Objects/dictobject.c:1247 _Py_dict_lookup
func (d *Dict) lookup(h int64, key Object) (idx int, found bool, err error) {
	return dispatchLookup(d, key, h)
}

func dictLen(o Object) (int, error) { return o.(*Dict).Len(), nil }

// dictMappingGet is the type-level __getitem__. Mirrors dict_subscript:
// on miss it raises KeyError with the key as the value, so user code
// `except KeyError` catches the failure instead of seeing the raw
// errKeyNotFound sentinel.
//
// CPython: Objects/dictobject.c:2229 dict_subscript
func dictMappingGet(o, key Object) (Object, error) {
	d := o.(*Dict)
	v, err := d.GetItem(key)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			repr, rerr := Repr(key)
			if rerr != nil {
				repr = "?"
			}
			return nil, fmt.Errorf("KeyError: %s", repr)
		}
		return nil, err
	}
	return v, nil
}

func dictMappingSet(o, key, value Object) error { return o.(*Dict).SetItem(key, value) }

func dictMappingDel(o, key Object) error { return o.(*Dict).DelItem(key) }

func dictRepr(o Object) (string, error) {
	d := o.(*Dict)
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, slot := range d.order {
		e := &d.entries[slot]
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
