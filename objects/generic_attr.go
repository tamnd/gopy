// Generic attribute access. PyObject_GenericGetAttr / GenericSetAttr
// are the default tp_getattro / tp_setattro slots: they walk the
// type MRO looking for a descriptor and dispatch through tp_descr_get /
// tp_descr_set. Built-in types whose attributes live on the type
// (rather than in an instance __dict__) wire these as their
// Getattro / Setattro slot.
//
// CPython: Objects/object.c:1809 _PyObject_GenericGetAttrWithDict

package objects

import "fmt"

// GenericGetAttr is the default Getattro slot. It looks up name in
// the type's MRO and:
//   - calls DescrGet if the descriptor is a data descriptor (has both
//     DescrGet and DescrSet);
//   - calls DescrGet if it is a non-data descriptor (has only
//     DescrGet);
//   - returns the descriptor itself if it has neither slot (a plain
//     class attribute);
//   - raises AttributeError when nothing is found.
//
// Instance __dict__ lookup is not yet implemented; data descriptors
// always win and missing attributes always raise AttributeError, which
// matches CPython's behavior for types without Py_TPFLAGS_MANAGED_DICT.
//
// CPython: Objects/object.c:1932 PyObject_GenericGetAttr
func GenericGetAttr(o Object, name Object) (Object, error) {
	if name == nil || name.Type() != strType {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, o, tp)
		}
		return descr, nil
	}
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
}

// GenericSetAttr is the default Setattro slot. It looks up name in the
// type's MRO and calls DescrSet when the descriptor implements it,
// raising AttributeError otherwise. value==nil signals a delete and is
// passed through to DescrSet (descriptors handle the dispatch).
//
// CPython: Objects/object.c:2040 PyObject_GenericSetAttr
func GenericSetAttr(o Object, name Object, value Object) error {
	if name == nil || name.Type() != strType {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp := o.Type()
	descr, _ := LookupDescriptor(tp, attrNameStr(name))
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
		// Non-data descriptor (no __set__): a store on the instance is
		// allowed and shadows the descriptor in the instance dict. Only
		// fail here when the type has no instance dict to fall back on.
		//
		// CPython: Objects/object.c:2040 PyObject_GenericSetAttr
		// (the "if (f != NULL) ... else PyObject_GenericSetAttr stores
		// in dict" arm)
	}
	if inst, ok := o.(*Instance); ok && inst.dict != nil {
		if value == nil {
			if _, err := inst.dict.GetItem(name); err != nil {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
			}
			return inst.dict.DelItem(name)
		}
		return inst.dict.SetItem(name, value)
	}
	if value == nil {
		return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
	}
	return fmt.Errorf("AttributeError: '%s' object has no attribute '%s' and no __dict__ for setting new attributes", tp.Name, attrNameStr(name))
}
