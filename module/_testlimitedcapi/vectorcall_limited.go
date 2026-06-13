// Port of Modules/_testlimitedcapi/vectorcall_limited.c. These fixtures
// exercise PEP 590 vectorcall through the limited C API: a class whose
// instances carry a per-instance vectorcall pointer, plus the
// PyObject_Vectorcall / PyObject_VectorcallMethod outgoing-call helpers.
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:1
package testlimitedcapi

import (
	"github.com/tamnd/gopy/objects"
)

// vcFunc is the gopy vectorcall closure shape: (callable, args, nargsf,
// kwnames).
type vcFunc = func(objects.Object, []objects.Object, uint, *objects.Tuple) (objects.Object, error)

// limitedVectorCallObject ports the instance struct shared by
// LimitedVectorCallClass and LimitedRelativeVectorCallClass: a single
// per-instance vectorcall pointer that LimitedVectorCallClass_new installs.
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:33 LimitedVectorCallClass_new
type limitedVectorCallObject struct {
	objects.Header
	vectorcall vcFunc
}

// InstanceVectorcall exposes the per-instance pointer the type's vectorcall
// offset names.
func (o *limitedVectorCallObject) InstanceVectorcall() vcFunc { return o.vectorcall }

// limitedVectorCallClassVectorcall returns "vectorcall called".
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:24 LimitedVectorCallClass_vectorcall
func limitedVectorCallClassVectorcall(_ objects.Object, _ []objects.Object, _ uint, _ *objects.Tuple) (objects.Object, error) {
	return objects.NewStr("vectorcall called"), nil
}

// limitedVectorCallClassTpcall returns "tp_call called".
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:20 LimitedVectorCallClass_tpcall
func limitedVectorCallClassTpcall(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewStr("tp_call called"), nil
}

// limitedVectorCallClassNew allocates an instance with its vectorcall field
// set, mirroring LimitedVectorCallClass_new and LimitedRelativeVectorCallClass_new
// (both store the same function pointer; gopy carries it in one field).
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:33 LimitedVectorCallClass_new
func limitedVectorCallClassNew(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	op := &limitedVectorCallObject{vectorcall: limitedVectorCallClassVectorcall}
	op.Header.Init(cls)
	return op, nil
}

// buildVectorcallLimitedTypes constructs the two limited-API vectorcall
// classes and registers them on the module dict under their short names
// (PyModule_AddType strips the module prefix).
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:221 _PyTestLimitedCAPI_Init_VectorcallLimited
func buildVectorcallLimitedTypes(d *objects.Dict) error {
	base := objects.NewType("_testlimitedcapi.LimitedVectorCallClass", []*objects.Type{objects.ObjectType()})
	// basicsize = sizeof(PyObject) + sizeof(vectorcallfunc); flags carry
	// HAVE_VECTORCALL and BASETYPE.
	base.BaseSize = 24
	base.TpFlags |= objects.TpFlagHaveVectorcall
	base.TpNew = limitedVectorCallClassNew
	base.Call = limitedVectorCallClassTpcall
	objects.AddCallSlotWrapper(base)

	// LimitedRelativeVectorCallClass uses a relative vectorcall offset but
	// behaves identically here: HAVE_VECTORCALL, no BASETYPE.
	rel := objects.NewType("_testlimitedcapi.LimitedRelativeVectorCallClass", []*objects.Type{objects.ObjectType()})
	rel.BaseSize = 24
	rel.TpFlags |= objects.TpFlagHaveVectorcall
	rel.TpFlags &^= objects.TpFlagBasetype
	rel.TpNew = limitedVectorCallClassNew
	rel.Call = limitedVectorCallClassTpcall
	objects.AddCallSlotWrapper(rel)

	if err := d.SetItem(objects.NewStr("LimitedVectorCallClass"), base); err != nil {
		return err
	}
	return d.SetItem(objects.NewStr("LimitedRelativeVectorCallClass"), rel)
}

// callVectorcall ports _testlimitedcapi.call_vectorcall: it calls callable
// via PyObject_Vectorcall with one positional ("foo") and one keyword
// (baz="bar").
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:51 _testlimitedcapi_call_vectorcall
func callVectorcall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("call_vectorcall", args, 1); err != nil {
		return nil, err
	}
	callable := args[0]
	stack := []objects.Object{objects.NewStr("foo"), objects.NewStr("bar")}
	kwnames := objects.NewTuple([]objects.Object{objects.NewStr("baz")})
	return objects.Vectorcall(callable, stack, 1, kwnames)
}

// callVectorcallMethod ports _testlimitedcapi.call_vectorcall_method: it
// calls callable.f via PyObject_VectorcallMethod with one positional ("foo")
// and one keyword (baz="bar"). gopy resolves the bound method by attribute
// lookup, the same binding PyObject_VectorcallMethod performs.
//
// CPython: Modules/_testlimitedcapi/vectorcall_limited.c:105 _testlimitedcapi_call_vectorcall_method
func callVectorcallMethod(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if err := arity("call_vectorcall_method", args, 1); err != nil {
		return nil, err
	}
	callable := args[0]
	meth, err := objects.GetAttr(callable, objects.NewStr("f"))
	if err != nil {
		return nil, err
	}
	stack := []objects.Object{objects.NewStr("foo"), objects.NewStr("bar")}
	kwnames := objects.NewTuple([]objects.Object{objects.NewStr("baz")})
	return objects.Vectorcall(meth, stack, 1, kwnames)
}
