// CALL family specializer.
//
// _Py_Specialize_Call dispatches on the callable's runtime type:
// builtins go through specialize_c_call (which picks one of the
// CALL_BUILTIN_* arms), Python functions through specialize_py_call
// (CALL_PY_EXACT_ARGS / CALL_PY_GENERAL), types through
// specialize_class_call, bound methods route through the underlying
// function, and method descriptors map by METH_* shape. Anything
// else collapses to CALL_NON_PY_GENERAL.
//
// Cache layout (3 codeunits, _PyCallCache):
//   cell 1   counter
//   cells 2-3 func_version (u32) - stamped by the py_call arms
//
// CPython: Python/specialize.c:2182 _Py_Specialize_Call

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// Call specializes the CALL at instr based on the callable on the
// stack and the (positional) argument count.
//
// CPython: Python/specialize.c:2182 _Py_Specialize_Call
func Call(callable objects.Object, code []byte, instr int, oparg, nargs int32) {
	switch v := callable.(type) {
	case *objects.Function:
		if specializePyCall(v, code, instr, nargs, false) {
			return
		}
	case *objects.BoundMethod:
		if fn, ok := v.Func().(*objects.Function); ok {
			if specializePyCall(fn, code, instr, nargs, true) {
				return
			}
		}
		Unspecialize(code, instr)
		return
	case *objects.Type:
		specializeClassCall(v, code, instr, oparg, nargs)
		return
	case *objects.CFunction:
		_ = v
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	case *objects.BuiltinFunction:
		_ = v
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	default:
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	}
	Unspecialize(code, instr)
}

// specializePyCall picks between CALL_PY_EXACT_ARGS and
// CALL_PY_GENERAL (or the bound-method twins) for Python-defined
// functions. Stamps func_version into cells 2..3.
//
// CPython: Python/specialize.c:2063 specialize_py_call
func specializePyCall(fn *objects.Function, code []byte, instr int, nargs int32, boundMethod bool) bool {
	if fn.Code == nil {
		return false
	}
	flags := uint32(fn.Code.Flags)
	if flags&(compile.CoVarargs|compile.CoVarkeywords) != 0 || fn.Code.KwonlyArgcount != 0 {
		return false
	}
	if flags&compile.CoOptimized == 0 {
		return false
	}
	version := fn.Version
	if version == 0 {
		return false
	}
	SetCacheU32(code, instr, 2, version)
	exact := int32(fn.Code.Argcount) == nargs+boolToInt32(boundMethod)
	switch {
	case exact && boundMethod:
		Specialize(code, instr, compile.CALL_BOUND_METHOD_EXACT_ARGS)
	case exact:
		Specialize(code, instr, compile.CALL_PY_EXACT_ARGS)
	case boundMethod:
		Specialize(code, instr, compile.CALL_BOUND_METHOD_GENERAL)
	default:
		Specialize(code, instr, compile.CALL_PY_GENERAL)
	}
	return true
}

// specializeClassCall handles `Cls(args...)`. CPython picks between
// CALL_TYPE_1 / CALL_STR_1 / CALL_TUPLE_1 for the unary builtin-type
// fast paths, CALL_BUILTIN_CLASS for any other immutable type with a
// vectorcall slot, CALL_ALLOC_AND_ENTER_INIT for simple
// user-defined classes with object.__new__, and otherwise falls
// through to CALL_NON_PY_GENERAL.
//
// CPython: Python/specialize.c:1965 specialize_class_call
func specializeClassCall(tp *objects.Type, code []byte, instr int, oparg, nargs int32) {
	if !tp.IsUser {
		// CPython treats every type with Py_TPFLAGS_IMMUTABLETYPE as
		// the eligible target for CALL_TYPE_1 / CALL_STR_1 /
		// CALL_TUPLE_1 / CALL_BUILTIN_CLASS. gopy's IsUser==false
		// stands in for IMMUTABLETYPE.
		if nargs == 1 && oparg == 1 {
			switch tp {
			case objects.StrType():
				Specialize(code, instr, compile.CALL_STR_1)
				return
			case objects.TypeType():
				Specialize(code, instr, compile.CALL_TYPE_1)
				return
			case objects.TupleType:
				Specialize(code, instr, compile.CALL_TUPLE_1)
				return
			}
		}
		Specialize(code, instr, compile.CALL_BUILTIN_CLASS)
		return
	}
	// User class: CPython only specializes the
	// CALL_ALLOC_AND_ENTER_INIT path when tp_new is object.__new__
	// and __init__ is a SIMPLE_FUNCTION. gopy does not yet wire the
	// init-cache machinery so we fall through to the generic arm.
	Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
}

func boolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
