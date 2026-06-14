// ClassMethodDescr is the gopy analog of PyClassMethodDescr_Type: the
// type-level descriptor generated for a PyMethodDef row tagged
// METH_CLASS. Binding it through a type (or an instance) yields a
// builtin_function_or_method whose m_self is the class, so the wrapped
// C function receives the class as its self argument. gopy needs this
// distinct from the plain classmethod object because the latter binds
// an arbitrary callable by prepending the class as a positional
// argument, which would break the strict METH_O / METH_NOARGS arity
// checks a CFunction enforces.
//
// CPython: Objects/descrobject.c:757 PyClassMethodDescr_Type

package objects

import "fmt"

// ClassMethodDescr wraps a MethodDef registered on owner. Its binding
// produces a CFunction bound to the accessed type, mirroring
// classmethod_get -> PyCMethod_New(descr->d_method, type, NULL, cls).
//
// CPython: Include/cpython/descrobject.h PyMethodDescrObject
type ClassMethodDescr struct {
	Header
	def   *MethodDef
	owner *Type
}

// ClassMethodDescrType is the type singleton for classmethod
// descriptors.
//
// CPython: Objects/descrobject.c:757 PyClassMethodDescr_Type
var ClassMethodDescrType = NewType("classmethod_descriptor", []*Type{objectType})

func init() {
	ClassMethodDescrType.Repr = classMethodDescrRepr
	ClassMethodDescrType.Str = classMethodDescrRepr
	ClassMethodDescrType.DescrGet = classMethodDescrGet2
	ClassMethodDescrType.Call = classMethodDescrCall
	ClassMethodDescrType.Hash = identityHash
	addDescrIntrospectionDescriptors(ClassMethodDescrType)
	// Expose __get__ so classmethod_get is reachable through the
	// attribute path (descr.__get__(obj, type)), mirroring add_operators
	// installing the tp_descr_get wrapper on the descriptor type.
	//
	// CPython: Objects/typeobject.c add_operators (tp_descr_get row)
	addDescriptorSlotWrappers(ClassMethodDescrType)
}

// NewClassMethodDescr builds a classmethod descriptor exposing def on
// owner.
//
// CPython: Objects/descrobject.c:973 PyDescr_NewClassMethod
func NewClassMethodDescr(owner *Type, def *MethodDef) *ClassMethodDescr {
	d := &ClassMethodDescr{def: def, owner: owner}
	d.init(ClassMethodDescrType)
	return d
}

// Name returns the attribute name this descriptor binds to.
func (d *ClassMethodDescr) Name() string { return d.def.Name }

// Owner returns the type this descriptor is registered on.
func (d *ClassMethodDescr) Owner() *Type { return d.owner }

func classMethodDescrRepr(o Object) (string, error) {
	d := o.(*ClassMethodDescr)
	return "<method '" + d.def.Name + "' of '" + d.owner.Name + "' objects>", nil
}

// classMethodDescrGet2 binds the descriptor to a type. Class methods
// ignore the instance and bind to the type itself, so accessing the
// descriptor on the class or on an instance both yield a CFunction
// whose self is the class.
//
// CPython: Objects/descrobject.c:95 classmethod_get
func classMethodDescrGet2(descr Object, obj Object, ownerType *Type) (Object, error) {
	d := descr.(*ClassMethodDescr)
	t := ownerType
	if t == nil {
		if obj != nil {
			t = obj.Type()
		} else {
			return nil, fmt.Errorf("TypeError: descriptor '%s' for type '%s' needs either an object or a type",
				d.def.Name, d.owner.Name)
		}
	}
	if !IsSubtype(t, d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' requires a subtype of '%s' but received '%s'",
			d.def.Name, d.owner.Name, t.Name)
	}
	var cls *Type
	if d.def.Flags&MethMethod != 0 {
		cls = d.owner
	}
	// classmethod_get -> PyCMethod_New yields a builtin_function_or_method
	// whose m_self is the bound class. gopy's BuiltinFunction stands in for
	// PyCFunction/PyCMethod, so the bound object must report
	// BuiltinFunctionType (types.BuiltinMethodType), not the internal
	// CFunction shim used purely for flag dispatch.
	//
	// CPython: Objects/descrobject.c:95 classmethod_get (PyCMethod_New)
	cf, err := NewCFunction(d.def, t, nil, cls)
	if err != nil {
		return nil, err
	}
	Incref(t)
	bf := &BuiltinFunction{
		Name:       d.def.Name,
		Conv:       MethVarargs | MethKeywords,
		Self:       t,
		ownsSelf:   true,
		methOrigin: d,
		Doc:        d.def.Doc,
		Fn: func(args []Object, kwargs map[string]Object) (Object, error) {
			return cfunctionCall(cf, args, kwargs)
		},
	}
	bf.init(BuiltinFunctionType)
	return bf, nil
}

// classMethodDescrCall is the unbound call: the first positional
// argument must be a subtype of the owner; it becomes the bound class.
//
// CPython: Objects/descrobject.c:491 classmethoddescr_call
func classMethodDescrCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	d := o.(*ClassMethodDescr)
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of '%s' object needs an argument", d.def.Name, d.owner.Name)
	}
	self := args[0]
	t, ok := self.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for type '%s' needs a type, not a '%s' as arg 2",
			d.def.Name, d.owner.Name, self.Type().Name)
	}
	bound, err := classMethodDescrGet2(d, nil, t)
	if err != nil {
		return nil, err
	}
	posTuple := NewTuple(append([]Object(nil), args[1:]...))
	var kw *Dict
	if len(kwargs) > 0 {
		kw = NewDict()
		for k, v := range kwargs {
			if serr := kw.SetItem(NewStr(k), v); serr != nil {
				return nil, serr
			}
		}
	}
	return Call(bound, posTuple, kw)
}
