// Port of the MethodDescriptor / VectorCallClass fixtures from
// Modules/_testcapi/vectorcall.c. These types exercise PEP 590 per-instance
// vectorcall: each instance stores its own vectorcall pointer
// (tp_vectorcall_offset), so gopy models them through the
// objects.InstanceVectorcaller interface rather than the shared
// Type.Vectorcall slot.
//
// CPython: Modules/_testcapi/vectorcall.c:275 MethodDescriptorObject
package testcapi

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// vcFunc is the gopy vectorcall closure shape: (callable, args, nargsf,
// kwnames).
type vcFunc = func(objects.Object, []objects.Object, uint, *objects.Tuple) (objects.Object, error)

// methodDescriptorObject ports both MethodDescriptorObject and
// MethodDescriptor2Object. The C structs differ only in where
// tp_vectorcall_offset points: MethodDescriptorObject reads its single
// vectorcall field, MethodDescriptor2Object reads the second field while the
// inherited base field stays NULL. gopy carries both as struct fields and
// flips useOwn to select which one the type's vectorcall offset names.
//
// CPython: Modules/_testcapi/vectorcall.c:275 MethodDescriptorObject
type methodDescriptorObject struct {
	objects.Header
	baseVectorcall vcFunc // MethodDescriptorObject.vectorcall
	ownVectorcall  vcFunc // MethodDescriptor2Object.vectorcall
	useOwn         bool   // tp_vectorcall_offset names ownVectorcall
}

// InstanceVectorcall returns the function the type's vectorcall offset names,
// mirroring *(vectorcallfunc*)((char*)self + tp_vectorcall_offset).
func (o *methodDescriptorObject) InstanceVectorcall() vcFunc {
	if o.useOwn {
		return o.ownVectorcall
	}
	return o.baseVectorcall
}

// methodDescriptorVectorcall returns True when called through the
// MethodDescriptorObject vectorcall field and False for
// MethodDescriptor2Object, whose base field is NULL.
//
// CPython: Modules/_testcapi/vectorcall.c:281 MethodDescriptor_vectorcall
func methodDescriptorVectorcall(callable objects.Object, _ []objects.Object, _ uint, _ *objects.Tuple) (objects.Object, error) {
	md := callable.(*methodDescriptorObject)
	return objects.NewBool(md.baseVectorcall != nil), nil
}

// methodDescriptorNew allocates a MethodDescriptorObject with its vectorcall
// field set.
//
// CPython: Modules/_testcapi/vectorcall.c:291 MethodDescriptor_new
func methodDescriptorNew(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	op := &methodDescriptorObject{baseVectorcall: methodDescriptorVectorcall}
	op.Header.Init(cls)
	return op, nil
}

// methodDescriptor2New allocates a MethodDescriptor2Object: the base
// vectorcall field is NULL and the second field carries the function the
// type's offset names.
//
// CPython: Modules/_testcapi/vectorcall.c:331 MethodDescriptor2_new
func methodDescriptor2New(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	op := &methodDescriptorObject{ownVectorcall: methodDescriptorVectorcall, useOwn: true}
	op.Header.Init(cls)
	return op, nil
}

// funcDescrGet binds the descriptor to an instance like a plain function:
// class-level access returns the descriptor, instance access returns a bound
// method.
//
// CPython: Modules/_testcapi/vectorcall.c:299 func_descr_get
func funcDescrGet(descr objects.Object, obj objects.Object, _ *objects.Type) (objects.Object, error) {
	if obj == nil || obj == objects.None() {
		return descr, nil
	}
	return objects.NewBoundMethod(descr, obj), nil
}

// nopDescrGet returns the descriptor unchanged on every access.
//
// CPython: Modules/_testcapi/vectorcall.c:308 nop_descr_get
func nopDescrGet(descr objects.Object, _ objects.Object, _ *objects.Type) (objects.Object, error) {
	return descr, nil
}

// callReturnArgs is the tp_call that returns its positional arguments,
// letting test_vectorcall_override confirm tp_call overrides vectorcall.
// This []Object form is reached on the vectorcall->tp_call adapter path,
// where CPython itself builds a fresh tuple; the identity-preserving path
// is callReturnArgsTuple below.
//
// CPython: Modules/_testcapi/vectorcall.c:314 call_return_args
func callReturnArgs(_ objects.Object, args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewTuple(append([]objects.Object(nil), args...)), nil
}

// callReturnArgsTuple is the ternaryfunc-faithful tp_call: it returns the
// very tuple it was handed (with a new reference), so test_vectorcall_override
// can assert f(*args) is args. CPython's call_return_args does Py_INCREF(args)
// then returns args.
//
// CPython: Modules/_testcapi/vectorcall.c:314 call_return_args
func callReturnArgsTuple(_ objects.Object, args *objects.Tuple, _ *objects.Dict) (objects.Object, error) {
	objects.Incref(args)
	return args, nil
}

