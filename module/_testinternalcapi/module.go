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
