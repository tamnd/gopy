package objects

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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
	// splitValues is the per-instance value array a split dict carries
	// in place of writing into d.entries[].value. Aligned with
	// sharedKeys.entries by slot index: splitValues[i] is the value
	// this instance stores for the key at slot i, or nil when this
	// instance hasn't set that attribute. Allocated by NewSplitDict
	// and cleared by ensureCombined; nil in combined mode.
	//
	// CPython: Include/internal/pycore_dict.h PyDictValues
	splitValues []Object
	// attrs holds instance attributes for dict subclass objects. Nil for
	// plain dict instances; allocated by dictSubclassSetAttr when first
	// written. Mirrors CPython's tp_dictoffset on dict subclasses.
	attrs *Dict
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

	// watcherTag mirrors CPython's _ma_watcher_tag. Bits 0..7 (one per
	// DICT_MAX_WATCHERS slot) flag which watchers have subscribed via
	// PyDict_Watch. Notification iterates the set bits and dispatches
	// through the package-level watcher table.
	//
	// CPython: Include/cpython/dictobject.h:23 _ma_watcher_tag
	watcherTag uint64
}

// DictType is the type singleton for dict. Mirrors PyDict_Type.
//
// CPython: Objects/dictobject.c:L4023 PyDict_Type
var DictType = NewType("dict", []*Type{objectType})

const dictMinSize = 8

func init() {
	DictType.TpFlags |= TpFlagMapping | TpFlagMatchSelf
	DictType.Repr = dictRepr
	DictType.Str = dictRepr
	DictType.Iter = dictIter
	DictType.RichCmp = dictRichCmp
	DictType.Mapping = &MappingMethods{
		Length:  dictLen,
		GetItem: dictMappingGet,
		SetItem: dictMappingSet,
		DelItem: dictMappingDel,
	}
	DictType.TpTraverse = dictTraverse
	DictType.Getattro = GenericGetAttr
	// TpNew creates a *Dict even for subclasses, so dict methods work
	// on subclass instances without type-asserting to *Instance.
	//
	// CPython: Objects/dictobject.c:4023 PyDict_Type (tp_new = dict_new)
	DictType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		d := NewDict()
		d.init(cls)
		return d, nil
	}
	// dict.__new__ slot wrapper. CPython installs tp_new_wrapper for every
	// type whose tp_new is not NULL; the wrapper calls type->tp_new(subtype,
	// args, kwds). Without this, dict.__new__(SubClass) falls through to
	// objectNewBuiltin which creates *Instance instead of *Dict, breaking
	// dict subclasses that define their own __new__ (e.g. collections.OrderedDict).
	//
	// CPython: Objects/typeobject.c:9952 tp_new_wrapper / add_tp_new_wrapper
	SetTypeDescr(DictType, "__new__", NewBuiltinFunction("dict.__new__", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: dict.__new__(): not enough arguments")
		}
		cls, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: dict.__new__(X): X is not a type object (%s)", typeNameOf(args[0]))
		}
		return DictType.TpNew(cls, args[1:], kwargs)
	}))
	// dict.__repr__ slot wrapper (tp_repr add_operators path).
	//
	// CPython: Objects/typeobject.c add_operators
	SetTypeDescr(DictType, "__repr__", NewMethodDescr(DictType, "__repr__", dictReprMethod))
	SetTypeDescr(DictType, "keys", NewMethodDescr(DictType, "keys", dictKeysMethod))
	SetTypeDescr(DictType, "values", NewMethodDescr(DictType, "values", dictValuesMethod))
	SetTypeDescr(DictType, "items", NewMethodDescr(DictType, "items", dictItemsMethod))
	SetTypeDescr(DictType, "get", NewMethodDescr(DictType, "get", dictGetMethod))
	SetTypeDescr(DictType, "__contains__", NewMethodDescr(DictType, "__contains__", dictContainsMethod))
	SetTypeDescr(DictType, "__getitem__", NewMethodDescr(DictType, "__getitem__", dictGetItemMethod))
	SetTypeDescr(DictType, "__setitem__", NewMethodDescr(DictType, "__setitem__", dictSetItemMethod))
	SetTypeDescr(DictType, "__delitem__", NewMethodDescr(DictType, "__delitem__", dictDelItemMethod))
	SetTypeDescr(DictType, "__len__", NewMethodDescr(DictType, "__len__", dictLenMethod))
	SetTypeDescr(DictType, "__eq__", NewMethodDescr(DictType, "__eq__", dictEqMethod))
	SetTypeDescr(DictType, "clear", NewMethodDescr(DictType, "clear", dictClearMethod))
	SetTypeDescr(DictType, "pop", NewMethodDescr(DictType, "pop", dictPopMethod))
	SetTypeDescr(DictType, "update", NewMethodDescr(DictType, "update", dictUpdateMethod))
	SetTypeDescr(DictType, "copy", NewMethodDescr(DictType, "copy", dictCopyMethod))
	SetTypeDescr(DictType, "setdefault", NewMethodDescr(DictType, "setdefault", dictSetDefaultMethod))
	SetTypeDescr(DictType, "fromkeys", NewClassMethod(NewBuiltinFunction("fromkeys", dictFromKeysMethod)))
	SetTypeDescr(DictType, "popitem", NewMethodDescr(DictType, "popitem", dictPopItemMethod))
	// __iter__ slot wrapper. CPython's add_operators installs this for
	// every type with a non-NULL tp_iter; without it, things like
	// `dict.__iter__(d)` and CrazyDict's `for x in self.d.__iter__()`
	// raise AttributeError.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_iter
	AddIterSlotWrappers(DictType)
}

