// WeakValueDictionary ports Lib/weakref.py:92. The dict's keys are
// strong (Python keys are usually hashable immutables), the values
// are weakly referenced. CPython uses a regular dict where each value
// is a KeyedRef carrying its key for O(1) callback removal; gopy
// stores (*objects.Weakref, key) pairs and looks up the key on the
// callback path through a reverse map.
//
// CPython: Lib/weakref.py:92 WeakValueDictionary

package weakref

import (
	"fmt"
	"sync"

	"github.com/tamnd/gopy/module/gc"
	"github.com/tamnd/gopy/objects"
)

// WeakValueDict is the Go-side WeakValueDictionary object.
//
// CPython: Lib/weakref.py:92 WeakValueDictionary
type WeakValueDict struct {
	objects.Header

	mu      sync.Mutex
	data    *objects.Dict                       // Python key -> *objects.Weakref
	byRef   map[*objects.Weakref]objects.Object // wr -> key (for the _remove callback)
	remover *objects.BuiltinFunction
}

// WeakValueDictType is the type singleton for weakref.WeakValueDictionary.
//
// CPython: Lib/weakref.py:92 WeakValueDictionary (class object)
var WeakValueDictType = objects.NewType("WeakValueDictionary", []*objects.Type{objects.ObjectType()})

func init() {
	WeakValueDictType.Repr = weakValueDictRepr
	WeakValueDictType.Str = weakValueDictRepr
	WeakValueDictType.Iter = weakValueDictIter
	WeakValueDictType.Mapping = &objects.MappingMethods{
		Length:  weakValueDictLen,
		GetItem: weakValueDictGetItem,
		SetItem: weakValueDictSetItem,
		DelItem: weakValueDictDelItem,
	}
	WeakValueDictType.TpTraverse = weakValueDictTraverse
}

// NewWeakValueDict builds an empty WeakValueDictionary. Pass nil for
// the no-arg form; pass a dict to seed via the Lib/weakref.py:113
// self.update(other) entry path.
//
// CPython: Lib/weakref.py:104 WeakValueDictionary.__init__
func NewWeakValueDict(other objects.Object) (*WeakValueDict, error) {
	d := &WeakValueDict{
		data:  objects.NewDict(),
		byRef: make(map[*objects.Weakref]objects.Object),
	}
	d.Init(WeakValueDictType)
	d.remover = objects.NewBuiltinFunction("_weakvaluedict_remove", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		if len(args) != 1 {
			return objects.None(), nil
		}
		wr, ok := args[0].(*objects.Weakref)
		if !ok {
			return objects.None(), nil
		}
		d.mu.Lock()
		key, ok := d.byRef[wr]
		if ok {
			delete(d.byRef, wr)
			_ = d.data.DelItem(key)
		}
		d.mu.Unlock()
		return objects.None(), nil
	})
	if other != nil {
		if err := d.Update(other); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Len returns the entry count without filtering dead refs.
//
// CPython: Lib/weakref.py:125 WeakValueDictionary.__len__
func (d *WeakValueDict) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Len()
}

// SetItem stores key -> value, wrapping value in a weakref whose
// callback prunes the entry when value dies.
//
// CPython: Lib/weakref.py:138 WeakValueDictionary.__setitem__
func (d *WeakValueDict) SetItem(key, value objects.Object) error {
	wr := objects.NewWeakref(value, d.remover)
	gc.RegisterWeakref(wr)
	d.mu.Lock()
	if old, err := d.data.GetItem(key); err == nil {
		if oldWr, ok := old.(*objects.Weakref); ok {
			delete(d.byRef, oldWr)
		}
	}
	if err := d.data.SetItem(key, wr); err != nil {
		d.mu.Unlock()
		return err
	}
	d.byRef[wr] = key
	d.mu.Unlock()
	return nil
}

// GetItem returns the live value or KeyError.
//
// CPython: Lib/weakref.py:115 WeakValueDictionary.__getitem__
func (d *WeakValueDict) GetItem(key objects.Object) (objects.Object, error) {
	d.mu.Lock()
	stored, err := d.data.GetItem(key)
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	wr, ok := stored.(*objects.Weakref)
	if !ok {
		return nil, fmt.Errorf("KeyError: %v", key)
	}
	r := wr.Referent()
	if r == objects.None() {
		return nil, fmt.Errorf("KeyError: %v", key)
	}
	return r, nil
}

// DelItem removes key.
//
// CPython: Lib/weakref.py:122 WeakValueDictionary.__delitem__
func (d *WeakValueDict) DelItem(key objects.Object) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	stored, err := d.data.GetItem(key)
	if err != nil {
		return err
	}
	if wr, ok := stored.(*objects.Weakref); ok {
		delete(d.byRef, wr)
	}
	return d.data.DelItem(key)
}

// Contains reports whether key resolves to a live value.
//
// CPython: Lib/weakref.py:128 WeakValueDictionary.__contains__
func (d *WeakValueDict) Contains(key objects.Object) (bool, error) {
	d.mu.Lock()
	stored, err := d.data.GetItem(key)
	d.mu.Unlock()
	if err != nil {
		// Lib/weakref.py:128 swallows the KeyError raised by self.data[key].
		return false, nil //nolint:nilerr // mirrors CPython __contains__
	}
	wr, ok := stored.(*objects.Weakref)
	if !ok {
		return false, nil
	}
	return wr.Referent() != objects.None(), nil
}

