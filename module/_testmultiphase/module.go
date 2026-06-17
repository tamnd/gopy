// Package testmultiphase is the gopy port of CPython's
// Modules/_testmultiphase.c, the C extension that exercises multi-phase
// initialization of extension modules (PEP 489). The standard-library
// test suite reaches for it indirectly: test.test_importlib.util runs
// import_helper.import_module("_testmultiphase") at import time, so any
// test that pulls in that helper (test_pkgutil, test_pyclbr, the
// test_importlib extension suites) raises SkipTest when the module is
// absent.
//
// gopy cannot dlopen the compiled extension, so every PyInit_* entry the C
// extension exposes is reproduced as a Go-native ExtModuleDef carrying the
// PEP 489 create/exec slot table its PyModuleDef declares. _imp.create_dynamic
// runs the create step (PyModule_FromDefAndSpec2) and _imp.exec_dynamic runs
// the exec slots (PyModule_ExecDef). The test loads the many variants through
// an explicit ExtensionFileLoader bound to the one _testmultiphase origin, the
// gopy analog of the single .so exposing many PyInit symbols, so the variants
// are registered as non-discoverable (Variant) defs.
//
// CPython: Modules/_testmultiphase.c:447 PyInit__testmultiphase
// CPython: Modules/_testmultiphase.c:381 execfunc
package testmultiphase

import (
	"fmt"
	"sync"

	pyerrors "github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// gilNotUsed is Py_MOD_GIL_NOT_USED, the value the test extension's defs put
// in their Py_mod_gil slot. gopy has a single GIL-free runtime, so the value
// only needs to round-trip through the slot scan.
//
// CPython: Include/moduleobject.h Py_MOD_GIL_NOT_USED
const gilNotUsed = 0

func init() {
	// The four "real" modules each have their own PyInit symbol and are
	// imported by name (test_import's SubinterpImportTests and the helper that
	// loads _testmultiphase), so they are materialized as discoverable stubs.
	//
	// CPython: Modules/_testmultiphase.c:438 main_slots (PER_INTERPRETER_GIL)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_testmultiphase",
		MultiPhase:         true,
		Doc:                "Test module _testmultiphase",
		Methods:            mainMethods(),
		Slots:              mainSlots(imp.MultiInterpPerInterpreterGIL, true),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
	})
	// CPython: Modules/_testmultiphase.c:940 non_isolated_slots (NOT_SUPPORTED)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_test_non_isolated",
		MultiPhase:         true,
		Doc:                "Test module _test_non_isolated",
		Methods:            mainMethods(),
		Slots:              mainSlots(imp.MultiInterpNotSupported, true),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpNotSupported,
	})
	// CPython: Modules/_testmultiphase.c:958 shared_gil_only_slots (SUPPORTED, explicit)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:               "_test_shared_gil_only",
		MultiPhase:         true,
		Doc:                "Test module _test_shared_gil_only",
		Methods:            mainMethods(),
		Slots:              mainSlots(imp.MultiInterpSupported, true),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpSupported,
	})
	// CPython: Modules/_testmultiphase.c:980 no_multiple_interpreter_slot_slots (no slot)
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "_test_no_multiple_interpreter_slot",
		MultiPhase: true,
		Doc:        "Test module _test_no_multiple_interpreter_slot",
		Methods:    mainMethods(),
		Slots:      mainSlots(0, false),
	})

	registerVariants()
}

// mainSlots builds the slot table the main-like defs share: an exec slot
// running execfunc, an optional multiple-interpreters slot, and a gil slot.
//
// CPython: Modules/_testmultiphase.c:438 main_slots
func mainSlots(multiInterp int, hasMultiInterp bool) []imp.ExtSlot {
	slots := []imp.ExtSlot{{ID: imp.ExtSlotExec, Exec: execMain}}
	if hasMultiInterp {
		slots = append(slots, imp.ExtSlot{ID: imp.ExtSlotMultipleInterpreters, Value: multiInterp})
	}
	slots = append(slots, imp.ExtSlot{ID: imp.ExtSlotGIL, Value: gilNotUsed})
	return slots
}

