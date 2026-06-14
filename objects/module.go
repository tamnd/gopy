// Module object. Mirrors PyModuleObject from Objects/moduleobject.c.
// Attributes live in __dict__; GetAttr first checks the dict then
// falls through to the PEP 562 __getattr__ hook if defined.
//
// CPython: Objects/moduleobject.c

package objects

import "fmt"

// Module mirrors PyModuleObject.
//
// CPython: Include/cpython/moduleobject.h:11 PyModuleObject
type Module struct {
	Header
	dict  *Dict
	state any // per-module state for Go-implemented modules
	// Initializing is true while the module body is executing. When set,
	// moduleGetAnnotations does not cache its result so that circular
	// imports see the annotations that existed at the point of access
	// rather than a stale cached snapshot.
	//
	// CPython: Objects/moduleobject.c:1307 is_initializing check via
	// __spec__._initializing; gopy uses an explicit flag instead.
	Initializing bool
}

// ModuleType is the type singleton for module objects.
//
// CPython: Objects/moduleobject.c:766 PyModule_Type
var ModuleType = NewType("module", []*Type{objectType})

func init() {
	ModuleType.Getattro = moduleGetattr
	ModuleType.Setattro = moduleSetattr
	// A module carries its namespace in md_dict, the moral equivalent of a
	// non-zero tp_dictoffset. Marking HasDict lets a ModuleType subclass
	// inherit the dict slot (type_new_descriptors then skips adding a fresh
	// __dict__ getset, since the base already provides one) and routes the
	// generic object.__setattr__ path through md_dict for module instances.
	//
	// CPython: Objects/moduleobject.c:1416 PyModule_Type (tp_dictoffset set)
	ModuleType.HasDict = true
	// A module also carries md_weaklist (a non-zero tp_weaklistoffset), so
	// a subclass inherits the weakref slot rather than adding its own. This
	// keeps a ModuleType subclass layout-compatible with module, which
	// object.__class__ assignment between the two requires.
	//
	// CPython: Objects/moduleobject.c:1416 PyModule_Type (tp_weaklistoffset)
	ModuleType.HasWeakref = true
	ModuleType.Repr = moduleRepr
	ModuleType.Str = moduleRepr
	// Modules are hashable by identity in CPython (tp_hash = PyObject_GenericHash).
	// CPython: Objects/moduleobject.c:766 PyModule_Type (tp_hash not overridden → id-based)
	ModuleType.Hash = IdentityHash

	// tp_new: allocate an empty module with an empty __dict__. The name
	// and optional doc are populated by __init__ below, matching CPython's
	// two-step new_module + module___init___impl.
	//
	// CPython: Objects/moduleobject.c:91 new_module_notrack
	ModuleType.TpNew = func(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
		m := &Module{dict: NewDict()}
		m.init(cls)
		return m, nil
	}

	// module.__new__: surface tp_new so M.__new__(M) allocates a real
	// *Module rather than inheriting object.__new__ (which would build a
	// generic *Instance whose Str slot then crashes module_repr).
	//
	// CPython: Objects/moduleobject.c module_new (tp_new)
	SetTypeDescr(ModuleType, "__new__", NewBuiltinFunction("module.__new__", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: module.__new__(): not enough arguments")
		}
		cls, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: module.__new__(X): X is not a type object (%s)", typeNameOf(args[0]))
		}
		return ModuleType.TpNew(cls, args[1:], kwargs)
	}))

	// module.__init__(name, doc=None): set __name__ and optionally __doc__
	// in the module's __dict__. Called by typeCallViaTpNew with self as
	// args[0], matching the MethodDescr calling convention.
	//
	// CPython: Objects/moduleobject.c:804 module___init___impl
	SetTypeDescr(ModuleType, "__init__", NewMethodDescr(ModuleType, "__init__", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: module.__init__() requires at least a name argument")
		}
		m, ok := args[0].(*Module)
		if !ok {
			return None(), nil
		}
		nameObj, ok := args[1].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: module.__init__() argument 'name' must be str, not '%s'", typeNameOf(args[1]))
		}
		// module_init_dict seeds the namespace with __name__, __doc__,
		// __package__, __loader__ and __spec__. __doc__ defaults to None when
		// no doc argument is supplied, and the other three are always None;
		// this keeps an initialized module distinct from one built via
		// __new__ alone (whose empty namespace lets __doc__ fall through to
		// the type's docstring).
		//
		// CPython: Objects/moduleobject.c:66 module_init_dict
		doc := None()
		if len(args) >= 3 {
			doc = args[2]
		}
		_ = m.dict.SetItem(NewStr("__name__"), nameObj)
		_ = m.dict.SetItem(NewStr("__doc__"), doc)
		_ = m.dict.SetItem(NewStr("__package__"), None())
		_ = m.dict.SetItem(NewStr("__loader__"), None())
		_ = m.dict.SetItem(NewStr("__spec__"), None())
		return None(), nil
	}))

	// PyModule_Type carries its own tp_doc. Recording it means both
	// ModuleType.__doc__ and an uninitialized module instance's __doc__
	// resolve to this string rather than inheriting object's docstring
	// through the MRO.
	//
	// CPython: Objects/moduleobject.c:1380 module_doc
	SetTypeDescr(ModuleType, "__doc__", NewStr("Create a module object.\n\nThe name must be a string; the optional doc argument can have any type."))

	// module.__dir__(): the keys of the module's __dict__, unless the
	// dict itself defines __dir__. A __dict__ that is not a dictionary
	// (a subclass can shadow it) raises TypeError.
	//
	// CPython: Objects/moduleobject.c:1190 module_dir
	SetTypeDescr(ModuleType, "__dir__", NewMethodDescr(ModuleType, "__dir__", moduleDir))
}