// Get returns the live value or default. Mirrors dict.get's no-throw
// shape.
//
// CPython: Lib/weakref.py:160 WeakValueDictionary.get
func (d *WeakValueDict) Get(key, def objects.Object) objects.Object {
	v, err := d.GetItem(key)
	if err != nil {
		return def
	}
	return v
}

// Keys returns a snapshot of the live keys.
//
// CPython: Lib/weakref.py:179 WeakValueDictionary.keys
func (d *WeakValueDict) Keys() []objects.Object {
	d.mu.Lock()
	keys := d.data.Keys()
	d.mu.Unlock()
	out := make([]objects.Object, 0, len(keys))
	for _, k := range keys {
		stored, err := d.data.GetItem(k)
		if err != nil {
			continue
		}
		if wr, ok := stored.(*objects.Weakref); ok && wr.Referent() != objects.None() {
			out = append(out, k)
		}
	}
	return out
}

// Values returns a snapshot of live values, in key-insertion order.
//
// CPython: Lib/weakref.py:198 WeakValueDictionary.values
func (d *WeakValueDict) Values() []objects.Object {
	d.mu.Lock()
	keys := d.data.Keys()
	d.mu.Unlock()
	out := make([]objects.Object, 0, len(keys))
	for _, k := range keys {
		stored, err := d.data.GetItem(k)
		if err != nil {
			continue
		}
		wr, ok := stored.(*objects.Weakref)
		if !ok {
			continue
		}
		if r := wr.Referent(); r != objects.None() {
			out = append(out, r)
		}
	}
	return out
}

// Items returns (key, value) snapshots for live entries.
//
// CPython: Lib/weakref.py:173 WeakValueDictionary.items
func (d *WeakValueDict) Items() [][2]objects.Object {
	d.mu.Lock()
	keys := d.data.Keys()
	d.mu.Unlock()
	out := make([][2]objects.Object, 0, len(keys))
	for _, k := range keys {
		stored, err := d.data.GetItem(k)
		if err != nil {
			continue
		}
		wr, ok := stored.(*objects.Weakref)
		if !ok {
			continue
		}
		if r := wr.Referent(); r != objects.None() {
			out = append(out, [2]objects.Object{k, r})
		}
	}
	return out
}

// Update copies entries from other (a dict-like with .Items() or an
// iterable of (k, v) pairs). gopy's port handles the common case of a
// *objects.Dict.
//
// CPython: Lib/weakref.py:235 WeakValueDictionary.update
func (d *WeakValueDict) Update(other objects.Object) error {
	if od, ok := other.(*objects.Dict); ok {
		for _, k := range od.Keys() {
			v, err := od.GetItem(k)
			if err != nil {
				return err
			}
			if err := d.SetItem(k, v); err != nil {
				return err
			}
		}
		return nil
	}
	if owd, ok := other.(*WeakValueDict); ok {
		for _, kv := range owd.Items() {
			if err := d.SetItem(kv[0], kv[1]); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("TypeError: WeakValueDictionary.update requires a mapping")
}

func weakValueDictLen(o objects.Object) (int, error) {
	return o.(*WeakValueDict).Len(), nil
}

func weakValueDictGetItem(o, key objects.Object) (objects.Object, error) {
	return o.(*WeakValueDict).GetItem(key)
}

func weakValueDictSetItem(o, key, value objects.Object) error {
	return o.(*WeakValueDict).SetItem(key, value)
}

func weakValueDictDelItem(o, key objects.Object) error {
	return o.(*WeakValueDict).DelItem(key)
}

// weakValueDictTraverse visits the keys (strong) and the stored
// weakrefs themselves, never the live values: that is what gives the
// container its weak semantics for the cycle collector.
//
// CPython: Lib/weakref.py — dict_traverse on a dict whose values are
// PyWeakReferences
func weakValueDictTraverse(o objects.Object, visit objects.Visitor) error {
	d := o.(*WeakValueDict)
	d.mu.Lock()
	keys := d.data.Keys()
	d.mu.Unlock()
	for _, k := range keys {
		if err := visit(k); err != nil {
			return err
		}
		stored, err := d.data.GetItem(k)
		if err != nil {
			continue
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return nil
}

func weakValueDictRepr(o objects.Object) (string, error) {
	d := o.(*WeakValueDict)
	return fmt.Sprintf("<WeakValueDictionary at %p>", d), nil
}

// weakValueDictIterator yields keys for which the value is still live.
//
// CPython: Lib/weakref.py:184 WeakValueDictionary.__iter__ = keys
type weakValueDictIterator struct {
	objects.Header
	keys []objects.Object
	pos  int
}

var weakValueDictIterType = objects.NewType("WeakValueDictionary_keyiterator", []*objects.Type{objects.ObjectType()})

func init() {
	weakValueDictIterType.Iter = func(o objects.Object) (objects.Object, error) { return o, nil }
	weakValueDictIterType.IterNext = func(o objects.Object) (objects.Object, error) {
		it := o.(*weakValueDictIterator)
		if it.pos >= len(it.keys) {
			return nil, objects.ErrStopIteration
		}
		v := it.keys[it.pos]
		it.pos++
		return v, nil
	}
}

func weakValueDictIter(o objects.Object) (objects.Object, error) {
	d := o.(*WeakValueDict)
	it := &weakValueDictIterator{keys: d.Keys()}
	it.Init(weakValueDictIterType)
	return it, nil
}