// mainMethods is testexport_methods: foo and call_state_registration_func.
//
// CPython: Modules/_testmultiphase.c:374 testexport_methods
func mainMethods() []imp.ExtMethod {
	return []imp.ExtMethod{
		{Name: "foo", Fn: testexportFoo},
		{Name: "call_state_registration_func", Fn: callStateRegistrationFunc},
	}
}

// registerVariants registers every test-only PyInit variant as a
// non-discoverable Variant def, reached only through an explicit
// ExtensionFileLoader against the _testmultiphase origin.
func registerVariants() {
	// PyInit_x and pkg._testmultiphase both return main_def: a normal
	// multi-phase module whose __name__ is the spec name (so 'x' /
	// 'pkg._testmultiphase'), exercising short and dotted names.
	//
	// CPython: Modules/_testmultiphase.c:577 PyInit_x
	for _, name := range []string{"x", "pkg._testmultiphase"} {
		imp.RegisterExtModule(&imp.ExtModuleDef{
			Name:               name,
			MultiPhase:         true,
			Variant:            true,
			Doc:                "Test module main",
			Methods:            mainMethods(),
			Slots:              mainSlots(imp.MultiInterpPerInterpreterGIL, true),
			HasMultiInterpSlot: true,
			MultiInterp:        imp.MultiInterpPerInterpreterGIL,
		})
	}

	// Non-ASCII-named modules: only a gil slot (no exec, no create), so the
	// loader builds a bare module and sets __name__ (spec name) and __doc__.
	//
	// CPython: Modules/_testmultiphase.c:527 nonascii_slots / def_nonascii_*
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "_testmultiphase_zkouška_načtení",
		MultiPhase: true,
		Variant:    true,
		Doc:        "Module named in Czech",
		Slots:      []imp.ExtSlot{{ID: imp.ExtSlotGIL, Value: gilNotUsed}},
	})
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "＿インポートテスト",
		MultiPhase: true,
		Variant:    true,
		Doc:        "Module named in Japanese",
		Slots:      []imp.ExtSlot{{ID: imp.ExtSlotGIL, Value: gilNotUsed}},
	})

	// Non-module create results.
	//
	// CPython: Modules/_testmultiphase.c:482 slots_create_nonmodule
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "_testmultiphase_nonmodule",
		MultiPhase: true,
		Variant:    true,
		Doc:        "Test module _testmultiphase_nonmodule",
		Slots:      []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createNonmodule}},
	})
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "_testmultiphase_nonmodule_with_methods",
		MultiPhase: true,
		Variant:    true,
		Doc:        "Test module _testmultiphase_nonmodule_with_methods",
		Methods:    []imp.ExtMethod{{Name: "bar", Fn: nonmoduleBar}},
		Slots:      []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createNonmodule}},
	})

	// NULL (empty) slot table.
	//
	// CPython: Modules/_testmultiphase.c:583 null_slots_def
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name:       "_testmultiphase_null_slots",
		MultiPhase: true,
		Variant:    true,
		Doc:        "Test module _testmultiphase_null_slots",
	})

	registerBadVariants()
}

