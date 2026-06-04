// The property data descriptor: wraps fget/fset/fdel callables and
// exposes them through __get__ / __set__ / __delete__ so attribute
// access on an instance routes through Python-level methods. The
// .getter / .setter / .deleter helpers return a copy of the property
// with one field replaced; that's what enables the @prop.setter
// decorator pattern.
//
// CPython: Objects/descrobject.c:1736 property_init / property_new
//
//nolint:dupl // getter/setter/deleter method bodies are intentionally parallel
package objects

import (
	"errors"
	"fmt"
)

// Property is the property descriptor.
//
// CPython: Include/cpython/descrobject.h:39 propertyobject
type Property struct {
	Header
	fget Object
	fset Object
	fdel Object
	doc  Object
	// name backs property.__name__ / __set_name__ (prop_name). nil means
	// it was never set, in which case __name__ falls back to fget.__name__.
	name Object
	// getterDoc records that doc was lifted from the getter's __doc__
	// rather than supplied explicitly. property_copy uses it to decide
	// whether a fresh getter should re-derive the docstring.
	//
	// CPython: Include/cpython/descrobject.h propertyobject.getter_doc
	getterDoc bool
	// attrs is the per-instance attribute dict a property *subclass*
	// instance carries (so the subclass can store __doc__ in its own
	// __dict__ instead of the inherited prop_doc member). nil for plain
	// property instances.
	attrs *Dict
}

// AttrDict / EnsureAttrDict implement AttrDictHolder so property
// subclasses can hold instance attributes (notably __doc__) through the
// generic attr machinery.
//
// CPython: Objects/typeobject.c:6776 subtype_dict
func (p *Property) AttrDict() *Dict { return p.attrs }

// EnsureAttrDict allocates the per-instance attribute dict on first use.
func (p *Property) EnsureAttrDict() *Dict {
	if p.attrs == nil {
		p.attrs = NewDict()
	}
	return p.attrs
}

// PropertyType is the type singleton for property.
//
// CPython: Objects/descrobject.c:1817 PyProperty_Type
var PropertyType = NewType("property", []*Type{objectType})

func init() {
	PropertyType.Repr = propertyRepr
	PropertyType.Str = propertyRepr
	PropertyType.DescrGet = propertyDescrGet
	PropertyType.DescrSet = propertyDescrSet
	PropertyType.TpNew = propertyNew
	// property inherits object's tp_getattro/tp_setattro
	// (PyObject_GenericGetAttr / GenericSetAttr), so __doc__ and __name__
	// are writable through their getset descriptors and a subclass can
	// store __doc__ in its instance dict.
	//
	// CPython: Objects/descrobject.c:1855 PyProperty_Type (tp_getattro/tp_setattro)
	PropertyType.Getattro = GenericGetAttr
	PropertyType.Setattro = GenericSetAttr
	addDescriptorSlotWrappers(PropertyType)

	// property_methods: .getter / .setter / .deleter decorators plus
	// __set_name__.
	// CPython: Objects/descrobject.c:1620 property_methods
	// Expose __new__ so property.__new__(SubClass) routes to propertyNew
	// instead of falling back to object.__new__, which would return a
	// generic *Instance that the getset/method casts cannot handle.
	// CPython: Objects/descrobject.c PyProperty_Type tp_new (default, via
	// the staticmethod wrapper inherited from object).
	SetTypeDescr(PropertyType, "__new__", NewBuiltinFunction("property.__new__",
		func(args []Object, kwargs map[string]Object) (Object, error) {
			if len(args) < 1 {
				return nil, errors.New("TypeError: property.__new__(): not enough arguments")
			}
			cls, ok := args[0].(*Type)
			if !ok {
				return nil, errors.New("TypeError: property.__new__(X): X is not a type object")
			}
			return propertyNew(cls, args[1:], kwargs)
		}))

	SetTypeDescr(PropertyType, "getter", NewMethodDescr(PropertyType, "getter", propertyGetterMethod))
	SetTypeDescr(PropertyType, "setter", NewMethodDescr(PropertyType, "setter", propertySetterMethod))
	SetTypeDescr(PropertyType, "deleter", NewMethodDescr(PropertyType, "deleter", propertyDeleterMethod))
	SetTypeDescr(PropertyType, "__set_name__", NewMethodDescr(PropertyType, "__set_name__", propertySetName))

	// property_members: fget / fset / fdel are read-only; __doc__ is
	// writable.
	// CPython: Objects/descrobject.c:1558 property_members
	SetTypeDescr(PropertyType, "__doc__", NewGetSetDescr("__doc__", propertyGetDoc, propertySetDoc))
	SetTypeDescr(PropertyType, "fget", NewGetSetDescr("fget", propertyGetFget, nil))
	SetTypeDescr(PropertyType, "fset", NewGetSetDescr("fset", propertyGetFset, nil))
	SetTypeDescr(PropertyType, "fdel", NewGetSetDescr("fdel", propertyGetFdel, nil))

	// property_getsetlist: __name__ (read/write) and
	// __isabstractmethod__ (read-only).
	// CPython: Objects/descrobject.c:1980 property_getsetlist
	SetTypeDescr(PropertyType, "__name__", NewGetSetDescr("__name__", propertyGetName, propertySetNameAttr))
	SetTypeDescr(PropertyType, "__isabstractmethod__", NewGetSetDescr("__isabstractmethod__", propertyGetIsAbstract, nil))
}

