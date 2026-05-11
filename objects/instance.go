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
}

// NewInstance allocates a fresh Instance bound to t. The instance
// __dict__ is empty when t.HasDict; the slots array is sized to
// t.Slots and starts all-nil (each slot reads as AttributeError until
// assigned).
//
// CPython: Objects/typeobject.c:1748 type_call (object_new path)
func NewInstance(t *Type) *Instance {
	inst := &Instance{}
	if t.HasDict {
		inst.dict = NewDict()
	}
	if n := len(t.Slots); n > 0 {
		inst.slots = make([]Object, n)
	}
	inst.init(t)
	return inst
}

// Dict returns the instance __dict__. Mutating it is how attribute
// stores land. Returns nil when the class declared __slots__ without
// __dict__.
func (i *Instance) Dict() *Dict { return i.dict }

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
		// __slots__ class without __dict__: any name not covered by a
		// type-level descriptor is rejected, mirroring CPython's
		// PyObject_GenericSetAttr when tp_dictoffset == 0.
		return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
	}
	if value == nil {
		if _, err := inst.dict.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
		}
		return inst.dict.DelItem(name)
	}
	return inst.dict.SetItem(name, value)
}