// registerBadVariants registers the misbehaving defs test_bad_modules drives,
// each of which must raise SystemError (chained for the unreported-exception
// cases).
//
// CPython: Modules/_testmultiphase.c:595 "Problematic modules"
func registerBadVariants() {
	// bad_slot_large: a slot ID one past _Py_mod_LAST_SLOT.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_bad_slot_large", MultiPhase: true, Variant: true,
		Doc:   "Test module _testmultiphase_bad_slot_large",
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotLast + 1}},
	})
	// bad_slot_negative: a negative slot ID.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_bad_slot_negative", MultiPhase: true, Variant: true,
		Doc:   "Test module _testmultiphase_bad_slot_negative",
		Slots: []imp.ExtSlot{{ID: -1}},
	})
	// create_int_with_state: a non-module create result with m_size > 0.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_create_int_with_state", MultiPhase: true, Variant: true,
		Doc:   "Not a PyModuleObject object, but requests per-module state",
		MSize: 10,
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createNonmodule}},
	})
	// negative_size: m_size < 0 is illegal for multi-phase init.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_negative_size", MultiPhase: true, Variant: true,
		Doc:   "PyModuleDef with negative m_size",
		MSize: -1,
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createNonmodule}},
	})
	// export_null: PyInit returned NULL without setting an exception.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_export_null", MultiPhase: true, Variant: true,
		InitKind: imp.ExtInitReturnedNil,
	})
	// export_uninitialized: PyInit returned a def that skipped PyModuleDef_Init.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_export_uninitialized", MultiPhase: true, Variant: true,
		InitKind: imp.ExtInitUninitialized,
	})
	// export_raise: PyInit set SystemError and returned NULL.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_export_raise", MultiPhase: true, Variant: true,
		InitKind:   imp.ExtInitReturnedNil,
		InitRaised: func() *pyerrors.Exception { return newSystemError("bad export function") },
	})
	// export_unreported_exception: PyInit returned a real def but left an
	// exception set; the loader chains a SystemError onto it.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_export_unreported_exception", MultiPhase: true, Variant: true,
		InitKind:   imp.ExtInitNormal,
		InitRaised: func() *pyerrors.Exception { return newSystemError("bad export function") },
	})
	// create_null: create slot returned NULL without setting an exception.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_create_null", MultiPhase: true, Variant: true,
		Doc:   "Test module _testmultiphase_create_null",
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createNull}},
	})
	// create_raise: create slot set SystemError and returned NULL.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_create_raise", MultiPhase: true, Variant: true,
		Doc:   "Test module _testmultiphase_create_null",
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createRaise}},
	})
	// create_unreported_exception: create slot returned a module but left an
	// exception set; the loader chains a SystemError onto it.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_create_unreported_exception", MultiPhase: true, Variant: true,
		Doc:   "Test module _testmultiphase_create_unreported_exception",
		Slots: []imp.ExtSlot{{ID: imp.ExtSlotCreate, Create: createUnreported}},
	})
	// nonmodule_with_exec_slots: a non-module create result paired with exec slots.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_nonmodule_with_exec_slots", MultiPhase: true, Variant: true,
		Doc: "Test module _testmultiphase_nonmodule_with_exec_slots",
		Slots: []imp.ExtSlot{
			{ID: imp.ExtSlotCreate, Create: createNonmodule},
			{ID: imp.ExtSlotExec, Exec: execMain},
			{ID: imp.ExtSlotMultipleInterpreters, Value: imp.MultiInterpPerInterpreterGIL},
			{ID: imp.ExtSlotGIL, Value: gilNotUsed},
		},
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
	})
	// exec_err: exec slot returned -1 without setting an exception.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_exec_err", MultiPhase: true, Variant: true,
		Doc:                "Test module _testmultiphase_exec_err",
		Slots:              execVariantSlots(execErr),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
	})
	// exec_raise: exec slot set SystemError and returned -1.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_exec_raise", MultiPhase: true, Variant: true,
		Doc:                "Test module _testmultiphase_exec_raise",
		Slots:              execVariantSlots(execRaise),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
	})
	// exec_unreported_exception: exec slot returned 0 but left an exception set.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_exec_unreported_exception", MultiPhase: true, Variant: true,
		Doc:                "Test module _testmultiphase_exec_unreported_exception",
		Slots:              execVariantSlots(execUnreported),
		HasMultiInterpSlot: true,
		MultiInterp:        imp.MultiInterpPerInterpreterGIL,
	})
	// multiple_create_slots: two Py_mod_create slots.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_multiple_create_slots", MultiPhase: true, Variant: true,
		Doc: "Test module _testmultiphase_multiple_create_slots",
		Slots: []imp.ExtSlot{
			{ID: imp.ExtSlotCreate, Create: createNoop},
			{ID: imp.ExtSlotCreate, Create: createNoop},
		},
	})
	// multiple_multiple_interpreters_slots: two Py_mod_multiple_interpreters slots.
	imp.RegisterExtModule(&imp.ExtModuleDef{
		Name: "_testmultiphase_multiple_multiple_interpreters_slots", MultiPhase: true, Variant: true,
		Doc: "Test module _testmultiphase_multiple_multiple_interpreters_slots",
		Slots: []imp.ExtSlot{
			{ID: imp.ExtSlotMultipleInterpreters, Value: imp.MultiInterpPerInterpreterGIL},
			{ID: imp.ExtSlotMultipleInterpreters, Value: imp.MultiInterpPerInterpreterGIL},
			{ID: imp.ExtSlotGIL, Value: gilNotUsed},
		},
	})
}

