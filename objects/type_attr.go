// type_getattro: attribute access on a type object. Looks up name in
// the metatype MRO first (data descriptors there win), then walks the
// type's own MRO and dispatches through tp_descr_get with owner=nil so
// class-level access (e.g. `D.method`) returns the unbound descriptor.
//
// CPython: Objects/typeobject.c:5063 type_getattro

package objects

import (
	"fmt"
	"strings"
)

func init() {
	typeType.Getattro = typeGetAttr
	typeType.Setattro = typeSetAttr
	// CPython: Objects/typeobject.c:6434 type_dealloc (set on typeType so
	// heap type objects are cleaned up when their refcount reaches zero).
	typeType.Dealloc = typeUserDealloc

	registerTypeAnnotationDescrs()
	registerTypeDictDescr()
	registerTypeLayoutDescrs()
	registerTypeAttrMethods()
	registerTypeNameDescrs()
}

// asType narrows a getset/method receiver to *Type, returning the same
// descriptor-requires-type TypeError CPython's getset wrappers raise when
// the slot is invoked on a non-type. Centralizing the cast keeps the
// per-descriptor closures (and init's cognitive complexity) small.
func asType(o Object, attr string) (*Type, error) {
	tp, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires 'type' object", attr)
	}
	return tp, nil
}

// registerTypeAnnotationDescrs installs __annotations__ and __annotate__ as
// GetSetDescr entries on typeType so they appear in type.__dict__. CPython
// exposes these as PyGetSetDescr_Type entries in type's tp_getset table so
// annotationlib can do:
//
//	_BASE_GET_ANNOTATIONS = type.__dict__["__annotations__"].__get__
//
// CPython: Objects/typeobject.c:2069 type_get_annotations (getset entry)
// CPython: Objects/typeobject.c:1990 type_get_annotate (getset entry)
func registerTypeAnnotationDescrs() {
	SetTypeDescr(typeType, "__annotations__", NewGetSetDescr("__annotations__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__annotations__")
			if err != nil {
				return nil, err
			}
			return typeGetAnnotations(tp)
		},
		func(o Object, v Object) error {
			tp, err := asType(o, "__annotations__")
			if err != nil {
				return err
			}
			return typeSetAnnotations(tp, v)
		},
	))
	SetTypeDescr(typeType, "__annotate__", NewGetSetDescr("__annotate__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__annotate__")
			if err != nil {
				return nil, err
			}
			return typeGetAnnotate(tp)
		},
		func(o Object, v Object) error {
			tp, err := asType(o, "__annotate__")
			if err != nil {
				return err
			}
			return typeSetAnnotate(tp, v)
		},
	))
}

// registerTypeDictDescr installs the type.__dict__ getset. inspect.py grabs
// this at module load via
//
//	_get_dunder_dict_of_class = type.__dict__["__dict__"].__get__
//
// and later calls it on a class to materialize the class's namespace. CPython
// exposes the descriptor through type_getsets so it shows up in type.__dict__.
//
// CPython: Objects/typeobject.c:1057 type_dict (getset entry)
func registerTypeDictDescr() {
	SetTypeDescr(typeType, "__dict__", NewGetSetDescr("__dict__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__dict__")
			if err != nil {
				return nil, err
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
}

// registerTypeLayoutDescrs installs __basicsize__, __itemsize__ and __flags__.
// The size getsets expose tp_basicsize and tp_itemsize so int.__basicsize__ +
// int.__itemsize__ * ndigits reproduces CPython's PyLongObject allocation
// footprint; lookup walks the MRO so a user subclass of int inherits the
// parent's layout sizes. __flags__ exposes tp_flags: copyreg._reduce_ex walks
// the MRO and stops at the first base whose Py_TPFLAGS_HEAPTYPE bit is clear,
// so OR the heap bit in for user types (IsUser is gopy's HEAPTYPE stand-in).
//
// CPython: Objects/typeobject.c:1245 type_basicsize (getset entry)
// CPython: Objects/typeobject.c:1252 type_itemsize (getset entry)
// CPython: Objects/typeobject.c:1109 type_flags (getset entry)
func registerTypeLayoutDescrs() {
	SetTypeDescr(typeType, "__basicsize__", NewGetSetDescr("__basicsize__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__basicsize__")
			if err != nil {
				return nil, err
			}
			return NewInt(int64(typeBasicSize(tp))), nil
		},
		nil,
	))
	SetTypeDescr(typeType, "__itemsize__", NewGetSetDescr("__itemsize__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__itemsize__")
			if err != nil {
				return nil, err
			}
			return NewInt(int64(typeItemSize(tp))), nil
		},
		nil,
	))
	SetTypeDescr(typeType, "__flags__", NewGetSetDescr("__flags__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__flags__")
			if err != nil {
				return nil, err
			}
			flags := tp.TpFlags
			if tp.IsUser {
				flags |= TpFlagHeapType
			}
			return NewInt(int64(flags)), nil
		},
		nil,
	))
}

