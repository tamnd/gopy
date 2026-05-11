// type_getattro: attribute access on a type object. Looks up name in
// the metatype MRO first (data descriptors there win), then walks the
// type's own MRO and dispatches through tp_descr_get with owner=nil so
// class-level access (e.g. `D.method`) returns the unbound descriptor.
//
// CPython: Objects/typeobject.c:5063 type_getattro

package objects

import "fmt"

func init() {
	typeType.Getattro = typeGetAttr
	typeType.Setattro = typeSetAttr
}

// typeGetAttr is the tp_getattro slot for typeType. The receiver is a
// *Type; lookup walks the metatype MRO for data descriptors, then the
// type's own MRO, then non-data descriptors from the metatype.
//
// CPython: Objects/typeobject.c:5063 type_getattro
func typeGetAttr(o Object, name Object) (Object, error) {
	if name == nil || name.Type() != strType {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp, ok := o.(*Type)
	if !ok {
		return GenericGetAttr(o, name)
	}
	metatype := tp.Type()
	nameStr := attrNameStr(name)

	metaAttr, _ := LookupDescriptor(metatype, nameStr)
	if metaAttr != nil {
		mt := metaAttr.Type()
		if mt.DescrGet != nil && mt.DescrSet != nil {
			return mt.DescrGet(metaAttr, o, metatype)
		}
	}

	attr, _ := LookupDescriptor(tp, nameStr)
	if attr != nil {
		at := attr.Type()
		if at.DescrGet != nil {
			return at.DescrGet(attr, nil, tp)
		}
		return attr, nil
	}

	if metaAttr != nil {
		mt := metaAttr.Type()
		if mt.DescrGet != nil {
			return mt.DescrGet(metaAttr, o, metatype)
		}
		return metaAttr, nil
	}

	return nil, fmt.Errorf("AttributeError: type object '%s' has no attribute '%s'", tp.Name, nameStr)
}

// typeSetAttr is the tp_setattro slot for typeType. Writes go to the
// type's own descriptor table (typeDescrTable) when there is no data
// descriptor on the metatype that intercepts. value==nil deletes.
//
// CPython: Objects/typeobject.c:5165 type_setattro
func typeSetAttr(o Object, name Object, value Object) error {
	if name == nil || name.Type() != strType {
		return fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	tp, ok := o.(*Type)
	if !ok {
		return GenericSetAttr(o, name, value)
	}
	if !tp.IsUser {
		return fmt.Errorf("TypeError: cannot set '%s' attribute of immutable type '%s'", attrNameStr(name), tp.Name)
	}
	metatype := tp.Type()
	descr, _ := LookupDescriptor(metatype, attrNameStr(name))
	if descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	nameStr := attrNameStr(name)
	if value == nil {
		m, ok := typeDescrTable[tp]
		if !ok {
			return fmt.Errorf("AttributeError: type object '%s' has no attribute '%s'", tp.Name, nameStr)
		}
		if _, ok := m[nameStr]; !ok {
			return fmt.Errorf("AttributeError: type object '%s' has no attribute '%s'", tp.Name, nameStr)
		}
		delete(m, nameStr)
		tp.InvalidateVersionTag()
		return nil
	}
	SetTypeDescr(tp, nameStr, value)
	tp.InvalidateVersionTag()
	return nil
}
