// Instance is the v0.10 carrier for an object whose class was defined
// in Python. Its Header.typ points at the user-defined Type built by
// __build_class__; attribute lookup walks the type's MRO via
// LookupDescriptor and, when t.HasDict, falls through to the
// per-instance __dict__. Classes that declare __slots__ without
// __dict__ have a nil dict and rely on the slots array plus the
// MemberDescr data descriptors registered on the type.
//
// CPython models the same shape with a PyObject whose tp_dictoffset
// optionally points at the dict and whose ht_slots/PyMemberDef table
// describes the fixed-offset slot fields.
//
// CPython: Objects/typeobject.c PyBaseObject_Type slots
//          (tp_getattro -> PyObject_GenericGetAttr,
//           tp_setattro -> PyObject_GenericSetAttr)

package objects

import "fmt"

// GCTrackHook is called from NewInstance whenever the type declares a
// Finalize slot (tp_finalize), so the cycle collector sees the object.
// The gc module registers its Track function here at startup to avoid
// an import cycle (objects cannot import gc).
//
// CPython: Include/internal/pycore_object.h:225 _PyObject_GC_TRACK
var GCTrackHook func(Object)

// GCUntrackHook is the inverse of GCTrackHook. It removes o from the
// cycle collector's candidate set. Called from tp_dealloc paths for
// built-in container types (set, frozenset) so the collector does not
// visit an already-freed object after dealloc runs via Decref.
//
// CPython: Include/internal/pycore_object.h:248 _PyObject_GC_UNTRACK
var GCUntrackHook func(Object)

// GCTrackSilentHook tracks o for cycle detection without incrementing
// gen0.count, so no auto-collection is triggered. Used for objects
// whose allocation bursts would otherwise push gen0 over its threshold
// at a time when the container under-incref issue (#223) would cause
// false unreachability. Split instance dicts use this path: they hold
// no cycle-relevant references when empty, but must be trackable once
// values are written.
//
// CPython: _PyObject_GC_Link increments gcstate->young.count which then
// drives the auto-trigger; this hook intentionally omits that step.
var GCTrackSilentHook func(Object)

// SaveCurrentExceptionHook returns the active thread's pending exception
// (a typed *pyerrors.Exception, returned here as any to avoid an objects
// to pyerrors import edge). RestoreCurrentExceptionHook puts it back.
// Used by genFinalize to mirror CPython's PyErr_GetRaisedException /
// PyErr_SetRaisedException pair: a finalize that runs synchronously on
// the active thread must not leak a GeneratorExit into the caller's
// thread-state slot.
//
// CPython: Objects/genobject.c:87 _PyGen_Finalize
var (
	SaveCurrentExceptionHook    func() any
	RestoreCurrentExceptionHook func(any)
)

// WriteUnraisableHook routes an exception that cannot be propagated
// (raised in __del__, in a generator's close path, in a weakref
// callback, etc) through sys.unraisablehook. The hook builds an
// UnraisableHookArgs object and invokes sys.unraisablehook(args).
// errMsg is the "Exception ignored while ..." prefix; obj is the object
// that produced the exception (the instance whose __del__ raised, the
// generator whose close errored). err is the exception that arose,
// either wrapped in a RaisedError or a bare Go sentinel; the hook
// converts the bare form into a typed Exception via the same
// synthesizeException path the unwind uses.
//
// objects/ wires this through a hook variable because the vm package
// owns the sys-module lookup machinery; objects cannot import
// imp/state/sys without creating a cycle.
//
// CPython: Python/errors.c:1380 _PyErr_WriteUnraisable
var WriteUnraisableHook func(obj Object, errMsg string, err error)

// WarnUnawaitedCoroutineHook routes a never-awaited coroutine through
// warnings._warn_unawaited_coroutine so the consumer sees a
// RuntimeWarning that names the coroutine's qualname. objects/ stays
// independent of the warnings module via this indirection.
//
// CPython: Python/_warnings.c:1573 _PyErr_WarnUnawaitedCoroutine
var WarnUnawaitedCoroutineHook func(coro Object)

// WarnUnawaitedAgenMethodHook routes a never-awaited async-generator
// asend/athrow/aclose awaitable through warnings so the consumer sees a
// RuntimeWarning of the form "coroutine method 'asend' of '<qualname>'
// was never awaited". The wrapper's tp_finalize calls this when the
// awaitable was created but never iterated.
//
// CPython: Python/_warnings.c:1558 _PyErr_WarnUnawaitedAgenMethod
var WarnUnawaitedAgenMethodHook func(agen Object, method Object)