// moduleDir backs module.__dir__.
//
// CPython: Objects/moduleobject.c:1190 module_dir
func moduleDir(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __dir__() takes no arguments")
	}
	dict, err := GetAttr(args[0], NewStr("__dict__"))
	if err != nil {
		return nil, err
	}
	d, ok := dict.(*Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: <module>.__dict__ is not a dictionary")
	}
	if dirfunc, _ := d.GetItem(NewStr("__dir__")); dirfunc != nil {
		return Call(dirfunc, NewTuple(nil), nil)
	}
	return NewList(d.Keys()), nil
}

// NewModule creates an empty module with the given name in its __dict__.
//
// CPython: Objects/moduleobject.c:74 PyModule_NewObject
func NewModule(name string) *Module {
	m := &Module{dict: NewDict()}
	m.init(ModuleType)
	_ = m.dict.SetItem(NewStr("__name__"), NewStr(name))
	return m
}

// NewModuleWithDict wraps an existing dict as a module's __dict__.
// Used by pythonrun to expose the running script as the __main__
// module: the script's globals dict IS the module dict, so test
// frameworks that look up the script's symbols via
// __import__("__main__") see the same objects user code is touching.
//
// CPython: Python/pythonrun.c:1276 pyrun_file sets globals to the
// __main__ module's dict; gopy keeps the globals dict and adopts it
// into a Module wrapper.
func NewModuleWithDict(name string, d *Dict) *Module {
	m := &Module{dict: d}
	m.init(ModuleType)
	if has, _ := d.Contains(NewStr("__name__")); !has {
		_ = d.SetItem(NewStr("__name__"), NewStr(name))
	}
	return m
}

// Dict returns the module's attribute dict (__dict__).
//
// CPython: Objects/moduleobject.c:459 PyModule_GetDict
func (m *Module) Dict() *Dict { return m.dict }

// AttrDict and EnsureAttrDict implement AttrDictHolder so the generic
// attribute machinery treats md_dict as the module's instance dict. This
// lets object.__setattr__/object.__getattribute__ store and read module
// attributes (e.g. a ModuleType subclass calling MT.__setattr__), matching
// CPython where module attribute access flows through tp_dictoffset.
//
// CPython: Objects/moduleobject.c:1416 PyModule_Type (tp_dictoffset)
func (m *Module) AttrDict() *Dict { return m.dict }

// EnsureAttrDict returns the module namespace; it is always present.
func (m *Module) EnsureAttrDict() *Dict { return m.dict }

// StampBuiltinModule walks the module's dict and sets the owning
// module name on every BuiltinFunction whose Module field is still
// empty. Mirrors PyModule_AddFunctions, which calls
// PyCFunction_NewEx with the module's __name__ so each function
// remembers its parent. Pickle's whichmodule reads __module__ off
// the BIF to find the import path for save_global.
//
// Idempotent: a function whose Module is already set keeps its
// existing value, so re-import does not clobber an explicit owner.
//
// CPython: Objects/moduleobject.c:606 PyModule_AddFunctions
func (m *Module) StampBuiltinModule() {
	name := ""
	if v, _ := m.dict.GetItem(NewStr("__name__")); v != nil {
		if s, ok := v.(*Unicode); ok {
			name = s.Value()
		}
	}
	if name == "" {
		return
	}
	for _, k := range m.dict.Keys() {
		v, err := m.dict.GetItem(k)
		if err != nil || v == nil {
			continue
		}
		if bf, ok := v.(*BuiltinFunction); ok {
			if bf.Module == "" {
				bf.Module = name
			}
			// PyCFunction_NewEx is handed the owning module as m_self;
			// meth_get__self__ returns it, so time.sleep.__self__ is
			// the time module. Skip classmethods/static (already bound
			// to a type or unbound) by only filling an empty Self.
			//
			// CPython: Objects/moduleobject.c:606 PyModule_AddFunctions (PyCFunction_NewEx self=module)
			if bf.Self == nil && bf.Conv&MethStatic == 0 {
				bf.Self = m
			}
		}
	}
}

