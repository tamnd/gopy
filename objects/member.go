// MemberDescr is the data descriptor that backs a __slots__ entry. It
// stores the slot name plus the index into the per-instance slots
// array; DescrGet/DescrSet read and write that index. An unset slot
// (nil entry) raises AttributeError on read, matching CPython's
// Py_T_OBJECT_EX behavior.
//
// CPython: Objects/descrobject.c:144 PyMemberDescr_Type plus
//          Objects/typeobject.c:4401 type_new_descriptors

package objects

import "fmt"

// MemberDescr is a (name, slot index) pair exposed as a data
// descriptor on the type that declared it.
//
// CPython: Objects/descrobject.c:1623 PyMemberDescr_Type
type MemberDescr struct {
	Header
	name  string
	index int
	owner *Type
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
// at index in the instance slots array.
//
// CPython: Objects/descrobject.c:1696 PyDescr_NewMember
func NewMemberDescr(name string, index int) *MemberDescr {
	d := &MemberDescr{name: name, index: index}
	d.init(MemberDescrType)
	return d
}

// Name returns the attribute name this descriptor binds to.
func (d *MemberDescr) Name() string { return d.name }

// Index returns the slot index this descriptor reads/writes.
func (d *MemberDescr) Index() int { return d.index }

func memberDescrRepr(o Object) (string, error) {
	d := o.(*MemberDescr)
	return "<member '" + d.name + "'>", nil
}

// memberDescrGet reads the slot. owner==nil means access on the class
// itself (class.slot_name), which returns the descriptor unchanged
// like other descriptors. Reading an unset slot raises AttributeError
// because Py_T_OBJECT_EX forbids the implicit None translation.
//
// CPython: Objects/descrobject.c:171 member_get
func memberDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	d := descr.(*MemberDescr)
	if owner == nil {
		return descr, nil
	}
	inst, ok := owner.(*Instance)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
			d.name, "instance", typeNameOf(owner))
	}
	if d.index < 0 || d.index >= len(inst.slots) {
		return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
			inst.Type().Name, d.name)
	}
	v := inst.slots[d.index]
	if v == nil {
		return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
			inst.Type().Name, d.name)
	}
	return v, nil
}

// memberDescrSet writes the slot. value==nil deletes (clears) it,
// after which a subsequent read raises AttributeError again.
//
// CPython: Objects/descrobject.c:198 member_set
func memberDescrSet(descr Object, owner Object, value Object) error {
	d := descr.(*MemberDescr)
	inst, ok := owner.(*Instance)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object",
			d.name, "instance", typeNameOf(owner))
	}
	if d.index < 0 || d.index >= len(inst.slots) {
		return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'",
			inst.Type().Name, d.name)
	}
	inst.slots[d.index] = value
	return nil
}
