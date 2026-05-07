// CALL_KW family specializer.
//
// _Py_Specialize_CallKw is a slimmer cousin of _Py_Specialize_Call:
// it only distinguishes Python functions (and bound methods that
// wrap one) from everything else. Builtins, types, and method
// descriptors all collapse to CALL_KW_NON_PY because the kw-args
// path does not have the dedicated arms that CALL grew over the
// years.
//
// Cache layout (3 codeunits, _PyCallCache, shared with CALL):
//   cell 1   counter
//   cells 2-3 func_version (u32)
//
// CPython: Python/specialize.c:2223 _Py_Specialize_CallKw

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// CallKw specializes the CALL_KW at instr based on the callable on
// the stack. nargs is the positional count (the keyword tuple
// itself rides on the stack).
//
// CPython: Python/specialize.c:2223 _Py_Specialize_CallKw
func CallKw(callable objects.Object, code []byte, instr int, nargs int32) {
	switch v := callable.(type) {
	case *objects.Function:
		if specializePyCallKw(v, code, instr, false) {
			return
		}
	case *objects.BoundMethod:
		if fn, ok := v.Func().(*objects.Function); ok {
			if specializePyCallKw(fn, code, instr, true) {
				return
			}
		}
		Unspecialize(code, instr)
		return
	default:
		Specialize(code, instr, compile.CALL_KW_NON_PY)
		return
	}
	Unspecialize(code, instr)
}

// specializePyCallKw picks between CALL_KW_PY and CALL_KW_BOUND_METHOD
// for Python-defined callables. Stamps func_version into cells 2..3.
//
// CPython: Python/specialize.c:2107 specialize_py_call_kw
func specializePyCallKw(fn *objects.Function, code []byte, instr int, boundMethod bool) bool {
	if fn.Code == nil {
		return false
	}
	if uint32(fn.Code.Flags)&compile.CoOptimized == 0 {
		return false
	}
	version := fn.Version
	if version == 0 {
		return false
	}
	SetCacheU32(code, instr, 2, version)
	if boundMethod {
		Specialize(code, instr, compile.CALL_KW_BOUND_METHOD)
	} else {
		Specialize(code, instr, compile.CALL_KW_PY)
	}
	return true
}