func propertyGetDoc(o Object) (Object, error) {
	p := o.(*Property)
	if p.doc != nil {
		return p.doc, nil
	}
	return None(), nil
}

func propertySetDoc(o Object, v Object) error {
	p := o.(*Property)
	p.doc = v
	return nil
}

func propertyGetFget(o Object) (Object, error) { return orNone(o.(*Property).fget), nil }
func propertyGetFset(o Object) (Object, error) { return orNone(o.(*Property).fset), nil }
func propertyGetFdel(o Object) (Object, error) { return orNone(o.(*Property).fdel), nil }

// orNone maps a Go-nil slot to the None singleton so member reads expose
// None rather than a nil interface.
func orNone(o Object) Object {
	if o == nil {
		return None()
	}
	return o
}

// propertyNew is the tp_new slot. property([fget[, fset[, fdel[, doc]]]])
// accepts any subset of the four positional arguments, with each also
// available as a keyword. The instance is allocated against cls so a
// subclass keeps its own type, then property_init runs the docstring and
// getter_doc bookkeeping.
//
// CPython: Objects/descrobject.c:1736 property_init
func propertyNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	var fget, fset, fdel, doc Object
	if len(args) >= 1 {
		fget = args[0]
	}
	if len(args) >= 2 {
		fset = args[1]
	}
	if len(args) >= 3 {
		fdel = args[2]
	}
	if len(args) >= 4 {
		doc = args[3]
	}
	if len(args) > 4 {
		return nil, fmt.Errorf("TypeError: property() takes at most 4 positional arguments (%d given)", len(args))
	}
	if v, ok := kwargs["fget"]; ok {
		fget = v
	}
	if v, ok := kwargs["fset"]; ok {
		fset = v
	}
	if v, ok := kwargs["fdel"]; ok {
		fdel = v
	}
	if v, ok := kwargs["doc"]; ok {
		doc = v
	}
	p := &Property{}
	p.init(cls)
	if err := propertyInit(p, fget, fset, fdel, doc); err != nil {
		return nil, err
	}
	return p, nil
}

// propertyInit ports property_init_impl: None arguments collapse to nil,
// then doc is taken from the explicit argument or lifted from the
// getter's __doc__ (recording getter_doc). For the base property type the
// docstring lands in the prop_doc member; for a subclass it is written
// onto the instance via SetAttr (so it is not shadowed by the class
// __doc__) and a failed write is silently dropped unless it came from the
// getter.
//
// CPython: Objects/descrobject.c:1839 property_init_impl
func propertyInit(p *Property, fget, fset, fdel, doc Object) error {
	if fget == None() {
		fget = nil
	}
	if fset == None() {
		fset = nil
	}
	if fdel == None() {
		fdel = nil
	}
	p.fget = fget
	p.fset = fset
	p.fdel = fdel
	p.doc = nil
	p.name = nil
	p.getterDoc = false

	var propDoc Object
	switch {
	case doc != nil && doc != None():
		propDoc = doc
	case fget != nil:
		// If no docstring given and the getter has one, use that one.
		got, err := LookupAttr(fget, NewStr("__doc__"))
		if err != nil {
			return err
		}
		if got != nil && got != None() {
			propDoc = got
			p.getterDoc = true
		}
	}

	if p.Type() == PropertyType {
		p.doc = propDoc
		return nil
	}
	// Property subclass: store __doc__ in the instance dict or slot so it
	// is not shadowed by the class's own __doc__.
	docVal := propDoc
	if docVal == nil {
		docVal = None()
	}
	err := SetAttr(p, NewStr("__doc__"), docVal)
	if err != nil {
		if !p.getterDoc && isAttributeError(err) {
			// Python silently dropped this doc assignment through 3.11;
			// preserved for backwards compatibility (gh-98963).
			return nil
		}
		return err
	}
	return nil
}

