// LOAD_ATTR family specializer.
//
// _Py_Specialize_LoadAttr fans out by owner kind: modules go through
// specialize_module_load_attr, types through specialize_class_load_attr,
// and everything else through specialize_instance_load_attr. CPython
// also handles property / classmethod / METHOD descriptors that need
// the function-version cache and the deferred-refcount check; gopy
// covers the subset its descriptor model can express today: module
// dict access, class attribute lookup, instance slot reads, and
// instance __dict__ reads with and without a populated key.
//
// Cache layout (9 codeunits, the _PyLoadMethodCache shape):
//   cell 1   counter
//   cells 2-3 type_version (u32)
//   cells 4-5 keys_version (u32) (or `index` at cell 4 for the module
//             arm, which uses the smaller _PyAttrCache layout)
//   cells 6-9 descr pointer (8 bytes)
//
// CPython: Python/specialize.c:1344 _Py_Specialize_LoadAttr

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// LoadAttr specializes the LOAD_ATTR at instr based on the owner and
// the attribute name being loaded.
//
// CPython: Python/specialize.c:1344 _Py_Specialize_LoadAttr
func LoadAttr(owner objects.Object, name *objects.Unicode, code []byte, instr int) {
	switch v := owner.(type) {
	case *objects.Module:
		if specializeModuleLoadAttr(v, name, code, instr) {
			return
		}
	case *objects.Type:
		if specializeClassLoadAttr(v, name, code, instr) {
			return
		}
	case *objects.Instance:
		if specializeInstanceLoadAttr(v, name, code, instr) {
			return
		}
	}
	Unspecialize(code, instr)
}

// specializeModuleLoadAttr handles `module.attr` when the module dict
// is unicode-keyed and does not define a PEP 562 __getattr__. Stamps
// keys_version (cells 2..3) and the dict slot index (cell 4).
//
// CPython: Python/specialize.c:773 specialize_module_load_attr_lock_held
func specializeModuleLoadAttr(m *objects.Module, name *objects.Unicode, code []byte, instr int) bool {
	d := m.Dict()
	if d == nil || !d.IsKeysUnicode() {
		return false
	}
	// PEP 562: a module-level __getattr__ shadows direct dict access.
	getattrName := objects.NewStr("__getattr__").(*objects.Unicode)
	if gidx, gok := d.LookupString(getattrName); gok && gidx != objects.DictKeyAbsent {
		return false
	}
	idx, ok := d.LookupString(name)
	if !ok || idx == objects.DictKeyAbsent || idx > 0xFFFF {
		return false
	}
	version := d.GetKeysVersion()
	if version == 0 {
		return false
	}
	SetCacheU32(code, instr, 2, version)
	SetCacheCell(code, instr, 4, uint16(idx))
	Specialize(code, instr, compile.LOAD_ATTR_MODULE)
	return true
}

// specializeClassLoadAttr handles `cls.attr` where cls is a type
// object. Specializes to LOAD_ATTR_CLASS when the descriptor lookup
// returns a value the metaclass would not intercept. Stamps
// type_version (cells 2..3); the descr pointer slots are left zero
// because gopy does not chase pointers through the cache yet.
//
// CPython: Python/specialize.c:1513 specialize_class_load_attr
func specializeClassLoadAttr(cls *objects.Type, name *objects.Unicode, code []byte, instr int) bool {
	descr, _ := objects.LookupDescriptor(cls, name.Value())
	if descr == nil {
		return false
	}
	version := cls.VersionTag()
	if version == 0 {
		return false
	}
	SetCacheU32(code, instr, 2, version)
	Specialize(code, instr, compile.LOAD_ATTR_CLASS)
	return true
}

// specializeInstanceLoadAttr handles `obj.attr` when obj is a user
// instance. Picks LOAD_ATTR_SLOT for MemberDescr, LOAD_ATTR_INSTANCE_VALUE
// when the name is already in __dict__, and LOAD_ATTR_WITH_HINT when
// the dict exists but the name is absent.
//
// CPython: Python/specialize.c:1330 specialize_instance_load_attr
func specializeInstanceLoadAttr(inst *objects.Instance, name *objects.Unicode, code []byte, instr int) bool {
	tp := inst.Type()
	version := tp.VersionTag()
	if version == 0 {
		return false
	}
	descr, _ := objects.LookupDescriptor(tp, name.Value())
	if m, ok := descr.(*objects.MemberDescr); ok {
		idx := m.Index()
		if idx < 0 || idx > 0xFFFF {
			return false
		}
		SetCacheU32(code, instr, 2, version)
		SetCacheCell(code, instr, 4, uint16(idx))
		Specialize(code, instr, compile.LOAD_ATTR_SLOT)
		return true
	}
	// Any descriptor with a getter that is not a MemberDescr is left
	// to the generic path - gopy does not yet model the
	// property / classmethod / function arms.
	if descr != nil {
		return false
	}
	d := inst.Dict()
	if d == nil {
		return false
	}
	idx, ok := d.LookupString(name)
	if !ok {
		return false
	}
	SetCacheU32(code, instr, 2, version)
	if idx == objects.DictKeyAbsent {
		SetCacheCell(code, instr, 4, 0)
		Specialize(code, instr, compile.LOAD_ATTR_WITH_HINT)
		return true
	}
	if idx > 0xFFFF {
		return false
	}
	SetCacheCell(code, instr, 4, uint16(idx))
	Specialize(code, instr, compile.LOAD_ATTR_INSTANCE_VALUE)
	return true
}
