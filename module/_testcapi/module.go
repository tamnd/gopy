// Package testcapi is the gopy port of CPython's Modules/_testcapi C
// extension. CPython builds _testcapi from many translation units that
// exercise the private C API; gopy ports the pieces the standard-library
// test suite reaches for. This file ports the PEP 590 vectorcall surface
// (Modules/_testcapi/vectorcall.c) plus the meth_* calling-convention
// fixtures and the Meth{Instance,Class,Static} types from
// Modules/_testcapimodule.c that test_call.py drives directly.
//
// CPython: Modules/_testcapi/vectorcall.c:1, Modules/_testcapimodule.c:1740
//
// Only the call-convention and vectorcall fixtures are ported here; the
// remaining _testcapi translation units are added as gates need them.
package testcapi

import (
	"fmt"
	"math"
	"math/big"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_testcapi", buildModule)
}

// callingMask isolates the calling-convention bits of a MethFlag,
// dropping the METH_CLASS / METH_STATIC binding modifiers.
const callingMask = objects.MethVarargs | objects.MethKeywords |
	objects.MethNoArgs | objects.MethO | objects.MethFastcall | objects.MethMethod

// res builds the result tuple the meth_* fixtures return.
func res(items ...objects.Object) objects.Object { return objects.NewTuple(items) }

// tupleFrom copies args into a fresh tuple, mirroring _fastcall_to_tuple.
//
// CPython: Modules/_testcapimodule.c:1762 _fastcall_to_tuple
func tupleFrom(args []objects.Object) *objects.Tuple {
	return objects.NewTuple(append([]objects.Object(nil), args...))
}

// dictFromMap materialises a kwargs map into a Python dict.
func dictFromMap(kwargs map[string]objects.Object) *objects.Dict {
	d := objects.NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(objects.NewStr(k), v)
	}
	return d
}

// methDef builds a PyMethodDef-shaped definition for one meth_* fixture.
// The closures construct the (self, args...) result tuples documented in
// _testcapimodule.c; self is whatever the bound CFunction carries
// (module, class, or None for METH_STATIC), so _null_to_none never fires.
//
// CPython: Modules/_testcapimodule.c:1740 meth_varargs (and siblings)
func methDef(name string, flags objects.MethFlag) *objects.MethodDef {
	d := &objects.MethodDef{Name: name, Flags: flags}
	switch flags & callingMask {
	case objects.MethVarargs:
		d.Varargs = func(self objects.Object, args *objects.Tuple) (objects.Object, error) {
			return res(self, args), nil
		}
	case objects.MethVarargs | objects.MethKeywords:
		d.VarargsKw = func(self objects.Object, args *objects.Tuple, kwargs *objects.Dict) (objects.Object, error) {
			return res(self, args, nullToNoneDict(kwargs)), nil
		}
	case objects.MethO:
		d.O = func(self, obj objects.Object) (objects.Object, error) {
			return res(self, obj), nil
		}
	case objects.MethNoArgs:
		d.NoArgs = func(self objects.Object) (objects.Object, error) {
			return self, nil
		}
	case objects.MethFastcall:
		d.Fastcall = func(self objects.Object, args []objects.Object) (objects.Object, error) {
			return res(self, tupleFrom(args)), nil
		}
	case objects.MethFastcall | objects.MethKeywords:
		d.FastcallKw = func(self objects.Object, args []objects.Object, kwnames *objects.Tuple) (objects.Object, error) {
			nkw := 0
			if kwnames != nil {
				nkw = kwnames.Len()
			}
			nargs := len(args) - nkw
			pos := tupleFrom(args[:nargs])
			var kw objects.Object = objects.None()
			if nkw > 0 {
				kd := objects.NewDict()
				for i := 0; i < nkw; i++ {
					_ = kd.SetItem(kwnames.Item(i), args[nargs+i])
				}
				kw = kd
			}
			return res(self, pos, kw), nil
		}
	}
	return d
}

