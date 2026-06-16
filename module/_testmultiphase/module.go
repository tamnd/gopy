// Package testmultiphase is the gopy port of CPython's
// Modules/_testmultiphase.c, the C extension that exercises multi-phase
// initialization of extension modules (PEP 489). The standard-library
// test suite reaches for it indirectly: test.test_importlib.util runs
// import_helper.import_module("_testmultiphase") at import time, so any
// test that pulls in that helper (test_pkgutil, test_pyclbr, the
// test_importlib extension suites) raises SkipTest when the module is
// absent.
//
// gopy cannot dlopen the compiled extension, so the main module is
// reproduced as a Go-native inittab entry: the same name, methods, types
// and constants the C execfunc installs. The many PyInit__testmultiphase_*
// variants (nonmodule, bad_slot_*, negative_size, ...) drive the
// extension-loader edge cases in test_importlib and are added when those
// suites need them.
//
// CPython: Modules/_testmultiphase.c:447 PyInit__testmultiphase
// CPython: Modules/_testmultiphase.c:392 execfunc
package testmultiphase

import (
	"fmt"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	// gopy cannot dlopen a compiled extension, so each PyInit_* entry the C
	// extension exposes is registered as a gopy extension module keyed by
	// name, carrying the PEP 489 Py_mod_multiple_interpreters slot value its
	// PyModuleDef declares. _imp.create_dynamic dispatches here and applies
	// the subinterpreter compat check before running the body.
	//
	// CPython: Modules/_testmultiphase.c:438 main_slots (PER_INTERPRETER_GIL)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_testmultiphase",
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
		Init:               func() (*objects.Module, error) { return buildModule("_testmultiphase") },
	})
	// CPython: Modules/_testmultiphase.c:943 non_isolated_slots (NOT_SUPPORTED)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_test_non_isolated",
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpNotSupported,
		Init:               func() (*objects.Module, error) { return buildModule("_test_non_isolated") },
	})
	// CPython: Modules/_testmultiphase.c:964 shared_gil_only_slots (SUPPORTED, explicit)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_test_shared_gil_only",
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpSupported,
		Init:               func() (*objects.Module, error) { return buildModule("_test_shared_gil_only") },
	})
	// CPython: Modules/_testmultiphase.c:980 no_multiple_interpreter_slot_slots (no slot)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_test_no_multiple_interpreter_slot",
		HasMultiInterpSlot: false,
		Init:               func() (*objects.Module, error) { return buildModule("_test_no_multiple_interpreter_slot") },
	})
}

// exampleObject backs _testimportexec.Example: a GC type whose attribute
// store is an explicit x_attr dict consulted ahead of the generic
// attribute machinery.
//
// CPython: Modules/_testmultiphase.c:25 ExampleObject
type exampleObject struct {
	objects.Header
	xAttr *objects.Dict
}

// exampleType / strType / errorType are the singletons installed by
// execfunc.
//
// CPython: Modules/_testmultiphase.c:124 Example_Type_spec
// CPython: Modules/_testmultiphase.c:366 Str_Type_spec
// CPython: Modules/_testmultiphase.c:399 PyErr_NewException("_testimportexec.error")
var (
	exampleType *objects.Type
	strType     *objects.Type
	errorType   *objects.Type
)

// exampleDemo ports Example_demo: demo(o=None) returns o when it is a
// str, otherwise None.
//
// CPython: Modules/_testmultiphase.c:57 Example_demo
func exampleDemo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: demo() missing self argument")
	}
	rest := args[1:]
	if len(rest) > 1 {
		return nil, fmt.Errorf("TypeError: demo() takes at most 1 argument (%d given)", len(rest))
	}
	if len(rest) == 1 {
		if _, ok := rest[0].(*objects.Unicode); ok {
			return rest[0], nil
		}
	}
	return objects.None(), nil
}

// exampleGetattro ports Example_getattro: consult x_attr first, then fall
// back to PyObject_GenericGetAttr.
//
// CPython: Modules/_testmultiphase.c:77 Example_getattro
func exampleGetattro(o objects.Object, name objects.Object) (objects.Object, error) {
	self, ok := o.(*exampleObject)
	if ok && self.xAttr != nil {
		found, err := self.xAttr.Contains(name)
		if err != nil {
			return nil, err
		}
		if found {
			v, err := self.xAttr.GetItem(name)
			if err != nil {
				return nil, err
			}
			objects.Incref(v)
			return v, nil
		}
	}
	return objects.GenericGetAttr(o, name)
}