// dictReprMethod is the slot wrapper for tp_repr. Binding it as a
// descriptor keeps `dict.__repr__` distinct from `object.__repr__` so
// pprint's dispatch table (keyed on type.__repr__) does not collapse
// dict/list/deque onto the same entry.
//
// CPython: Objects/typeobject.c add_operators slot wrapper for tp_repr
func dictReprMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __repr__() takes no arguments (%d given)", len(args)-1)
	}
	d, ok := args[0].(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__repr__' requires a 'dict' object")
	}
	s, err := dictRepr(d)
	if err != nil {
		return nil, err
	}
	return NewStr(s), nil
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

// dictContainsMethod backs dict.__contains__. CPython's dict_contains
// returns 0/1/-1: missing keys produce 0, not a raised KeyError. The
// equivalent here is to swallow the errKeyNotFound sentinel and report
// False; anything else (e.g. a TypeError from an unhashable key) bubbles
// up.
//
// CPython: Objects/dictobject.c:3735 dict_contains
func dictContainsMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __contains__() takes exactly one argument (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return NewBool(false), nil
		}
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
		k := d.slotKey(slot)
		if k != nil {
			if err := visit(k); err != nil {
				return err
			}
		}
		if v := d.slotValue(slot); v != nil {
			if err := visit(v); err != nil {
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
		out = append(out, d.slotKey(slot))
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
	return d.slotValue(idx), nil
}

// GetItemKnownHash is GetItem with a caller-supplied hash. The
// LOAD_NAME / LOAD_GLOBAL slow arm threads the *Unicode's cached
// hash straight in, skipping the PyObject_Hash vtable dispatch.
//
// CPython: Objects/dictobject.c:1965 _PyDict_GetItem_KnownHash
func (d *Dict) GetItemKnownHash(key Object, h int64) (Object, error) {
	idx, ok, err := d.lookup(h, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errKeyNotFound
	}
	return d.slotValue(idx), nil
}

// ContainsKnownHash is Contains with a caller-supplied hash.
//
// CPython: Objects/dictobject.c:2530 _PyDict_Contains_KnownHash
func (d *Dict) ContainsKnownHash(key Object, h int64) (bool, error) {
	_, ok, err := d.lookup(h, key)
	return ok, err
}

// SetItemKnownHash is SetItem with a caller-supplied hash.
//
// CPython: Objects/dictobject.c:2069 _PyDict_SetItem_KnownHash
func (d *Dict) SetItemKnownHash(key, value Object, h int64) error {
	return dictInsert(d, h, key, value)
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
// Split-mode wrapper: dispatchLookup uses the shared entries table,
// which only tells us whether the key name is in the shared set. The
// per-instance value lives in splitValues; an empty splitValues slot
// means this instance doesn't carry that attribute, so we override
// found=false even though the keys table reported a hit. Insert and
// delete paths inspect found+splitValues directly, so they're not
// fooled by the override.
//
// CPython: Objects/dictobject.c:1247 _Py_dict_lookup
func (d *Dict) lookup(h int64, key Object) (int, bool, error) {
	idx, found, err := dispatchLookup(d, key, h)
	if err != nil || !found {
		return idx, found, err
	}
	if d.sharedKeys != nil && d.splitValues[idx] == nil {
		return idx, false, nil
	}
	return idx, true, nil
}

func dictLen(o Object) (int, error) { return o.(*Dict).Len(), nil }

// dictMappingGet is the type-level __getitem__. Mirrors dict_subscript:
// on miss it raises KeyError with the key as the value, so user code
// `except KeyError` catches the failure instead of seeing the raw
// errKeyNotFound sentinel. For dict subclasses, calls __missing__(key)
// on a cache miss before raising KeyError.
//
// CPython: Objects/dictobject.c:2229 dict_subscript
func dictMappingGet(o, key Object) (Object, error) {
	d := o.(*Dict)
	v, err := d.GetItem(key)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			// For dict subclasses, invoke __missing__ before raising KeyError.
			// CPython: Objects/dictobject.c:2242 (non-exact dict __missing__ path)
			if d.Type() != DictType {
				if missingFn, merr := GetAttr(o, NewStr("__missing__")); merr == nil && missingFn != nil {
					return CallOneArg(missingFn, key)
				}
			}
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

// dictSubclassGetAttr is the tp_getattro slot for user-defined dict
// subclasses. The instance is a *Dict (not *Instance), so we look in
// d.attrs for per-instance attributes before walking the type MRO.
//
// CPython: Objects/typeobject.c:5165 slot_tp_getattr_hook (dict path)
func dictSubclassGetAttr(o Object, name Object) (Object, error) {
	d, ok := o.(*Dict)
	if !ok {
		return GenericGetAttr(o, name)
	}
	tp := d.Type()
	// Type-level data descriptors win over instance attrs.
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			if descr.Type().DescrSet != nil {
				// data descriptor
				return dget(descr, o, tp)
			}
		}
	}
	// Instance attrs from d.attrs.
	if d.attrs != nil {
		if v, err := d.attrs.GetItem(name); err == nil {
			return v, nil
		}
	}
	// Non-data descriptors and class attrs.
	if descr != nil {
		if dget := descr.Type().DescrGet; dget != nil {
			return dget(descr, o, tp)
		}
		return descr, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
}

// dictSubclassSetAttr is the tp_setattro slot for user-defined dict
// subclasses. Instance attributes land in d.attrs.
//
// CPython: Objects/object.c:2040 PyObject_GenericSetAttr (dict-subclass path)
func dictSubclassSetAttr(o Object, name Object, value Object) error {
	d, ok := o.(*Dict)
	if !ok {
		return GenericSetAttr(o, name, value)
	}
	tp := d.Type()
	nameStr := attrNameStr(name)
	// Type-level data descriptors take priority.
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	// Store in instance attrs.
	if d.attrs == nil {
		d.attrs = NewDict()
	}
	if value == nil {
		if _, err := d.attrs.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, nameStr)
		}
		return d.attrs.DelItem(name)
	}
	return d.attrs.SetItem(name, value)
}