// nullToNoneDict mirrors _null_to_none: a NULL kwargs dict renders as
// None, a populated one renders as itself.
//
// CPython: Modules/_testcapimodule.c:1730 _null_to_none
func nullToNoneDict(d *objects.Dict) objects.Object {
	if d == nil {
		return objects.None()
	}
	return d
}

// instanceMethod wraps a meth_* fixture as a plain instance method
// descriptor: the receiver is args[0], the rest are the call arguments.
func instanceMethod(owner *objects.Type, name string, flags objects.MethFlag) *objects.MethodDescr {
	calling := flags & callingMask
	fn := func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		self := args[0]
		rest := args[1:]
		switch calling {
		case objects.MethVarargs:
			return res(self, tupleFrom(rest)), nil
		case objects.MethVarargs | objects.MethKeywords, objects.MethFastcall | objects.MethKeywords:
			var kw objects.Object = objects.None()
			if len(kwargs) > 0 {
				kw = dictFromMap(kwargs)
			}
			return res(self, tupleFrom(rest), kw), nil
		case objects.MethO:
			return res(self, rest[0]), nil
		case objects.MethNoArgs:
			return self, nil
		case objects.MethFastcall:
			return res(self, tupleFrom(rest)), nil
		}
		return nil, fmt.Errorf("SystemError: meth %s: bad call flags", name)
	}
	return objects.NewMethodDescrConv(owner, name, flags, fn)
}

