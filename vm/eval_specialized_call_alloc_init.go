// Fast-path arm for CALL_ALLOC_AND_ENTER_INIT.
//
// _Py_Specialize_Call picks this opcode when the callable is a user
// heap class whose tp_new is object.__new__, whose tp_alloc is the
// generic allocator, and whose __init__ is a SIMPLE_FUNCTION
// (CO_OPTIMIZED, no *args / **kwargs / kwonly). The specializer
// stashes the resolved Function pointer on the type's spec cache
// alongside the tp_version_tag at stamp time, and writes the same
// version into cache cells 2-3 so the fast arm can re-validate.
//
// CPython composes the bytecode body as:
//
//	CALL_ALLOC_AND_ENTER_INIT =
//	    unused/1 + _CHECK_PEP_523 + _CHECK_AND_ALLOCATE_OBJECT +
//	    _CREATE_INIT_FRAME + _PUSH_FRAME
//
// _CHECK_AND_ALLOCATE_OBJECT validates the cached tp_version still
// matches, loads init from cls->_spec_cache.init, allocates the
// instance via PyType_GenericAlloc, and swaps the (cls, NULL, args)
// stack window into (init, self, args). _CREATE_INIT_FRAME pushes
// the _Py_InitCleanup shim frame (a 2-instruction prologue:
// EXIT_INIT_CHECK + RETURN_VALUE) plus a real Python frame for
// init, then _PUSH_FRAME DISPATCH_INLINEDs into the init body. When
// init returns, the shim's EXIT_INIT_CHECK verifies the return is
// None and RETURN_VALUE pushes self back to the caller.
//
// gopy folds the shim into the fast arm directly: Eval is a Go call
// so we get init's return value back without needing a separate
// bytecode-level shim frame. The arm validates None, raises
// TypeError("__init__() should return None, ...") on mismatch, and
// pushes the freshly allocated instance.
//
// CPython: Python/bytecodes.c:4186 CALL_ALLOC_AND_ENTER_INIT macro
// CPython: Python/bytecodes.c:4137 _CHECK_AND_ALLOCATE_OBJECT
// CPython: Python/bytecodes.c:4161 _CREATE_INIT_FRAME
// CPython: Python/bytecodes.c:4193 EXIT_INIT_CHECK

package vm

import (
	"fmt"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
)

// fastCallAllocAndEnterInit runs CALL_ALLOC_AND_ENTER_INIT. Stack on
// entry: [callable, NULL, arg0, ..., arg(oparg-1)] with callable a
// *Type whose IsUser bit is set. Returns ok=true when the arm took
// the dispatch; on guard miss ok=false and the caller deopts.
//
// CPython: Python/bytecodes.c:4186 CALL_ALLOC_AND_ENTER_INIT
func (e *evalState) fastCallAllocAndEnterInit(oparg uint32) (int, bool, error) {
	argc := int(oparg)
	if e.peek(argc).AsObject() != nil {
		// _CHECK_AND_ALLOCATE_OBJECT DEOPTs on a non-null self slot:
		// only direct class calls (no bound-method shape) are eligible.
		return 0, false, nil
	}
	cls, ok := e.peek(argc + 1).AsObject().(*objects.Type)
	if !ok || !cls.IsUser {
		return 0, false, nil
	}
	init := cls.SpecCacheInit()
	if init == nil || init.Code == nil {
		return 0, false, nil
	}
	idx := e.instrIdx()
	cachedVer := specialize.CallFuncVersion(e.f.Code.Code, idx)
	liveVer := cls.VersionTag()
	if liveVer == 0 || liveVer != cachedVer || liveVer != cls.SpecCacheInitVersion() {
		return 0, false, nil
	}
	co := init.Code
	if co.Argcount != argc+1 {
		// __init__ expects (self, *positionals); the cache assumed
		// the same shape that was stamped, but oparg can change at
		// the call site (e.g. specializer fired on a one-arg call
		// and we now hit it with two). Deopt rather than mismatch.
		return 0, false, nil
	}
	inst := objects.NewInstance(cls)
	stack := frameStackFor(e.ts)
	f2 := stack.Push(co, init.Globals, init.Builtins, init)
	f2.SetLocal(0, stackref.FromObject(inst))
	for i := range argc {
		argObj := e.peek(argc - 1 - i).AsObject()
		f2.SetLocal(i+1, stackref.FromObject(argObj))
	}
	out, err := Eval(e.ts, f2)
	stack.Pop()
	if err != nil {
		return 0, true, err
	}
	if !objects.IsNone(out) {
		return 0, true, fmt.Errorf("TypeError: __init__() should return None, not '%s'", typeNameOf(out))
	}
	e.drop(argc + 2)
	e.pushObject(inst)
	return e.cacheAdvance(compile.CALL), true, nil
}

// typeNameOf returns the Python-visible type name for o, falling back
// to "NoneType" / a placeholder when the type is unset. Matches the
// shape of CPython's Py_TYPE(o)->tp_name format in the EXIT_INIT_CHECK
// error message.
//
// CPython: Python/bytecodes.c:4193 EXIT_INIT_CHECK (PyErr_Format format string)
func typeNameOf(o objects.Object) string {
	if o == nil {
		return "NoneType"
	}
	if t := o.Type(); t != nil {
		return t.Name
	}
	return "object"
}