// State returns the per-module state pointer set by Go-implemented modules.
//
// CPython: Objects/moduleobject.c:476 PyModule_GetState
func (m *Module) State() any { return m.state }

// SetState stores a per-module state value.
//
// CPython: Objects/moduleobject.c:486 PyModule_SetState (gopy analog)
func (m *Module) SetState(s any) { m.state = s }

// moduleGetattr implements __getattr__ for module objects. It checks
// __dict__ first, then falls back to the PEP 562 __getattr__ callable
// stored in __dict__ under "__getattr__".
//
// CPython: Objects/moduleobject.c:147 module_getattro
func moduleGetattr(o Object, name Object) (Object, error) {
	m, ok := o.(*Module)
	if !ok {
		return GenericGetAttr(o, name)
	}
	key := attrNameStr(name)
	// __dict__ is the canonical module attribute that exposes the
	// backing namespace. CPython sets it via module_init_dict and
	// answers tp_getattro by checking the slot before the dict; gopy
	// keeps __dict__ off the dict to avoid the obvious self-reference,
	// so satisfy the lookup here.
	//
	// CPython: Objects/moduleobject.c:88 module_init_dict
	if key == "__dict__" {
		// A subclass may shadow __dict__ with a plain class attribute, e.g.
		// `class M(ModuleType): __dict__ = 8`. CPython resolves __dict__
		// through generic getattr, which returns that shadowing attribute
		// when it is not a data descriptor and the instance namespace holds
		// no "__dict__" key, rather than always handing back md_dict. Mirror
		// that ordering so dir() on such an instance sees the non-dict value
		// and raises TypeError, matching CPython.
		//
		// CPython: Objects/object.c:1450 _PyObject_GenericGetAttrWithDict
		if descr, owner := LookupDescriptor(o.Type(), "__dict__"); descr != nil && owner != objectType {
			dt := descr.Type()
			if dt.DescrGet != nil && dt.DescrSet != nil {
				return dt.DescrGet(descr, o, o.Type())
			}
			if _, err := m.dict.GetItem(name); err != nil {
				if dt.DescrGet != nil {
					return dt.DescrGet(descr, o, o.Type())
				}
				Incref(descr)
				return descr, nil
			}
		}
		// Borrowed slot handed to a caller that treats the result as a
		// new strong reference (pushObject decrefs it later), so incref
		// to keep the module's own ownership intact. Without this the
		// caller's Decref drives the namespace dict toward refcount zero
		// and dict_dealloc clears a live module's globals.
		//
		// CPython: Objects/moduleobject.c:147 module_getattro (Py_INCREF
		// before returning the dict via the __dict__ getset)
		Incref(m.dict)
		return m.dict, nil
	}
	// PEP 649: __annotations__ on a module is lazy when the body
	// installed __annotate__. __annotate__ defaults to None when
	// missing, matching CPython's module_get_annotate behavior.
	//
	// CPython: Objects/moduleobject.c:1288 module_get_annotations
	// CPython: Objects/moduleobject.c:1233 module_get_annotate
	if key == "__annotations__" {
		return moduleGetAnnotations(m)
	}
	if key == "__annotate__" {
		return moduleGetAnnotate(m)
	}
	// Generic attribute resolution against the type MRO with md_dict as
	// the instance dict: data descriptors (e.g. __class__) win, then the
	// module namespace, then non-data descriptors and plain class
	// attributes a subclass defines. Only on AttributeError do we fall
	// through to the PEP 562 __getattr__ hook.
	//
	// CPython: Objects/moduleobject.c:991 _Py_module_getattro_impl
	//          (_PyObject_GenericGetAttrWithDict first)
	if v, err := moduleGenericGetAttr(m, name); err == nil {
		return v, nil
	} else if !isAttributeError(err) {
		return nil, err
	}
	// PEP 562: look for __getattr__ in the module dict.
	gaObj, gaErr := m.dict.GetItem(NewStr("__getattr__"))
	if gaErr == nil {
		return callOneArg(gaObj, name)
	}
	// Best-effort error message mirroring module_getattro's tail.
	//
	// CPython: Objects/moduleobject.c:1042 PyErr_Format module has no attribute
	if modName := moduleStrAttr(m, "__name__"); modName != "" {
		return nil, fmt.Errorf("AttributeError: module '%s' has no attribute '%s'", modName, key)
	}
	return nil, fmt.Errorf("AttributeError: module has no attribute '%s'", key)
}

