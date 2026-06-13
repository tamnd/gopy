package objects

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
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

	// structVersion bumps on every structural change to the entry table
	// (an inserted or deleted key), but not on a value-only replacement.
	// dict iterators snapshot it and bail with RuntimeError if it moves
	// mid-walk. CPython gets the same coverage for free from its
	// append-only dk_entries table plus the di->len counter; gopy
	// compacts d.order on delete, so a delete+reinsert that leaves
	// ma_used unchanged needs this explicit version to be caught.
	//
	// CPython: Objects/dictobject.c:5237 dictiter_iternextkey_lock_held
	structVersion uint64

	// watcherTag mirrors CPython's _ma_watcher_tag. Bits 0..7 (one per
	// DICT_MAX_WATCHERS slot) flag which watchers have subscribed via
	// PyDict_Watch. Notification iterates the set bits and dispatches
	// through the package-level watcher table.
	//
	// CPython: Include/cpython/dictobject.h:23 _ma_watcher_tag
	watcherTag uint64

	// mu serializes table reads and writes across goroutines, standing in
	// for the per-object ob_mutex CPython 3.14's free-threaded build locks
	// in every Py_BEGIN_CRITICAL_SECTION(mp). gopy has no GIL, so without
	// it concurrent dict ops race the entries/order slices. See dict_lock.go.
	//
	// CPython: Include/object.h ob_mutex (free-threaded PyObject)
	mu dictMutex
}

// DictType is the type singleton for dict. Mirrors PyDict_Type.
//
// CPython: Objects/dictobject.c:L4023 PyDict_Type
var DictType = NewType("dict", []*Type{objectType})

const dictMinSize = 8

