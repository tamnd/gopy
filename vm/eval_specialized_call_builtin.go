// Fast-path arms for the builtin / method-descriptor / type-call
// CALL family. Each arm assumes the specializer has already proven
// the callable's shape (Conv tag, identity match against the
// callable cache, or *Type fast call) and skips the generic
// objects.Vectorcall path. Guard misses return ok=false so the
// outer dispatch routes through maybeDeopt.
//
// The arms mirror the matching bodies in Python/bytecodes.c. Each
// builtin row's call wrapper still goes through the bf.Fn /
// d.fn closure (gopy stores everything as []objects.Object), so the
// arm's saving over generic CALL is: skip the vectorcall slot
// lookup, skip the kwargs map allocation, and skip the prepend in
// the method-shape branch by passing the raw args window.
//
// Stack layout on entry matches CPython 3.14: [callable, self_or_null,
// arg0, ..., arg(oparg-1)]. self_or_null is nil for a plain function
// call and non-nil when LOAD_ATTR emitted the unbound-method shape.
//
// CPython: Python/bytecodes.c _CALL_BUILTIN_O / _CALL_BUILTIN_FAST /
// _CALL_BUILTIN_FAST_WITH_KEYWORDS / _CALL_LEN / CALL_ISINSTANCE /
// CALL_LIST_APPEND / _CALL_TYPE_1 / _CALL_STR_1 / _CALL_TUPLE_1 /
// _CALL_BUILTIN_CLASS / _CALL_METHOD_DESCRIPTOR_NOARGS /
// _CALL_METHOD_DESCRIPTOR_O / _CALL_METHOD_DESCRIPTOR_FAST /
// _CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// callFrameArgs reads the [callable, self_or_null, args[oparg]] window
// and returns (callable, selfOrNull, args, totalArgs). args is a
// freshly-allocated slice that prepends selfOrNull when the method
// shape is in use, matching CPython's `arguments-- ; total_args++`
// adjustment.
//
// CPython: Python/bytecodes.c _CALL_* prologue (arguments / total_args)
func (e *evalState) callFrameArgs(oparg uint32) (callable objects.Object, args []objects.Object) {
	argc := int(oparg)
	selfOrNull := e.peek(argc).AsObject()
	callable = e.peek(argc + 1).AsObject()
	total := argc
	if selfOrNull != nil {
		total++
	}
	args = make([]objects.Object, total)
	off := 0
	if selfOrNull != nil {
		args[0] = selfOrNull
		off = 1
	}
	for i := range argc {
		args[off+i] = e.peek(argc - 1 - i).AsObject()
	}
	return callable, args
}

// finishCall drops the call frame and pushes the result, advancing
// past the CALL opcode plus its cache cells.
//
// CPython: Python/bytecodes.c _CALL_* exit path (DECREF_INPUTS + push res)
func (e *evalState) finishCall(oparg uint32, result objects.Object) int {
	e.drop(int(oparg) + 2)
	e.pushObject(result)
	return e.cacheAdvance(compile.CALL)
}

