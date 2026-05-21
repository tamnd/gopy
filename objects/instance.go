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

// instanceTraverse visits every Object reachable from a user-class
// instance: each non-nil slot value plus every key and value stored
// in the per-instance __dict__. The cycle collector calls this
// through Type.TpTraverse to detect cycles whose back-edges run
// through instance attributes.
//
// gopy walks the dict's entries inline rather than visiting the dict
// object itself (CPython's subtype_traverse does Py_VISIT(*dictptr)
// because the dict has its own gc-tracked head). Inlining avoids the
// requirement that every per-instance dict be tracked separately,
// which gopy does not do today, and keeps the traversal complete.
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
		if err := dictTraverse(i.dict, visit); err != nil {
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
// CPython: Objects/descrobject.c:200 member_set (inline access path)
func (i *Instance) SetSlotAt(idx int, value Object) bool {
	if idx < 0 || idx >= len(i.slots) {
		return false
	}
	i.slots[idx] = value
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
	if name == nil || name.Type() != strType {
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
			return v, nil
		}
	}
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, o, tp)
		}
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
	if name == nil || name.Type() != strType {
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
			// __slots__ class without __dict__: any name not covered by
			// a type-level descriptor is rejected, mirroring CPython's
			// PyObject_GenericSetAttr when tp_dictoffset == 0.
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
	return inst.dict.SetItem(name, value)
}
