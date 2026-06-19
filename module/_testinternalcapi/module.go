// Package testinternalcapi is the gopy port of CPython's
// Modules/_testinternalcapi.c. CPython exposes internal-API probes to the
// standard-library test suite through this extension; gopy ports the
// pieces the vendored Lib/test/ files reach for. Today that is the
// inline-values / split-keys dict introspection that test_class.py drives
// directly.
//
// CPython: Modules/_testinternalcapi.c:1
package testinternalcapi

import (
	"fmt"

	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/module/sys"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_testinternalcapi", buildModule)
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_testinternalcapi")
	d := m.Dict()

	fns := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"has_inline_values", hasInlineValues},
		{"has_split_table", hasSplitTable},
		{"get_static_builtin_types", getStaticBuiltinTypes},
		{"identify_type_slot_wrappers", identifyTypeSlotWrappers},
		{"get_recursion_depth", getRecursionDepth},
		{"run_in_subinterp_with_config", runInSubinterpWithConfig},
		{"clear_extension", clearExtension},
		{"dict_getitem_knownhash", dictGetitemKnownhash},
		{"get_code_var_counts", getCodeVarCounts},
		{"get_co_localskinds", getCoLocalskinds},
		{"code_returns_only_none", codeReturnsOnlyNone},
		{"verify_stateless_code", verifyStatelessCode},
	}
	for _, f := range fns {
		if err := d.SetItem(objects.NewStr(f.name), objects.NewBuiltinFunction(f.name, f.fn)); err != nil {
			return nil, err
		}
	}
	// module_exec publishes the adaptive-specialization warmup thresholds so
	// the test suite can drive an instruction past the point where the
	// interpreter quickens it. The values are the initial counter values plus
	// one (the counter counts down to zero before the quickened form kicks
	// in): SPECIALIZATION_THRESHOLD = ADAPTIVE_WARMUP_VALUE (1) + 1,
	// SPECIALIZATION_COOLDOWN = ADAPTIVE_COOLDOWN_VALUE (52) + 1,
	// TIER2_THRESHOLD = JUMP_BACKWARD_INITIAL_VALUE (4095) + 1.
	//
	// CPython: Modules/_testinternalcapi.c:2658 module_exec
	//          (Include/internal/pycore_code.h ADAPTIVE_WARMUP_VALUE,
	//           Include/internal/pycore_backoff.h JUMP_BACKWARD_INITIAL_VALUE)
	ints := []struct {
		name string
		val  int64
	}{
		{"TIER2_THRESHOLD", 4096},
		{"SPECIALIZATION_THRESHOLD", 2},
		{"SPECIALIZATION_COOLDOWN", 53},
	}
	for _, c := range ints {
		if err := d.SetItem(objects.NewStr(c.name), objects.NewInt(c.val)); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// runInSubinterpWithConfig ports run_in_subinterp_with_config(code, config,
// xi=False). CPython spins up a fresh PyInterpreterState configured by the
// PyInterpreterConfig the test built, runs the code with
// PyRun_SimpleStringFlags, tears the interpreter down, and returns that
// status. gopy compiles every extension into the runtime as a Go builtin
// (multi-phase by construction), so the config's isolation and
// check_multi_interp_extensions fields never reject an import: a faithful
// run is a fresh-namespace exec whose only observable output is the
// PyRun_SimpleString status code. The config object is accepted and
// ignored.
//
// CPython: Modules/_testinternalcapi.c:1816 run_in_subinterp_with_config
func runInSubinterpWithConfig(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: run_in_subinterp_with_config() missing required argument 'code' (pos 1)")
	}
	code, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: run_in_subinterp_with_config() argument 'code' must be str, not %s", args[0].Type().Name)
	}

	// gopy cannot spin up a real OS-level subinterpreter, so the run is a
	// fresh-namespace exec. The config's gil and check_multi_interp_extensions
	// fields are honoured: they are pushed onto the interpreter-state stack
	// the PEP 489 extension compat check (imp.CheckExtSubinterpCompat) reads,
	// so importing an incompatible extension from the script raises the same
	// ImportError CPython's subinterpreter would. own_gil follows
	// config.gil == 'own' (the ISOLATED gil=2 case).
	//
	// CPython: Python/pylifecycle.c:586 init_interp_create_gil (own_gil)
	ownGil, checkMulti := false, false
	if len(args) >= 2 && !objects.IsNone(args[1]) {
		config := args[1]
		if gilObj, err := objects.GetAttr(config, objects.NewStr("gil")); err == nil {
			if gilStr, ok := gilObj.(*objects.Unicode); ok {
				ownGil = gilStr.Value() == "own"
			}
		}
		if checkObj, err := objects.GetAttr(config, objects.NewStr("check_multi_interp_extensions")); err == nil {
			if t, terr := objects.IsTruthy(checkObj); terr == nil {
				checkMulti = t
			}
		}
	}

	imp.PushSubinterp(ownGil, checkMulti)
	defer imp.PopSubinterp()
	return objects.NewInt(int64(builtins.RunInFreshNamespace(code.Value()))), nil
}

