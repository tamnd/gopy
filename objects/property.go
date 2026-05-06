// The property data descriptor: wraps fget/fset/fdel callables and
// exposes them through __get__ / __set__ / __delete__ so attribute
// access on an instance routes through Python-level methods. The
// .getter / .setter / .deleter helpers return a copy of the property
// with one field replaced; that's what enables the @prop.setter
// decorator pattern.
//
// CPython: Objects/descrobject.c:1736 property_init / property_new

package objects

import "errors"

// Property is the property descriptor.
//
// CPython: Include/cpython/descrobject.h:39 propertyobject
type Property struct {
	Header
	fget Object
	fset Object
	fdel Object
	doc  Object
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
}

// NewProperty builds a property from up to four arguments. Any of
// them may be nil; nil fget makes the property write-only (read
// raises AttributeError), nil fset makes it read-only, and so on.
//
// CPython: Objects/descrobject.c:1736 property_init
func NewProperty(fget, fset, fdel, doc Object) *Property {
	p := &Property{fget: fget, fset: fset, fdel: fdel, doc: doc}
	p.init(PropertyType)
	return p
}

// Getter returns a copy of p with fget replaced. Powers the
// @prop.getter decorator.
//
// CPython: Objects/descrobject.c:1683 property_getter
func (p *Property) Getter(fget Object) *Property {
	return NewProperty(fget, p.fset, p.fdel, p.doc)
}

// Setter returns a copy of p with fset replaced. Powers the
// @prop.setter decorator.
//
// CPython: Objects/descrobject.c:1689 property_setter
func (p *Property) Setter(fset Object) *Property {
	return NewProperty(p.fget, fset, p.fdel, p.doc)
}

// Deleter returns a copy of p with fdel replaced. Powers the
// @prop.deleter decorator.
//
// CPython: Objects/descrobject.c:1695 property_deleter
func (p *Property) Deleter(fdel Object) *Property {
	return NewProperty(p.fget, p.fset, fdel, p.doc)
}

func propertyRepr(o Object) (string, error) {
	return "<property object>", nil
}

// propertyDescrGet is the __get__ slot. Class-level access (owner is
// nil) returns the property unchanged; instance access calls fget
// with the instance, raising AttributeError when fget is missing.
//
// CPython: Objects/descrobject.c:1576 property_descr_get
func propertyDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	p := descr.(*Property)
	if owner == nil || owner == None() {
		return descr, nil
	}
	if p.fget == nil || p.fget == None() {
		return nil, errors.New("AttributeError: unreadable attribute")
	}
	return CallOneArg(p.fget, owner)
}

// propertyDescrSet handles both assignment and deletion. CPython
// signals deletion by passing NULL for value; gopy uses a nil Object.
// Missing fset / fdel raise AttributeError.
//
// CPython: Objects/descrobject.c:1611 property_descr_set
func propertyDescrSet(descr Object, owner Object, value Object) error {
	p := descr.(*Property)
	if value == nil {
		if p.fdel == nil || p.fdel == None() {
			return errors.New("AttributeError: can't delete attribute")
		}
		_, err := CallOneArg(p.fdel, owner)
		return err
	}
	if p.fset == nil || p.fset == None() {
		return errors.New("AttributeError: can't set attribute")
	}
	args := NewTuple([]Object{owner, value})
	_, err := Call(p.fset, args, nil)
	return err
}