// methNames lists the six calling-convention fixtures and their flags.
//
// CPython: Modules/_testcapimodule.c:3031 meth_instance_methods
var methNames = []struct {
	name  string
	flags objects.MethFlag
}{
	{"meth_varargs", objects.MethVarargs},
	{"meth_varargs_keywords", objects.MethVarargs | objects.MethKeywords},
	{"meth_o", objects.MethO},
	{"meth_noargs", objects.MethNoArgs},
	{"meth_fastcall", objects.MethFastcall},
	{"meth_fastcall_keywords", objects.MethFastcall | objects.MethKeywords},
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_testcapi")
	d := m.Dict()
	modName := objects.NewStr("_testcapi")

	// Module-level meth_* functions: m_self is the module itself, so the
	// fixture's self argument is the module.
	//
	// CPython: Modules/_testcapimodule.c module method table
	for _, mm := range methNames {
		cf, err := objects.NewCFunction(methDef(mm.name, mm.flags), m, modName, nil)
		if err != nil {
			return nil, err
		}
		if err := d.SetItem(objects.NewStr(mm.name), cf); err != nil {
			return nil, err
		}
	}

	// MethInstance: instance methods. self is the instance.
	//
	// CPython: Modules/_testcapimodule.c:3041 MethInstance_Type
	methInstance := objects.NewType("MethInstance", []*objects.Type{objects.ObjectType()})
	methInstance.TpNew = objects.TypeGenericNew
	for _, mm := range methNames {
		objects.SetTypeDescr(methInstance, mm.name, instanceMethod(methInstance, mm.name, mm.flags))
	}

	// MethClass: class methods. self is the class, whether the descriptor
	// is reached through the class or an instance.
	//
	// CPython: Modules/_testcapimodule.c:3063 MethClass_Type
	methClass := objects.NewType("MethClass", []*objects.Type{objects.ObjectType()})
	methClass.TpNew = objects.TypeGenericNew
	for _, mm := range methNames {
		objects.SetTypeDescr(methClass, mm.name, objects.NewClassMethodDescr(methClass, methDef(mm.name, mm.flags)))
	}

	// MethStatic: static methods. self is NULL, rendered as None.
	//
	// CPython: Modules/_testcapimodule.c:3085 MethStatic_Type
	methStatic := objects.NewType("MethStatic", []*objects.Type{objects.ObjectType()})
	methStatic.TpNew = objects.TypeGenericNew
	for _, mm := range methNames {
		cf, err := objects.NewCFunction(methDef(mm.name, mm.flags), objects.None(), nil, nil)
		if err != nil {
			return nil, err
		}
		objects.SetTypeDescr(methStatic, mm.name, objects.NewStaticMethod(cf))
	}

	for name, t := range map[string]*objects.Type{
		"MethInstance": methInstance,
		"MethClass":    methClass,
		"MethStatic":   methStatic,
		"awaitType":    awaitType,
		"Generic":      genericType,
		"GenericAlias": genericAliasType,
	} {
		if err := d.SetItem(objects.NewStr(name), t); err != nil {
			return nil, err
		}
	}

	// MethodDescriptor* types: per-instance vectorcall fixtures.
	//
	// CPython: Modules/_testcapi/vectorcall.c:381 _PyTestCapi_Init_Vectorcall
	if err := buildMethodDescriptorTypes(d); err != nil {
		return nil, err
	}

	wrappers := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"pyobject_fastcalldict", pyobjectFastcalldict},
		{"pyobject_vectorcall", pyobjectVectorcall},
		{"pyvectorcall_call", pyvectorcallCall},
		{"pycfunction_call", pycfunctionCall},
		{"make_vectorcall_class", makeVectorcallClass},
		{"has_vectorcall_flag", hasVectorcallFlag},
		{"function_setvectorcall", functionSetvectorcall},
		{"test_with_docstring", testWithDocstring},
		{"bad_get", badGet},
	}
	for _, w := range wrappers {
		if err := d.SetItem(objects.NewStr(w.name), objects.NewBuiltinFunction(w.name, w.fn)); err != nil {
			return nil, err
		}
	}

	// _Py_STACK_GROWS_DOWN: 1 on every platform gopy targets.
	//
	// CPython: Modules/_testcapimodule.c:3352 PyModule_AddIntMacro
	if err := d.SetItem(objects.NewStr("_Py_STACK_GROWS_DOWN"), objects.NewInt(1)); err != nil {
		return nil, err
	}

	// The C <limits.h>/<float.h> bounds the module exposes for the
	// string-formatting overflow tests (a huge width/precision forces the
	// MemoryError and OverflowError paths) and the struct round-trip tests.
	// gopy targets 64-bit LP64, so long is 8 bytes; the unsigned 64-bit and
	// signed-63-bit maxima exceed int64 and go through big.Int.
	//
	// CPython: Modules/_testcapimodule.c:3315 PyModule_AddObject (limit block)
	maxUint64 := new(big.Int).SetUint64(math.MaxUint64)
	intConsts := []struct {
		name string
		val  *big.Int
	}{
		{"SHRT_MAX", big.NewInt(math.MaxInt16)},
		{"SHRT_MIN", big.NewInt(math.MinInt16)},
		{"USHRT_MAX", big.NewInt(math.MaxUint16)},
		{"INT_MAX", big.NewInt(math.MaxInt32)},
		{"INT_MIN", big.NewInt(math.MinInt32)},
		{"UINT_MAX", big.NewInt(math.MaxUint32)},
		{"LONG_MAX", big.NewInt(math.MaxInt64)},
		{"LONG_MIN", big.NewInt(math.MinInt64)},
		{"ULONG_MAX", maxUint64},
		{"LLONG_MAX", big.NewInt(math.MaxInt64)},
		{"LLONG_MIN", big.NewInt(math.MinInt64)},
		{"ULLONG_MAX", maxUint64},
		{"PY_SSIZE_T_MAX", big.NewInt(math.MaxInt64)},
		{"PY_SSIZE_T_MIN", big.NewInt(math.MinInt64)},
		{"SIZE_MAX", maxUint64},
	}
	for _, c := range intConsts {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewIntFromBig(c.val)); err != nil {
			return nil, err
		}
	}

	// FLT_MAX / FLT_MIN: the C <float.h> single-precision bounds, exposed
	// as Python floats (doubles holding the float32 extremes) so
	// test_float can compare struct.pack("<f", ...) round-trips. FLT_MIN
	// is the smallest positive *normalized* float32 (2**-126), which Go
	// has no named constant for (SmallestNonzeroFloat32 is the subnormal
	// minimum). DBL_MAX/DBL_MIN are the double bounds.
	floatConsts := []struct {
		name string
		val  float64
	}{
		{"FLT_MAX", math.MaxFloat32},
		{"FLT_MIN", math.Ldexp(1, -126)},
		{"DBL_MAX", math.MaxFloat64},
		{"DBL_MIN", math.Ldexp(1, -1022)},
	}
	for _, c := range floatConsts {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewFloat(c.val)); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// fastcallArgs unpacks a tuple-or-None argument into a positional slice