// Instance backs a Python-level object whose type is a user-defined
// class. Header.typ is the class; dict holds per-instance attributes
// (nil when the class declared __slots__ without __dict__); slots
// holds the fixed-index storage for __slots__ entries.
type Instance struct {
	Header

	dict  *Dict
	slots []Object

	// inlineValid mirrors PyDictValues.valid: true while the instance
	// is still in the default inline-values shape (no DELETE_ATTR,
	// no key replacement that would materialize a separate dict).
	// The LOAD_ATTR_*_WITH_VALUES fast arms deopt when this flag is
	// cleared.
	//
	// CPython: Include/internal/pycore_dict.h PyDictValues.valid
	inlineValid bool

	// dictExposed records that Python code has fetched this instance's
	// __dict__ (via objectGetDict), so a live mapping object aliases the
	// instance's attribute storage. CPython materializes such a dict over
	// the inline-values array and, at dealloc, must detach it (copy the
	// values into dict-owned storage) before the inline array is freed. The
	// detach allocates, so it is the point _testcapi.set_nomemory can fault.
	// gopy's dict already owns its storage, so the detach is a no-op on
	// success; the flag exists only to know whether a detach (and its
	// fault-prone allocation) is owed at dealloc.
	//
	// CPython: Objects/dictobject.c:7530 _PyDict_DetachFromObject
	dictExposed bool

	// typeReleased guards the single _Py_DECREF_TYPE that instanceDealloc
	// performs. CPython's refcounting is exact, so subtype_dealloc runs
	// exactly once per object; gopy's VM still under-counts some borrowed
	// references, so an *Instance can reach refcount 0 more than once and
	// re-enter Decref's dealloc path. Without this latch the second pass
	// would decref the type again and free a class that still has live
	// instances. The latch keeps the type release one-to-one with the
	// NewInstance incref regardless of how many times dealloc re-fires.
	typeReleased bool
}

// NewInstance allocates a fresh Instance bound to t. The instance
// __dict__ is empty when t.HasDict and t.HasInlineValues (INLINE_VALUES
// shape: dict ships pre-allocated so the WITH_VALUES specializer arms
// can stamp keys_version without a materialization step). LAZY_DICT
// shape (HasDict && !HasInlineValues, the case for heap subclasses of
// built-ins) leaves dict nil until instanceSetAttr lands the first
// store; that null state is the runtime guard the
// LOAD_ATTR_METHOD_LAZY_DICT fast arm checks. The slots array is
// sized to t.Slots and starts all-nil (each slot reads as
// AttributeError until assigned).
//
// CPython: Objects/typeobject.c:1748 type_call (object_new path)
// CPython: Objects/typeobject.c:4153 type_new (INLINE_VALUES gate)
func NewInstance(t *Type) *Instance {
	inst := &Instance{}
	if t.HasDict && t.HasInlineValues() {
		// Split-keys shape: when the type has already cached at least
		// one attribute name, allocate a split __dict__ that shares the
		// type's keys table and only carries this instance's value
		// array. The shared table is fixed-size (NewEmptySharedKeys
		// returns dictMinSize slots) so its entries slice header stays
		// valid for the lifetime of every attached split dict.
		//
		// CPython: Objects/dictobject.c:897 new_dict_with_shared_keys
		if sk := t.SharedKeys(); sk != nil && sk.Len() > 0 {
			inst.dict = NewSplitDict(sk)
		} else {
			inst.dict = NewDict()
		}
	}
	// The slot array carries one entry per name in the cumulative
	// chain: SlotsBase counts every slot inherited from the layout
	// base, len(t.Slots) covers the names declared on this class.
	// MemberDescrs from parent classes index into the SlotsBase
	// prefix; the current class's MemberDescrs index above it.
	//
	// CPython: Objects/typeobject.c:4404 type_new_descriptors (slotoffset
	// starts at ctx->base->tp_basicsize)
	if n := t.SlotsBase + len(t.Slots); n > 0 {
		inst.slots = make([]Object, n)
	}
	// Every fresh instance starts with inline values valid; only a
	// DELETE_ATTR (or other mutation that materializes a dict outside
	// the shared-keys shape) clears it.
	//
	// CPython: Objects/object.c _PyObject_InitInlineValues
	inst.inlineValid = true
	inst.init(t)
	// Every object holds a counted reference to its type. _PyObject_Init
	// calls _Py_INCREF_TYPE, which bumps the refcount for heap (user)
	// types and is a no-op for the immortal static built-ins. gopy mirrors
	// the HEAPTYPE branch with the IsUser gate: a class defined inside a
	// local scope must stay alive as long as any instance references it,
	// otherwise typeUserDealloc clears its descriptor table out from under
	// a live instance. The matching release happens in instanceDealloc
	// (subtype_dealloc's _Py_DECREF_TYPE).
	//
	// CPython: Include/internal/pycore_object.h:507 _PyObject_Init
	//          (_Py_INCREF_TYPE(typeobj))
	if t.IsUser {
		Incref(t)
	}
	// Register with the cycle collector for any type that has tp_traverse
	// (not only types with tp_finalize). CPython calls _PyObject_GC_TRACK
	// for all heap types, which always have tp_traverse; restricting to
	// tp_finalize-only excluded plain user instances from cycle detection.
	//
	// CPython: Objects/typeobject.c:1748 type_call _PyObject_GC_TRACK
	if t.TpTraverse != nil {
		if h := GCTrackHook; h != nil {
			h(inst)
		}
	}
	return inst
}