// dictGetItemMethod backs dict.__getitem__.
//
// CPython: Objects/dictobject.c:2229 dict_subscript
func dictGetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __getitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	return dictMappingGet(args[0], args[1])
}

// dictSetItemMethod backs dict.__setitem__.
//
// CPython: Objects/dictobject.c:2266 dict_ass_sub (set branch)
func dictSetItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: __setitem__() takes exactly 2 arguments (%d given)", len(args)-1)
	}
	return None(), args[0].(*Dict).SetItem(args[1], args[2])
}

// dictDelItemMethod backs dict.__delitem__.
//
// CPython: Objects/dictobject.c:2266 dict_ass_sub (del branch)
func dictDelItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __delitem__() takes exactly one argument (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	if err := d.DelItem(args[1]); err != nil {
		return nil, fmt.Errorf("KeyError: %v", args[1])
	}
	return None(), nil
}

// dictLenMethod backs dict.__len__.
//
// CPython: Objects/dictobject.c:3127 dict_length
func dictLenMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: __len__() takes no arguments (%d given)", len(args)-1)
	}
	return NewInt(int64(args[0].(*Dict).Len())), nil
}

// DictBacking is implemented by every type whose instances are a dict
// subclass (collections.defaultdict, collections.OrderedDict, ...).
// asDictBacking() returns the underlying *Dict so dict's tp_richcompare,
// MATCH_KEYS, and friends can operate on the storage that the subclass
// inherits from PyDict_Type, mirroring CPython's tp_basicsize sharing.
//
// CPython: Objects/dictobject.c PyDict_Check / PyDictObject layout
type DictBacking interface {
	AsDictBacking() *Dict
}

// AsDictBacking returns d itself; *Dict implements DictBacking.
func (d *Dict) AsDictBacking() *Dict { return d }

