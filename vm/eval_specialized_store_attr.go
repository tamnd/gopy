// Fast-path arms for the STORE_ATTR family.
//
// Cache layout (4 codeunits per pycore_code.h _PyStoreAttrCache):
//   cell 0..1: counter
//   cell 2..3: owner type_version (uint32)
//   cell 4:    member-slot index (SLOT) or dict entry hint
//             (INSTANCE_VALUE / WITH_HINT — not yet shipped, see notes)
//
// Stack at entry: [value, owner] (owner on top). After a successful
// hit both are popped; STORE_ATTR pushes nothing.
//
// CPython: Python/bytecodes.c STORE_ATTR_SLOT

package vm

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
// CPython: Python/bytecodes.c STORE_ATTR_SLOT
func (e *evalState) fastStoreAttrSlot(_ uint32) (int, bool) {
	inst, isInst := e.peek(0).AsObject().(*objects.Instance)
	if !isInst {
		return 0, false
	}
	tp := inst.Type()
	idx := e.instrIdx()
	code := e.f.Code.Code
	cachedVer := specialize.CacheU32(code, idx, 2)
	curVer := tp.VersionTag()
	if curVer == 0 || curVer != cachedVer {
		return 0, false
	}
	slot := int(specialize.CacheCell(code, idx, 4))
	value := e.peek(1).AsObject()
	if !inst.SetSlotAt(slot, value) {
		return 0, false
	}
	e.pop()
	e.pop()
	return e.cacheAdvance(compile.STORE_ATTR), true
}
