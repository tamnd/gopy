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
	PropertyType.TpNew = propertyNew
	addDescriptorSlotWrappers(PropertyType)

	// property_methods: .getter / .setter / .deleter decorators.
	// Each returns a copy of the property with one field replaced.
	// CPython: Objects/descrobject.c:1683 property_getter / _setter / _deleter
	SetTypeDescr(PropertyType, "getter", NewMethodDescr(PropertyType, "getter", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, errors.New("TypeError: getter() requires self and fget")
		}
		p, ok := args[0].(*Property)
		if !ok {
			return nil, errors.New("TypeError: descriptor 'getter' requires a 'property' object")
		}
		return p.Getter(args[1]), nil
	}))
	SetTypeDescr(PropertyType, "setter", NewMethodDescr(PropertyType, "setter", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, errors.New("TypeError: setter() requires self and fset")
		}
		p, ok := args[0].(*Property)
		if !ok {
			return nil, errors.New("TypeError: descriptor 'setter' requires a 'property' object")
		}
		return p.Setter(args[1]), nil
	}))
	SetTypeDescr(PropertyType, "deleter", NewMethodDescr(PropertyType, "deleter", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, errors.New("TypeError: deleter() requires self and fdel")
		}
		p, ok := args[0].(*Property)
		if !ok {
			return nil, errors.New("TypeError: descriptor 'deleter' requires a 'property' object")
		}
		return p.Deleter(args[1]), nil
	}))
	// property_getset: __doc__, fget, fset, fdel
	// CPython: Objects/descrobject.c:1795 property_getset
	SetTypeDescr(PropertyType, "__doc__", NewGetSetDescr("__doc__",
		func(o Object) (Object, error) {
			p := o.(*Property)
			if p.doc != nil {
				return p.doc, nil
			}
			return None(), nil
		},
		func(o Object, v Object) error {
			p := o.(*Property)
			p.doc = v
			return nil
		},
	))
	SetTypeDescr(PropertyType, "fget", NewGetSetDescr("fget",
		func(o Object) (Object, error) {
			p := o.(*Property)
			if p.fget != nil {
				return p.fget, nil
			}
			return None(), nil
		},
		nil,
	))
	SetTypeDescr(PropertyType, "fset", NewGetSetDescr("fset",
		func(o Object) (Object, error) {
			p := o.(*Property)
			if p.fset != nil {
				return p.fset, nil
			}
			return None(), nil
		},
		nil,
	))
	SetTypeDescr(PropertyType, "fdel", NewGetSetDescr("fdel",
		func(o Object) (Object, error) {
			p := o.(*Property)
			if p.fdel != nil {
				return p.fdel, nil
			}
			return None(), nil
		},
		nil,
	))
}

// propertyNew is the tp_new slot. property([fget[, fset[, fdel[, doc]]]])
// accepts any subset of the four positional arguments, with `doc` also
// available as a keyword. nil fget gives a write-only property, nil fset
// gives the read-only flavor that @property emits on its own. The
// constructor mirrors property_init since the descriptor has no
// post-init step in CPython either.
//
// CPython: Objects/descrobject.c:1736 property_init
func propertyNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
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
	return NewProperty(fget, fset, fdel, doc), nil
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

// Fget returns the property's getter function, or nil when the
// property is write-only. Specializers use it to decide whether
// LOAD_ATTR_PROPERTY is applicable.
//
// CPython: Include/cpython/descrobject.h propertyobject.prop_get
func (p *Property) Fget() Object { return p.fget }

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
	if owner == nil {
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
