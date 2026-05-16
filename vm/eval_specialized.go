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
	case compile.LOAD_ATTR_PROPERTY:
		return e.fastLoadAttrProperty(oparg)
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
	cachedVer := specialize.CacheU32(code, idx, 2)
	curVer := d.GetKeysVersion()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.CacheCell(code, idx, 4))
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
	cachedVer := specialize.CacheU32(code, idx, 2)
	curVer := inst.Type().VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.CacheCell(code, idx, 4))
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
	cachedVer := specialize.CacheU32(code, idx, 2)
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
	cachedVer := specialize.CacheU32(code, idx, 2)
	curVer := cls.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	meta := cls.Type()
	if meta == nil {
		return 0, false
	}
	cachedMeta := specialize.CacheU32(code, idx, 4)
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
	cachedVer := specialize.CacheU32(code, idx, 2)
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
	cachedVer := specialize.CacheU32(code, idx, 2)
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
	cachedVer := specialize.CacheU32(code, idx, 2)
	if curVer := tp.VersionTag(); curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	d := inst.Dict()
	if d == nil {
		return 0, false
	}
	cachedKeys := specialize.CacheU32(code, idx, 4)
	if curKeys := d.GetKeysVersion(); curKeys == 0 || curKeys != cachedKeys {
		return 0, false
	}
	slot := int(specialize.CacheCell(code, idx, 6))
	_, value, found := d.EntryAt(slot)
	if !found || value == nil {
		return 0, false
	}
	e.pushAttrResult(value, oparg)
	return e.cacheAdvance(compile.LOAD_ATTR), true
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
	cachedVer := specialize.CacheU32(code, idx, 2)
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
