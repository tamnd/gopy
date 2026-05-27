// MethodDescr is the type-level descriptor for built-in methods.
// CPython generates one per PyMethodDef row when the type table is
// loaded and stores it in tp_dict; gopy wires it explicitly via
// SetTypeDescr until tp_dict lands. Fetching the descriptor through
// an instance (`obj.method`) returns a BoundMethod that prepends the
// instance to the call, mirroring method_get + cfunction_call.
//
// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type

package objects

import "fmt"

// MethodDescr wraps a Go function whose first positional argument is
// the receiver. The fn signature matches BuiltinFunction so the same
// closure can serve as the unbound call target after BoundMethod
// prepends self.
//
// Conv is the METH_* calling-convention tag inherited from the
// PyMethodDef row. specialize_method_descriptor reads it to pick
// CALL_METHOD_DESCRIPTOR_NOARGS / _O / _FAST / _FAST_WITH_KEYWORDS.
// Defaults to MethVarargs|MethKeywords so legacy NewMethodDescr
// callers keep matching the closure shape they always passed.
//
// CPython: Include/cpython/descrobject.h PyMethodDescrObject
type MethodDescr struct {
	Header
	name  string
	doc   string
	conv  MethFlag
	fn    func(args []Object, kwargs map[string]Object) (Object, error)
	owner *Type
}

// MethodDescrType is the type singleton for method descriptors.
//
// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type
var MethodDescrType = NewType("method_descriptor", []*Type{objectType})

func init() {
	MethodDescrType.Repr = methodDescrRepr
	MethodDescrType.Str = methodDescrRepr
	MethodDescrType.DescrGet = methodDescrGet
	MethodDescrType.Call = methodDescrCall
	MethodDescrType.Vectorcall = methodDescrVectorcall
	// Identity hash so method descriptors are hashable.
	MethodDescrType.Hash = identityHash
	addDescriptorSlotWrappers(MethodDescrType)
	AddCallSlotWrapper(MethodDescrType)
	addDescrIntrospectionDescriptors(MethodDescrType)
}

// Owner returns the type this method descriptor is registered on.
func (d *MethodDescr) Owner() *Type { return d.owner }

// NewMethodDescr builds a method descriptor that exposes name on
// owner. fn receives the receiver as args[0]. Conv defaults to
// MethVarargs|MethKeywords.
//
// CPython: Objects/descrobject.c:1100 PyDescr_NewMethod
func NewMethodDescr(owner *Type, name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodDescr {
	d := &MethodDescr{name: name, conv: MethVarargs | MethKeywords, fn: fn, owner: owner}
	d.init(MethodDescrType)
	return d
}

// NewMethodDescrConv builds a method descriptor with an explicit
// METH_* tag. Mirrors PyDescr_NewMethod when the PyMethodDef row
// declares METH_NOARGS / METH_O / METH_FASTCALL / etc.
//
// CPython: Objects/descrobject.c:1100 PyDescr_NewMethod
func NewMethodDescrConv(owner *Type, name string, conv MethFlag, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodDescr {
	d := &MethodDescr{name: name, conv: conv, fn: fn, owner: owner}
	d.init(MethodDescrType)
	return d
}

// Name returns the attribute name this descriptor binds to.
func (d *MethodDescr) Name() string { return d.name }

// Doc returns the documentation string for this descriptor, or "".
func (d *MethodDescr) Doc() string { return d.doc }

// Conv returns the METH_* calling-convention tag the specializer
// uses to pick the descriptor-specific CALL fast arm.
func (d *MethodDescr) Conv() MethFlag { return d.conv }

func methodDescrRepr(o Object) (string, error) {
	d := o.(*MethodDescr)
	return "<method '" + d.name + "' of '" + d.owner.Name + "' objects>", nil
}

// methodDescrGet binds the descriptor to an instance. Class-level
// access (owner==nil) returns the descriptor unchanged so attribute
// lookup on the type itself sees the descriptor object.
//
// CPython: Objects/descrobject.c:230 method_get
func methodDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	if owner == nil {
		return descr, nil
	}
	return NewBoundMethod(descr, owner), nil
}

// methodDescrCall is the unbound call: the first positional argument
// is the receiver, mirroring how a class-level method descriptor is
// dispatched in CPython.
//
// CPython: Objects/descrobject.c:296 method_call
func methodDescrCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	d := o.(*MethodDescr)
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of '%s' object needs an argument", d.name, d.owner.Name)
	}
	if !IsSubtype(args[0].Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, args[0].Type().Name)
	}
	return d.fn(args, kwargs)
}

// methodDescrVectorcall converts the (args, nargsf, kwnames) shape
// into the (positional, kwargs) closure the descriptor stores.
// Mirrors method_vectorcall + cfunction_vectorcall_FASTCALL_KEYWORDS.
//
// CPython: Objects/descrobject.c:330 method_vectorcall_FASTCALL_KEYWORDS
func methodDescrVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	d := callable.(*MethodDescr)
	nargs := VectorcallNargs(nargsf)
	if nargs == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '%s' of '%s' object needs an argument", d.name, d.owner.Name)
	}
	if !IsSubtype(args[0].Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, args[0].Type().Name)
	}
	pos := make([]Object, nargs)
	copy(pos, args[:nargs])
	var kwargs map[string]Object
	if kwnames != nil && kwnames.Len() > 0 {
		nkw := kwnames.Len()
		kwargs = make(map[string]Object, nkw)
		for i := 0; i < nkw; i++ {
			name, err := Str(kwnames.Item(i))
			if err != nil {
				return nil, err
			}
			kwargs[name] = args[nargs+i]
		}
	}
	return d.fn(pos, kwargs)
}