// InlineValid reports whether the instance is still in the inline-
// values shape the LOAD_ATTR_*_WITH_VALUES fast arms expect.
//
// CPython: Include/internal/pycore_dict.h PyDictValues.valid
func (i *Instance) InlineValid() bool { return i.inlineValid }

// InvalidateInlineValues flips inlineValid to false. The next fast-arm
// guard miss deopts the call site. Called from instanceSetAttr on
// delete (and from any future path that breaks the shared-keys shape).
//
// CPython: Objects/dictobject.c:6857 make_dict_from_instance_attributes
// (called when the inline-values shape is broken)
func (i *Instance) InvalidateInlineValues() { i.inlineValid = false }

// Dict returns the instance __dict__. Mutating it is how attribute
// stores land. Returns nil when the class declared __slots__ without
// __dict__ and also nil for the LAZY_DICT shape before the first store
// has materialized the dict. Callers that need a non-nil dict to mutate
// should use EnsureDict.
func (i *Instance) Dict() *Dict { return i.dict }

// EnsureDict returns i.dict, allocating it on demand when the class
// declares __dict__ but the instance is in the LAZY_DICT shape (heap
// subclasses of built-ins, where NewInstance leaves dict nil until the
// first SetAttr). Returns nil only when the class lacks __dict__ in the
// first place (pure __slots__ class). Mirrors CPython's
// make_dict_from_instance_attributes which materializes the managed
// dict when a store can't fit in the inline-values shape.
//
// CPython: Objects/dictobject.c:6857 make_dict_from_instance_attributes
func (i *Instance) EnsureDict() *Dict {
	if i.dict == nil && i.Type().HasDict {
		i.dict = NewDict()
	}
	return i.dict
}

// instanceDealloc is the tp_dealloc slot for pure user-class instances
// (the *Instance layout, classes whose only storage base is object).
// It releases the counted reference the instance took on its type in
// NewInstance, mirroring the _Py_DECREF_TYPE(type) that subtype_dealloc
// performs once a heap-type instance is reclaimed. The IsUser/type-assert
// guards keep it a no-op for any object that did not take the matching
// reference (built-in subclass instances never route through NewInstance,
// so they never incref their type and must not decref it here).
//
// gopy leaves the per-instance __dict__ and slot array to the cycle
// collector and Go's GC, which already walk them via instanceTraverse;
// the type reference is the one piece refcounting alone owns, so it is
// the only release this slot performs.
//
// CPython: Objects/typeobject.c:2782 subtype_dealloc (_Py_DECREF_TYPE)
func instanceDealloc(o Object) {
	inst, ok := o.(*Instance)
	if !ok {
		return
	}
	if inst.typeReleased {
		return
	}
	// Detach a materialized __dict__ before the instance storage goes away.
	// CPython's dealloc path copies the inline values into dict-owned
	// storage (_PyObject_FreeInstanceAttributes -> _PyDict_DetachFromObject),
	// an allocation that the _testcapi.set_nomemory injector can fail. On
	// failure CPython cannot complete the copy, so it clears the dict and
	// reports the MemoryError through the unraisable hook (the dealloc path
	// has no caller to propagate to). gopy's dict already owns its storage,
	// so a successful detach is a no-op; only the fault path has observable
	// effect, and it must match CPython exactly: empty the dict and route a
	// MemoryError to sys.unraisablehook.
	//
	// CPython: Objects/dictobject.c:7530 _PyDict_DetachFromObject
	//          Objects/typeobject.c:2782 subtype_dealloc (clear_dict branch)
	if inst.dictExposed && inst.dict != nil && inst.inlineValid {
		inst.dictExposed = false
		if ConsumeAllocFault() {
			inst.dict.lock()
			inst.dict.clearContents()
			inst.dict.unlock()
			inst.inlineValid = false
			if WriteUnraisableHook != nil {
				WriteUnraisableHook(inst, "Exception ignored while detaching the instance dictionary", fmt.Errorf("MemoryError"))
			}
		}
	}
	t := inst.Type()
	if t != nil && t.IsUser {
		inst.typeReleased = true
		Decref(t)
	}
}