func init() {
	DictType.TpFlags |= TpFlagMapping | TpFlagMatchSelf | TpFlagHaveGC
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
	// nb_or / nb_inplace_or back PEP 584's | and |= on dict. Wiring them
	// as number slots (not just method descriptors) is what makes
	// `d |= mapping` update d in place: the |= dispatch consults the
	// in-place number slot first, so it no longer falls through to a
	// right-hand __ror__ that would rebuild the value as the other type.
	//
	// CPython: Objects/dictobject.c:3930 dict_as_number
	DictType.Number = &NumberMethods{
		Or:        dictNumberOr,
		InPlaceOr: dictNumberIOr,
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
	SetTypeDescr(DictType, "__reversed__", NewMethodDescrConv(DictType, "__reversed__", MethNoArgs, dictReversedMethod))
	SetTypeDescr(DictType, "keys", NewMethodDescrConv(DictType, "keys", MethNoArgs, dictKeysMethod))
	SetTypeDescr(DictType, "values", NewMethodDescrConv(DictType, "values", MethNoArgs, dictValuesMethod))
	SetTypeDescr(DictType, "items", NewMethodDescrConv(DictType, "items", MethNoArgs, dictItemsMethod))
	SetTypeDescr(DictType, "get", NewMethodDescr(DictType, "get", dictGetMethod))
	// CPython: Objects/clinic/dictobject.c.h:66 METH_O|METH_COEXIST
	SetTypeDescr(DictType, "__contains__", NewMethodDescrConv(DictType, "__contains__", MethO, dictContainsMethod))
	SetTypeDescr(DictType, "__getitem__", NewMethodDescr(DictType, "__getitem__", dictGetItemMethod))
	SetTypeDescr(DictType, "__setitem__", NewMethodDescr(DictType, "__setitem__", dictSetItemMethod))
	SetTypeDescr(DictType, "__delitem__", NewMethodDescr(DictType, "__delitem__", dictDelItemMethod))
	SetTypeDescr(DictType, "__len__", NewMethodDescr(DictType, "__len__", dictLenMethod))
	SetTypeDescr(DictType, "__eq__", NewMethodDescr(DictType, "__eq__", dictEqMethod))
	SetTypeDescr(DictType, "clear", NewMethodDescrConv(DictType, "clear", MethNoArgs, dictClearMethod))
	SetTypeDescr(DictType, "pop", NewMethodDescr(DictType, "pop", dictPopMethod))
	SetTypeDescr(DictType, "update", NewMethodDescr(DictType, "update", dictUpdateMethod))
	SetTypeDescr(DictType, "copy", NewMethodDescrConv(DictType, "copy", MethNoArgs, dictCopyMethod))
	SetTypeDescr(DictType, "setdefault", NewMethodDescr(DictType, "setdefault", dictSetDefaultMethod))
	SetTypeDescr(DictType, "fromkeys", NewClassMethod(NewBuiltinFunction("fromkeys", dictFromKeysMethod)))
	SetTypeDescr(DictType, "popitem", NewMethodDescrConv(DictType, "popitem", MethNoArgs, dictPopItemMethod))
	SetTypeDescr(DictType, "__or__", NewMethodDescr(DictType, "__or__", dictOrMethod))
	SetTypeDescr(DictType, "__ior__", NewMethodDescr(DictType, "__ior__", dictIOrMethod))
	SetTypeDescr(DictType, "__ror__", NewMethodDescr(DictType, "__ror__", dictROrMethod))
	// __iter__ slot wrapper. CPython's add_operators installs this for
	// every type with a non-NULL tp_iter; without it, things like
	// `dict.__iter__(d)` and CrazyDict's `for x in self.d.__iter__()`
	// raise AttributeError.
	//
	// CPython: Objects/typeobject.c add_operators slot wrapper for tp_iter
	AddIterSlotWrappers(DictType)
	// CPython: Objects/dictobject.c:2498 dict.__hash__ = None
	SetTypeDescr(DictType, "__hash__", None())
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
	if err := CheckPositional("get", len(args)-1, 1, 2); err != nil {
		return nil, err
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err != nil {
		// Only a genuine miss falls back to the default. A TypeError from
		// an unhashable key or an exception raised by a key's __eq__ must
		// propagate, matching dict_get_impl which returns NULL on error.
		//
		// CPython: Objects/dictobject.c:4387 dict_get_impl
		if !errors.Is(err, errKeyNotFound) {
			return nil, err
		}
		v = nil
	}
	// dict.get returns a new reference in every branch: the found value
	// comes back incref'd from _Py_dict_lookup_threadsafe, and the default
	// (explicit arg or None) is wrapped in Py_NewRef. GetItem returns the
	// stored value borrowed, so incref here to match.
	//
	// CPython: Objects/dictobject.c:4387 dict_get_impl (Py_NewRef)
	var res Object
	switch {
	case v != nil:
		res = v
	case len(args) == 3:
		res = args[2]
	default:
		res = None()
	}
	Incref(res)
	return res, nil
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

// dictTraverse visits every key and value for combined dicts, and only
// per-instance values for split dicts. Split dicts share their key table
// with SharedKeys (which the type object owns), so visiting the keys
// would double-decrement them during subtractRefs. CPython's dict_traverse
// skips the dk_kind==DICT_KEYS_SPLIT branch in the same way.
//
// CPython: Objects/dictobject.c:4022 dict_traverse
func dictTraverse(o Object, visit Visitor) error {
	d := o.(*Dict)
	if d.sharedKeys != nil {
		for _, slot := range d.sharedKeys.order {
			if v := d.splitValues[slot]; v != nil {
				if err := visit(v); err != nil {
					return err
				}
			}
		}
		return nil
	}
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
	// CPython: Objects/dictobject.c:737 new_dict (PyObject_GC_Track at end)
	if h := GCTrackHook; h != nil {
		h(d)
	}
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
	// CPython: Objects/dictobject.c:3193 keys_lock_held (Py_BEGIN_CRITICAL_SECTION(op))
	d.lock()
	defer d.unlock()
	out := make([]Object, 0, d.used)
	for _, slot := range d.order {
		out = append(out, d.slotKey(slot))
	}
	return out
}

// ForEachWithHash iterates over (key, hash) pairs without recomputing
// hashes, mirroring _PyDict_Next. Used by set construction to avoid
// rehashing keys that the dict already has cached hash values for.
//
// CPython: Objects/dictobject.c:3512 _PyDict_Next
func (d *Dict) ForEachWithHash(fn func(key Object, hash int64) error) error {
	// CPython: Objects/dictobject.c:3492 _PyDict_Next (Py_BEGIN_CRITICAL_SECTION(self))
	d.lock()
	defer d.unlock()
	for _, slot := range d.order {
		e := &d.entries[slot]
		if !e.used {
			continue
		}
		if err := fn(e.key, e.hash); err != nil {
			return err
		}
	}
	return nil
}

// SetItem inserts or replaces key. Mirrors PyDict_SetItem.
//
// dictKeyHash hashes key for a dict operation, wrapping a TypeError from
// an unhashable key in the "cannot use '<type>' as a dict key (<exc>)"
// message dict_unhashable_type produces in 3.14. A custom __hash__ that
// raises something other than TypeError propagates unchanged.
//
// CPython: Objects/dictobject.c:2352 dict_unhashable_type
func dictKeyHash(key Object) (int64, error) {
	h, err := Hash(key)
	if err == nil {
		return h, nil
	}
	inner, ok := strings.CutPrefix(err.Error(), "TypeError: ")
	if !ok {
		return 0, err
	}
	return 0, fmt.Errorf("TypeError: cannot use '%s' as a dict key (%s)", key.Type().Name, inner)
}

// CPython: Objects/dictobject.c:1985 PyDict_SetItem
func (d *Dict) SetItem(key, value Object) error {
	h, err := dictKeyHash(key)
	if err != nil {
		return err
	}
	return dictInsert(d, h, key, value)
}

// GetItem looks up key. Returns errKeyNotFound when absent.
//
// CPython: Objects/dictobject.c:L1925 PyDict_GetItemWithError
func (d *Dict) GetItem(key Object) (Object, error) {
	h, err := dictKeyHash(key)
	if err != nil {
		return nil, err
	}
	// CPython: Objects/dictobject.c:1576 PyDict_GetItemRef (Py_BEGIN_CRITICAL_SECTION(op))
	d.lock()
	defer d.unlock()
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
	// CPython: Objects/dictobject.c:1576 PyDict_GetItemRef (Py_BEGIN_CRITICAL_SECTION(op))
	d.lock()
	defer d.unlock()
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
	// CPython: Objects/dictobject.c:2706 _PyDict_Contains_KnownHash (Py_BEGIN_CRITICAL_SECTION(mp))
	d.lock()
	defer d.unlock()
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
	h, err := dictKeyHash(key)
	if err != nil {
		return false, err
	}
	// CPython: Objects/dictobject.c:2706 PyDict_Contains (Py_BEGIN_CRITICAL_SECTION(mp))
	d.lock()
	defer d.unlock()
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
	d, ok := o.(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__getitem__' requires a 'dict' object but received a '%s'", o.Type().Name)
	}
	v, err := d.GetItem(key)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			// For dict subclasses, invoke __missing__ before raising
			// KeyError. dict_subscript uses _PyObject_LookupSpecial, a
			// type-level lookup, so an instance variable named
			// __missing__ has no effect (test_missing case F).
			//
			// CPython: Objects/dictobject.c:2242 (non-exact dict __missing__ path)
			if d.Type() != DictType {
				missingFn, merr := LookupSpecial(o, "__missing__")
				if merr != nil {
					return nil, merr
				}
				if missingFn != nil {
					return CallOneArg(missingFn, key)
				}
			}
			return nil, raiseKeyError(key)
		}
		return nil, err
	}
	return v, nil
}

// dictMappingSet is the mp_ass_subscript slot for dict. When the
// receiver is not a real *Dict the slot raises a clean TypeError
// instead of panicking. This protects against pathological MROs
// (e.g. a metaclass that injects dict into a non-dict layout) where
// CPython would either reject the class at type-ready time or, failing
// that, raise TypeError on the first __setitem__ attempt.
//
// CPython: Objects/dictobject.c:2407 dict_ass_sub
func dictMappingSet(o, key, value Object) error {
	d, ok := o.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__setitem__' requires a 'dict' object but received a '%s'", o.Type().Name)
	}
	return d.SetItem(key, value)
}