// dictGetitemKnownhash ports dict_getitem_knownhash(mp, key, hash): it
// looks up key in mp with the caller-supplied hash. A non-dict first
// argument is a SystemError (the C code passes a non-dict to
// _PyDict_GetItem_KnownHash, which trips its assert/bad-internal-call);
// a missing key reports KeyError(key); a __eq__ that raises propagates.
//
// CPython: Modules/_testinternalcapi.c:1562 dict_getitem_knownhash
func dictGetitemKnownhash(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: dict_getitem_knownhash expected 3 arguments, got %d", len(args))
	}
	d, ok := args[0].(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("SystemError: bad argument to internal function")
	}
	h, err := objects.NumberIndex(args[2])
	if err != nil {
		return nil, err
	}
	hi, ok := h.(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("SystemError: bad argument to internal function")
	}
	hv, _ := hi.Int64()
	return d.GetItemKnownHashOrKeyError(args[1], hv)
}

// clearExtension ports clear_extension(name, filename): it clears all
// internally cached data for a single-phase extension module so the test
// suite can re-import it fresh. It delegates to _PyImport_ClearExtension.
//
// CPython: Modules/_testinternalcapi.c:893 clear_extension
//
//	(Python/import.c:903 _PyImport_ClearExtension)
func clearExtension(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: clear_extension() takes exactly 2 arguments (%d given)", len(args))
	}
	name, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: clear_extension() argument 1 must be str, not %s", args[0].Type().Name)
	}
	path := ""
	if !objects.IsNone(args[1]) {
		filename, ok := args[1].(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: clear_extension() argument 2 must be str, not %s", args[1].Type().Name)
		}
		path = filename.Value()
	}
	if err := imp.ClearExtension(name.Value(), path); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// getRecursionDepth returns the Python recursion depth of the caller,
// matching tstate->py_recursion_limit - tstate->py_recursion_remaining.
// gopy tracks depth by the active interpreter-frame chain, so the count
// of frames from the caller back to the root is the same quantity. The
// C probe pushes no Python frame, so the caller's frame is the base.
//
// CPython: Modules/_testinternalcapi.c:110 get_recursion_depth
func getRecursionDepth(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if sys.CurrentInterpreterFrameHook == nil {
		return objects.NewInt(0), nil
	}
	depth := int64(0)
	for f := sys.CurrentInterpreterFrameHook(); f != nil; f = f.FrameBack() {
		depth++
	}
	return objects.NewInt(depth), nil
}