// asDictBacking returns the *Dict stored inside o, or (nil, false) if
// o is not a dict subclass. Used by dict slots to accept defaultdict
// and similar subclass instances.
func asDictBacking(o Object) (*Dict, bool) {
	if d, ok := o.(*Dict); ok {
		return d, true
	}
	if b, ok := o.(DictBacking); ok {
		if d := b.AsDictBacking(); d != nil {
			return d, true
		}
	}
	return nil, false
}

// dictEqual reports whether two dicts compare equal by key/value.
//
// CPython: Objects/dictobject.c:3494 dict_equal
func dictEqual(a, b *Dict) (bool, error) {
	if a.Len() != b.Len() {
		return false, nil
	}
	for _, k := range a.Keys() {
		av, err := a.GetItem(k)
		if err != nil {
			return false, err
		}
		bv, err := b.GetItem(k)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				return false, nil
			}
			return false, err
		}
		eq, err := RichCmpBool(av, bv, CompareEQ)
		if err != nil {
			return false, err
		}
		if !eq {
			return false, nil
		}
	}
	return true, nil
}

// dictRichCmp is the tp_richcompare slot for dict. Only EQ and NE are
// defined; ordered comparisons fall through to NotImplemented.
//
// CPython: Objects/dictobject.c:3554 dict_richcompare
func dictRichCmp(a, b Object, op CompareOp) (Object, error) {
	ad, ok := asDictBacking(a)
	if !ok {
		return notImplemented(), nil
	}
	bd, ok := asDictBacking(b)
	if !ok {
		return notImplemented(), nil
	}
	if op != CompareEQ && op != CompareNE {
		return notImplemented(), nil
	}
	eq, err := dictEqual(ad, bd)
	if err != nil {
		return nil, err
	}
	if op == CompareNE {
		eq = !eq
	}
	return NewBool(eq), nil
}

// dictEqMethod backs dict.__eq__.
//
// CPython: Objects/dictobject.c:3554 dict_richcompare (Py_EQ branch)
func dictEqMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __eq__() takes exactly one argument (%d given)", len(args)-1)
	}
	a, ok := asDictBacking(args[0])
	if !ok {
		return NotImplemented(), nil
	}
	b, ok := asDictBacking(args[1])
	if !ok {
		return NotImplemented(), nil
	}
	eq, err := dictEqual(a, b)
	if err != nil {
		return nil, err
	}
	return NewBool(eq), nil
}

// dictClearMethod backs dict.clear().
//
// CPython: Objects/dictobject.c:3783 dict_clear
func dictClearMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: clear() takes no arguments (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	// CPython fires a single CLEARED rather than one DELETED per
	// entry (dictobject.c:2979 inside PyDict_Clear). Fire it before
	// the per-key DelItem calls so a watcher that re-subscribes
	// after CLEARED does not also see the synthetic DELETEDs.
	notifyDictEvent(DictEventCleared, d, nil, nil)
	had := atomic.LoadUint64(&d.watcherTag) & dictWatcherMask
	// Suppress the per-entry DELETED events that the DelItem loop
	// would otherwise emit, so the semantics match the C path which
	// blows the table away in one shot.
	atomic.AndUint64(&d.watcherTag, ^dictWatcherMask)
	for _, k := range d.Keys() {
		_ = d.DelItem(k)
	}
	atomic.OrUint64(&d.watcherTag, had)
	return None(), nil
}

// dictPopMethod backs dict.pop(key[, default]).
//
// CPython: Objects/dictobject.c:3821 dict_pop_impl
func dictPopMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: pop expected 1 to 2 arguments, got %d", len(args)-1)
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			if len(args) == 3 {
				return args[2], nil
			}
			repr, _ := Repr(args[1])
			return nil, fmt.Errorf("KeyError: %s", repr)
		}
		return nil, err
	}
	_ = d.DelItem(args[1])
	return v, nil
}

// dictUpdateMethod backs dict.update(). Accepts a mapping or iterable of
// pairs, plus keyword arguments.
//
// CPython: Objects/dictobject.c:3795 dict_update_common
func dictUpdateMethod(args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: update expected at most 1 argument, got %d", len(args)-1)
	}
	d := args[0].(*Dict)
	if len(args) == 2 {
		if err := dictMergeFromArg(d, args[1]); err != nil {
			return nil, err
		}
	}
	for k, v := range kwargs {
		if err := d.SetItem(NewStr(k), v); err != nil {
			return nil, err
		}
	}
	return None(), nil
}