// registerTypeAttrMethods installs type.__setattr__ / type.__delattr__ wrapping
// typeSetAttr so that super().__setattr__(...) / super().__delattr__(...) in
// user metaclass bodies (e.g. EnumType) route through typeSetAttr rather than
// falling through to object's GenericSetAttr which does not understand *Type
// receivers.
//
// CPython: Objects/typeobject.c:5165 type_setattro (both branches)
func registerTypeAttrMethods() {
	SetTypeDescr(typeType, "__setattr__", NewMethodDescr(typeType, "__setattr__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("TypeError: type.__setattr__() takes exactly 3 arguments (%d given)", len(args))
		}
		if err := typeSetAttr(args[0], args[1], args[2]); err != nil {
			return nil, err
		}
		return None(), nil
	}))
	SetTypeDescr(typeType, "__delattr__", NewMethodDescr(typeType, "__delattr__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: type.__delattr__() takes exactly 2 arguments (%d given)", len(args))
		}
		if err := typeSetAttr(args[0], args[1], nil); err != nil {
			return nil, err
		}
		return None(), nil
	}))
}

// setTypeStrField is the shared setter for __qualname__/__name__/__module__:
// each rejects non-heap types with the immutable-type guard CPython's
// check_set_special_type_attr applies, then requires a str value before
// writing it back through assign. attr names the descriptor for error text.
func setTypeStrField(o Object, v Object, attr string, assign func(*Type, string)) error {
	tp, err := asType(o, attr)
	if err != nil {
		return err
	}
	if !tp.IsUser {
		return fmt.Errorf("TypeError: cannot set '%s' attribute of immutable type '%s'", attr, tp.Name)
	}
	u, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: type %s must be a str, not %s", attr, typeNameOf(v))
	}
	assign(tp, u.v)
	return nil
}