// NewProperty builds a property from up to four arguments. Any of them
// may be nil; nil fget makes the property write-only (read raises
// AttributeError), nil fset makes it read-only, and so on. The docstring
// and getter_doc bookkeeping mirror property_init for the base type.
//
// CPython: Objects/descrobject.c:1736 property_init
func NewProperty(fget, fset, fdel, doc Object) *Property {
	p := &Property{}
	p.init(PropertyType)
	// propertyInit never errors for the base type (the subclass SetAttr
	// path is the only failure mode).
	_ = propertyInit(p, fget, fset, fdel, doc)
	return p
}

// Fget returns the property's getter function, or nil when the property
// is write-only. Specializers use it to decide whether
// LOAD_ATTR_PROPERTY is applicable.
//
// CPython: Include/cpython/descrobject.h propertyobject.prop_get
func (p *Property) Fget() Object { return p.fget }

// Getter returns a copy of p with fget replaced. Powers the
// @prop.getter decorator from Go callers (specializer/tests).
//
// CPython: Objects/descrobject.c:1573 property_getter
func (p *Property) Getter(fget Object) *Property {
	np, _ := propertyCopy(p, fget, nil, nil)
	return np.(*Property)
}

// Setter returns a copy of p with fset replaced.
//
// CPython: Objects/descrobject.c:1589 property_setter
func (p *Property) Setter(fset Object) *Property {
	np, _ := propertyCopy(p, nil, fset, nil)
	return np.(*Property)
}

// Deleter returns a copy of p with fdel replaced.
//
// CPython: Objects/descrobject.c:1601 property_deleter
func (p *Property) Deleter(fdel Object) *Property {
	np, _ := propertyCopy(p, nil, nil, fdel)
	return np.(*Property)
}

// propertyCopy ports property_copy: build a fresh instance of the same
// type carrying the union of the old fields and the supplied overrides.
// When the old property took its doc from the getter and a new getter is
// supplied, doc is reset to None so the copy re-derives it. The original
// __name__ is carried over, but only when the copy is a real property
// (a subclass __new__ may legally return something else).
//
// CPython: Objects/descrobject.c:1700 property_copy
func propertyCopy(old *Property, get, set, del Object) (Object, error) {
	if get == nil || get == None() {
		get = orNone(old.fget)
	}
	if set == nil || set == None() {
		set = orNone(old.fset)
	}
	if del == nil || del == None() {
		del = orNone(old.fdel)
	}
	var doc Object
	if old.getterDoc && get != None() {
		// make property_init use __doc__ from the getter.
		doc = None()
	} else {
		doc = orNone(old.doc)
	}
	newObj, err := Call(old.Type(), NewTuple([]Object{get, set, del, doc}), nil)
	if err != nil {
		return nil, err
	}
	if np, ok := newObj.(*Property); ok {
		np.name = old.name
	}
	return newObj, nil
}

func propertyGetterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: getter() takes exactly one argument")
	}
	p, ok := args[0].(*Property)
	if !ok {
		return nil, errors.New("TypeError: descriptor 'getter' requires a 'property' object")
	}
	return propertyCopy(p, args[1], nil, nil)
}

func propertySetterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: setter() takes exactly one argument")
	}
	p, ok := args[0].(*Property)
	if !ok {
		return nil, errors.New("TypeError: descriptor 'setter' requires a 'property' object")
	}
	return propertyCopy(p, nil, args[1], nil)
}

func propertyDeleterMethod(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, errors.New("TypeError: deleter() takes exactly one argument")
	}
	p, ok := args[0].(*Property)
	if !ok {
		return nil, errors.New("TypeError: descriptor 'deleter' requires a 'property' object")
	}
	return propertyCopy(p, nil, nil, args[1])
}

// propertySetName backs property.__set_name__(owner, name): it records
// the name the property was bound under so error messages and __name__
// can reference it.
//
// CPython: Objects/descrobject.c:1606 property_set_name
func propertySetName(args []Object, _ map[string]Object) (Object, error) {
	// args[0] is self; the rest are the positional arguments the caller
	// passed, which must be exactly (owner, name).
	got := len(args) - 1
	if got != 2 {
		return nil, fmt.Errorf("TypeError: __set_name__() takes 2 positional arguments but %d were given", got)
	}
	p, ok := args[0].(*Property)
	if !ok {
		return nil, errors.New("TypeError: descriptor '__set_name__' requires a 'property' object")
	}
	p.name = args[2]
	return None(), nil
}

// propertyNameOf ports property_name: the explicit __name__ wins,
// otherwise fall back to fget.__name__ (absent getter or missing
// attribute reads as "no name"). A returned nil object means no name.
//
// CPython: Objects/descrobject.c:1646 property_name
func propertyNameOf(p *Property) (Object, error) {
	if p.name != nil {
		return p.name, nil
	}
	if p.fget == nil {
		return nil, nil
	}
	return LookupAttr(p.fget, NewStr("__name__"))
}