// moduleGenericGetAttr resolves name against the module type's MRO using
// the module namespace (md_dict) as the instance dict, mirroring
// _PyObject_GenericGetAttrWithDict. It lets __class__ and the methods or
// class attributes a ModuleType subclass declares resolve, which the
// dict-only lookup would miss.
//
// CPython: Objects/object.c:1809 _PyObject_GenericGetAttrWithDict
func moduleGenericGetAttr(m *Module, name Object) (Object, error) {
	tp := m.Type()
	nameStr := attrNameStr(name)
	descr, _ := LookupDescriptor(tp, nameStr)
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil && dt.DescrSet != nil {
			return dt.DescrGet(descr, m, tp)
		}
	}
	if v, err := m.dict.GetItem(name); err == nil {
		// dict.GetItem returns a borrowed reference; callers in the eval
		// loop (pushObject) treat the return as a new strong ref and
		// will Decref it later. Incref so the borrowed dict slot stays
		// valid and the caller's Decref balances correctly.
		//
		// CPython: Objects/moduleobject.c:875 module_getattro
		Incref(v)
		return v, nil
	}
	if descr != nil {
		dt := descr.Type()
		if dt.DescrGet != nil {
			return dt.DescrGet(descr, m, tp)
		}
		Incref(descr)
		return descr, nil
	}
	return nil, fmt.Errorf("AttributeError: module has no attribute '%s'", nameStr)
}

// callOneArg calls a callable with a single argument.
func callOneArg(fn, arg Object) (Object, error) {
	tp := fn.Type()
	if tp.Call != nil {
		return tp.Call(fn, []Object{arg}, nil)
	}
	return nil, fmt.Errorf("TypeError: object is not callable")
}

// moduleSetattr writes an attribute into the module's __dict__.
//
// CPython: Objects/moduleobject.c:209 module_setattro
func moduleSetattr(o Object, name, value Object) error {
	m := o.(*Module)
	key := attrNameStr(name)
	if key == "__annotate__" {
		return moduleSetAnnotate(m, value)
	}
	if key == "__annotations__" {
		return moduleSetAnnotations(m, value)
	}
	// A data descriptor on the type (e.g. __class__, or a property a
	// ModuleType subclass declares) takes precedence over the namespace.
	//
	// CPython: Objects/object.c:1693 _PyObject_GenericSetAttrWithDict
	if descr, _ := LookupDescriptor(m.Type(), key); descr != nil {
		if dset := descr.Type().DescrSet; dset != nil {
			return dset(descr, o, value)
		}
	}
	if value == nil {
		return m.dict.DelItem(name)
	}
	return m.dict.SetItem(name, value)
}

// moduleRepr returns the canonical module repr.
// Four forms mirror CPython:
//   - <module 'name' from 'file'> when __file__ is set
//   - <module 'name' (built-in)> when __spec__.origin == 'built-in'
//   - <module 'name' (frozen)> when __spec__.origin == 'frozen'
//   - <module 'name'> otherwise
//
// CPython: Objects/moduleobject.c:228 module_repr
func moduleRepr(o Object) (string, error) {
	m := o.(*Module)
	name := moduleStrAttr(m, "__name__")
	if file := moduleStrAttr(m, "__file__"); file != "" {
		return fmt.Sprintf("<module '%s' from '%s'>", name, file), nil
	}
	if spec, err := m.dict.GetItem(NewStr("__spec__")); err == nil && spec != nil {
		if sm, ok := spec.(*Module); ok {
			origin := moduleStrAttr(sm, "origin")
			if origin == "built-in" {
				return fmt.Sprintf("<module '%s' (built-in)>", name), nil
			}
			if origin == "frozen" {
				return fmt.Sprintf("<module '%s' (frozen)>", name), nil
			}
		}
	}
	return fmt.Sprintf("<module '%s'>", name), nil
}

func moduleStrAttr(m *Module, attr string) string {
	v, err := m.dict.GetItem(NewStr(attr))
	if err != nil {
		return ""
	}
	if s, ok := v.(*Unicode); ok {
		return s.v
	}
	return ""
}