// and count, mirroring fastcall_args.
//
// CPython: Modules/_testcapi/vectorcall.c:18 fastcall_args
func fastcallArgs(args objects.Object) ([]objects.Object, int, error) {
	if args == objects.None() {
		return nil, 0, nil
	}
	if t, ok := args.(*objects.Tuple); ok {
		stack := make([]objects.Object, t.Len())
		for i := 0; i < t.Len(); i++ {
			stack[i] = t.Item(i)
		}
		return stack, t.Len(), nil
	}
	return nil, 0, fmt.Errorf("TypeError: args must be None or a tuple")
}

// pyobjectFastcalldict ports _testcapi.pyobject_fastcalldict, a thin
// wrapper over PyObject_VectorcallDict.
//
// CPython: Modules/_testcapi/vectorcall.c:44 _testcapi_pyobject_fastcalldict_impl
func pyobjectFastcalldict(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: pyobject_fastcalldict expected 3 arguments, got %d", len(args))
	}
	fn, funcArgs, kwargs := args[0], args[1], args[2]
	stack, nargs, err := fastcallArgs(funcArgs)
	if err != nil {
		return nil, err
	}
	var kw *objects.Dict
	if kwargs != objects.None() {
		kd, ok := kwargs.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: kwnames must be None or a dict")
		}
		kw = kd
	}
	return objects.VectorcallDict(fn, stack, uint(nargs), kw)
}

// pyobjectVectorcall ports _testcapi.pyobject_vectorcall, a thin wrapper
// over PyObject_Vectorcall.
//
// CPython: Modules/_testcapi/vectorcall.c:74 _testcapi_pyobject_vectorcall_impl
func pyobjectVectorcall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: pyobject_vectorcall expected 3 arguments, got %d", len(args))
	}
	fn, funcArgs, kwnamesArg := args[0], args[1], args[2]
	stack, nargs, err := fastcallArgs(funcArgs)
	if err != nil {
		return nil, err
	}
	var kwnames *objects.Tuple
	if kwnamesArg != objects.None() {
		t, ok := kwnamesArg.(*objects.Tuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: kwnames must be None or a tuple")
		}
		nkw := t.Len()
		if nargs < nkw {
			return nil, fmt.Errorf("ValueError: kwnames longer than args")
		}
		nargs -= nkw
		kwnames = t
	}
	return objects.Vectorcall(fn, stack, uint(nargs), kwnames)
}

// pyvectorcallCall ports _testcapi.pyvectorcall_call over
// PyVectorcall_Call.
//
// CPython: Modules/_testcapi/vectorcall.c:130 _testcapi_pyvectorcall_call_impl
func pyvectorcallCall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: pyvectorcall_call expected 2 or 3 arguments, got %d", len(args))
	}
	fn := args[0]
	argstuple, ok := args[1].(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: args must be a tuple")
	}
	var kw *objects.Dict
	if len(args) == 3 && args[2] != objects.None() {
		kd, ok := args[2].(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: kwargs must be a dict")
		}
		kw = kd
	}
	return objects.Call(fn, argstuple, kw)
}

// pycfunctionCall ports _testcapi.pycfunction_call over PyCFunction_Call.
//
// CPython: Modules/_testcapimodule.c:1801 test_pycfunction_call
func pycfunctionCall(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: pycfunction_call expected 2 or 3 arguments, got %d", len(args))
	}
	fn := args[0]
	posArgs, ok := args[1].(*objects.Tuple)
	if !ok {
		return nil, fmt.Errorf("TypeError: argument 2 must be tuple, not %s", args[1].Type().Name)
	}
	var kw *objects.Dict
	if len(args) == 3 {
		kd, ok := args[2].(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: argument 3 must be dict, not %s", args[2].Type().Name)
		}
		kw = kd
	}
	return objects.Call(fn, posArgs, kw)
}