// execVariantSlots builds the slot table the exec_* variants share: one exec
// slot plus multiple-interpreters and gil slots.
func execVariantSlots(exec func(objects.Object) (int, *pyerrors.Exception)) []imp.ExtSlot {
	return []imp.ExtSlot{
		{ID: imp.ExtSlotExec, Exec: exec},
		{ID: imp.ExtSlotMultipleInterpreters, Value: imp.MultiInterpPerInterpreterGIL},
		{ID: imp.ExtSlotGIL, Value: gilNotUsed},
	}
}

// newSystemError builds (without raising) a SystemError(msg) instance, the
// gopy analog of PyErr_SetString(PyExc_SystemError, msg) leaving an exception
// "set" before a slot returns.
func newSystemError(msg string) *pyerrors.Exception {
	return pyerrors.New(pyerrors.PyExc_SystemError, objects.NewTuple([]objects.Object{objects.NewStr(msg)}))
}

// createNonmodule ports createfunc_nonmodule: a SimpleNamespace(three=3).
//
// CPython: Modules/_testmultiphase.c:461 createfunc_nonmodule
func createNonmodule() (objects.Object, *pyerrors.Exception) {
	ns := objects.NewNamespace()
	_ = ns.Dict().SetItem(objects.NewStr("three"), objects.NewInt(3))
	return ns, nil
}

// createNoop ports createfunc_noop: PyModule_New("spam").
//
// CPython: Modules/_testmultiphase.c:683 createfunc_noop
func createNoop() (objects.Object, *pyerrors.Exception) {
	return objects.NewModule("spam"), nil
}

// createNull ports createfunc_null: returns NULL with no exception set.
//
// CPython: Modules/_testmultiphase.c:710 createfunc_null
func createNull() (objects.Object, *pyerrors.Exception) {
	return nil, nil
}

// createRaise ports createfunc_raise: sets SystemError and returns NULL.
//
// CPython: Modules/_testmultiphase.c:726 createfunc_raise
func createRaise() (objects.Object, *pyerrors.Exception) {
	return nil, newSystemError("bad create function")
}

// createUnreported ports createfunc_unreported_exception: sets SystemError but
// returns a module, so the loader chains a SystemError onto the leftover one.
//
// CPython: Modules/_testmultiphase.c:746 createfunc_unreported_exception
func createUnreported() (objects.Object, *pyerrors.Exception) {
	return objects.NewModule("foo"), newSystemError("bad create function")
}

// execErr ports execfunc_err: returns -1 without setting an exception.
//
// CPython: Modules/_testmultiphase.c:786 execfunc_err
func execErr(objects.Object) (int, *pyerrors.Exception) {
	return -1, nil
}

// execRaise ports execfunc_raise: sets SystemError and returns -1.
//
// CPython: Modules/_testmultiphase.c:805 execfunc_raise
func execRaise(objects.Object) (int, *pyerrors.Exception) {
	return -1, newSystemError("bad exec function")
}

// execUnreported ports execfunc_unreported_exception: sets SystemError but
// returns 0, so the loader chains a SystemError onto the leftover one.
//
// CPython: Modules/_testmultiphase.c:826 execfunc_unreported_exception
func execUnreported(objects.Object) (int, *pyerrors.Exception) {
	return 0, newSystemError("bad exec function")
}

// nonmoduleBar ports nonmodule_bar: bar(i, j) returns i - j.
//
// CPython: Modules/_testmultiphase.c:501 nonmodule_bar
func nonmoduleBar(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: bar() takes exactly 2 arguments (%d given)", len(args))
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
	return objects.NewInt(iv - jv), nil
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
// CPython: Modules/_testmultiphase.c:360 Str_Type_spec
// CPython: Modules/_testmultiphase.c:388 PyErr_NewException("_testimportexec.error")
var (
	exampleType    *objects.Type
	strType        *objects.Type
	errorType      *objects.Type
	buildTypesOnce sync.Once
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
// CPython: Modules/_testmultiphase.c:309 testexport_foo
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

// callStateRegistrationFunc ports call_state_registration_func. gopy has no
// per-module C state registry: PyState_FindModule has nothing to find (case 0
// returns None) and PyState_AddModule / PyState_RemoveModule fail with a
// SystemError on a multi-phase module, exactly the behaviour the test asserts.
//
// CPython: Modules/_testmultiphase.c:328 call_state_registration_func
func callStateRegistrationFunc(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: call_state_registration_func() takes exactly 1 argument (%d given)", len(args))
	}
	iv, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required (got type %s)", args[0].Type().Name)
	}
	switch n, _ := iv.Int64(); n {
	case 0:
		// PyState_FindModule: nothing registered, so None.
		return objects.None(), nil
	case 1:
		// PyState_AddModule on a multi-phase module fails.
		return nil, objects.NewRaisedError(newSystemError("PyState_AddModule failed"), "SystemError: PyState_AddModule failed")
	case 2:
		// PyState_RemoveModule on a multi-phase module fails.
		return nil, objects.NewRaisedError(newSystemError("PyState_RemoveModule failed"), "SystemError: PyState_RemoveModule failed")
	}
	return objects.None(), nil
}

