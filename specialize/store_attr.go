// STORE_ATTR family specializer.
//
// _Py_Specialize_StoreAttr classifies the descriptor (if any) on the
// owner's type for the given name and rewrites STORE_ATTR into the
// matching specialized arm. CPython has three live destinations:
//
//   - STORE_ATTR_SLOT          the type defines a writable member
//                              (PyMemberDescr_Type with offset)
//   - STORE_ATTR_INSTANCE_VALUE the value lives at a known dict slot
//                              that is already populated
//   - STORE_ATTR_WITH_HINT      the value lives in __dict__ but the
//                              cache only holds a hint
//
// gopy mirrors the same shape: MemberDescr maps to STORE_ATTR_SLOT,
// names already present in the instance __dict__ map to
// STORE_ATTR_INSTANCE_VALUE, and other writable names map to
// STORE_ATTR_WITH_HINT.
//
// CPython: Python/specialize.c:1376 _Py_Specialize_StoreAttr

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// StoreAttr specializes the STORE_ATTR at instr based on the owner
// and the attribute name being stored. The cache layout is 4 codeunits:
// counter at cell 1, type version uint32 at cells 2..3, index uint16
// at cell 4.
//
// CPython: Python/specialize.c:1376 _Py_Specialize_StoreAttr
func StoreAttr(owner objects.Object, name *objects.Unicode, code []byte, instr int) {
	inst, ok := owner.(*objects.Instance)
	if !ok {
		Unspecialize(code, instr)
		return
	}
	tp := inst.Type()
	version := tp.VersionTag()
	if version == 0 {
		Unspecialize(code, instr)
		return
	}
	descr, _ := objects.LookupDescriptor(tp, name.Value())
	if descr != nil {
		// Slot writes to a fixed offset on the instance.
		if m, ok := descr.(*objects.MemberDescr); ok {
			idx := m.Index()
			if idx < 0 || idx > 0xFFFF {
				Unspecialize(code, instr)
				return
			}
			SetCacheU32(code, instr, 2, version)
			SetCacheCell(code, instr, 4, uint16(idx))
			Specialize(code, instr, compile.STORE_ATTR_SLOT)
			return
		}
		// Any other descriptor with a setter (property, getset,
		// overriding) bails out. CPython's analyze_descriptor_store
		// classifies these as OVERRIDING / METHOD / PROPERTY etc.
		if descr.Type().DescrSet != nil {
			Unspecialize(code, instr)
			return
		}
	}
	// No data descriptor on the type - the store goes to __dict__.
	d := inst.Dict()
	if d == nil {
		Unspecialize(code, instr)
		return
	}
	idx, ok2 := d.LookupString(name)
	if !ok2 {
		Unspecialize(code, instr)
		return
	}
	SetCacheU32(code, instr, 2, version)
	if idx == objects.DictKeyAbsent {
		SetCacheCell(code, instr, 4, 0)
		Specialize(code, instr, compile.STORE_ATTR_WITH_HINT)
		return
	}
	if idx > 0xFFFF {
		Unspecialize(code, instr)
		return
	}
	SetCacheCell(code, instr, 4, uint16(idx))
	Specialize(code, instr, compile.STORE_ATTR_INSTANCE_VALUE)
}