// instanceClear is the tp_clear slot for pure user-class instances. The
// cycle collector calls it from delete_garbage once the instance is
// proven unreachable, releasing the references its __dict__ holds so a
// cycle that runs through instance attributes is broken and the held
// values become collectible. A dict handed to Python (dictExposed) may
// be aliased by a live mapping object, so it is left to its own owner;
// only the sole-owner case is cleared, matching the dealloc path's old
// guard but firing from the collector instead of from refcount zero.
//
// CPython: Objects/typeobject.c:1411 subtype_clear
func instanceClear(o Object) {
	inst, ok := o.(*Instance)
	if !ok {
		return
	}
	if inst.dict != nil && !inst.dictExposed {
		ClearOwnedContents(inst.dict)
	}
}

// instanceTraverse visits every Object reachable from a user-class
// instance: each non-nil slot value plus the per-instance __dict__.
// The cycle collector calls this through Type.TpTraverse to detect
// cycles whose back-edges run through instance attributes.
//
// CPython's subtype_traverse does Py_VISIT(*dictptr); we mirror that
// because every Dict is now gc-tracked, so visiting the dict object
// itself (rather than walking its entries inline) keeps refcount
// accounting consistent with the dict's own tp_traverse.
//
// CPython: Objects/typeobject.c:1356 subtype_traverse
func instanceTraverse(o Object, visit Visitor) error {
	i, ok := o.(*Instance)
	if !ok {
		return nil
	}
	for _, s := range i.slots {
		if s == nil {
			continue
		}
		if err := visit(s); err != nil {
			return err
		}
	}
	if i.dict != nil {
		if err := visit(i.dict); err != nil {
			return err
		}
	}
	return nil
}

// SlotAt returns the value stored in slot idx, or nil when the slot
// is out of range or unset. Used by the LOAD_ATTR_SLOT fast-path arm
// to skip the descriptor protocol.
//
// CPython: Objects/descrobject.c:171 member_get (inline access path)
func (i *Instance) SlotAt(idx int) Object {
	if idx < 0 || idx >= len(i.slots) {
		return nil
	}
	return i.slots[idx]
}

// SetSlotAt writes value into slot idx. Returns false when the slot
// index is out of range so the caller can fall back. Used by the
// STORE_ATTR_SLOT fast-path arm to skip the descriptor protocol.
//
// The caller transfers its reference to value (steal semantics), so
// the slot is not incref'd here. Any value already in the slot is
// released, mirroring _STORE_ATTR_SLOT's Py_XDECREF(old_value) after
// it steals the new value into the member offset.
//
// CPython: Objects/descrobject.c:200 member_set (inline access path)
// CPython: Python/bytecodes.c:2540 _STORE_ATTR_SLOT
func (i *Instance) SetSlotAt(idx int, value Object) bool {
	if idx < 0 || idx >= len(i.slots) {
		return false
	}
	old := i.slots[idx]
	i.slots[idx] = value
	if old != nil {
		Decref(old)
	}
	return true
}

// instanceGetAttr is the tp_getattro slot for user-defined types.
// Lookup order matches CPython: type-level data descriptors win first,
// then instance __dict__, then type-level non-data descriptors. When
// no attribute is found, falls back to a user-defined __getattr__ hook
// on the type, mirroring slot_tp_getattr_hook.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr
// CPython: Objects/typeobject.c:8895 slot_tp_getattr_hook
func instanceGetAttr(o Object, name Object) (Object, error) {
	inst, ok := o.(*Instance)
	if !ok {
		return GenericGetAttr(o, name)
	}
	if name == nil || !IsSubtype(name.Type(), strType) {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := inst.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	// Data descriptor on the type wins.
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil && dt.DescrSet != nil {
			return dt.DescrGet(descr, o, tp)
		}
	}
	if inst.dict != nil {
		if v, err := inst.dict.GetItem(name); err == nil {
			Incref(v)
			return v, nil
		}
	}
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, o, tp)
		}
		// CPython: Objects/object.c:995 _PyObject_GenericGetAttrWithDict
		// Py_INCREF(res) before returning a plain type-level attribute.
		Incref(descr)
		return descr, nil
	}
	// Fall back to __getattr__ if the class defines it. The hook
	// receives the attribute name as its single argument and is
	// expected to return the value or raise AttributeError itself.
	if ga, _ := LookupDescriptor(tp, "__getattr__"); ga != nil {
		bound := ga
		if dg := ga.Type().DescrGet; dg != nil {
			b, err := dg(ga, o, tp)
			if err != nil {
				return nil, err
			}
			bound = b
		}
		return Call(bound, NewTuple([]Object{name}), nil)
	}
	if AttributeErrorFactory != nil {
		return nil, AttributeErrorFactory(o, attrNameStr(name))
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
}

