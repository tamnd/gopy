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

// AttrDictHolder is implemented by objects that carry their own
// per-instance attribute dict. The generic attr machinery consults it
// for objects that are not plain *Instance so a Python subclass of a
// C-port type (e.g. random.Random subclassing _random.Random) can store
// attributes set in its __init__ without each type needing its own
// getattro/setattro slot.
//
// AttrDict returns the current dict (nil if no attribute has been
// stored yet); EnsureAttrDict allocates on first call and returns it.
//
// CPython: Include/object.h MANAGED_DICT flag + _PyObject_GetDictPtr
type AttrDictHolder interface {
	AttrDict() *Dict
	EnsureAttrDict() *Dict
}

// GenericGetAttr is the default Getattro slot. It looks up name in
// the type's MRO and:
//   - calls DescrGet if the descriptor is a data descriptor (has both
//     DescrGet and DescrSet);
//   - falls through to the instance __dict__ when the object exposes
//     one via AttrDictHolder or is an *Instance;
//   - calls DescrGet if it is a non-data descriptor (has only
//     DescrGet);
//   - returns the descriptor itself if it has neither slot (a plain
//     class attribute);
//   - raises AttributeError when nothing is found.
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
		if dt.DescrGet != nil && dt.DescrSet != nil {
			return dt.DescrGet(descr, o, tp)
		}
	}
	if d := instanceAttrDict(o); d != nil {
		if v, err := d.GetItem(name); err == nil {
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
	return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
}

// GenericSetAttr is the default Setattro slot. It looks up name in the
// type's MRO and calls DescrSet when the descriptor implements it,
// otherwise stores in the instance __dict__ (from *Instance.dict or an
// AttrDictHolder). value==nil signals a delete.
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
	}
	if inst, ok := o.(*Instance); ok && inst.dict != nil {
		if value == nil {
			if _, err := inst.dict.GetItem(name); err != nil {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
			}
			inst.inlineValid = false
			return inst.dict.DelItem(name)
		}
		// Mirror instanceSetAttr: any name first stored on an instance
		// must enter the type's shared-keys set so the LOAD_ATTR
		// specializer refuses NONDESCRIPTOR_WITH_VALUES for it. Frozen
		// dataclass __init__ writes through object.__setattr__, which
		// lands here instead of instanceSetAttr, so the AddCachedKey
		// hook has to live in both places.
		//
		// CPython: Objects/dictobject.c:5132 insert_split_key
		if u, ok := name.(*Unicode); ok {
			tp.AddCachedKey(u.v)
		}
		return inst.dict.SetItem(name, value)
	}
	if h, ok := o.(AttrDictHolder); ok && tp.HasDict {
		if value == nil {
			d := h.AttrDict()
			if d == nil {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
			}
			if _, err := d.GetItem(name); err != nil {
				return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
			}
			return d.DelItem(name)
		}
		if u, ok := name.(*Unicode); ok {
			tp.AddCachedKey(u.v)
		}
		return h.EnsureAttrDict().SetItem(name, value)
	}
	if value == nil {
		return fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", tp.Name, attrNameStr(name))
	}
	return fmt.Errorf("AttributeError: '%s' object has no attribute '%s' and no __dict__ for setting new attributes", tp.Name, attrNameStr(name))
}

// instanceAttrDict returns the per-instance attribute dict for o (an
// *Instance's dict or an AttrDictHolder's AttrDict). Returns nil when
// the object has no per-instance dict slot or has not allocated one
// yet.
func instanceAttrDict(o Object) *Dict {
	switch v := o.(type) {
	case *Instance:
		return v.dict
	case AttrDictHolder:
		return v.AttrDict()
	}
	return nil
}