func dictMappingDel(o, key Object) error {
	d, ok := o.(*Dict)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__delitem__' requires a 'dict' object but received a '%s'", o.Type().Name)
	}
	return d.DelItem(key)
}

// AttrDict implements AttrDictHolder so dict subclasses can carry
// instance attributes and expose them to objectGetStateDefault.
//
// CPython: Objects/object.c _PyObject_GetDictPtr (dict-subclass path)
func (d *Dict) AttrDict() *Dict { return d.attrs }

// EnsureAttrDict allocates the instance attrs dict on first use.
func (d *Dict) EnsureAttrDict() *Dict {
	if d.attrs == nil {
		d.attrs = NewDict()
	}
	return d.attrs
}

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
			Incref(v)
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
		if errors.Is(err, errKeyNotFound) {
			return nil, raiseKeyError(args[1])
		}
		return nil, err
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
	if a == b {
		return true, nil
	}
	if a.Len() != b.Len() {
		return false, nil
	}
	if err := enterRecursiveCall(" in comparison"); err != nil {
		return false, err
	}
	defer leaveRecursiveCall()
	for _, k := range a.Keys() {
		av, err := a.GetItem(k)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				// The key vanished from a between Keys() and the read;
				// treat the snapshot as stale and not-equal.
				return false, nil
			}
			return false, err
		}
		// Hold a counted reference on the key and aval BEFORE the b lookup.
		// Looking key up in b runs key.__hash__/__eq__, which can clear or
		// resize a and free k or av out from under us. CPython pins both
		// before the b probe and the RichCompareBool (bpo-27945, bpo-38588).
		//
		// CPython: Objects/dictobject.c:3494 dict_equal (Py_INCREF key/aval)
		Incref(k)
		Incref(av)
		bv, err := b.GetItem(k)
		if err != nil {
			Decref(k)
			Decref(av)
			if errors.Is(err, errKeyNotFound) {
				return false, nil
			}
			return false, err
		}
		// Pin bval too across the value compare; a re-entrant __eq__ can
		// clear b. CPython: Objects/dictobject.c:3506 dict_equal (Py_INCREF bval).
		Incref(bv)
		eq, err := RichCmpBool(av, bv, CompareEQ)
		Decref(k)
		Decref(av)
		Decref(bv)
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
// CPython: Objects/dictobject.c:3783 dict_clear / Objects/dictobject.c:2979 PyDict_Clear
func dictClearMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: clear() takes no arguments (%d given)", len(args)-1)
	}
	d := args[0].(*Dict)
	// CPython's PyDict_Clear atomically swaps in a fresh empty keys
	// table without touching __eq__ or __hash__. The iter-then-DelItem
	// approach previously used here triggered __eq__ on each probe,
	// allowing re-entrant clears that corrupted d.used.
	//
	// CPython: Objects/dictobject.c:2979 PyDict_Clear
	notifyDictEvent(DictEventCleared, d, nil, nil)
	// CPython: Objects/dictobject.c:2938 PyDict_Clear (Py_BEGIN_CRITICAL_SECTION(op))
	d.lock()
	d.clearContents()
	d.unlock()
	return None(), nil
}

