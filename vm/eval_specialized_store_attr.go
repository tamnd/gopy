// Fast-path arms for the STORE_ATTR family.
//
// Cache layout (4 codeunits per pycore_code.h _PyStoreAttrCache):
//   cell 0..1: counter
//   cell 2..3: owner type_version (uint32)
//   cell 4:    member-slot index (SLOT) or dict entry index
//             (INSTANCE_VALUE / WITH_HINT)
//
// Stack at entry: [value, owner] (owner on top). After a successful
// hit both are popped; STORE_ATTR pushes nothing.
//
// CPython: Python/bytecodes.c STORE_ATTR_SLOT / STORE_ATTR_INSTANCE_VALUE / STORE_ATTR_WITH_HINT

package vm

// DEPRECATED (spec 1714): Spec 1714 phase 6: STORE_ATTR_* arms migrate to typed op<NAME> bodies; cache decode/deopt/advance live in the generated harness.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/specialize"
)

// fastStoreAttrSlot implements STORE_ATTR_SLOT: validate the owner's
// type_version then write directly into the cached member-slot. This
// is a faithful 1-1 port of CPython's macro: no descriptor walk, no
// dict touch, just the slot write.
//
// CPython: Python/bytecodes.c:2628 _STORE_ATTR_SLOT
func (e *evalState) fastStoreAttrSlot(_ uint32) (int, bool) {
	inst, isInst := e.peek(0).AsObject().(*objects.Instance)
	if !isInst {
		return 0, false
	}
	tp := inst.Type()
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.AttrCacheVersion(code, idx)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.AttrCacheIndex(code, idx))
	value := e.peek(1).AsObject()
	if !inst.SetSlotAt(slot, value) {
		return 0, false
	}
	// Stack is (value, owner) with owner on top. SetSlotAt stole the
	// value's stack reference into the slot, so the value entry is
	// dropped without a close; the owner reference is released.
	// CPython: _STORE_ATTR_SLOT closes owner after stealing value.
	owner := e.pop()
	e.pop()
	owner.Close()
	return e.cacheAdvance(compile.STORE_ATTR), true
}

// fastStoreAttrInstanceValue implements STORE_ATTR_INSTANCE_VALUE.
//
// Guards: owner is *Instance, owner's type_version matches cells 2..3,
// instance dict is allocated, the cached slot index at cell 4 still
// names a live unicode entry whose key matches co_names[oparg]. The
// slot-key string compare is the runtime safety check that mirrors
// CPython's _STORE_ATTR_WITH_HINT key validation: the STORE_ATTR
// inline cache only stamps type_version (not keys_version), so a
// delete + re-insert on the instance dict could leave the cached
// slot pointing at a stale name. CPython skips the key compare on
// INSTANCE_VALUE because the inline-values layout is fixed by the
// type (Py_TPFLAGS_INLINE_VALUES); gopy stores attributes directly
// in the instance dict so it needs the check.
//
// CPython: Python/bytecodes.c:2559 _STORE_ATTR_INSTANCE_VALUE
func (e *evalState) fastStoreAttrInstanceValue(oparg uint32) (int, bool) {
	inst, ok := e.peek(0).AsObject().(*objects.Instance)
	if !ok {
		return 0, false
	}
	tp := inst.Type()
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.AttrCacheVersion(code, idx)
	if curVer := tp.VersionTag(); curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	d := inst.Dict()
	if d == nil {
		return 0, false
	}
	co := e.f.Code
	nameIdx := int(oparg)
	if nameIdx < 0 || nameIdx >= len(co.Names) {
		return 0, false
	}
	slot := int(specialize.AttrCacheIndex(code, idx))
	value := e.peek(1).AsObject()
	if !d.StoreEntryAtName(slot, co.Names[nameIdx], value) {
		return 0, false
	}
	// Stack is (value, owner) with owner on top. StoreEntryAtName stole
	// the value's stack reference into the dict slot, so the value entry
	// is dropped without a close; the owner reference is released.
	// CPython: _STORE_ATTR_INSTANCE_VALUE/_WITH_HINT close owner after
	// stealing value and decref'ing any previous slot value.
	owner := e.pop()
	e.pop()
	owner.Close()
	return e.cacheAdvance(compile.STORE_ATTR), true
}

// fastStoreAttrWithHint implements STORE_ATTR_WITH_HINT.
//
// In CPython, INSTANCE_VALUE (inline-values mode) and WITH_HINT
// (materialized managed dict) write to different storage. gopy
// stores every instance attribute in the instance dict, so both
// arms do the same dict-slot write here. They stay separate opcodes
// so the specializer's classification matches CPython 1:1 and the
// deopt counters track each path independently.
//
// CPython: Python/bytecodes.c:2583 _STORE_ATTR_WITH_HINT
func (e *evalState) fastStoreAttrWithHint(oparg uint32) (int, bool) {
	return e.fastStoreAttrInstanceValue(oparg)
}
