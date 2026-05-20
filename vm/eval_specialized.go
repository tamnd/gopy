// Fast-path arms for specialized LOAD_ATTR variants.
//
// dispatch consults trySpecialized BEFORE maybeDeopt so a hot site
// that has already been Specialized can skip the adaptive parent's
// generic body. On guard miss the arm returns ok=false; the existing
// maybeDeopt path then rewrites the bytecode back to the adaptive
// parent and the generic LOAD_ATTR runs (and resets the counter).
//
// Each arm mirrors the matching CPython body in Python/bytecodes.c:
// validate cached version(s), read the cached descr pointer (or slot
// index) from the inline cache, and push the value with the right
// shape for the trailing CALL. The descr pointer cache itself lives
// on Code.CacheObjects since Go can't stash GC-tracked pointers in
// the codeunit []byte (CPython's read_obj path).
//
// CPython: Python/bytecodes.c LOAD_ATTR_<variant> macros.

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: fully deleted; specialized-arm dispatch moves into vm/eval_dispatch_gen.go.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
	"github.com/tamnd/gopy/stackref"
)

// trySpecialized runs the fast-path arm for op. Returns ok=true when
// the arm took the dispatch (caller forwards next as the new PC).
// ok=false means guard miss or unsupported variant; the caller must
// continue through maybeDeopt so the adaptive parent body runs.
//
//nolint:gocyclo // one arm per specialized opcode; collapsing them defeats the point of the fast-path table.
func (e *evalState) trySpecialized(op compile.Opcode, oparg uint32) (next int, ok bool, err error) {
	if !e.f.Code.Quickened {
		return 0, false, nil
	}
	switch op {
	case compile.LOAD_ATTR_MODULE:
		next, ok = e.fastLoadAttrModule(oparg)
	case compile.LOAD_ATTR_SLOT:
		next, ok = e.fastLoadAttrSlot(oparg)
	case compile.LOAD_ATTR_CLASS:
		next, ok = e.fastLoadAttrClass(oparg)
	case compile.LOAD_ATTR_CLASS_WITH_METACLASS_CHECK:
		next, ok = e.fastLoadAttrClassWithMetaclassCheck(oparg)
	case compile.LOAD_ATTR_METHOD_NO_DICT:
		next, ok = e.fastLoadAttrMethodNoDict(oparg)
	case compile.LOAD_ATTR_NONDESCRIPTOR_NO_DICT:
		next, ok = e.fastLoadAttrNondescriptorNoDict(oparg)
	case compile.LOAD_ATTR_METHOD_WITH_VALUES:
		next, ok = e.fastLoadAttrMethodWithValues(oparg)
	case compile.LOAD_ATTR_METHOD_LAZY_DICT:
		next, ok = e.fastLoadAttrMethodLazyDict(oparg)
	case compile.LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES:
		next, ok = e.fastLoadAttrNondescriptorWithValues(oparg)
	case compile.LOAD_ATTR_PROPERTY:
		return e.fastLoadAttrProperty(oparg)
	case compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN:
		return e.fastLoadAttrGetattributeOverridden(oparg)
	case compile.LOAD_ATTR_INSTANCE_VALUE:
		next, ok = e.fastLoadAttrInstanceValue(oparg)
	case compile.TO_BOOL_BOOL:
		next, ok = e.fastToBoolBool()
	case compile.TO_BOOL_INT:
		next, ok = e.fastToBoolInt()
	case compile.TO_BOOL_LIST:
		next, ok = e.fastToBoolList()
	case compile.TO_BOOL_NONE:
		next, ok = e.fastToBoolNone()
	case compile.TO_BOOL_STR:
		next, ok = e.fastToBoolStr()
	case compile.TO_BOOL_ALWAYS_TRUE:
		next, ok = e.fastToBoolAlwaysTrue()
	case compile.COMPARE_OP_FLOAT:
		next, ok = e.fastCompareOpFloat(oparg)
	case compile.COMPARE_OP_INT:
		next, ok = e.fastCompareOpInt(oparg)
	case compile.COMPARE_OP_STR:
		next, ok = e.fastCompareOpStr(oparg)
	case compile.CONTAINS_OP_DICT:
		return e.fastContainsOpDict(oparg)
	case compile.CONTAINS_OP_SET:
		return e.fastContainsOpSet(oparg)
	case compile.UNPACK_SEQUENCE_TWO_TUPLE:
		next, ok = e.fastUnpackSequenceTwoTuple(oparg)
	case compile.UNPACK_SEQUENCE_TUPLE:
		next, ok = e.fastUnpackSequenceTuple(oparg)
	case compile.UNPACK_SEQUENCE_LIST:
		next, ok = e.fastUnpackSequenceList(oparg)
	case compile.STORE_SUBSCR_LIST_INT:
		next, ok = e.fastStoreSubscrListInt(oparg)
	case compile.STORE_SUBSCR_DICT:
		return e.fastStoreSubscrDict(oparg)
	case compile.BINARY_OP_ADD_INT:
		next, ok = e.fastBinaryOpAddInt()
	case compile.BINARY_OP_SUBTRACT_INT:
		next, ok = e.fastBinaryOpSubtractInt()
	case compile.BINARY_OP_MULTIPLY_INT:
		next, ok = e.fastBinaryOpMultiplyInt()
	case compile.BINARY_OP_ADD_FLOAT:
		next, ok = e.fastBinaryOpAddFloat()
	case compile.BINARY_OP_SUBTRACT_FLOAT:
		next, ok = e.fastBinaryOpSubtractFloat()
	case compile.BINARY_OP_MULTIPLY_FLOAT:
		next, ok = e.fastBinaryOpMultiplyFloat()
	case compile.BINARY_OP_ADD_UNICODE, compile.BINARY_OP_INPLACE_ADD_UNICODE:
		next, ok = e.fastBinaryOpAddUnicode()
	case compile.BINARY_OP_SUBSCR_LIST_INT:
		next, ok = e.fastBinaryOpSubscrListInt()
	case compile.BINARY_OP_SUBSCR_TUPLE_INT:
		next, ok = e.fastBinaryOpSubscrTupleInt()
	case compile.BINARY_OP_SUBSCR_STR_INT:
		next, ok = e.fastBinaryOpSubscrStrInt()
	case compile.BINARY_OP_SUBSCR_DICT:
		return e.fastBinaryOpSubscrDict()
	case compile.BINARY_OP_SUBSCR_LIST_SLICE:
		return e.fastBinaryOpSubscrListSlice()
	case compile.LOAD_GLOBAL_MODULE:
		next, ok = e.fastLoadGlobalModule(oparg)
	case compile.LOAD_GLOBAL_BUILTIN:
		next, ok = e.fastLoadGlobalBuiltin(oparg)
	case compile.STORE_ATTR_SLOT:
		next, ok = e.fastStoreAttrSlot(oparg)
	case compile.STORE_ATTR_INSTANCE_VALUE:
		next, ok = e.fastStoreAttrInstanceValue(oparg)
	case compile.STORE_ATTR_WITH_HINT:
		next, ok = e.fastStoreAttrWithHint(oparg)
	case compile.CALL_PY_EXACT_ARGS:
		return e.fastCallPyExactArgs(oparg)
	case compile.CALL_BOUND_METHOD_EXACT_ARGS:
		return e.fastCallBoundMethodExactArgs(oparg)
	case compile.CALL_BUILTIN_O:
		return e.fastCallBuiltinO(oparg)
	case compile.CALL_BUILTIN_FAST:
		return e.fastCallBuiltinFast(oparg)
	case compile.CALL_BUILTIN_FAST_WITH_KEYWORDS:
		return e.fastCallBuiltinFastWithKeywords(oparg)
	case compile.CALL_LEN:
		return e.fastCallLen(oparg)
	case compile.CALL_ISINSTANCE:
		return e.fastCallIsinstance(oparg)
	case compile.CALL_LIST_APPEND:
		return e.fastCallListAppend(oparg)
	case compile.CALL_TYPE_1:
		return e.fastCallType1(oparg)
	case compile.CALL_STR_1:
		return e.fastCallStr1(oparg)
	case compile.CALL_TUPLE_1:
		return e.fastCallTuple1(oparg)
	case compile.CALL_BUILTIN_CLASS:
		return e.fastCallBuiltinClass(oparg)
	case compile.CALL_METHOD_DESCRIPTOR_O:
		return e.fastCallMethodDescriptorO(oparg)
	case compile.CALL_METHOD_DESCRIPTOR_FAST:
		return e.fastCallMethodDescriptorFast(oparg)
	case compile.CALL_METHOD_DESCRIPTOR_FAST_WITH_KEYWORDS:
		return e.fastCallMethodDescriptorFastKw(oparg)
	case compile.CALL_METHOD_DESCRIPTOR_NOARGS:
		return e.fastCallMethodDescriptorNoArgs(oparg)
	case compile.FOR_ITER_LIST:
		next, ok = e.fastForIterList(oparg)
	case compile.FOR_ITER_TUPLE:
		next, ok = e.fastForIterTuple(oparg)
	case compile.FOR_ITER_RANGE:
		next, ok = e.fastForIterRange(oparg)
	case compile.LOAD_SUPER_ATTR_ATTR:
		return e.fastLoadSuperAttrAttr(oparg)
	case compile.LOAD_SUPER_ATTR_METHOD:
		return e.fastLoadSuperAttrMethod(oparg)
	case compile.SEND_GEN:
		return e.fastSendGen(oparg)
	case compile.CALL_ALLOC_AND_ENTER_INIT:
		return e.fastCallAllocAndEnterInit(oparg)
	}
	return next, ok, nil
}