// vectorcallCallSlot is the tp_call that PyVectorcall_Call installs on the
// MethodDescriptor base types: it dispatches the instance's own vectorcall.
//
// CPython: Objects/call.c:265 PyVectorcall_Call
func vectorcallCallSlot(callable objects.Object, args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	iv, ok := callable.(objects.InstanceVectorcaller)
	if !ok {
		return nil, fmt.Errorf("TypeError: '%s' object does not support vectorcall", callable.Type().Name)
	}
	fn := iv.InstanceVectorcall()
	if fn == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not callable", callable.Type().Name)
	}
	var kwnames *objects.Tuple
	stack := append([]objects.Object(nil), args...)
	if len(kwargs) > 0 {
		names := make([]objects.Object, 0, len(kwargs))
		for k, v := range kwargs {
			names = append(names, objects.NewStr(k))
			stack = append(stack, v)
		}
		kwnames = objects.NewTuple(names)
	}
	return fn(callable, stack, uint(len(args)), kwnames)
}

// buildMethodDescriptorTypes constructs the four MethodDescriptor types and
// registers them on the module dict.
//
// CPython: Modules/_testcapi/vectorcall.c:381 _PyTestCapi_Init_Vectorcall
func buildMethodDescriptorTypes(d *objects.Dict) error {
	base := objects.NewType("MethodDescriptorBase", []*objects.Type{objects.ObjectType()})
	// MethodDescriptorObject = PyObject_HEAD + one vectorcallfunc, so the
	// base fixes its own instance layout (solid_base): a multiple-inheritance
	// subclass like MethodDescriptorSuper(SuperBase, MethodDescriptorBase)
	// then resolves best_base to MethodDescriptorBase and inherits its tp_new.
	//
	// CPython: Modules/_testcapi/vectorcall.c:275 MethodDescriptorObject
	base.BaseSize = 24
	base.TpFlags |= objects.TpFlagMethodDescriptor | objects.TpFlagHaveVectorcall
	base.TpNew = methodDescriptorNew
	base.Call = vectorcallCallSlot
	base.Vectorcall = methodDescriptorVectorcall
	base.DescrGet = funcDescrGet
	objects.AddCallSlotWrapper(base)

	derived := objects.NewType("MethodDescriptorDerived", []*objects.Type{base})
	derived.BaseSize = base.BaseSize
	// A static subtype inherits METHOD_DESCRIPTOR and HAVE_VECTORCALL from
	// its base during PyType_Ready; NewType does not run that inheritance,
	// so stamp the inherited flags explicitly.
	derived.TpFlags |= objects.TpFlagMethodDescriptor | objects.TpFlagHaveVectorcall
	derived.TpNew = methodDescriptorNew
	// inherit_slots copies tp_call, tp_descr_get and the vectorcall slot from
	// the base, so PyCallable_Check(MethodDescriptorDerived()) is true and the
	// instance dispatches the same way as the base.
	derived.Call = base.Call
	derived.Vectorcall = base.Vectorcall
	derived.DescrGet = base.DescrGet

	nopGet := objects.NewType("MethodDescriptorNopGet", []*objects.Type{base})
	nopGet.BaseSize = base.BaseSize
	// NopGet defines its own tp_call, so it does not inherit HAVE_VECTORCALL
	// or METHOD_DESCRIPTOR from the base.
	nopGet.TpFlags &^= objects.TpFlagMethodDescriptor | objects.TpFlagHaveVectorcall
	nopGet.TpNew = methodDescriptorNew
	nopGet.Call = callReturnArgs
	nopGet.CallTuple = callReturnArgsTuple
	nopGet.Vectorcall = nil
	nopGet.DescrGet = nopDescrGet

	md2 := objects.NewType("MethodDescriptor2", []*objects.Type{base})
	// MethodDescriptor2Object adds a second vectorcallfunc field.
	md2.BaseSize = 32
	md2.TpFlags &^= objects.TpFlagMethodDescriptor
	md2.TpFlags |= objects.TpFlagHaveVectorcall
	md2.TpNew = methodDescriptor2New
	// MethodDescriptor2 inherits tp_call (the vectorcall trampoline) and
	// tp_descr_get from the base; its own tp_new fills the second vectorcall
	// field while the base field stays NULL.
	md2.Call = base.Call
	md2.Vectorcall = base.Vectorcall
	md2.DescrGet = base.DescrGet

	for name, t := range map[string]*objects.Type{
		"MethodDescriptorBase":    base,
		"MethodDescriptorDerived": derived,
		"MethodDescriptorNopGet":  nopGet,
		"MethodDescriptor2":       md2,
	} {
		if err := d.SetItem(objects.NewStr(name), t); err != nil {
			return err
		}
	}
	return nil
}

