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
