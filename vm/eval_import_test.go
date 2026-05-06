// Tests for the IMPORT_NAME and IMPORT_FROM bytecode arms.
//
// CPython: Python/bytecodes.c IMPORT_NAME / IMPORT_FROM
package vm

import (
	"errors"
	"testing"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// importNameCode builds bytecode that executes:
//
//	RESUME 0
//	LOAD_CONST <level>   (TOS1)
//	LOAD_CONST <fromlist> (TOS)
//	IMPORT_NAME <nameIdx>
//	RETURN_VALUE
func importNameCode(nameIdx int, level int64, fromlistConst any, names []string, consts []any) *objects.Code {
	bc := instr(compile.RESUME, 0)
	bc = append(bc, instr(compile.LOAD_CONST, byte(len(consts)))...)
	consts = append(consts, level)
	bc = append(bc, instr(compile.LOAD_CONST, byte(len(consts)))...)
	consts = append(consts, fromlistConst)
	bc = append(bc, instr(compile.IMPORT_NAME, byte(nameIdx))...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	return &objects.Code{
		Code:      bc,
		Stacksize: 4,
		Consts:    consts,
		Names:     names,
	}
}

// TestImportNameBuiltin pins that IMPORT_NAME resolves a built-in module
// registered in the inittab.
func TestImportNameBuiltin(t *testing.T) {
	const modname = "_vm_import_name_test_builtin"
	imp.RemoveModule(modname)
	_ = imp.AppendInittab(modname, func() (*objects.Module, error) {
		return objects.NewModule(modname), nil
	})
	defer imp.RemoveModule(modname)

	co := importNameCode(0, 0, nil, []string{modname}, nil)
	globals := objects.NewDict()
	_ = globals.SetItem(objects.NewStr("__name__"), objects.NewStr("__main__"))

	ts := state.NewThread()
	got, err := EvalCode(ts, co, globals, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	mod, ok := got.(*objects.Module)
	if !ok {
		t.Fatalf("got %T, want *objects.Module", got)
	}
	nameObj, _ := mod.Dict().GetItem(objects.NewStr("__name__"))
	var gotName string
	if nameObj != nil {
		if tp := nameObj.Type(); tp.Str != nil {
			gotName, _ = tp.Str(nameObj)
		}
	}
	if gotName != modname {
		t.Errorf("module __name__ = %q, want %q", gotName, modname)
	}
}

// TestImportNameNotFound pins that IMPORT_NAME propagates ErrModuleNotFound
// when no finder knows the module.
func TestImportNameNotFound(t *testing.T) {
	const modname = "_vm_import_no_such_module_xyz"
	imp.RemoveModule(modname)

	co := importNameCode(0, 0, nil, []string{modname}, nil)
	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, imp.ErrModuleNotFound) {
		t.Errorf("error = %v, want wrapping imp.ErrModuleNotFound", err)
	}
}

// TestImportNameSysModulesCache pins that IMPORT_NAME returns a cached entry
// from sys.modules without re-initializing.
func TestImportNameSysModulesCache(t *testing.T) {
	const modname = "_vm_import_name_cached"
	sentinel := objects.NewModule(modname)
	imp.AddModule(modname, sentinel)
	defer imp.RemoveModule(modname)

	co := importNameCode(0, 0, nil, []string{modname}, nil)
	ts := state.NewThread()
	got, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	if got != sentinel {
		t.Error("IMPORT_NAME did not return cached sys.modules entry")
	}
}

// TestImportFromAttribute pins that IMPORT_FROM pushes both the module and
// the named attribute without removing the module from the stack.
func TestImportFromAttribute(t *testing.T) {
	const modname = "_vm_import_from_test"
	imp.RemoveModule(modname)
	mod := objects.NewModule(modname)
	_ = mod.Dict().SetItem(objects.NewStr("answer"), objects.NewInt(42))
	imp.AddModule(modname, mod)
	defer imp.RemoveModule(modname)

	// Bytecode:
	//   RESUME 0
	//   LOAD_CONST 0 (level=0)
	//   LOAD_CONST 1 (fromlist=("answer",))
	//   IMPORT_NAME 0  -> pushes module
	//   IMPORT_FROM 1  -> pushes attribute, leaves module below it
	//   RETURN_VALUE   -> returns attribute (TOS)
	bc := instr(compile.RESUME, 0)
	bc = append(bc, instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.IMPORT_NAME, 0)...)
	bc = append(bc, instr(compile.IMPORT_FROM, 1)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{
		Code:      bc,
		Stacksize: 8,
		Consts:    []any{int64(0), nil},
		Names:     []string{modname, "answer"},
	}

	ts := state.NewThread()
	got, err := EvalCode(ts, co, nil, nil)
	if err != nil {
		t.Fatalf("EvalCode: %v", err)
	}
	iv, ok := got.(*objects.Int)
	if !ok {
		t.Fatalf("got %T, want *objects.Int", got)
	}
	v, _ := iv.Int64()
	if v != 42 {
		t.Errorf("attribute value = %d, want 42", v)
	}
}

// TestImportFromMissingAttr pins that IMPORT_FROM returns an error when the
// attribute does not exist on the module.
func TestImportFromMissingAttr(t *testing.T) {
	const modname = "_vm_import_from_missing"
	imp.RemoveModule(modname)
	imp.AddModule(modname, objects.NewModule(modname))
	defer imp.RemoveModule(modname)

	bc := instr(compile.RESUME, 0)
	bc = append(bc, instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.IMPORT_NAME, 0)...)
	bc = append(bc, instr(compile.IMPORT_FROM, 1)...)
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{
		Code:      bc,
		Stacksize: 8,
		Consts:    []any{int64(0), nil},
		Names:     []string{modname, "no_such_attr"},
	}

	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing attribute, got nil")
	}
}