// pushAttrResult finalizes the stack shape for a LOAD_ATTR fast-path
// hit: pop the owner, push the attribute, and emit a NULL self slot
// when oparg requests the unbound-method shape (bit 0 set).
//
// CPython: Python/bytecodes.c _LOAD_ATTR exit path.
func (e *evalState) pushAttrResult(attr objects.Object, oparg uint32) {
	e.pop()
	if oparg&1 != 0 {
		e.pushObject(attr)
		e.push(stackref.Null)
	} else {
		e.pushObject(attr)
	}
}

// fastLoadAttrModule implements LOAD_ATTR_MODULE.
//
// Guards: owner is *Module, dict is unicode-keyed, dict's keys_version
// matches cells 2..3, slot index at cell 4 still resolves to a live
// entry.
//
// CPython: Python/bytecodes.c LOAD_ATTR_MODULE
func (e *evalState) fastLoadAttrModule(oparg uint32) (int, bool) {
	owner, ok := e.peek(0).AsObject().(*objects.Module)
	if !ok {
		return 0, false
	}
	d := owner.Dict()
	if d == nil || !d.IsKeysUnicode() {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.AttrCacheVersion(code, idx)
	curVer := d.GetKeysVersion()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.AttrCacheIndex(code, idx))
	_, value, found := d.EntryAt(slot)
	if !found || value == nil {
		return 0, false
	}
	e.pushAttrResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrSlot implements LOAD_ATTR_SLOT.
//
// Guards: owner is *Instance, owner's type_version matches cells 2..3,
// slot index at cell 4 is in range, slot holds a non-nil value.
//
// CPython: Python/bytecodes.c LOAD_ATTR_SLOT
func (e *evalState) fastLoadAttrSlot(oparg uint32) (int, bool) {
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := inst.Type().VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.AttrCacheIndex(code, idx))
	value := inst.SlotAt(slot)
	if value == nil {
		return 0, false
	}
	e.pushAttrResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrClass implements LOAD_ATTR_CLASS.
//
// Guards: owner is *Type, type_version matches cells 2..3, the
// descriptor cached at instr in CacheObjects is non-nil.
//
// CPython: Python/bytecodes.c LOAD_ATTR_CLASS
func (e *evalState) fastLoadAttrClass(oparg uint32) (int, bool) {
	cls, ok := e.peek(0).AsObject().(*objects.Type)
	if !ok {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := cls.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	e.pushAttrResult(descr, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrClassWithMetaclassCheck implements
// LOAD_ATTR_CLASS_WITH_METACLASS_CHECK. Same shape as the CLASS arm
// but the cache also pins the metaclass version (cells 4..5); both
// versions must match before the cached descriptor is read.
//
// CPython: Python/bytecodes.c LOAD_ATTR_CLASS_WITH_METACLASS_CHECK
func (e *evalState) fastLoadAttrClassWithMetaclassCheck(oparg uint32) (int, bool) {
	cls, ok := e.peek(0).AsObject().(*objects.Type)
	if !ok {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := cls.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	meta := cls.Type()
	if meta == nil {
		return 0, false
	}
	cachedMeta := specialize.LoadMethodMetaVersion(code, idx)
	curMeta := meta.VersionTag()
	if curMeta == 0 || curMeta != cachedMeta {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	e.pushAttrResult(descr, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrMethodNoDict implements LOAD_ATTR_METHOD_NO_DICT.
//
// Guards: oparg requests the unbound-method shape (bit 0 set), owner
// is *Instance with no instance dict, owner's type_version matches
// cells 2..3, descriptor cached at instr is non-nil. On hit pushes
// (descr, self) so the following CALL sees the unbound-method
// (callable, self) shape.
//
// CPython: Python/bytecodes.c LOAD_ATTR_METHOD_NO_DICT
func (e *evalState) fastLoadAttrMethodNoDict(oparg uint32) (int, bool) {
	if oparg&1 == 0 {
		return 0, false
	}
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	if tp.HasDict {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	self := e.pop()
	e.pushObject(descr)
	e.push(self)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrNondescriptorNoDict implements
// LOAD_ATTR_NONDESCRIPTOR_NO_DICT. Class-level non-descriptor on a
// dict-less type; pushes the cached value straight, without the
// unbound-method shape (the arm refuses when oparg requests one).
//
// CPython: Python/bytecodes.c LOAD_ATTR_NONDESCRIPTOR_NO_DICT
func (e *evalState) fastLoadAttrNondescriptorNoDict(oparg uint32) (int, bool) {
	if oparg&1 != 0 {
		return 0, false
	}
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	if tp.HasDict {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	e.pop()
	e.pushObject(descr)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrMethodWithValues implements LOAD_ATTR_METHOD_WITH_VALUES.
//
// Guards: oparg requests the unbound-method shape (bit 0 set), owner
// is *Instance, owner's type_version matches cells 2..3, the type's
// cached_keys_version matches cells 4..5, and inst.InlineValid() is
// still true (no DELETE_ATTR has materialized a separate dict). The
// cached descriptor is the class-level method; on hit pushes (descr,
// self) so the following CALL sees the unbound-method (callable, self)
// shape.
//
// The specializer asserts at stamp time that the name being looked up
// is NOT in the type's cached_keys (CPython:
// Python/specialize.c:1614). Together with the keys_version guard,
// that proves no instance has ever stored an attribute under this
// name, so the load returns the class-level descriptor verbatim.
//
// CPython: Python/bytecodes.c LOAD_ATTR_METHOD_WITH_VALUES
func (e *evalState) fastLoadAttrMethodWithValues(oparg uint32) (int, bool) {
	if oparg&1 == 0 {
		return 0, false
	}
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	if !tp.HasInlineValues() {
		return 0, false
	}
	if !inst.InlineValid() {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	cachedKeys := specialize.LoadMethodKeysVersion(code, idx)
	curKeys := tp.CachedKeysVersion()
	if curKeys == 0 || curKeys != cachedKeys {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	self := e.pop()
	e.pushObject(descr)
	e.push(self)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrNondescriptorWithValues implements
// LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES. Same guards as the METHOD
// variant; pushes only the cached descriptor (oparg&1 == 0, no
// unbound-method shape) and consumes the owner.
//
// CPython: Python/bytecodes.c LOAD_ATTR_NONDESCRIPTOR_WITH_VALUES
func (e *evalState) fastLoadAttrNondescriptorWithValues(oparg uint32) (int, bool) {
	if oparg&1 != 0 {
		return 0, false
	}
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	if !tp.HasInlineValues() {
		return 0, false
	}
	if !inst.InlineValid() {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	cachedKeys := specialize.LoadMethodKeysVersion(code, idx)
	curKeys := tp.CachedKeysVersion()
	if curKeys == 0 || curKeys != cachedKeys {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	e.pop()
	e.pushObject(descr)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrMethodLazyDict implements LOAD_ATTR_METHOD_LAZY_DICT.
//
// Guards: oparg requests the unbound-method shape (bit 0 set), owner
// is *Instance whose type carries MANAGED_DICT without INLINE_VALUES
// (the LAZY_DICT shape for heap subclasses of built-ins), the managed
// dict is still nil (no per-instance store has materialized it), and
// the type_version matches cells 2..3. On hit pushes (descr, self) so
// the following CALL sees the unbound-method (callable, self) shape.
//
// The dict-is-nil guard is gopy's equivalent of CPython's
// _PyManagedDictPointer_GET(owner)->dict != NULL check: the moment
// instanceSetAttr materializes the managed dict, an instance-level
// store could shadow the class descriptor, so the arm deopts and the
// generic LOAD_ATTR path repeats the descriptor lookup.
//
// CPython: Python/bytecodes.c LOAD_ATTR_METHOD_LAZY_DICT
func (e *evalState) fastLoadAttrMethodLazyDict(oparg uint32) (int, bool) {
	if oparg&1 == 0 {
		return 0, false
	}
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	if !tp.HasManagedDict() || tp.HasInlineValues() {
		return 0, false
	}
	if inst.Dict() != nil {
		return 0, false
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	descr := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if descr == nil {
		return 0, false
	}
	self := e.pop()
	e.pushObject(descr)
	e.push(self)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrInstanceValue implements LOAD_ATTR_INSTANCE_VALUE.
//
// Guards: owner is *Instance, owner's type_version matches cells 2..3,
// instance dict's keys_version matches cells 4..5, slot index at
// cell 6 is in range and live. Reads the value out of the instance
// dict without going through GetAttr.
//
// CPython: Python/bytecodes.c LOAD_ATTR_INSTANCE_VALUE
func (e *evalState) fastLoadAttrInstanceValue(oparg uint32) (int, bool) {
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	if curVer := tp.VersionTag(); curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	d := inst.Dict()
	if d == nil {
		return 0, false
	}
	cachedKeys := specialize.LoadMethodKeysVersion(code, idx)
	if curKeys := d.GetKeysVersion(); curKeys == 0 || curKeys != cachedKeys {
		return 0, false
	}
	slot := int(specialize.LoadAttrInstanceValueSlot(code, idx))
	_, value, found := d.EntryAt(slot)
	if !found || value == nil {
		return 0, false
	}
	e.pushAttrResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
}

// fastLoadAttrGetattributeOverridden implements
// LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN. Owner is an instance whose type
// owns a Python __getattribute__ override and has no __getattr__ hook;
// the cached function pointer (CacheObjects[instr]) is invoked with
// (owner, name) so user code observes the attribute fetch.
//
// CPython inlines the user function as a Python frame via
// DISPATCH_INLINED so the resolved descriptor never escapes the
// dispatch loop. gopy can't bounce frames the same way from inside a
// fast arm; we call the function synchronously through objects.Call,
// which still beats the generic LOAD_ATTR path (no descriptor walk,
// no instance-dict lookup, no slot dispatcher).
//
// The arm refuses the unbound-method oparg shape because
// _Py_slot_tp_getattro never produces a (descr, self) pair: the user
// function decides the return shape itself.
//
// CPython: Python/bytecodes.c:2518 LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN
func (e *evalState) fastLoadAttrGetattributeOverridden(oparg uint32) (int, bool, error) {
	if oparg&1 != 0 {
		return 0, false, nil
	}
	owner := e.peek(0).AsObject()
	tp := owner.Type()
	if tp == nil {
		return 0, false, nil
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false, nil
	}
	getattribute := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if getattribute == nil {
		return 0, false, nil
	}
	co := e.f.Code
	nameIdx := int(oparg >> 1)
	if nameIdx < 0 || nameIdx >= len(co.Names) {
		return 0, false, nil
	}
	name := co.NameObj(nameIdx)
	e.pop()
	result, err := objects.Call(getattribute, objects.NewTuple([]objects.Object{owner, name}), nil)
	if err != nil {
		return 0, true, err
	}
	e.pushObject(result)
	return e.cacheAdvance(compile.LOAD_ATTR), true, nil
}

// fastLoadAttrProperty implements LOAD_ATTR_PROPERTY. Owner is an
// instance whose type binds a property descriptor to the requested
// name; the cached fget pointer (CacheObjects[instr]) is invoked
// with the owner. The fast-path arm pops the owner before the call
// so the result lands cleanly on top.
//
// CPython: Python/bytecodes.c LOAD_ATTR_PROPERTY
func (e *evalState) fastLoadAttrProperty(oparg uint32) (int, bool, error) {
	// PROPERTY does not produce the unbound-method shape.
	if oparg&1 != 0 {
		return 0, false, nil
	}
	owner := e.peek(0).AsObject()
	tp := owner.Type()
	if tp == nil {
		return 0, false, nil
	}
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.LoadMethodTypeVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false, nil
	}
	fget := specialize.CacheObject(e.f.Code.CacheObjects, idx)
	if fget == nil {
		return 0, false, nil
	}
	e.pop()
	value, err := objects.CallOneArg(fget, owner)
	if err != nil {
		return 0, true, err
	}
	e.pushObject(value)
	return e.cacheAdvance(compile.LOAD_ATTR), true, nil
}