// dictMergeFromArg merges src into dst following CPython's three-step
// fallback: fast path for *Dict, then anything with a keys() method
// (mappingproxy, custom mapping), then iterable-of-pairs.
//
// CPython: Objects/dictobject.c:2873 PyDict_Merge
func dictMergeFromArg(dst *Dict, src Object) error {
	if d, ok := src.(*Dict); ok {
		return dictMergeFromDict(dst, d)
	}
	if keysAttr, err := GetAttr(src, NewStr("keys")); err == nil {
		return dictMergeFromKeys(dst, src, keysAttr)
	}
	return dictMergeFromPairs(dst, src)
}

func dictMergeFromDict(dst, src *Dict) error {
	for _, k := range src.Keys() {
		v, err := src.GetItem(k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
	return nil
}

func dictMergeFromKeys(dst *Dict, src Object, keysAttr Object) error {
	keysObj, err := Call(keysAttr, NewTuple(nil), nil)
	if err != nil {
		return err
	}
	it, err := Iter(keysObj)
	if err != nil {
		return err
	}
	for {
		k, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return nil
			}
			return err
		}
		v, err := GetItem(src, k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
	}
}

func dictMergeFromPairs(dst *Dict, src Object) error {
	it, err := Iter(src)
	if err != nil {
		return err
	}
	i := 0
	for {
		v, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return nil
			}
			return err
		}
		pair, err := IterToSlice(v)
		if err != nil {
			return err
		}
		if len(pair) != 2 {
			return fmt.Errorf("ValueError: dictionary update sequence element #%d has length %d; 2 is required", i, len(pair))
		}
		if err := dst.SetItem(pair[0], pair[1]); err != nil {
			return err
		}
		i++
	}
}

// dictCopyMethod backs dict.copy().
//
// CPython: Objects/dictobject.c:3820 dict_copy
func dictCopyMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: copy() takes no arguments (%d given)", len(args)-1)
	}
	src := args[0].(*Dict)
	dst := NewDict()
	// CPython fires CLONED on the destination once per dict_merge
	// fastpath (dictobject.c:3915). The source dict is passed as the
	// "key" so watchers can identify where the entries came from.
	notifyDictEvent(DictEventCloned, dst, src, nil)
	had := atomic.LoadUint64(&dst.watcherTag) & dictWatcherMask
	atomic.AndUint64(&dst.watcherTag, ^dictWatcherMask)
	for _, k := range src.Keys() {
		v, _ := src.GetItem(k)
		if err := dst.SetItem(k, v); err != nil {
			atomic.OrUint64(&dst.watcherTag, had)
			return nil, err
		}
	}
	atomic.OrUint64(&dst.watcherTag, had)
	return dst, nil
}

// dictSetDefaultMethod backs dict.setdefault(key[, default]).
//
// CPython: Objects/dictobject.c:3863 dict_setdefault_impl
func dictSetDefaultMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: setdefault expected 1 to 2 arguments, got %d", len(args)-1)
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, errKeyNotFound) {
		return nil, err
	}
	var dflt Object
	if len(args) == 3 {
		dflt = args[2]
	} else {
		dflt = None()
	}
	if err := d.SetItem(args[1], dflt); err != nil {
		return nil, err
	}
	return dflt, nil
}

// dictFromKeysMethod backs dict.fromkeys(iterable[, value]).
// This is a classmethod: args[0] is the class (dict or subclass).
//
// CPython: Objects/dictobject.c:3869 dict_fromkeys_impl
func dictFromKeysMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: fromkeys expected 1 to 2 arguments, got %d", len(args)-1)
	}
	var value Object
	if len(args) == 3 {
		value = args[2]
	} else {
		value = None()
	}
	out := NewDict()
	it, err := Iter(args[1])
	if err != nil {
		return nil, err
	}
	for {
		k, err := IterNext(it)
		if errors.Is(err, ErrStopIteration) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := out.SetItem(k, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// dictPopItemMethod backs dict.popitem().
//
// CPython: Objects/dictobject.c:3898 dict_popitem_impl
func dictPopItemMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: popitem() takes no arguments (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	if d.Len() == 0 {
		return nil, fmt.Errorf("KeyError: 'popitem(): dictionary is empty'")
	}
	lastSlot := d.order[len(d.order)-1]
	k := d.slotKey(lastSlot)
	v := d.slotValue(lastSlot)
	_ = d.DelItem(k)
	return NewTuple([]Object{k, v}), nil
}

func dictRepr(o Object) (string, error) {
	d := o.(*Dict)
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, slot := range d.order {
		if !first {
			b.WriteString(", ")
		}
		first = false
		ks, err := Repr(d.slotKey(slot))
		if err != nil {
			return "", err
		}
		vs, err := Repr(d.slotValue(slot))
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