// exampleSetattro ports Example_setattr: store into the lazily created
// x_attr dict; a delete of a missing key raises AttributeError.
//
// CPython: Modules/_testmultiphase.c:93 Example_setattr
func exampleSetattro(o objects.Object, name objects.Object, value objects.Object) error {
	self, ok := o.(*exampleObject)
	if !ok {
		return fmt.Errorf("TypeError: not an Example")
	}
	if self.xAttr == nil {
		self.xAttr = objects.NewDict()
	}
	if value == nil {
		found, err := self.xAttr.Contains(name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("AttributeError: delete non-existing Example attribute")
		}
		return self.xAttr.DelItem(name)
	}
	return self.xAttr.SetItem(name, value)
}

// exampleTraverse keeps x_attr reachable for the collector.
//
// CPython: Modules/_testmultiphase.c:42 Example_traverse
func exampleTraverse(o objects.Object, visit objects.Visitor) error {
	self, ok := o.(*exampleObject)
	if !ok || self.xAttr == nil {
		return nil
	}
	return visit(self.xAttr)
}

// exampleNew constructs a bare Example instance.
//
// CPython: Modules/_testmultiphase.c:124 Example_Type_spec (tp_new via
// PyType_GenericNew default)
func exampleNew(cls *objects.Type, _ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	o := &exampleObject{}
	o.Init(cls)
	return o, nil
}

// testexportFoo ports testexport_foo: foo(i, j) returns i + j.
//
// CPython: Modules/_testmultiphase.c:308 testexport_foo
func testexportFoo(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: foo() takes exactly 2 arguments (%d given)", len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[0].Type().Name)
	}
	j, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[1].Type().Name)
	}
	iv, _ := i.Int64()
	jv, _ := j.Int64()
	return objects.NewInt(iv + jv), nil
}

// callStateRegistrationFunc ports call_state_registration_func. gopy has
// no per-module C state registry (PyState_FindModule / PyState_AddModule
// / PyState_RemoveModule), so the lookup case returns None and the
// add/remove cases are no-ops; the function exists only so the main
// module's surface matches the extension.
//
// CPython: Modules/_testmultiphase.c:328 call_state_registration_func
func callStateRegistrationFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: call_state_registration_func() takes exactly 1 argument (%d given)", len(args))
	}
	if _, ok := args[0].(*objects.Int); !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[0].Type().Name)
	}
	return objects.None(), nil
}

func init() {
	// CPython: Modules/_testmultiphase.c:114 Example_Type_slots
	exampleType = objects.NewType("Example", []*objects.Type{objects.ObjectType()})
	exampleType.Module = "_testimportexec"
	exampleType.TpFlags |= objects.TpFlagHaveGC
	exampleType.TpNew = exampleNew
	exampleType.Getattro = exampleGetattro
	exampleType.Setattro = exampleSetattro
	exampleType.TpTraverse = exampleTraverse
	objects.SetTypeDescr(exampleType, "demo", objects.NewMethodDescr(exampleType, "demo", exampleDemo))

	// CPython: Modules/_testmultiphase.c:361 Str_Type_slots (Py_tp_base
	// filled with &PyUnicode_Type in execfunc).
	strType = objects.NewType("Str", []*objects.Type{objects.StrType()})
	strType.Module = "_testimportexec"
	strType.TpFlags |= objects.TpFlagBasetype

	// CPython: Modules/_testmultiphase.c:399 PyErr_NewException
	errorType = pyerrors.NewExcType("error", []*objects.Type{pyerrors.PyExc_Exception})
	errorType.Module = "_testimportexec"
}

// buildModule assembles the _testmultiphase main module: the exported
// methods plus the Example/error/Str types and the int_const/str_const
// constants execfunc installs.
//
// CPython: Modules/_testmultiphase.c:392 execfunc
// CPython: Modules/_testmultiphase.c:444 main_def
func buildModule(name string) (*objects.Module, error) {
	m := objects.NewModule(name)
	d := m.Dict()

	// CPython: Modules/_testmultiphase.c:374 testexport_methods
	methods := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"foo", testexportFoo},
		{"call_state_registration_func", callStateRegistrationFunc},
	}
	for _, mm := range methods {
		if err := d.SetItem(objects.NewStr(mm.name), objects.NewBuiltinFunction(mm.name, mm.fn)); err != nil {
			return nil, err
		}
	}

	if err := d.SetItem(objects.NewStr("Example"), exampleType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("error"), errorType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("Str"), strType); err != nil {
		return nil, err
	}
	// CPython: Modules/_testmultiphase.c:415 PyModule_AddIntConstant int_const 1969
	if err := d.SetItem(objects.NewStr("int_const"), objects.NewInt(1969)); err != nil {
		return nil, err
	}
	// CPython: Modules/_testmultiphase.c:419 PyModule_AddStringConstant str_const
	if err := d.SetItem(objects.NewStr("str_const"), objects.NewStr("something different")); err != nil {
		return nil, err
	}
	return m, nil
}
