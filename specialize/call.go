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
		specializeCCall(v, code, instr, oparg, nargs)
		return
	case *objects.MethodDescr:
		specializeMethodDescriptor(v, code, instr, oparg, nargs)
		return
	default:
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	}
	Unspecialize(code, instr)
}

// specializeCCall picks between CALL_BUILTIN_O / CALL_BUILTIN_FAST /
// CALL_BUILTIN_FAST_WITH_KEYWORDS / CALL_LEN / CALL_ISINSTANCE based
// on bf.Conv and the identity comparisons against
// interp->callable_cache.{len, isinstance}.
//
// CPython: Python/specialize.c:2137 specialize_c_call
func specializeCCall(bf *objects.BuiltinFunction, code []byte, instr int, oparg, nargs int32) {
	if bf.Fn == nil {
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	}
	mask := objects.MethVarargs | objects.MethFastcall | objects.MethNoArgs | objects.MethO | objects.MethKeywords | objects.MethMethod
	switch bf.Conv & mask {
	case objects.MethO:
		if nargs != 1 {
			Unspecialize(code, instr)
			return
		}
		if bf == objects.CallableCacheLen() && oparg == 1 {
			Specialize(code, instr, compile.CALL_LEN)
			return
		}
		Specialize(code, instr, compile.CALL_BUILTIN_O)
		return
	case objects.MethFastcall:
		if nargs == 2 && bf == objects.CallableCacheIsinstance() {
			Specialize(code, instr, compile.CALL_ISINSTANCE)
			return
		}
		Specialize(code, instr, compile.CALL_BUILTIN_FAST)
		return
	case objects.MethFastcall | objects.MethKeywords:
		Specialize(code, instr, compile.CALL_BUILTIN_FAST_WITH_KEYWORDS)
		return
	default:
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	}
}

// specializeMethodDescriptor picks between CALL_METHOD_DESCRIPTOR_*
// arms (and CALL_LIST_APPEND for the canonical list.append shape)
// based on the descriptor's METH_* tag.
//
// CPython: Python/specialize.c:2019 specialize_method_descriptor
func specializeMethodDescriptor(d *objects.MethodDescr, code []byte, instr int, oparg, nargs int32) {
	mask := objects.MethVarargs | objects.MethFastcall | objects.MethNoArgs | objects.MethO | objects.MethKeywords | objects.MethMethod
	switch d.Conv() & mask {
	case objects.MethNoArgs:
		if nargs != 1 {
			Unspecialize(code, instr)
			return
		}
		Specialize(code, instr, compile.CALL_METHOD_DESCRIPTOR_NOARGS)
		return
	case objects.MethO:
		if nargs != 2 {
			Unspecialize(code, instr)
			return
		}
		// CALL_LIST_APPEND only fires when the following bytecode is
		// POP_TOP (CPython demands the caller drops the None result)
		// and oparg == 1. Check the next codeunit (instr is the start
		// of the current opcode in bytes; the CALL family has
		// INLINE_CACHE_ENTRIES_CALL cache cells after it).
		//
		// CPython: Python/specialize.c:2040 specialize_method_descriptor (METH_O / list.append)
		if d == objects.CallableCacheListAppend() && oparg == 1 && callFollowedByPopTop(code, instr) {
			Specialize(code, instr, compile.CALL_LIST_APPEND)
			return
		}
		Specialize(code, instr, compile.CALL_METHOD_DESCRIPTOR_O)
		return
	case objects.MethFastcall:
		Specialize(code, instr, compile.CALL_METHOD_DESCRIPTOR_FAST)
		return
	case objects.MethFastcall | objects.MethKeywords:
		Specialize(code, instr, compile.CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS)
		return
	default:
		Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
		return
	}
}

// callFollowedByPopTop reports whether the codeunit after the CALL
// (plus its inline cache cells) is POP_TOP. CALL has 3 inline cache
// codeunits after it; each codeunit is 2 bytes.
//
// CPython: Python/specialize.c:2041 (reads instr[INLINE_CACHE_ENTRIES_CALL + 1])
func callFollowedByPopTop(code []byte, instr int) bool {
	// instr is the byte offset of the CALL opcode; the next opcode
	// lives (1 + INLINE_CACHE_ENTRIES_CALL) codeunits later. CALL's
	// cache entry count is 3 (see compile/opcodes_gen.go), so the
	// follow-up opcode byte is at instr + 2*4 = instr + 8.
	next := instr + 2*(1+3)
	if next >= len(code) {
		return false
	}
	return compile.Opcode(code[next]) == compile.POP_TOP
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
	callCacheAt(code, instr).setFuncVersion(version)
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
	// User class: CPython picks CALL_ALLOC_AND_ENTER_INIT when tp_new
	// is object.__new__ (gopy: TpNew == nil on a user heap class),
	// tp_alloc is the generic allocator (gopy: NewInstance), and
	// __init__ is a SIMPLE_FUNCTION (CO_OPTIMIZED, no *args/**kwargs/
	// kwonly). The init pointer + matching tp_version_tag get stashed
	// in the type's spec cache; the fast arm consumes both.
	//
	// CPython: Python/specialize.c:1965 specialize_class_call
	if tp.TpNew == nil {
		if init := lookupInitFunction(tp); init != nil && isSimpleFunction(init) {
			version, ok := tp.CacheInitForSpecialization(init)
			if ok {
				callCacheAt(code, instr).setFuncVersion(version)
				Specialize(code, instr, compile.CALL_ALLOC_AND_ENTER_INIT)
				return
			}
		}
	}
	Specialize(code, instr, compile.CALL_NON_PY_GENERAL)
}

// lookupInitFunction walks tp's MRO looking for `__init__`, returning
// it only when it resolves to a Python `Function` (not a builtin,
// method-descriptor, or wrapped slot). The CALL_ALLOC_AND_ENTER_INIT
// fast arm needs a Function to push a frame against.
//
// CPython: Python/specialize.c:1950 _PyType_LookupRefAndVersion
// (filtered down to PyFunction_Check)
func lookupInitFunction(tp *objects.Type) *objects.Function {
	descr, _ := objects.LookupDescriptor(tp, "__init__")
	if descr == nil {
		return nil
	}
	fn, ok := descr.(*objects.Function)
	if !ok {
		return nil
	}
	return fn
}

// isSimpleFunction mirrors CPython's SIMPLE_FUNCTION classification:
// CO_OPTIMIZED set, and no *args / **kwargs / kwonly parameters. The
// init cache only fires for classes whose __init__ obeys this shape.
//
// CPython: Python/specialize.c:1785 function_kind
func isSimpleFunction(fn *objects.Function) bool {
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
	return true
}

func boolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