// propertyGetName backs the __name__ getset. A missing name raises
// AttributeError("'property' object has no attribute '__name__'").
//
// CPython: Objects/descrobject.c:1916 property_get__name__
func propertyGetName(o Object) (Object, error) {
	name, err := propertyNameOf(o.(*Property))
	if err != nil {
		return nil, err
	}
	if name == nil {
		return nil, errors.New("AttributeError: 'property' object has no attribute '__name__'")
	}
	return name, nil
}

// propertySetNameAttr backs the __name__ setter; it stores the value
// (including None) verbatim.
//
// CPython: Objects/descrobject.c:1931 property_set__name__
func propertySetNameAttr(o Object, v Object) error {
	o.(*Property).name = v
	return nil
}

// propertyGetIsAbstract reports whether any of fget/fset/fdel is flagged
// abstract, mirroring _PyObject_IsAbstract over the three callables.
//
// CPython: Objects/descrobject.c:1939 property_get___isabstractmethod__
func propertyGetIsAbstract(o Object) (Object, error) {
	p := o.(*Property)
	for _, fn := range []Object{p.fget, p.fset, p.fdel} {
		abstract, err := isAbstractMethod(fn)
		if err != nil {
			return nil, err
		}
		if abstract {
			return True(), nil
		}
	}
	return False(), nil
}

// isAbstractMethod ports _PyObject_IsAbstract: read __isabstractmethod__
// and coerce to bool. A nil callable or a missing attribute reads as
// False; a __bool__ that raises propagates.
//
// CPython: Objects/object.c:1235 _PyObject_IsAbstract
func isAbstractMethod(fn Object) (bool, error) {
	if fn == nil {
		return false, nil
	}
	v, err := LookupAttr(fn, NewStr("__isabstractmethod__"))
	if err != nil {
		return false, err
	}
	if v == nil {
		return false, nil
	}
	return IsTruthy(v)
}

func propertyRepr(Object) (string, error) {
	return "<property object>", nil
}

// propertyDescrGet is the __get__ slot. Class-level access (owner is
// nil) returns the property unchanged; instance access calls fget with
// the instance, raising AttributeError naming the property and owner
// when fget is missing.
//
// CPython: Objects/descrobject.c:1664 property_descr_get
func propertyDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	p := descr.(*Property)
	if owner == nil || owner == None() {
		return descr, nil
	}
	if p.fget == nil {
		return nil, propertyUnreachableError(p, owner, "getter")
	}
	return CallOneArg(p.fget, owner)
}

// propertyDescrSet handles both assignment and deletion. CPython signals
// deletion by passing NULL for value; gopy uses a nil Object. Missing
// fset / fdel raise AttributeError naming the property and owner.
//
// CPython: Objects/descrobject.c:1699 property_descr_set
func propertyDescrSet(descr Object, owner Object, value Object) error {
	p := descr.(*Property)
	if value == nil {
		if p.fdel == nil {
			return propertyUnreachableError(p, owner, "deleter")
		}
		_, err := CallOneArg(p.fdel, owner)
		return err
	}
	if p.fset == nil {
		return propertyUnreachableError(p, owner, "setter")
	}
	args := NewTuple([]Object{owner, value})
	_, err := Call(p.fset, args, nil)
	return err
}

// propertyUnreachableError builds the AttributeError raised when a
// getter/setter/deleter is missing, naming the property and the owning
// type's qualname when available.
//
// CPython: Objects/descrobject.c:1664 property_descr_get (error branch)
func propertyUnreachableError(p *Property, owner Object, kind string) error {
	name, err := propertyNameOf(p)
	if err != nil {
		return err
	}
	var qualname string
	if owner != nil {
		qualname = owner.Type().Qualname
		if qualname == "" {
			qualname = owner.Type().Name
		}
	}
	switch {
	case name != nil && qualname != "":
		nameRepr, rerr := Repr(name)
		if rerr != nil {
			return rerr
		}
		return fmt.Errorf("AttributeError: property %s of %s object has no %s", nameRepr, quoteStr(qualname), kind)
	case qualname != "":
		return fmt.Errorf("AttributeError: property of %s object has no %s", quoteStr(qualname), kind)
	default:
		return fmt.Errorf("AttributeError: property has no %s", kind)
	}
}

// quoteStr renders a Go string the way repr() renders a str: single
// quotes around the value.
func quoteStr(s string) string {
	r, err := Repr(NewStr(s))
	if err != nil {
		return "'" + s + "'"
	}
	return r
}
