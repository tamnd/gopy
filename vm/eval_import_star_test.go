// Pins `from x import *`. CALL_INTRINSIC_1 with UnaryImportStarID
// pops the source module, calls importStar, and pushes None. The
// importStar helper has two paths: __all__ (no underscore filter) and
// fallback to __dict__ (underscore filter on). Both are exercised
// here against a hand-built Module.
//
// CPython: Python/intrinsics.c:124 import_star
// CPython: Python/intrinsics.c:40 import_all_from

package vm

import (
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/intrinsics"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

func runImportStar(t *testing.T, mod *objects.Module) *objects.Dict {
	t.Helper()
	dst := objects.NewDict()
	co := &objects.Code{
		Code: concat(
			instr(compile.LOAD_CONST, 0),
			instr(compile.CALL_INTRINSIC_1, byte(intrinsics.UnaryImportStarID)),
			instr(compile.RETURN_VALUE, 0),
		),
		Consts:    []any{mod},
		Stacksize: 4,
	}
	ts := state.NewThread()
	if _, err := EvalCode(ts, co, dst, nil); err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	return dst
}

// TestEvalImportStarUsesAll: when __all__ is defined the helper copies
// every named attribute regardless of leading underscore. Pins the
// "honor __all__" rule from CPython's import_all_from.
func TestEvalImportStarUsesAll(t *testing.T) {
	mod := objects.NewModule("src")
	d := mod.Dict()
	_ = d.SetItem(objects.NewStr("foo"), objects.NewInt(1))
	_ = d.SetItem(objects.NewStr("_bar"), objects.NewInt(2))
	_ = d.SetItem(objects.NewStr("baz"), objects.NewInt(3))
	_ = d.SetItem(objects.NewStr("__all__"), objects.NewList([]objects.Object{
		objects.NewStr("foo"),
		objects.NewStr("_bar"),
	}))

	dst := runImportStar(t, mod)

	for _, name := range []string{"foo", "_bar"} {
		v, err := dst.GetItem(objects.NewStr(name))
		if err != nil || v == nil {
			t.Errorf("dst[%q] missing after import-star (__all__): %v", name, err)
		}
	}
	if v, err := dst.GetItem(objects.NewStr("baz")); err == nil && v != nil {
		t.Errorf("dst[baz] should not be set when __all__ excludes it; got %v", v)
	}
}

// TestEvalImportStarFallsBackToDict: when __all__ is absent the helper
// iterates the module's __dict__ keys and skips names that start with
// "_". Pins the dunder-skip behaviour of CPython's import_all_from.
func TestEvalImportStarFallsBackToDict(t *testing.T) {
	mod := objects.NewModule("src")
	d := mod.Dict()
	_ = d.SetItem(objects.NewStr("foo"), objects.NewInt(1))
	_ = d.SetItem(objects.NewStr("_bar"), objects.NewInt(2))
	_ = d.SetItem(objects.NewStr("baz"), objects.NewInt(3))
	// importStar fetches __dict__ via GetAttr; module's getattr looks
	// the literal key up in the same dict, so we route it back to d.
	_ = d.SetItem(objects.NewStr("__dict__"), d)

	dst := runImportStar(t, mod)

	for _, name := range []string{"foo", "baz"} {
		v, err := dst.GetItem(objects.NewStr(name))
		if err != nil || v == nil {
			t.Errorf("dst[%q] missing after import-star (__dict__): %v", name, err)
		}
	}
	for _, name := range []string{"_bar", "__name__", "__dict__"} {
		v, err := dst.GetItem(objects.NewStr(name))
		if err == nil && v != nil {
			t.Errorf("dst[%q] should be skipped (underscore filter); got %v", name, v)
		}
	}
}