// registerTypeNameDescrs installs __qualname__, __name__ and __module__. Each
// getter falls back to the static tp_name components for built-ins; each
// setter is permitted only on heap types (IsUser==true).
//
// CPython: Objects/typeobject.c:1469 type_qualname / :1484 type_qualname_set
// CPython: Objects/typeobject.c:1415 type_name / :1430 type_set_name
// CPython: Objects/typeobject.c:1537 type_module / :1557 type_set_module
func registerTypeNameDescrs() {
	SetTypeDescr(typeType, "__qualname__", NewGetSetDescr("__qualname__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__qualname__")
			if err != nil {
				return nil, err
			}
			return TypeGetQualName(tp), nil
		},
		func(o Object, v Object) error {
			return setTypeStrField(o, v, "__qualname__", func(tp *Type, s string) { tp.Qualname = s })
		},
	))
	SetTypeDescr(typeType, "__name__", NewGetSetDescr("__name__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__name__")
			if err != nil {
				return nil, err
			}
			return TypeGetName(tp), nil
		},
		func(o Object, v Object) error {
			return setTypeStrField(o, v, "__name__", func(tp *Type, s string) { tp.Name = s })
		},
	))
	SetTypeDescr(typeType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			tp, err := asType(o, "__module__")
			if err != nil {
				return nil, err
			}
			return TypeGetModuleName(tp)
		},
		func(o Object, v Object) error {
			return setTypeStrField(o, v, "__module__", func(tp *Type, s string) { tp.Module = s })
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
// LookupDescriptor returns a borrowed reference from typeDescrTable.
// Every return path that hands the value to the caller must either
// Incref it directly or replace it with a new ref produced by DescrGet.
// This mirrors CPython's type_getattro which Py_INCREF's after every
// _PyType_Lookup call.
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

	// CPython: Objects/typeobject.c:5091 Py_INCREF(meta_attribute) after lookup
	metaAttr, _ := LookupDescriptor(metatype, nameStr)
	if metaAttr != nil {
		Incref(metaAttr)
		mt := metaAttr.Type()
		if mt.DescrGet != nil && mt.DescrSet != nil {
			// Data descriptor on metatype wins. DescrGet returns a new ref;
			// release our borrowed-to-owned incref.
			res, err := mt.DescrGet(metaAttr, o, metatype)
			Decref(metaAttr)
			return res, err
		}
	}

	// CPython: Objects/typeobject.c:5110 Py_INCREF(attribute) after lookup
	attr, _ := LookupDescriptor(tp, nameStr)
	if attr != nil {
		Incref(attr)
		at := attr.Type()
		if at.DescrGet != nil {
			res, err := at.DescrGet(attr, nil, tp)
			Decref(attr)
			return res, err
		}
		// Non-descriptor class attribute: return with our Incref as the
		// caller's new reference.
		if metaAttr != nil {
			Decref(metaAttr)
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
		if metaAttr != nil {
			Decref(metaAttr)
		}
		return typeGetAnnotations(tp)
	}
	if nameStr == "__annotate__" {
		if metaAttr != nil {
			Decref(metaAttr)
		}
		return typeGetAnnotate(tp)
	}

	if metaAttr != nil {
		mt := metaAttr.Type()
		if mt.DescrGet != nil {
			res, err := mt.DescrGet(metaAttr, o, metatype)
			Decref(metaAttr)
			return res, err
		}
		// Non-data descriptor on metatype: return with our Incref.
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
	// CPython gates type attribute writes on Py_TPFLAGS_IMMUTABLETYPE, not on
	// whether the type was defined in Python. Heap types built in C (struct
	// sequences) clear that flag and accept attribute writes.
	//
	// CPython: Objects/typeobject.c:5165 type_setattro (IMMUTABLETYPE check)
	if !tp.IsUser && tp.TpFlags&TpFlagImmutable != 0 {
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
		old := m[nameStr]
		delete(m, nameStr)
		// Release the typeDescrTable's owned reference.
		//
		// CPython: Objects/typeobject.c:5165 type_setattro
		// (PyObject_GenericSetAttr -> PyDict_DelItemString drops the ref)
		Decref(old)
		if tp.ClassAttrDict != nil {
			_ = tp.ClassAttrDict.DelItem(NewStr(nameStr))
		}
		// Deleting a special method must re-derive the affected slot from the
		// base MRO (and refresh inheritors) the same way assignment does.
		//
		// CPython: Objects/typeobject.c:11455 update_slot
		if isSlotDunderName(nameStr) {
			refixupSlotDispatchers(tp)
		}
		tp.InvalidateVersionTag()
		return nil
	}
	SetTypeDescr(tp, nameStr, value)
	// Assigning a generic __call__ re-points tp_call at slot_tp_call,
	// which is incompatible with vectorcall: clear Py_TPFLAGS_HAVE_VECTORCALL
	// on tp and every subclass that inherits the slot.
	//
	// CPython: Objects/typeobject.c:11392 update_one_slot
	if nameStr == "__call__" {
		clearVectorcallForCallOverride(tp)
	}
	// Assigning a special method to a live type must re-wire the matching
	// C-level slot (and its inheritors), so e.g. `C.__int__ = lambda ...`
	// makes int(c) dispatch to it. CPython runs update_one_slot for each
	// slotdef whose name matches; gopy re-runs the per-type fixup pass.
	//
	// CPython: Objects/typeobject.c:11455 update_slot / fixup_slot_dispatchers
	if isSlotDunderName(nameStr) {
		refixupSlotDispatchers(tp)
	}
	tp.InvalidateVersionTag()
	return nil
}

// isSlotDunderName reports whether name is a __dunder__ that could back a
// C-level type slot, so a write to it on a live type triggers a slot
// re-fixup. Plain attribute writes skip the work.
func isSlotDunderName(name string) bool {
	return len(name) > 4 && strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
}

// refixupSlotDispatchers re-runs fixupSlotDispatchers on t and every
// subclass that does not override the affected slots, mirroring how
// update_slot walks the subclass tree.
//
// CPython: Objects/typeobject.c:11455 update_slot (recursion over subclasses)
func refixupSlotDispatchers(t *Type) {
	fixupSlotDispatchers(t)
	for _, sub := range t.Subclasses() {
		refixupSlotDispatchers(sub)
	}
}

// clearVectorcallForCallOverride re-wires tp_call to the generic
// dispatcher and drops Py_TPFLAGS_HAVE_VECTORCALL on t, then recurses
// into every subclass that does not define its own __call__ (a subclass
// with its own __call__ governs its own subtree, so the walk stops).
//
// CPython: Objects/typeobject.c:11406 update_slots_callback / update_subclasses
func clearVectorcallForCallOverride(t *Type) {
	t.Call = slotTpCall
	t.Vectorcall = nil
	t.TpFlags &^= TpFlagHaveVectorcall
	for _, sub := range t.Subclasses() {
		if isOwnDescriptor(sub, "__call__") {
			continue
		}
		clearVectorcallForCallOverride(sub)
	}
}
