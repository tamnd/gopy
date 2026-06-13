// MemberDescr is the data descriptor that backs a __slots__ entry and the
// PyMemberDef rows built-in types expose (complex.real, complex.imag). For
// a slot it stores the slot name plus the index into the per-instance
// slots array; DescrGet/DescrSet read and write that index. An unset slot
// (nil entry) raises AttributeError on read, matching CPython's
// Py_T_OBJECT_EX behavior. For a built-in member it carries a getter (and
// optional setter) closure that reads the value off the host object's Go
// fields, so the descriptor still reports as member_descriptor the way
// PyMemberDescr_Type does.
//
// CPython: Objects/descrobject.c:144 PyMemberDescr_Type plus
//          Objects/typeobject.c:4401 type_new_descriptors
//          Objects/complexobject.c:1337 complex_members

package objects

import "fmt"

// MemberDescr is a slot/member descriptor exposed as a data descriptor on
// the type that declared it. Slot descriptors use index; built-in member
// descriptors use getter/setter.
//
// CPython: Objects/descrobject.c:1623 PyMemberDescr_Type
type MemberDescr struct {
	Header
	name  string
	index int
	owner *Type
	doc   string
	// getter/setter, when non-nil, back a built-in PyMemberDef rather than
	// a __slots__ array index. A nil setter on a getter-backed member means
	// the member is read-only (Py_READONLY).
	getter   func(self Object) (Object, error)
	setter   func(self Object, value Object) error
	readonly bool
}

// MemberDescrType is the type singleton for __slots__ descriptors.
//
// CPython: Objects/descrobject.c:1623 PyMemberDescr_Type
var MemberDescrType = NewType("member_descriptor", []*Type{objectType})

func init() {
	MemberDescrType.Repr = memberDescrRepr
	MemberDescrType.Str = memberDescrRepr
	MemberDescrType.DescrGet = memberDescrGet
	MemberDescrType.DescrSet = memberDescrSet
	addDescriptorSlotWrappers(MemberDescrType)
	addDescrIntrospectionDescriptors(MemberDescrType)
}

// Owner returns the type this member descriptor is registered on.
func (d *MemberDescr) Owner() *Type { return d.owner }

// NewMemberDescr builds a slot descriptor for name backed by the slot
// at index in the instance slots array of owner.
//
// CPython: Objects/descrobject.c:1696 PyDescr_NewMember
func NewMemberDescr(owner *Type, name string, index int) *MemberDescr {
	d := &MemberDescr{name: name, index: index, owner: owner}
	d.init(MemberDescrType)
	return d
}

// NewBuiltinMember builds a member descriptor backed by getter/setter
// closures, mirroring a PyMemberDef row on a built-in type. A nil setter
// makes the member read-only (Py_READONLY), so writes raise the
// "not writable" AttributeError.
//
// CPython: Objects/descrobject.c:1696 PyDescr_NewMember
func NewBuiltinMember(owner *Type, name, doc string, getter func(Object) (Object, error), setter func(Object, Object) error) *MemberDescr {
	d := &MemberDescr{name: name, owner: owner, doc: doc, getter: getter, setter: setter, readonly: setter == nil}
	d.init(MemberDescrType)
	return d
}

// Name returns the attribute name this descriptor binds to.
func (d *MemberDescr) Name() string { return d.name }

// Doc returns the member's docstring, or "".
func (d *MemberDescr) Doc() string { return d.doc }

// Index returns the slot index this descriptor reads/writes.
func (d *MemberDescr) Index() int { return d.index }

// memberDescrRepr renders the descriptor. CPython's member_repr prints the
// owning type, e.g. <member 'real' of 'complex' objects>.
//
// CPython: Objects/descrobject.c:148 descr_repr / member_repr
func memberDescrRepr(o Object) (string, error) {
	d := o.(*MemberDescr)
	if d.owner != nil {
		return "<member '" + d.name + "' of '" + d.owner.Name + "' objects>", nil
	}
	return "<member '" + d.name + "'>", nil
}

