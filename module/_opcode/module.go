// Package _opcode is the gopy port of CPython's Modules/_opcode.c.
// It backs Lib/opcode.py with the C-level predicates over the opcode
// metadata table plus a few constant lists (intrinsic names, special
// method names, binary-op symbols).
//
// CPython: Modules/_opcode.c
package _opcode

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_opcode", buildModule)
}

// CPython: Include/internal/pycore_intrinsics.h:9 INTRINSIC_*.
var intrinsic1Names = []string{
	"INTRINSIC_1_INVALID",
	"INTRINSIC_PRINT",
	"INTRINSIC_IMPORT_STAR",
	"INTRINSIC_STOPITERATION_ERROR",
	"INTRINSIC_ASYNC_GEN_WRAP",
	"INTRINSIC_UNARY_POSITIVE",
	"INTRINSIC_LIST_TO_TUPLE",
	"INTRINSIC_TYPEVAR",
	"INTRINSIC_PARAMSPEC",
	"INTRINSIC_TYPEVARTUPLE",
	"INTRINSIC_SUBSCRIPT_GENERIC",
	"INTRINSIC_TYPEALIAS",
}

// CPython: Include/internal/pycore_intrinsics.h:26 INTRINSIC_2_*.
var intrinsic2Names = []string{
	"INTRINSIC_2_INVALID",
	"INTRINSIC_PREP_RERAISE_STAR",
	"INTRINSIC_TYPEVAR_WITH_BOUND",
	"INTRINSIC_TYPEVAR_WITH_CONSTRAINTS",
	"INTRINSIC_SET_FUNCTION_TYPE_PARAMS",
	"INTRINSIC_SET_TYPEPARAM_DEFAULT",
}

// CPython: Include/internal/pycore_ceval.h:380 SPECIAL___ENTER__ etc.
var specialMethodNames = []string{
	"__enter__",
	"__exit__",
	"__aenter__",
	"__aexit__",
}

// CPython: Python/_opcode.c:251 ADD_NB_OP table.
var nbOps = [][2]string{
	{"NB_ADD", "+"},
	{"NB_AND", "&"},
	{"NB_FLOOR_DIVIDE", "//"},
	{"NB_LSHIFT", "<<"},
	{"NB_MATRIX_MULTIPLY", "@"},
	{"NB_MULTIPLY", "*"},
	{"NB_REMAINDER", "%"},
	{"NB_OR", "|"},
	{"NB_POWER", "**"},
	{"NB_RSHIFT", ">>"},
	{"NB_SUBTRACT", "-"},
	{"NB_TRUE_DIVIDE", "/"},
	{"NB_XOR", "^"},
	{"NB_INPLACE_ADD", "+="},
	{"NB_INPLACE_AND", "&="},
	{"NB_INPLACE_FLOOR_DIVIDE", "//="},
	{"NB_INPLACE_LSHIFT", "<<="},
	{"NB_INPLACE_MATRIX_MULTIPLY", "@="},
	{"NB_INPLACE_MULTIPLY", "*="},
	{"NB_INPLACE_REMAINDER", "%="},
	{"NB_INPLACE_OR", "|="},
	{"NB_INPLACE_POWER", "**="},
	{"NB_INPLACE_RSHIFT", ">>="},
	{"NB_INPLACE_SUBTRACT", "-="},
	{"NB_INPLACE_TRUE_DIVIDE", "/="},
	{"NB_INPLACE_XOR", "^="},
	{"NB_SUBSCR", "[]"},
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_opcode")
	d := m.Dict()

	entries := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"stack_effect", stackEffect},
		{"is_valid", isValid},
		{"has_arg", hasArg},
		{"has_const", hasConst},
		{"has_name", hasName},
		{"has_jump", hasJump},
		{"has_free", hasFree},
		{"has_local", hasLocal},
		{"has_exc", hasExc},
		{"get_specialization_stats", getSpecializationStats},
		{"get_nb_ops", getNBOps},
		{"get_intrinsic1_descs", getIntrinsic1Descs},
		{"get_intrinsic2_descs", getIntrinsic2Descs},
		{"get_special_method_names", getSpecialMethodNames},
		{"get_executor", getExecutor},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), objects.NewBuiltinFunction(e.name, e.fn)); err != nil {
			return nil, err
		}
	}
	if err := d.SetItem(objects.NewStr("ENABLE_SPECIALIZATION"), objects.NewInt(0)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("ENABLE_SPECIALIZATION_FT"), objects.NewInt(0)); err != nil {
		return nil, err
	}
	return m, nil
}

func opcodeArg(name string, args []objects.Object, kwargs map[string]objects.Object) (int, error) {
	if len(kwargs) != 0 {
		return 0, fmt.Errorf("TypeError: %s() takes no keyword arguments", name)
	}
	if len(args) < 1 {
		return 0, fmt.Errorf("TypeError: %s() expected at least 1 argument, got %d", name, len(args))
	}
	i, ok := args[0].(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: %s() argument must be int", name)
	}
	v, ok := i.Int64()
	if !ok {
		return 0, fmt.Errorf("OverflowError: opcode value too large")
	}
	return int(v), nil
}