// instanceSetAttr is the tp_setattro slot for user-defined types. A
// data descriptor on the type intercepts the store; otherwise the
// value lands in the instance __dict__. value==nil signals a delete.
//
// CPython: Objects/object.c:2040 PyObject_GenericSetAttr
func instanceSetAttr(o Object, name Object, value Object) error {
	inst, ok := o.(*Instance)
	if !ok {
		return GenericSetAttr(o, name, value)
	}
	if name == nil || !IsSubtype(name.Type(), strType) {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := inst.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	if inst.dict == nil {
		if !tp.HasDict {
			// __slots__ class without __dict__: the computed dict pointer
			// is NULL, so any name not covered by a type-level descriptor
			// is rejected. A type-level descriptor with no setter reads as
			// read-only; a missing name reports the longer "no __dict__"
			// message when tp_setattro is still the generic slot, and the
			// short message when a Python __setattr__ override forwarded
			// here through super().
			//
			// CPython: Objects/object.c:1990 _PyObject_GenericSetAttrWithDict
			if descr != nil {
				return fmt.Errorf("AttributeError: '%s' object attribute '%s' is read-only", tp.Name, attrNameStr(name))
			}
			if _, saOwner := LookupDescriptor(tp, "__setattr__"); saOwner == nil || saOwner == objectType {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s' and no __dict__ for setting new attributes", tp.Name, attrNameStr(name))
			}
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
		}
		if value == nil {
			// LAZY_DICT shape with a still-null managed dict: nothing to
			// delete, so raise AttributeError without materializing.
			//
			// CPython: Objects/object.c PyObject_GenericSetAttr (NULL dict,
			// delete branch)
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
		}
		// LAZY_DICT shape: first store materializes the managed dict.
		// CPython does this in _PyObject_StoreInstanceAttribute via
		// new_values / make_dict_from_instance_attributes.
		//
		// CPython: Objects/dictobject.c:6857 make_dict_from_instance_attributes
		inst.dict = NewDict()
	}
	if value == nil {
		if _, err := inst.dict.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
		}
		// Deleting an instance attribute materializes the dict in
		// CPython (clears PyDictValues.valid). gopy already keeps a
		// real per-instance dict; flipping inlineValid drops the
		// instance out of WITH_VALUES specialization.
		inst.inlineValid = false
		return inst.dict.DelItem(name)
	}
	// Record the attribute name in the type's shared-keys set the
	// first time it shows up on any instance. CPython updates
	// ht_cached_keys lazily through insert_split_key; gopy keeps the
	// same monotonic invariant: cachedKeys only grows, and every
	// growth bumps cachedKeysVersion so caches stamped before the
	// change reject on the next hit.
	//
	// CPython: Objects/dictobject.c:5132 insert_split_key
	if u, ok := name.(*Unicode); ok {
		tp.AddCachedKey(u.v)
	}
	// The inline values / shared-keys table holds at most
	// SHARED_KEYS_MAX_SIZE distinct attribute names. Storing a new name
	// once the instance already carries that many leaves no usable slot,
	// so CPython falls back to a combined dict and clears the values'
	// valid flag. gopy keeps one real dict either way, so it only has to
	// flip inlineValid to drop out of the WITH_VALUES specialization.
	//
	// CPython: Objects/dictobject.c:1900 insertdict (no space in shared keys)
	if inst.inlineValid {
		if _, err := inst.dict.GetItem(name); err != nil && inst.dict.Len() >= sharedKeysMaxSize {
			inst.inlineValid = false
		}
	}
	return inst.dict.SetItem(name, value)
}

// sharedKeysMaxSize caps the number of attribute names a type's shared
// keys table (and thus an instance's inline values array) can hold.
//
// CPython: Include/internal/pycore_dict.h:226 SHARED_KEYS_MAX_SIZE
const sharedKeysMaxSize = 30
