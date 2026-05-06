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

// Dict returns the module's attribute dict (__dict__).
//
// CPython: Objects/moduleobject.c:459 PyModule_GetDict
func (m *Module) Dict() *Dict { return m.dict }

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
	v, err := m.dict.GetItem(name)
	if err == nil {
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
		return fmt.Sprintf("<module %q from %q>", name, file), nil
	}
	if spec, err := m.dict.GetItem(NewStr("__spec__")); err == nil && spec != nil {
		if sm, ok := spec.(*Module); ok {
			origin := moduleStrAttr(sm, "origin")
			if origin == "built-in" {
				return fmt.Sprintf("<module %q (built-in)>", name), nil
			}
			if origin == "frozen" {
				return fmt.Sprintf("<module %q (frozen)>", name), nil
			}
		}
	}
	return fmt.Sprintf("<module %q>", name), nil
}

func moduleStrAttr(m *Module, attr string) string {
	v, err := m.dict.GetItem(NewStr(attr))
	if err != nil {
		return ""
	}
	if s, ok := v.(*strStub); ok {
		return s.v
	}
	return ""
}
