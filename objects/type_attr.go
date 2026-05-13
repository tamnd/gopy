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

	// Register __annotations__ and __annotate__ as GetSetDescr entries on
	// typeType so they appear in type.__dict__. CPython exposes these as
	// PyGetSetDescr_Type entries in type's tp_getset table; gopy mirrors
	// that by installing them here so annotationlib can do:
	//   _BASE_GET_ANNOTATIONS = type.__dict__["__annotations__"].__get__
	//
	// CPython: Objects/typeobject.c:2069 type_get_annotations (getset entry)
	// CPython: Objects/typeobject.c:1990 type_get_annotate (getset entry)
	SetTypeDescr(typeType, "__annotations__", NewGetSetDescr("__annotations__",
		func(o Object) (Object, error) {
			tp, ok := o.(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__annotations__' requires 'type' object")
			}
			return typeGetAnnotations(tp)
		},
		func(o Object, v Object) error {
			tp, ok := o.(*Type)
			if !ok {
				return fmt.Errorf("TypeError: descriptor '__annotations__' requires 'type' object")
			}
			return typeSetAnnotations(tp, v)
		},
	))
	SetTypeDescr(typeType, "__annotate__", NewGetSetDescr("__annotate__",
		func(o Object) (Object, error) {
			tp, ok := o.(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__annotate__' requires 'type' object")
			}
			return typeGetAnnotate(tp)
		},
		func(o Object, v Object) error {
			tp, ok := o.(*Type)
			if !ok {
				return fmt.Errorf("TypeError: descriptor '__annotate__' requires 'type' object")
			}
			return typeSetAnnotate(tp, v)
		},
	))
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

	// type.__dict__ returns a view of the type's own descriptor table.
	// CPython: Objects/typeobject.c:5091 type_getattro (__dict__ special-case via
	// tp_dict / PyObject_GenericGetDict)
	if nameStr == "__dict__" {
		d := NewDict()
		if descrs, ok2 := typeDescrTable[tp]; ok2 {
			for k, v := range descrs {
				if err := d.SetItem(NewStr(k), v); err != nil {
					return nil, err
				}
			}
		}
		return d, nil
	}

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

	// PEP 649 lazy __annotations__ / __annotate__. The getters in
	// objects/type_annotations.go own the full slot semantics
	// (HEAPTYPE check, None fallback for __annotate__, cache on first
	// read for __annotations__). Routing through them here keeps the
	// observable behavior consistent whether the caller goes via
	// typeGetAttr or via type.__annotate__ on a metatype descriptor.
	//
	// CPython: Objects/typeobject.c:2069 type_get_annotations
	// CPython: Objects/typeobject.c:1990 type_get_annotate
	if nameStr == "__annotations__" {
		return typeGetAnnotations(tp)
	}
	if nameStr == "__annotate__" {
		return typeGetAnnotate(tp)
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
	// PEP 649 setters carry side effects: writing __annotate__ drops
	// the cached __annotations__, and writing __annotations__ drops
	// __annotate__. Built-in types reject both.
	//
	// CPython: Objects/typeobject.c:2030 type_set_annotate
	// CPython: Objects/typeobject.c:2139 type_set_annotations
	if nameStr == "__annotate__" {
		return typeSetAnnotate(tp, value)
	}
	if nameStr == "__annotations__" {
		return typeSetAnnotations(tp, value)
	}
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