// hasInlineValues reports whether obj currently keeps its attributes in
// the type's inline-values array. It mirrors the C probe: the owning type
// must carry Py_TPFLAGS_INLINE_VALUES and the instance's value array must
// still be valid (a __dict__ deletion or an attribute spill flips it).
//
// CPython: Modules/_testinternalcapi.c:2270 has_inline_values
func hasInlineValues(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: has_inline_values() takes exactly one argument (%d given)", len(args))
	}
	obj := args[0]
	inst, ok := obj.(*objects.Instance)
	if ok && obj.Type().HasInlineValues() && inst.InlineValid() {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// getStaticBuiltinTypes returns gopy's static builtin type objects, the
// same role _PyStaticType_GetBuiltins fills: the types created at
// interpreter startup and shared across (sub)interpreters. test.support's
// iter_builtin_types yields from this list to sweep slot-wrapper
// inheritance across the static type set.
//
// CPython: Modules/_testinternalcapi.c:2334 get_static_builtin_types
//
//	(Objects/typeobject.c _PyStaticType_GetBuiltins)
func getStaticBuiltinTypes(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	types := []*objects.Type{
		objects.ObjectType(), objects.TypeType(),
		objects.IntType, objects.BoolType, objects.FloatType, objects.ComplexType,
		objects.StrType(), objects.BytesType, objects.ByteArrayType,
		objects.ListType, objects.TupleType, objects.RangeType,
		objects.DictType, objects.SetType, objects.SliceType,
	}
	items := make([]objects.Object, len(types))
	for i, t := range types {
		items[i] = t
	}
	return objects.NewList(items), nil
}

// slotWrapperNames is every dunder backed by a tp-slot in CPython's
// slotdefs table, in slotdefs order. _PyType_GetSlotWrapperNames returns
// exactly this list; test.support.identify_type_slot_wrappers de-dupes it
// and iter_slot_wrappers walks it per type, keeping only the names that
// resolve to a wrapper_descriptor on that type.
//
// CPython: Objects/typeobject.c:11494 _PyType_GetSlotWrapperNames
//
//	(Objects/typeobject.c:10952 slotdefs)
var slotWrapperNames = []string{
	"__getattribute__", "__getattr__", "__setattr__", "__delattr__",
	"__repr__", "__hash__", "__call__", "__str__",
	"__lt__", "__le__", "__eq__", "__ne__", "__gt__", "__ge__",
	"__iter__", "__next__", "__get__", "__set__", "__delete__",
	"__init__", "__new__", "__del__", "__await__", "__aiter__", "__anext__",
	"__add__", "__radd__", "__sub__", "__rsub__", "__mul__", "__rmul__",
	"__mod__", "__rmod__", "__divmod__", "__rdivmod__", "__pow__", "__rpow__",
	"__neg__", "__pos__", "__abs__", "__bool__", "__invert__",
	"__lshift__", "__rlshift__", "__rshift__", "__rrshift__",
	"__and__", "__rand__", "__xor__", "__rxor__", "__or__", "__ror__",
	"__int__", "__float__",
	"__iadd__", "__isub__", "__imul__", "__imod__", "__ipow__",
	"__ilshift__", "__irshift__", "__iand__", "__ixor__", "__ior__",
	"__floordiv__", "__rfloordiv__", "__truediv__", "__rtruediv__",
	"__ifloordiv__", "__itruediv__", "__index__",
	"__matmul__", "__rmatmul__", "__imatmul__",
	"__len__", "__getitem__", "__setitem__", "__delitem__", "__contains__",
}

// identifyTypeSlotWrappers returns the slot-wrapper dunder names from the
// slotdefs table.
//
// CPython: Modules/_testinternalcapi.c:2341 identify_type_slot_wrappers
//
//	(Objects/typeobject.c:11494 _PyType_GetSlotWrapperNames)
func identifyTypeSlotWrappers(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	items := make([]objects.Object, len(slotWrapperNames))
	for i, n := range slotWrapperNames {
		items[i] = objects.NewStr(n)
	}
	return objects.NewList(items), nil
}

// hasSplitTable reports whether obj's __dict__ shares its keys table with
// the type (the split-keys shape).
//
// CPython: Modules/_testinternalcapi.c:2280 has_split_table
func hasSplitTable(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: has_split_table() takes exactly one argument (%d given)", len(args))
	}
	inst, ok := args[0].(*objects.Instance)
	if !ok {
		return objects.False(), nil
	}
	dict := inst.Dict()
	if dict == nil {
		return objects.False(), nil
	}
	if dict.IsSplit() {
		return objects.True(), nil
	}
	return objects.False(), nil
}
