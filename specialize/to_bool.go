// TO_BOOL family specializer.
//
// _Py_Specialize_ToBool walks the single operand and rewrites TO_BOOL
// into one of the per-type variants when the operand has a known
// shape. The user-class arm stamps the type's tp_version_tag into the
// inline cache so the dispatch loop can deopt on shape change.
//
// CPython: Python/specialize.c:3034 _Py_Specialize_ToBool

package specialize

// DEPRECATED (spec 1714): Spec 1714 phases 3+4: raw cache writes migrate to typed accessors; family/deopt literals move to specialize/family_gen.go. File shrinks to specialize-policy.
// See website/docs/specs/1700/1714_bytecodes_dsl_codegen.md.

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
	if t := objects.ExactType(value); t != nil && t.IsUser {
		// CPython's check_type_always_true rejects any class that
		// exposes nb_bool, mp_length, or sq_length: those are the
		// hooks the abstract layer asks first when computing truthiness,
		// and a user-defined __bool__ / __len__ may return False.
		// gopy installs Number.Bool when __bool__ is defined and
		// Mapping.Length / Sequence.Length when __len__ is defined
		// (see objects/usertype.go), so the same triple of slot
		// checks tells us whether always-true is safe.
		//
		// CPython: Objects/object.c:1505 check_type_always_true
		if (t.Number != nil && t.Number.Bool != nil) ||
			(t.Mapping != nil && t.Mapping.Length != nil) ||
			(t.Sequence != nil && t.Sequence.Length != nil) {
			Unspecialize(code, instr)
			return
		}
		version := t.VersionTag()
		if version == 0 {
			Unspecialize(code, instr)
			return
		}
		toBoolCacheAt(code, instr).setVersion(version)
		Specialize(code, instr, compile.TO_BOOL_ALWAYS_TRUE)
		return
	}
	Unspecialize(code, instr)
}