// TestImportNameNameIndexOOB pins that IMPORT_NAME with a bad name index
// returns an error rather than panicking.
func TestImportNameNameIndexOOB(t *testing.T) {
	bc := instr(compile.RESUME, 0)
	bc = append(bc, instr(compile.LOAD_CONST, 0)...)
	bc = append(bc, instr(compile.LOAD_CONST, 1)...)
	bc = append(bc, instr(compile.IMPORT_NAME, 5)...) // index 5 out of range
	bc = append(bc, instr(compile.RETURN_VALUE, 0)...)
	co := &objects.Code{
		Code:      bc,
		Stacksize: 4,
		Consts:    []any{int64(0), nil},
		Names:     []string{"only_one_name"},
	}
	ts := state.NewThread()
	_, err := EvalCode(ts, co, nil, nil)
	if err == nil {
		t.Fatal("expected error for out-of-range name index")
	}
}

// TestImportLevelHelper pins importLevel for nil, *objects.Int, and non-int.
func TestImportLevelHelper(t *testing.T) {
	if got := importLevel(nil); got != 0 {
		t.Errorf("importLevel(nil) = %d, want 0", got)
	}
	if got := importLevel(objects.NewStr("x")); got != 0 {
		t.Errorf("importLevel(str) = %d, want 0", got)
	}
	if got := importLevel(objects.NewInt(2)); got != 2 {
		t.Errorf("importLevel(2) = %d, want 2", got)
	}
	if got := importLevel(objects.NewInt(-1)); got != 0 {
		t.Errorf("importLevel(-1) = %d, want 0", got)
	}
}

// TestGlobalNameHelper pins globalName for nil, non-dict, and a dict with __name__.
func TestGlobalNameHelper(t *testing.T) {
	if got := globalName(nil); got != "" {
		t.Errorf("globalName(nil) = %q, want \"\"", got)
	}
	if got := globalName(objects.NewStr("x")); got != "" {
		t.Errorf("globalName(str) = %q, want \"\"", got)
	}
	d := objects.NewDict()
	_ = d.SetItem(objects.NewStr("__name__"), objects.NewStr("mypkg"))
	if got := globalName(d); got != "mypkg" {
		t.Errorf("globalName(dict) = %q, want \"mypkg\"", got)
	}
}