// fastCallBuiltinO runs CALL_BUILTIN_O. Guards: callable is a
// *BuiltinFunction with Conv == METH_O and total_args (oparg +
// has_self) == 1.
//
// CPython: Python/bytecodes.c:4233 _CALL_BUILTIN_O
func (e *evalState) fastCallBuiltinO(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	bf, ok := callable.(*objects.BuiltinFunction)
	if !ok {
		return 0, false, nil
	}
	if bf.Conv&methConvMask != objects.MethO {
		return 0, false, nil
	}
	if len(args) != 1 {
		return 0, false, nil
	}
	res, err := bf.Fn(args, nil)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallBuiltinFast runs CALL_BUILTIN_FAST. Guards: callable is a
// *BuiltinFunction tagged METH_FASTCALL (no kwargs).
//
// CPython: Python/bytecodes.c:4268 _CALL_BUILTIN_FAST
func (e *evalState) fastCallBuiltinFast(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	bf, ok := callable.(*objects.BuiltinFunction)
	if !ok {
		return 0, false, nil
	}
	if bf.Conv&methConvMask != objects.MethFastcall {
		return 0, false, nil
	}
	res, err := bf.Fn(args, nil)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallBuiltinFastWithKeywords runs CALL_BUILTIN_FAST_WITH_KEYWORDS.
// Guards: callable is a *BuiltinFunction tagged
// METH_FASTCALL|METH_KEYWORDS. The CALL opcode itself never carries
// kwnames (that's CALL_KW), so kwargs is always nil here; the arm
// still beats generic CALL by skipping vectorcall slot lookup.
//
// CPython: Python/bytecodes.c:4305 _CALL_BUILTIN_FAST_WITH_KEYWORDS
func (e *evalState) fastCallBuiltinFastWithKeywords(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	bf, ok := callable.(*objects.BuiltinFunction)
	if !ok {
		return 0, false, nil
	}
	if bf.Conv&methConvMask != objects.MethFastcall|objects.MethKeywords {
		return 0, false, nil
	}
	res, err := bf.Fn(args, nil)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallLen runs CALL_LEN: len(arg). Guards: callable is the
// canonical len pointer, oparg == 1, self_or_null is null.
//
// CPython: Python/bytecodes.c:4354 _CALL_LEN
func (e *evalState) fastCallLen(oparg uint32) (int, bool, error) {
	if oparg != 1 {
		return 0, false, nil
	}
	if e.peek(1).AsObject() != nil {
		return 0, false, nil
	}
	callable := e.peek(2).AsObject()
	if callable != objects.CallableCacheLen() {
		return 0, false, nil
	}
	arg := e.peek(0).AsObject()
	n, err := objects.Length(arg)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, objects.NewInt(int64(n))), true, nil
}

// fastCallIsinstance runs CALL_ISINSTANCE: isinstance(obj, cls).
// Guards: callable is the canonical isinstance pointer, total_args == 2.
// CPython's arm calls PyObject_IsInstance directly; gopy keeps the
// isinstance logic in builtins.IsInstance, so we invoke it through
// the cached BuiltinFunction's Fn pointer. Skipping the kwargs map
// is the remaining win over generic CALL.
//
// CPython: Python/bytecodes.c:4374 CALL_ISINSTANCE
func (e *evalState) fastCallIsinstance(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	if len(args) != 2 {
		return 0, false, nil
	}
	bf := objects.CallableCacheIsinstance()
	if bf == nil || callable != bf {
		return 0, false, nil
	}
	res, err := bf.Fn(args, nil)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallListAppend runs CALL_LIST_APPEND: L.append(x) followed by
// POP_TOP. Guards: callable is the canonical list.append descriptor,
// oparg == 1, self_or_null is the list. The arm also consumes the
// trailing POP_TOP by advancing one extra codeunit (CPython does the
// same via SKIP_OVER(1)).
//
// CPython: Python/bytecodes.c:4400 CALL_LIST_APPEND
func (e *evalState) fastCallListAppend(oparg uint32) (int, bool, error) {
	if oparg != 1 {
		return 0, false, nil
	}
	callable := e.peek(2).AsObject()
	if callable != objects.CallableCacheListAppend() {
		return 0, false, nil
	}
	self := e.peek(1).AsObject()
	if self == nil {
		return 0, false, nil
	}
	list, ok := self.(*objects.List)
	if !ok {
		return 0, false, nil
	}
	arg := e.peek(0).AsObject()
	list.Append(arg)
	// Drop [callable, self, arg]. The POP_TOP that would normally
	// discard the None result is skipped: advance past CALL + its
	// caches, then add one more codeunit for the POP_TOP.
	e.drop(3)
	return e.cacheAdvance(compile.CALL) + 2, true, nil
}

// fastCallType1 runs CALL_TYPE_1: type(x). Guards: callable is
// objects.TypeType(), oparg == 1, self_or_null is null.
//
// CPython: Python/bytecodes.c:4061 _CALL_TYPE_1
func (e *evalState) fastCallType1(oparg uint32) (int, bool, error) {
	if oparg != 1 {
		return 0, false, nil
	}
	if e.peek(1).AsObject() != nil {
		return 0, false, nil
	}
	if e.peek(2).AsObject() != objects.TypeType() {
		return 0, false, nil
	}
	arg := e.peek(0).AsObject()
	return e.finishCall(oparg, arg.Type()), true, nil
}

// fastCallStr1 runs CALL_STR_1: str(x). Guards: callable is
// objects.StrType(), oparg == 1, self_or_null is null.
//
// CPython: Python/bytecodes.c:4086 _CALL_STR_1
func (e *evalState) fastCallStr1(oparg uint32) (int, bool, error) {
	if oparg != 1 {
		return 0, false, nil
	}
	if e.peek(1).AsObject() != nil {
		return 0, false, nil
	}
	if e.peek(2).AsObject() != objects.StrType() {
		return 0, false, nil
	}
	arg := e.peek(0).AsObject()
	s, err := objects.Str(arg)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, objects.NewStr(s)), true, nil
}

// fastCallTuple1 runs CALL_TUPLE_1: tuple(iterable). Guards: callable
// is objects.TupleType, oparg == 1, self_or_null is null.
//
// CPython: Python/bytecodes.c:4114 _CALL_TUPLE_1
func (e *evalState) fastCallTuple1(oparg uint32) (int, bool, error) {
	if oparg != 1 {
		return 0, false, nil
	}
	if e.peek(1).AsObject() != nil {
		return 0, false, nil
	}
	if e.peek(2).AsObject() != objects.TupleType {
		return 0, false, nil
	}
	arg := e.peek(0).AsObject()
	res, err := objects.SequenceTuple(arg)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallBuiltinClass runs CALL_BUILTIN_CLASS: a type with a
// vectorcall slot, called like a function. Guards: callable is a
// *Type with a non-nil Vectorcall.
//
// CPython: Python/bytecodes.c:4203 _CALL_BUILTIN_CLASS
func (e *evalState) fastCallBuiltinClass(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	tp, ok := callable.(*objects.Type)
	if !ok {
		return 0, false, nil
	}
	if tp.Vectorcall == nil {
		return 0, false, nil
	}
	res, err := tp.Vectorcall(tp, args, uint(len(args)), nil)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallMethodDescriptorO runs CALL_METHOD_DESCRIPTOR_O. Guards:
// callable is a *MethodDescr with Conv == METH_O, total_args == 2,
// and args[0]'s type matches the descriptor's owner.
//
// CPython: Python/bytecodes.c:4424 _CALL_METHOD_DESCRIPTOR_O
func (e *evalState) fastCallMethodDescriptorO(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	d, ok := callable.(*objects.MethodDescr)
	if !ok {
		return 0, false, nil
	}
	if d.Conv()&methConvMask != objects.MethO {
		return 0, false, nil
	}
	if len(args) != 2 {
		return 0, false, nil
	}
	res, err := descrInvoke(d, args)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallMethodDescriptorFast runs CALL_METHOD_DESCRIPTOR_FAST.
// Guards: callable is a *MethodDescr with Conv == METH_FASTCALL,
// total_args >= 1, args[0]'s type matches owner.
//
// CPython: Python/bytecodes.c:4543 _CALL_METHOD_DESCRIPTOR_FAST
func (e *evalState) fastCallMethodDescriptorFast(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	d, ok := callable.(*objects.MethodDescr)
	if !ok {
		return 0, false, nil
	}
	if d.Conv()&methConvMask != objects.MethFastcall {
		return 0, false, nil
	}
	if len(args) == 0 {
		return 0, false, nil
	}
	res, err := descrInvoke(d, args)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallMethodDescriptorFastKw runs
// CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS. The CALL opcode carries
// no kwnames so kwargs is always nil; the arm still beats generic
// CALL by skipping the vectorcall slot lookup.
//
// CPython: Python/bytecodes.c:4463 _CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS
func (e *evalState) fastCallMethodDescriptorFastKw(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	d, ok := callable.(*objects.MethodDescr)
	if !ok {
		return 0, false, nil
	}
	if d.Conv()&methConvMask != objects.MethFastcall|objects.MethKeywords {
		return 0, false, nil
	}
	if len(args) == 0 {
		return 0, false, nil
	}
	res, err := descrInvoke(d, args)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// fastCallMethodDescriptorNoArgs runs CALL_METHOD_DESCRIPTOR_NOARGS.
// Guards: callable is a *MethodDescr with Conv == METH_NOARGS,
// total_args == 1, args[0]'s type matches owner.
//
// CPython: Python/bytecodes.c:4505 _CALL_METHOD_DESCRIPTOR_NOARGS
func (e *evalState) fastCallMethodDescriptorNoArgs(oparg uint32) (int, bool, error) {
	callable, args := e.callFrameArgs(oparg)
	d, ok := callable.(*objects.MethodDescr)
	if !ok {
		return 0, false, nil
	}
	if d.Conv()&methConvMask != objects.MethNoArgs {
		return 0, false, nil
	}
	if len(args) != 1 {
		return 0, false, nil
	}
	res, err := descrInvoke(d, args)
	if err != nil {
		return 0, true, err
	}
	return e.finishCall(oparg, res), true, nil
}

// descrInvoke is the shared call path for the method-descriptor
// arms. It performs the owner-type check CPython encodes as
// `Py_IS_TYPE(self, method->d_common.d_type)` and then invokes the
// descriptor's closure with the full positional window.
//
// CPython: Objects/descrobject.c:296 method_call (descriptor check + dispatch)
func descrInvoke(d *objects.MethodDescr, args []objects.Object) (objects.Object, error) {
	// gopy keeps the call shape uniform; dispatching through the
	// type-call slot reuses the existing owner-type check.
	return d.Type().Call(d, args, nil)
}

// methConvMask masks the METH_* tag bits the specializer reads.
// CPython uses the same mask in specialize_c_call and
// specialize_method_descriptor.
//
// CPython: Python/specialize.c:2143 (METH_VARARGS | METH_FASTCALL | ...)
const methConvMask = objects.MethVarargs | objects.MethFastcall | objects.MethNoArgs | objects.MethO | objects.MethKeywords | objects.MethMethod