// clearContents drops every key/value reference the dict owns and
// resets the table to empty. Shared between dict.clear() and the
// throwaway-kwargs release path. dictInsert increfs values (and
// stores keys, which for kwargs and attribute dicts are interned
// strings whose Decref is a no-op), so releasing both here balances
// the insert path.
//
// CPython: Objects/dictobject.c:2979 PyDict_Clear (clear_keys loop)
func (d *Dict) clearContents() {
	if d.sharedKeys != nil {
		// Split-table: decref all per-instance values, reset values array.
		for i, v := range d.splitValues {
			if v != nil {
				Decref(v)
				d.splitValues[i] = nil
			}
		}
	} else {
		// Combined-table: decref keys and values directly without lookup.
		for i := range d.entries {
			e := &d.entries[i]
			if e.used {
				if e.key != nil {
					Decref(e.key)
				}
				if e.value != nil {
					Decref(e.value)
				}
				d.entries[i] = dictEntry{}
			}
		}
	}
	d.order = d.order[:0]
	d.used = 0
	d.fill = 0
	d.invalidateKeysVersion()
}

// DecrefThrowawayKwargs releases the temporary keyword dict the eval
// loop builds for a CALL_FUNCTION_EX (BUILD_MAP followed by DICT_MERGE).
// Dropping the call's reference normally leaves the dict at refcount
// zero, but gopy dicts carry no synchronous tp_dealloc (a global one is
// unsafe: namespace dicts such as a module or type __dict__ are reachable
// only through an un-counted Go field, so their Python refcount routinely
// sits at zero while the dict is live), so the values the merge incref'd
// into it would stay pinned by a refcount no container actually holds.
// The cycle collector cannot help: those values are still reachable from
// the caller's live locals when the throwaway dict dies, so it never
// classifies them as garbage and the pin never lifts.
//
// Clearing is gated on the dict reaching refcount zero, which at this
// call site is a precise signal that nothing else references it (the
// compiler builds this dict solely for the unpack). A dict that some
// other caller still holds keeps a positive refcount and is left
// untouched.
//
// CPython: Objects/dictobject.c:2768 dict_dealloc (PyDict_Clear on the
// final decref of the keyword dict CALL_FUNCTION_EX owns)
func DecrefThrowawayKwargs(d *Dict) {
	Decref(d)
	if d.Hdr().Refcnt() == 0 {
		d.clearContents()
	}
}

