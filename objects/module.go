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
		_ = m.dict.SetItem(NewStr("__name__"), nameObj)
		if len(args) >= 3 && args[2] != None() {
			_ = m.dict.SetItem(NewStr("__doc__"), args[2])
		}
		return None(), nil
	}))

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
		if bf, ok := v.(*BuiltinFunction); ok && bf.Module == "" {
			bf.Module = name
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
	m := o.(*Module)
	key := attrNameStr(name)
	// __dict__ is the canonical module attribute that exposes the
	// backing namespace. CPython sets it via module_init_dict and
	// answers tp_getattro by checking the slot before the dict; gopy
	// keeps __dict__ off the dict to avoid the obvious self-reference,
	// so satisfy the lookup here.
	//
	// CPython: Objects/moduleobject.c:88 module_init_dict
	if key == "__dict__" {
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
	v, err := m.dict.GetItem(name)
	if err == nil {
		// dict.GetItem returns a borrowed reference; callers in the eval
		// loop (pushObject) treat the return as a new strong ref and
		// will Decref it later. Incref so the borrowed dict slot stays
		// valid and the caller's Decref balances correctly.
		//
		// CPython: Objects/moduleobject.c:875 module_getattro
		Incref(v)
		return v, nil
	}
	// PEP 562: look for __getattr__ in the module dict.
	gaObj, gaErr := m.dict.GetItem(NewStr("__getattr__"))
	if gaErr == nil {
		return callOneArg(gaObj, name)
	}
	return nil, fmt.Errorf("AttributeError: module has no attribute %q", key)
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