// buildTypes constructs the Example / Str / error types. It runs lazily at
// module-exec time rather than at Go package init: str's tp_new is wired by
// the builtins package during interpreter startup, and a str subclass built
// before that wiring inherits a nil tp_new (so Str(1) would allocate a plain
// instance instead of a real str). Deferring to exec guarantees str is ready.
func buildTypes() {
	// CPython: Modules/_testmultiphase.c:114 Example_Type_slots
	exampleType = objects.NewType("Example", []*objects.Type{objects.ObjectType()})
	exampleType.Module = "_testimportexec"
	exampleType.TpFlags |= objects.TpFlagHaveGC
	exampleType.TpNew = exampleNew
	exampleType.Getattro = exampleGetattro
	exampleType.Setattro = exampleSetattro
	exampleType.TpTraverse = exampleTraverse
	objects.SetTypeDescr(exampleType, "demo", objects.NewMethodDescr(exampleType, "demo", exampleDemo))

	// Str is a str subclass (Py_tp_base = &PyUnicode_Type). Building it through
	// NewUserType is the faithful PyType_FromSpec analog: it inherits str's
	// tp_new, so Str() and Str(1) construct real str-subclass instances.
	//
	// CPython: Modules/_testmultiphase.c:360 Str_Type_spec (BASETYPE)
	strType = objects.NewUserType("Str", []*objects.Type{objects.StrType()}, objects.NewDict())
	strType.Module = "_testimportexec"
	strType.TpFlags |= objects.TpFlagBasetype

	// CPython: Modules/_testmultiphase.c:388 PyErr_NewException
	errorType = pyerrors.NewExcType("error", []*objects.Type{pyerrors.PyExc_Exception})
	errorType.Module = "_testimportexec"
}

// execMain ports execfunc: the exec slot that installs the Example / error /
// Str types and the int_const / str_const constants on the module.
//
// CPython: Modules/_testmultiphase.c:381 execfunc
func execMain(m objects.Object) (int, *pyerrors.Exception) {
	mod, ok := m.(*objects.Module)
	if !ok {
		return -1, newSystemError("execfunc: not a module")
	}
	buildTypesOnce.Do(buildTypes)
	d := mod.Dict()
	// CPython: Modules/_testmultiphase.c:393 PyModule_Add "Example"
	if err := d.SetItem(objects.NewStr("Example"), exampleType); err != nil {
		return -1, newSystemError(err.Error())
	}
	// CPython: Modules/_testmultiphase.c:399 PyModule_Add "error"
	if err := d.SetItem(objects.NewStr("error"), errorType); err != nil {
		return -1, newSystemError(err.Error())
	}
	// CPython: Modules/_testmultiphase.c:405 PyModule_Add "Str"
	if err := d.SetItem(objects.NewStr("Str"), strType); err != nil {
		return -1, newSystemError(err.Error())
	}
	// CPython: Modules/_testmultiphase.c:409 PyModule_AddIntConstant int_const 1969
	if err := d.SetItem(objects.NewStr("int_const"), objects.NewInt(1969)); err != nil {
		return -1, newSystemError(err.Error())
	}
	// CPython: Modules/_testmultiphase.c:413 PyModule_AddStringConstant str_const
	if err := d.SetItem(objects.NewStr("str_const"), objects.NewStr("something different")); err != nil {
		return -1, newSystemError(err.Error())
	}
	return 0, nil
}