// isValidOpcode mirrors IS_VALID_OPCODE: the opcode lies inside the
// metadata range and has at least one flag set (filtering out unused
// slots in the flag table).
//
// CPython: Include/internal/pycore_opcode_metadata.h IS_VALID_OPCODE
func isValidOpcode(op int) bool {
	if op < 0 || op > 266 {
		return false
	}
	return compile.Opcode(op).Name() != ""
}

// stackEffect implements stack_effect(opcode, oparg=None, *, jump=None),
// returning the net operand-stack effect of opcode. jump selects the
// branch column for jumping/exception opcodes (None/True/False).
//
// CPython: Modules/_opcode.c:37 _opcode_stack_effect_impl
func stackEffect(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: stack_effect() takes 1 or 2 positional arguments")
	}
	opObj, ok := args[0].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: stack_effect() argument 'opcode' must be int")
	}
	opv, ok := opObj.Int64()
	if !ok {
		return nil, fmt.Errorf("OverflowError: opcode value too large")
	}

	oparg := int32(0)
	if len(args) == 2 && args[1] != objects.None() {
		argObj, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: stack_effect() argument 'oparg' must be int or None")
		}
		v, ok := argObj.Int64()
		if !ok {
			return nil, fmt.Errorf("OverflowError: oparg value too large")
		}
		oparg = int32(v)
	}

	// jump: None -> -1, True -> 1, False -> 0.
	jump := -1
	switch kwargs["jump"] {
	case nil, objects.None():
		jump = -1
	case objects.True():
		jump = 1
	case objects.False():
		jump = 0
	default:
		return nil, fmt.Errorf("ValueError: stack_effect: jump must be False, True or None")
	}

	effect := compile.OpcodeStackEffectWithJump(int(opv), oparg, jump)
	if effect == compile.InvalidStackEffect {
		return nil, fmt.Errorf("ValueError: invalid opcode or oparg")
	}
	return objects.NewInt(int64(effect)), nil
}

// CPython: Modules/_opcode.c:83 _opcode_is_valid_impl
func isValid(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("is_valid", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op)), nil
}

// CPython: Modules/_opcode.c:99 _opcode_has_arg_impl
func hasArg(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_arg", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasArg()), nil
}

// CPython: Modules/_opcode.c:115 _opcode_has_const_impl
func hasConst(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_const", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasConst()), nil
}

// CPython: Modules/_opcode.c:131 _opcode_has_name_impl
func hasName(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_name", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasName()), nil
}

// CPython: Modules/_opcode.c:147 _opcode_has_jump_impl
func hasJump(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_jump", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasJump()), nil
}

// CPython: Modules/_opcode.c:168 _opcode_has_free_impl
func hasFree(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_free", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasFree()), nil
}

// CPython: Modules/_opcode.c:184 _opcode_has_local_impl
func hasLocal(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_local", args, kwargs)
	if err != nil {
		return nil, err
	}
	return objects.NewBool(isValidOpcode(op) && compile.Opcode(op).HasLocal()), nil
}

// has_exc returns True for SETUP_FINALLY / SETUP_WITH / SETUP_CLEANUP.
//
// CPython: Modules/_opcode.c:200 _opcode_has_exc_impl
// CPython: Include/internal/pycore_opcode_utils.h:17 IS_BLOCK_PUSH_OPCODE
func hasExc(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	op, err := opcodeArg("has_exc", args, kwargs)
	if err != nil {
		return nil, err
	}
	if !isValidOpcode(op) {
		return objects.NewBool(false), nil
	}
	o := compile.Opcode(op)
	return objects.NewBool(o == compile.SETUP_FINALLY || o == compile.SETUP_WITH || o == compile.SETUP_CLEANUP), nil
}

// CPython: Modules/_opcode.c:214 _opcode_get_specialization_stats_impl
func getSpecializationStats(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return objects.None(), nil
}

// CPython: Modules/_opcode.c:234 _opcode_get_nb_ops_impl
func getNBOps(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	out := make([]objects.Object, len(nbOps))
	for i, e := range nbOps {
		out[i] = objects.NewTuple([]objects.Object{objects.NewStr(e[0]), objects.NewStr(e[1])})
	}
	return objects.NewList(out), nil
}

// CPython: Modules/_opcode.c:301 _opcode_get_intrinsic1_descs_impl
func getIntrinsic1Descs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	out := make([]objects.Object, len(intrinsic1Names))
	for i, n := range intrinsic1Names {
		out[i] = objects.NewStr(n)
	}
	return objects.NewList(out), nil
}

// CPython: Modules/_opcode.c:328 _opcode_get_intrinsic2_descs_impl
func getIntrinsic2Descs(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	out := make([]objects.Object, len(intrinsic2Names))
	for i, n := range intrinsic2Names {
		out[i] = objects.NewStr(n)
	}
	return objects.NewList(out), nil
}

// CPython: Modules/_opcode.c:354 _opcode_get_special_method_names_impl
func getSpecialMethodNames(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	out := make([]objects.Object, len(specialMethodNames))
	for i, n := range specialMethodNames {
		out[i] = objects.NewStr(n)
	}
	return objects.NewList(out), nil
}

// CPython: Modules/_opcode.c:383 _opcode_get_executor_impl
func getExecutor(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("RuntimeError: Executors are not available in this build")
}
