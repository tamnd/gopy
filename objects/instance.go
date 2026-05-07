// Instance is the v0.10 carrier for an object whose class was defined
// in Python. Its Header.typ points at the user-defined Type built by
// __build_class__; attribute lookup walks the type's MRO via
// LookupDescriptor and falls through to the per-instance __dict__.
//
// CPython models the same shape with a PyObject + tp_dictoffset slot;
// gopy keeps the dict directly on the struct since every user class
// gets one until __slots__ lands (1672).
//
// CPython: Objects/typeobject.c PyBaseObject_Type slots
//          (tp_getattro -> PyObject_GenericGetAttr,
//           tp_setattro -> PyObject_GenericSetAttr)

package objects

import "fmt"

// Instance backs a Python-level object whose type is a user-defined
// class. Header.typ is the class; dict holds per-instance attributes.
type Instance struct {
	Header

	dict *Dict
}

// NewInstance allocates a fresh Instance bound to t. The instance
// __dict__ is empty.
//
// CPython: Objects/typeobject.c:1748 type_call (object_new path)
func NewInstance(t *Type) *Instance {
	inst := &Instance{dict: NewDict()}
	inst.init(t)
	return inst
}

// Dict returns the instance __dict__. Mutating it is how attribute
// stores land.
func (i *Instance) Dict() *Dict { return i.dict }

// instanceGetAttr is the tp_getattro slot for user-defined types.
// Lookup order matches CPython: type-level data descriptors win first,
// then instance __dict__, then type-level non-data descriptors.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr
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
	if v, err := inst.dict.GetItem(name); err == nil {
		return v, nil
	}
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, o, tp)
		}
		return descr, nil
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
	if value == nil {
		if _, err := inst.dict.GetItem(name); err != nil {
			return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
		}
		return inst.dict.DelItem(name)
	}
	return inst.dict.SetItem(name, value)
}
