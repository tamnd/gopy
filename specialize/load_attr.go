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

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// LoadAttr specializes the LOAD_ATTR at instr based on the owner and
// the attribute name being loaded.
//
// CPython: Python/specialize.c:1344 _Py_Specialize_LoadAttr
func LoadAttr(owner objects.Object, name *objects.Unicode, co *objects.Code, instr int) {
	code := co.Code
	switch v := owner.(type) {
	case *objects.Module:
		if specializeModuleLoadAttr(v, name, code, instr) {
			return
		}
	case *objects.Type:
		if specializeClassLoadAttr(v, name, co, instr) {
			return
		}
	case *objects.Instance:
		if specializeInstanceLoadAttr(v, name, co, instr) {
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
	c := attrCacheAt(code, instr)
	c.setVersion(version)
	c.setIndex(uint16(idx))
	Specialize(code, instr, compile.LOAD_ATTR_MODULE)
	return true
}

// specializeClassLoadAttr handles `cls.attr` where cls is a type
// object. Stamps type_version (cells 2..3) and picks between
// LOAD_ATTR_CLASS and LOAD_ATTR_CLASS_WITH_METACLASS_CHECK based on
// metaclass mutability — when the metaclass is user-defined the
// fast-path arm has to revalidate the metaclass version on every hit,
// so the cache also stamps meta_version into cells 4..5.
//
// CPython: Python/specialize.c:1513 specialize_class_load_attr
func specializeClassLoadAttr(cls *objects.Type, name *objects.Unicode, co *objects.Code, instr int) bool {
	code := co.Code
	descr, _ := objects.LookupDescriptor(cls, name.Value())
	if descr == nil {
		return false
	}
	// CPython: Python/specialize.c:1557 — only METHOD / NON_DESCRIPTOR
	// kinds are safe to push from the cache verbatim. Anything that
	// needs a __get__ binding (classmethod, property, slot, …) falls
	// through to the generic LOAD_ATTR body.
	switch ClassifyDescriptor(descr) {
	case KindMethod, KindNonDescriptor:
	default:
		return false
	}
	version := cls.VersionTag()
	if version == 0 {
		return false
	}
	// CPython gates METACLASS_CHECK on the metaclass missing
	// Py_TPFLAGS_IMMUTABLETYPE. gopy's stand-in is Type.IsUser: a
	// heap-allocated user-defined type is mutable, a builtin is not.
	meta := cls.Type()
	if meta != nil && meta.IsUser {
		metaVersion := meta.VersionTag()
		if metaVersion == 0 {
			return false
		}
		lm := loadMethodCacheAt(code, instr)
		lm.setTypeVersion(version)
		lm.setMetaVersion(metaVersion)
		SetCacheObject(co.CacheObjects, instr, descr)
		Specialize(code, instr, compile.LOAD_ATTR_CLASS_WITH_METACLASS_CHECK)
		return true
	}
	loadMethodCacheAt(code, instr).setTypeVersion(version)
	SetCacheObject(co.CacheObjects, instr, descr)
	Specialize(code, instr, compile.LOAD_ATTR_CLASS)
	return true
}

// specializeGetattributeOverridden emits LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN
// when the user type owns a Python __getattribute__ and no __getattr__
// fallback is present. Returns true when the cache was stamped, false
// when the caller should keep classifying.
//
// Cache layout (the _PyLoadMethodCache shape used by ATTR_PROPERTY too):
//
//	cells 2..3 type_version
//	cells 4..5 unused (CPython stamps func_version; gopy doesn't track
//	            per-function versions yet so we leave them zero and rely on
//	            type_version invalidation via the descriptor mutation path)
//	cells 6..9 cached Python function pointer (in the CacheObjects slab)
//
// CPython: Python/specialize.c:1260 GETATTRIBUTE_IS_PYTHON_FUNCTION
func specializeGetattributeOverridden(tp *objects.Type, version uint32, co *objects.Code, code []byte, instr int) bool {
	ga, owner := objects.LookupDescriptor(tp, "__getattribute__")
	if ga == nil || owner == nil || owner == objects.ObjectType() {
		return false
	}
	fn, ok := ga.(*objects.Function)
	if !ok {
		return false
	}
	// Bail when __getattr__ exists too: slot_tp_getattr_hook has to
	// fall back on AttributeError, and we don't emit a hookful arm.
	if gx, _ := objects.LookupDescriptor(tp, "__getattr__"); gx != nil {
		return false
	}
	loadMethodCacheAt(code, instr).setTypeVersion(version)
	SetCacheObject(co.CacheObjects, instr, fn)
	Specialize(code, instr, compile.LOAD_ATTR_GETATTRIBUTE_OVERRIDDEN)
	return true
}

// specializeInstanceLoadAttr handles `obj.attr` when obj is a user
// instance. Fans out by descriptor kind (ClassifyDescriptor): slot
// reads → LOAD_ATTR_SLOT, property getters → LOAD_ATTR_PROPERTY,
// method-like attributes on a dict-less type → LOAD_ATTR_METHOD_NO_DICT,
// plain class-attribute reads on a dict-less type →
// LOAD_ATTR_NONDESCRIPTOR_NO_DICT, and an absent descriptor falls
// through to the dict-access panel that picks LOAD_ATTR_INSTANCE_VALUE
// vs LOAD_ATTR_WITH_HINT.
//
// gopy does not yet model Py_TPFLAGS_INLINE_VALUES or the lazy
// managed-dict offset, so the *_WITH_VALUES and METHOD_LAZY_DICT
// variants are deliberately unreachable here. Those land alongside an
// object-model extension; see spec 1712 P1.4.
//
// CPython: Python/specialize.c:1330 specialize_instance_load_attr
// CPython: Python/specialize.c:1146 do_specialize_instance_load_attr
func specializeInstanceLoadAttr(inst *objects.Instance, name *objects.Unicode, co *objects.Code, instr int) bool {
	code := co.Code
	tp := inst.Type()
	version := tp.VersionTag()
	if version == 0 {
		return false
	}
	// GETATTRIBUTE_IS_PYTHON_FUNCTION arm: the user class overrides
	// __getattribute__ with a plain Python function. CPython gates on
	// tp_getattro == _Py_slot_tp_getattr_hook, no __getattr__ present,
	// and the descriptor being PyFunction_Type. gopy detects the same
	// situation by checking that LookupDescriptor(__getattribute__) is
	// not owned by objectType and is a *Function, and that
	// LookupDescriptor(__getattr__) is absent (so a fallback hook isn't
	// involved). Specialization fails when oparg requests the
	// unbound-method shape, matching CPython's SPEC_FAIL_ATTR_METHOD.
	//
	// CPython: Python/specialize.c:1260 GETATTRIBUTE_IS_PYTHON_FUNCTION
	if name.Value() != "__getattribute__" && name.Value() != "__getattr__" {
		if ga := specializeGetattributeOverridden(tp, version, co, code, instr); ga {
			return true
		}
	}
	descr, _ := objects.LookupDescriptor(tp, name.Value())
	kind := ClassifyDescriptor(descr)
	switch kind {
	case KindObjectSlot:
		m := descr.(*objects.MemberDescr)
		idx := m.Index()
		if idx < 0 || idx > 0xFFFF {
			return false
		}
		c := attrCacheAt(code, instr)
		c.setVersion(version)
		c.setIndex(uint16(idx))
		Specialize(code, instr, compile.LOAD_ATTR_SLOT)
		return true
	case KindProperty:
		// CPython: Python/specialize.c:1180-1216 PROPERTY case in
		// do_specialize_instance_load_attr. The cache writes
		// type_version (cells 2-3) and the fget pointer; we stash
		// the property's fget in the parallel CacheObjects slab so
		// the fast-path arm reads it back without re-resolving.
		p := descr.(*objects.Property)
		if p.Fget() == nil {
			return false
		}
		loadMethodCacheAt(code, instr).setTypeVersion(version)
		SetCacheObject(co.CacheObjects, instr, p.Fget())
		Specialize(code, instr, compile.LOAD_ATTR_PROPERTY)
		return true
	case KindMethod:
		// CPython: Python/specialize.c:1162-1179 METHOD case → routes
		// through specialize_attr_loadclassattr which picks
		// METHOD_NO_DICT when tp_dictoffset == 0. gopy expresses
		// "no instance dict" as Type.HasDict == false.
		if tp.HasDict {
			return false
		}
		loadMethodCacheAt(code, instr).setTypeVersion(version)
		SetCacheObject(co.CacheObjects, instr, descr)
		Specialize(code, instr, compile.LOAD_ATTR_METHOD_NO_DICT)
		return true
	case KindNonDescriptor:
		// CPython: Python/specialize.c:1300-1311 NON_DESCRIPTOR case →
		// LOAD_ATTR_NONDESCRIPTOR_NO_DICT when tp_dictoffset == 0 and
		// the attribute is not requested as a bound method.
		if tp.HasDict {
			return false
		}
		loadMethodCacheAt(code, instr).setTypeVersion(version)
		SetCacheObject(co.CacheObjects, instr, descr)
		Specialize(code, instr, compile.LOAD_ATTR_NONDESCRIPTOR_NO_DICT)
		return true
	case KindAbsent:
		// fall through to dict-access panel below
	default:
		// MUTABLE / OVERRIDING / OTHER_SLOT / NON_OVERRIDING /
		// classmethod variants / GETSET_OVERRIDDEN: gopy cannot
		// faithfully specialize these yet, so fail out and let the
		// generic LOAD_ATTR body handle them.
		return false
	}
	// INSTANCE_VALUE panel. CPython reads inline values from a known
	// offset (Py_TPFLAGS_INLINE_VALUES); gopy reads the instance dict
	// by cached slot. Both type_version and the dict's keys_version
	// are stamped so the arm rejects on either a type mutation or a
	// dict rehash. Layout: cells 2..3 type_version, cells 4..5
	// keys_version, cell 6 slot index.
	//
	// CPython: Python/specialize.c specialize_dict_access INSTANCE_VALUE arm.
	d := inst.Dict()
	if d == nil {
		return false
	}
	idx, found := d.LookupString(name)
	if !found || idx == objects.DictKeyAbsent {
		// WITH_HINT in CPython covers absent / hinted keys via the
		// managed-dict offset. gopy can't model that yet; bail so the
		// generic LOAD_ATTR runs.
		return false
	}
	if idx > 0xFFFF {
		return false
	}
	keysVer := d.GetKeysVersion()
	if keysVer == 0 {
		return false
	}
	lm := loadMethodCacheAt(code, instr)
	lm.setTypeVersion(version)
	lm.setKeysVersion(keysVer)
	writeCell(code, instr, 6, uint16(idx)) // slot index lives in the descr quadword
	Specialize(code, instr, compile.LOAD_ATTR_INSTANCE_VALUE)
	return true
}