// vectorCallObject ports VectorCallClass instances: each carries a
// per-instance vectorcall pointer, NULL until set_vectorcall installs one.
//
// CPython: Modules/_testcapi/vectorcall.c:140 VectorCallClass tp_call
type vectorCallObject struct {
	objects.Header
	vectorcall vcFunc
}

// InstanceVectorcall exposes the per-instance pointer.
func (o *vectorCallObject) InstanceVectorcall() vcFunc { return o.vectorcall }

// vectorCallClassTpcall returns "tp_call"; it is reached when no per-instance
// vectorcall has been installed.
//
// CPython: Modules/_testcapi/vectorcall.c:148 VectorCallClass_tpcall
func vectorCallClassTpcall(_ objects.Object, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return objects.NewStr("tp_call"), nil
}

// vectorCallClassVectorcall returns "vectorcall"; set_vectorcall installs it
// as an instance's vectorcall pointer.
//
// CPython: Modules/_testcapi/vectorcall.c:153 VectorCallClass_vectorcall
func vectorCallClassVectorcall(_ objects.Object, _ []objects.Object, _ uint, _ *objects.Tuple) (objects.Object, error) {
	return objects.NewStr("vectorcall"), nil
}

// makeVectorcallClass ports _testcapi.make_vectorcall_class: it builds a fresh
// class whose instances return "tp_call" until set_vectorcall installs a
// per-instance vectorcall returning "vectorcall".
//
// CPython: Modules/_testcapi/vectorcall.c:225 _testcapi_make_vectorcall_class_impl
func makeVectorcallClass(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	base := objects.ObjectType()
	if len(args) >= 1 && args[0] != objects.None() {
		bt, ok := args[0].(*objects.Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: argument must be a type")
		}
		base = bt
	}
	t := objects.NewType("_testcapi.VectorcallClass", []*objects.Type{base})
	// PyType_FromSpec builds a mutable heap type: clear IMMUTABLETYPE so the
	// test can assign TestType.__call__ and disable vectorcall.
	t.TpFlags &^= objects.TpFlagImmutable
	t.TpFlags |= objects.TpFlagHaveVectorcall
	t.Call = vectorCallClassTpcall
	t.TpNew = func(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		op := &vectorCallObject{}
		op.Header.Init(cls)
		return op, nil
	}
	objects.AddCallSlotWrapper(t)
	objects.SetTypeDescr(t, "set_vectorcall", objects.NewMethodDescrConv(t, "set_vectorcall", objects.MethO,
		func(margs []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
			return vectorCallSetVectorcall(margs)
		}))
	return t, nil
}

// vectorCallSetVectorcall ports VectorCallClass.set_vectorcall: it installs
// the "vectorcall" function on self's per-instance pointer after checking the
// type carries a vectorcall offset.
//
// CPython: Modules/_testcapi/vectorcall.c:175 _testcapi_VectorCallClass_set_vectorcall_impl
func vectorCallSetVectorcall(args []objects.Object) (objects.Object, error) {
	self := args[0]
	t, ok := args[1].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: set_vectorcall argument must be a type")
	}
	if !objects.IsSubtype(self.Type(), t) {
		return nil, fmt.Errorf("TypeError: expected %s instance", t.Name)
	}
	if t.TpFlags&objects.TpFlagHaveVectorcall == 0 {
		return nil, fmt.Errorf("TypeError: type %s has no vectorcall offset", t.Name)
	}
	vc, ok := self.(*vectorCallObject)
	if !ok {
		return nil, fmt.Errorf("TypeError: set_vectorcall self is not a VectorcallClass instance")
	}
	vc.vectorcall = vectorCallClassVectorcall
	return objects.None(), nil
}

// hasVectorcallFlag ports _testcapi.has_vectorcall_flag.
//
// CPython: Modules/_testcapi/vectorcall.c:255 _testcapi_has_vectorcall_flag_impl
func hasVectorcallFlag(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: has_vectorcall_flag expected 1 argument, got %d", len(args))
	}
	t, ok := args[0].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument must be a type")
	}
	return objects.NewBool(t.TpFlags&objects.TpFlagHaveVectorcall != 0), nil
}

// functionSetvectorcall ports _testcapi.function_setvectorcall: it installs
// override_vectorcall on a Python function so every call returns "overridden".
//
// CPython: Modules/_testcapi/vectorcall.c:110 function_setvectorcall
func functionSetvectorcall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: function_setvectorcall expected 1 argument, got %d", len(args))
	}
	fn, ok := args[0].(*objects.Function)
	if !ok {
		return nil, fmt.Errorf("TypeError: 'func' must be a function")
	}
	fn.SetVectorcall(overrideVectorcall)
	return objects.None(), nil
}

// overrideVectorcall returns "overridden" for every call.
//
// CPython: Modules/_testcapi/vectorcall.c:104 override_vectorcall
func overrideVectorcall(_ objects.Object, _ []objects.Object, _ uint, _ *objects.Tuple) (objects.Object, error) {
	return objects.NewStr("overridden"), nil
}
