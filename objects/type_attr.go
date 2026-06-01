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
	// type.__dict__ getset. inspect.py grabs this at module load via
	//   _get_dunder_dict_of_class = type.__dict__["__dict__"].__get__
	// and later calls it on a class to materialize the class's namespace.
	// CPython exposes the descriptor through type_getsets so it shows up
	// in type.__dict__; the gopy port mirrors that here.
	//
	// CPython: Objects/typeobject.c:1057 type_dict (getset entry)
	SetTypeDescr(typeType, "__dict__", NewGetSetDescr("__dict__",
		func(o Object) (Object, error) {
			tp, ok := o.(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__dict__' requires 'type' object")
			}
			d := NewDict()
			var setErr error
			TypeOwnDescrItems(tp, func(k string, v Object) {
				if setErr != nil {
					return
				}
				setErr = d.SetItem(NewStr(k), v)
			})
			if setErr != nil {
				return nil, setErr
			}
			// type_dict wraps the namespace in a read-only mappingproxy so
			// callers cannot mutate a class's tp_dict through __dict__.
			// CPython: Objects/typeobject.c:1064 type_dict returns
			// PyDictProxy_New(type->tp_dict).
			return NewMappingProxy(d)
		},
		nil,
	))
	// type.__basicsize__ / type.__itemsize__ getsets expose tp_basicsize
	// and tp_itemsize so int.__basicsize__ + int.__itemsize__ * ndigits
	// reproduces CPython's PyLongObject allocation footprint. Lookup walks
	// the MRO so a user subclass of int inherits the parent's layout
	// sizes without needing every type to stamp its own.
	//
	// CPython: Objects/typeobject.c:1245 type_basicsize (getset entry)
	// CPython: Objects/typeobject.c:1252 type_itemsize (getset entry)
	SetTypeDescr(typeType, "__basicsize__", NewGetSetDescr("__basicsize__",
		func(o Object) (Object, error) {
			tp, ok := o.(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__basicsize__' requires 'type' object")
			}
			return NewInt(int64(typeBasicSize(tp))), nil
		},
		nil,
	))
	SetTypeDescr(typeType, "__itemsize__", NewGetSetDescr("__itemsize__",
		func(o Object) (Object, error) {
			tp, ok := o.(*Type)
			if !ok {
				return nil, fmt.Errorf("TypeError: descriptor '__itemsize__' requires 'type' object")
			}
			return NewInt(int64(typeItemSize(tp))), nil
		},
		nil,
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

// typeBasicSize walks the MRO and returns the first non-zero BaseSize.
// A user subclass of int inherits the parent's tp_basicsize this way
// rather than having every NewUserTypeMetaE call stamp its own.
//
// CPython: Objects/typeobject.c:1245 type_basicsize (getter reads
// tp_basicsize on self directly; gopy stores BaseSize on the built-in
// type alone and falls back via MRO for heap subclasses)
func typeBasicSize(t *Type) int {
	for _, cls := range t.MRO {
		if cls.BaseSize != 0 {
			return cls.BaseSize
		}
	}
	return 0
}

// typeItemSize is the tp_itemsize companion of typeBasicSize.
//
// CPython: Objects/typeobject.c:1252 type_itemsize
func typeItemSize(t *Type) int {
	for _, cls := range t.MRO {
		if cls.ItemSize != 0 {
			return cls.ItemSize
		}
	}
	return 0
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
		var setErr error
		TypeOwnDescrItems(tp, func(k string, v Object) {
			if setErr != nil {
				return
			}
			setErr = d.SetItem(NewStr(k), v)
		})
		if setErr != nil {
			return nil, setErr
		}
		// type_dict hands back a read-only mappingproxy, not the bare
		// namespace, so a class's tp_dict cannot be mutated through
		// __dict__. CPython: Objects/typeobject.c:1064 type_dict.
		return NewMappingProxy(d)
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
		if tp.ClassAttrDict != nil {
			_ = tp.ClassAttrDict.DelItem(NewStr(nameStr))
		}
		tp.InvalidateVersionTag()
		return nil
	}
	SetTypeDescr(tp, nameStr, value)
	tp.InvalidateVersionTag()
	return nil
}
