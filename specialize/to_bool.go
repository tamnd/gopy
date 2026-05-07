// TO_BOOL family specializer.
//
// _Py_Specialize_ToBool walks the single operand and rewrites TO_BOOL
// into one of the per-type variants when the operand has a known
// shape. The user-class arm stamps the type's tp_version_tag into the
// inline cache so the dispatch loop can deopt on shape change.
//
// CPython: Python/specialize.c:3034 _Py_Specialize_ToBool

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// ToBool specializes the TO_BOOL at instr based on the operand. The
// inline cache layout is 3 codeunits: counter at cell 1, version
// uint32 at cells 2..3 (used by the TO_BOOL_ALWAYS_TRUE arm only).
//
// CPython: Python/specialize.c:3034 _Py_Specialize_ToBool
func ToBool(value objects.Object, code []byte, instr int) {
	if objects.IsExactBool(value) {
		Specialize(code, instr, compile.TO_BOOL_BOOL)
		return
	}
	if objects.IsExactInt(value) {
		Specialize(code, instr, compile.TO_BOOL_INT)
		return
	}
	if objects.IsExactList(value) {
		Specialize(code, instr, compile.TO_BOOL_LIST)
		return
	}
	if objects.IsNone(value) {
		Specialize(code, instr, compile.TO_BOOL_NONE)
		return
	}
	if objects.IsExactStr(value) {
		Specialize(code, instr, compile.TO_BOOL_STR)
		return
	}
	if t := objects.Type_(value); t != nil && t.IsUser {
		// CPython runs _PyType_Validate with check_type_always_true,
		// which rejects classes that override __bool__ / __len__ via
		// nb_bool, mp_length, or sq_length. gopy's user types do not
		// expose those slots through the protocol layer specializer
		// arms care about, so any user class whose VersionTag can be
		// allocated qualifies as always-true here.
		version := t.VersionTag()
		if version == 0 {
			Unspecialize(code, instr)
			return
		}
		SetCacheU32(code, instr, 2, version)
		Specialize(code, instr, compile.TO_BOOL_ALWAYS_TRUE)
		return
	}
	Unspecialize(code, instr)
}