// memberDescrGet reads the slot. owner==nil means access on the class
// itself (class.slot_name), which returns the descriptor unchanged
// like other descriptors. Reading an unset slot raises AttributeError
// because Py_T_OBJECT_EX forbids the implicit None translation.
//
// For non-Instance objects that implement AttrDictHolder (C-extension
// type subclasses), the slot value is stored by name in the attrs dict.
//
// CPython: Objects/descrobject.c:171 member_get
func memberDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	d := descr.(*MemberDescr)
	if owner == nil {
		return descr, nil
	}
	// Built-in member backed by a getter closure (complex.real, ...).
	if d.getter != nil {
		if d.owner != nil && !IsSubtype(owner.Type(), d.owner) {
			return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
				d.name, d.owner.Name, typeNameOf(owner))
		}
		return d.getter(owner)
	}
	if inst, ok := owner.(*Instance); ok {
		if d.index < 0 || d.index >= len(inst.slots) {
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
				inst.Type().FullyQualifiedName(), d.name)
		}
		v := inst.slots[d.index]
		if v == nil {
			return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
				inst.Type().FullyQualifiedName(), d.name)
		}
		// CPython: Objects/descrobject.c:171 member_get Py_INCREF(v)
		Incref(v)
		return v, nil
	}
	// C-extension subclass: fall back to per-instance attrs dict.
	if h, ok := owner.(AttrDictHolder); ok {
		ad := h.AttrDict()
		if ad != nil {
			key := NewStr(d.name)
			if v, err := ad.GetItem(key); err == nil {
				Incref(v)
				return v, nil
			}
		}
		return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
			owner.Type().FullyQualifiedName(), d.name)
	}
	return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
		d.name, "instance", typeNameOf(owner))
}

// memberDescrSet writes the slot. value==nil deletes (clears) it,
// after which a subsequent read raises AttributeError again.
//
// For non-Instance objects that implement AttrDictHolder (C-extension
// type subclasses), the slot value is stored by name in the attrs dict.
//
// CPython: Objects/descrobject.c:198 member_set
func memberDescrSet(descr Object, owner Object, value Object) error {
	d := descr.(*MemberDescr)
	// Built-in member: read-only members reject all writes; a writable one
	// dispatches to its setter.
	if d.getter != nil {
		if d.readonly || d.setter == nil {
			return fmt.Errorf("AttributeError: attribute '%s' of '%s' objects is not writable", d.name, d.owner.Name)
		}
		return d.setter(owner, value)
	}
	if inst, ok := owner.(*Instance); ok {
		if d.index < 0 || d.index >= len(inst.slots) {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
				inst.Type().FullyQualifiedName(), d.name)
		}
		// Py_T_OBJECT_EX: adopt a new reference to the value, then drop
		// the slot's previous occupant. Setting the new value before the
		// decref matches CPython so a self-referential overwrite cannot
		// finalize the value mid-store.
		//
		// CPython: Programs/_testcapi/structmembers.c via
		//          Python/structmember.c:218 PyMember_SetOne (Py_T_OBJECT_EX)
		old := inst.slots[d.index]
		// Deleting an already-unset Py_T_OBJECT_EX member raises, matching
		// PyMember_SetOne's NULL-on-NULL guard.
		//
		// CPython: Python/structmember.c:282 PyMember_SetOne (T_OBJECT_EX)
		if value == nil && old == nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
				inst.Type().FullyQualifiedName(), d.name)
		}
		if value != nil {
			Incref(value)
		}
		inst.slots[d.index] = value
		if old != nil {
			Decref(old)
		}
		return nil
	}
	// C-extension subclass: fall back to per-instance attrs dict.
	if h, ok := owner.(AttrDictHolder); ok {
		key := NewStr(d.name)
		if value == nil {
			ad := h.AttrDict()
			if ad == nil {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
					owner.Type().FullyQualifiedName(), d.name)
			}
			return ad.DelItem(key)
		}
		return h.EnsureAttrDict().SetItem(key, value)
	}
	return fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
		d.name, "instance", typeNameOf(owner))
}