// ReleaseDeadDictContents drops one reference on d and, when that leaves
// it at refcount zero, releases the references the dict owns on its
// stored values. It is the frame-clear analog of DecrefThrowawayKwargs:
// when frame.clear() closes a local slot holding a **kwargs parameter
// dict, the slot's Close decrefs the dict, but with no synchronous dict
// tp_dealloc the captured values would stay pinned by a refcount nothing
// holds. Gating on refcount zero keeps this safe (a dict the caller still
// references is left intact); clearContents decrefs only the dict's owned
// values, leaving its borrowed keys alone.
//
// CPython: Objects/dictobject.c:2768 dict_dealloc (final decref of a
// frame-local dict during frame_clear)
func ReleaseDeadDictContents(d *Dict) {
	if d.Hdr().Refcnt() == 0 {
		d.clearContents()
	}
}

// dictPopMethod backs dict.pop(key[, default]).
//
// CPython: Objects/dictobject.c:3821 dict_pop_impl
func dictPopMethod(args []Object, _ map[string]Object) (Object, error) {
	if err := CheckPositional("pop", len(args)-1, 1, 2); err != nil {
		return nil, err
	}
	d := args[0].(*Dict)
	v, err := d.GetItem(args[1])
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			if len(args) == 3 {
				// dict_pop_default returns Py_NewRef(default_value); the
				// caller owns the result it discards or stores.
				Incref(args[2])
				return args[2], nil
			}
			return nil, raiseKeyError(args[1])
		}
		return nil, err
	}
	// GetItem hands back the borrowed slot value. Take a reference before
	// DelItem releases the dict's: _PyDict_Pop transfers the entry's own
	// reference to the caller (delitem_common(..., Py_NewRef(old_value));
	// *result = old_value), leaving the object's count unchanged.
	//
	// CPython: Objects/dictobject.c:3144 _PyDict_Pop_KnownHash
	Incref(v)
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
	// CPython gates the fast PyDict_Next copy on the source's tp_iter
	// being the exact dict iterator. A subclass that overrides __iter__
	// (OrderedDict walks its linked list) must merge through keys() so the
	// caller observes the subclass's iteration order, not the underlying
	// hash-table order.
	//
	// CPython: Objects/dictobject.c:2901 dict_merge (Py_TYPE(b)->tp_iter == dict_iter)
	if d, ok := src.(*Dict); ok && !dictIterOverridden(src.Type()) {
		return dictMergeFromDict(dst, d)
	}
	if keysAttr, err := GetAttr(src, NewStr("keys")); err == nil {
		return dictMergeFromKeys(dst, src, keysAttr)
	}
	return dictMergeFromPairs(dst, src)
}

// dictIterOverridden reports whether t overrides dict.__iter__. It is the
// gopy stand-in for CPython's Py_TYPE(b)->tp_iter != dict_iter gate: when
// the slot differs, callers that care about iteration order (PyDict_Merge,
// type_new's namespace copy) must walk the Python iterator rather than the
// raw hash table.
//
// CPython: Objects/dictobject.c:2901 dict_merge
func dictIterOverridden(t *Type) bool {
	if t == nil || t == DictType {
		return false
	}
	base, _ := LookupDescriptor(DictType, "__iter__")
	sub, _ := LookupDescriptor(t, "__iter__")
	return base != sub
}

// DictIterOverridden exposes the dict_merge tp_iter gate to the builtins
// package so the dict() constructor can mirror PyDict_Merge: a subclass that
// overrides __iter__ (OrderedDict walks its linked list) must merge through
// keys() so the copy preserves the subclass's iteration order.
//
// CPython: Objects/dictobject.c:2901 dict_merge
func DictIterOverridden(t *Type) bool { return dictIterOverridden(t) }

func dictMergeFromDict(dst, src *Dict) error {
	// dict_dict_merge snapshots the source's live count and rechecks it
	// after every insert: an insert into dst can fire a key's __eq__,
	// which may clear or grow src mid-merge. CPython raises rather than
	// iterate a table that shifted under it.
	//
	// CPython: Objects/dictobject.c:3981 dict_dict_merge
	origSize := src.used
	for _, k := range src.Keys() {
		v, err := src.GetItem(k)
		if err != nil {
			return err
		}
		if err := dst.SetItem(k, v); err != nil {
			return err
		}
		if origSize != src.used {
			return fmt.Errorf("RuntimeError: dict mutated during update")
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
		// PySequence_Fast carries the literal "object is not iterable"
		// message, and on a TypeError dict_merge_from_seq2 attaches a
		// note pinning the failing element index.
		//
		// CPython: Objects/dictobject.c:3823 merge_from_seq2_lock_held
		fast, err := SequenceFast(v, "object is not iterable")
		if err != nil {
			if strings.HasPrefix(err.Error(), "TypeError:") && FormatNoteHook != nil {
				FormatNoteHook(fmt.Sprintf("Cannot convert dictionary update sequence element #%d to a sequence", i))
			}
			return err
		}
		pair, err := IterToSlice(fast)
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
	// Hold src's table lock across the whole snapshot-and-read loop so a
	// concurrent goroutine cannot delete an entry between Keys() and the
	// matching GetItem (test_threaded_weak_key_dict_copy). This mirrors
	// CPython's copy_lock_held running under Py_BEGIN_CRITICAL_SECTION(o);
	// the per-dict lock is goroutine-reentrant, so the nested Keys()/
	// GetItem acquisitions on the same goroutine pass straight through.
	//
	// CPython: Objects/dictobject.c:4147 copy_lock_held
	src.lock()
	for _, k := range src.Keys() {
		v, err := src.GetItem(k)
		if err != nil || v == nil {
			// Even under the lock a key can be absent if a key's __eq__
			// rejected it during the GetItem probe; skip rather than
			// inserting a nil value.
			continue
		}
		if err := dst.SetItem(k, v); err != nil {
			src.unlock()
			atomic.OrUint64(&dst.watcherTag, had)
			return nil, err
		}
	}
	src.unlock()
	atomic.OrUint64(&dst.watcherTag, had)
	return dst, nil
}

// dictOrMethod backs dict.__or__(other). Returns a new dict with all
// entries from self followed by entries from other (other wins on
// duplicate keys). Mirrors PEP 584 / dict___or___impl.
//
// CPython: Objects/dictobject.c:3890 dict___or___impl
func dictOrMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __or__() takes exactly one argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	other, ok := args[1].(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	dst := NewDict()
	for _, k := range self.Keys() {
		v, _ := self.GetItem(k)
		_ = dst.SetItem(k, v)
	}
	for _, k := range other.Keys() {
		v, _ := other.GetItem(k)
		_ = dst.SetItem(k, v)
	}
	return dst, nil
}

// dictROrMethod backs dict.__ror__(other). Same as __or__ with swapped
// operands.
//
// CPython: Objects/dictobject.c:3908 dict___ror___impl
func dictROrMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __ror__() takes exactly one argument (%d given)", len(args)-1)
	}
	other, ok := args[0].(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	self, ok := args[1].(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	dst := NewDict()
	for _, k := range self.Keys() {
		v, _ := self.GetItem(k)
		_ = dst.SetItem(k, v)
	}
	for _, k := range other.Keys() {
		v, _ := other.GetItem(k)
		_ = dst.SetItem(k, v)
	}
	return dst, nil
}

// dictIOrMethod backs dict.__ior__(other) (the |= operator). Updates
// self in place with entries from other and returns self.
//
// CPython: Objects/dictobject.c:3922 dict___ior___impl
func dictIOrMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __ior__() takes exactly one argument (%d given)", len(args)-1)
	}
	self, ok := args[0].(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	// |= delegates to dict_update_arg, which accepts any mapping or any
	// iterable of key/value pairs, exactly like dict.update. This is
	// looser than __or__ (which only pairs two dicts): `d |= [(1, 1)]`
	// is valid.
	//
	// CPython: Objects/dictobject.c:3922 dict___ior___impl (dict_update_arg)
	if err := dictMergeFromArg(self, args[1]); err != nil {
		return nil, err
	}
	return self, nil
}

// dictNumberOr is the nb_or slot: a | b. Returns NotImplemented unless
// both operands are dicts (or dict subclasses), then copies the left
// and merges the right on top, so the right wins on duplicate keys.
//
// CPython: Objects/dictobject.c:3909 dict_or
func dictNumberOr(a, b Object) (Object, error) {
	self, ok := a.(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	if _, ok := b.(*Dict); !ok {
		return NotImplemented(), nil
	}
	dst := NewDict()
	for _, k := range self.Keys() {
		v, _ := self.GetItem(k)
		_ = dst.SetItem(k, v)
	}
	if err := dictMergeFromArg(dst, b); err != nil {
		return nil, err
	}
	return dst, nil
}

// dictNumberIOr is the nb_inplace_or slot: a |= b. Updates the left dict
// in place from any mapping or iterable of pairs (dict_update_arg) and
// returns it, so `d |= mapping` keeps d's identity and type.
//
// CPython: Objects/dictobject.c:3924 dict_ior
func dictNumberIOr(a, b Object) (Object, error) {
	self, ok := a.(*Dict)
	if !ok {
		return NotImplemented(), nil
	}
	if err := dictMergeFromArg(self, b); err != nil {
		return nil, err
	}
	return self, nil
}

// dictSetDefaultMethod backs dict.setdefault(key[, default]).
//
// CPython: Objects/dictobject.c:3863 dict_setdefault_impl
func dictSetDefaultMethod(args []Object, _ map[string]Object) (Object, error) {
	if err := CheckPositional("setdefault", len(args)-1, 1, 2); err != nil {
		return nil, err
	}
	d := args[0].(*Dict)
	// setdefault hashes (and compares) the key exactly once: it threads a
	// single PyObject_Hash through the lookup and the insert.
	//
	// CPython: Objects/dictobject.c:4376 dict_setdefault_ref_lock_held
	h, err := dictKeyHash(args[1])
	if err != nil {
		return nil, err
	}
	var dflt Object
	if len(args) == 3 {
		dflt = args[2]
	} else {
		dflt = None()
	}
	// dictSetDefault is the incref_result=0 variant: it returns the stored
	// value borrowed. dict.setdefault returns a new reference, so incref
	// here to match dict_setdefault_impl's incref_result=1 call.
	//
	// CPython: Objects/dictobject.c:4542 dict_setdefault_impl
	//	(dict_setdefault_ref_lock_held with incref_result=1, Py_NewRef(value))
	val, err := dictSetDefault(d, h, args[1], dflt)
	if err != nil {
		return nil, err
	}
	Incref(val)
	return val, nil
}

// dictFromKeysMethod backs dict.fromkeys(iterable[, value]).
// This is a classmethod: args[0] is the class (dict or subclass).
//
// CPython: Objects/dictobject.c:3869 dict_fromkeys_impl
func dictFromKeysMethod(args []Object, _ map[string]Object) (Object, error) {
	if err := CheckPositional("fromkeys", len(args)-1, 1, 2); err != nil {
		return nil, err
	}
	var value Object
	if len(args) == 3 {
		value = args[2]
	} else {
		value = None()
	}
	// _PyDict_FromKeys builds the result by calling cls(), so a dict
	// subclass produces an instance of that subclass and a class whose
	// __new__ returns a foreign mapping (collections.UserDict) is honored
	// too. The set/dict fast paths only fire on an empty exact dict.
	//
	// CPython: Objects/dictobject.c:3924 _PyDict_FromKeys
	cls := args[0]
	d, err := CallNoArgs(cls)
	if err != nil {
		return nil, err
	}
	out, exact := d.(*Dict)
	exact = exact && out.Type() == DictType && out.Len() == 0
	if exact {
		// Set/frozenset fast path: reuse cached hashes to avoid calling
		// __hash__ on each key again.
		//
		// CPython: Objects/dictobject.c:3885 dict_fromkeys_impl (PySet_CheckExact)
		if ss, ok := args[1].(*Set); ok {
			for _, e := range ss.Entries() {
				if err := out.SetItemKnownHash(e.Key, value, e.Hash); err != nil {
					return nil, err
				}
			}
			return out, nil
		}
	}
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
		if exact {
			if err := out.SetItem(k, value); err != nil {
				return nil, err
			}
		} else if err := SetItem(d, k, value); err != nil {
			return nil, err
		}
	}
	return d, nil
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

// dictReprInProgress guards against recursive dict repr: {1: {...}}.
//
// CPython: Objects/object.c:2256 Py_ReprEnter
var dictReprInProgress sync.Map

func dictRepr(o Object) (string, error) {
	d := o.(*Dict)
	ptr := uintptr(unsafe.Pointer(d))
	if _, loaded := dictReprInProgress.LoadOrStore(ptr, struct{}{}); loaded {
		return "{...}", nil
	}
	defer dictReprInProgress.Delete(ptr)

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
