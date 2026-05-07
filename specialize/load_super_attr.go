// LOAD_SUPER_ATTR family specializer.
//
// _Py_Specialize_LoadSuperAttr only fires when the global `super` is
// the unshadowed builtin and the second operand is an actual type
// object. The choice between the METHOD and ATTR variant comes from
// the loadMethod flag the bytecode passes through.
//
// CPython: Python/specialize.c:827 _Py_Specialize_LoadSuperAttr

package specialize

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// LoadSuperAttr specializes the LOAD_SUPER_ATTR at instr. globalSuper
// is the value the bytecode just looked up under the name `super`;
// cls is the second positional argument. loadMethod mirrors
// LOAD_SUPER_ATTR's "is this for a method call?" flag.
//
// CPython: Python/specialize.c:827 _Py_Specialize_LoadSuperAttr
func LoadSuperAttr(globalSuper, cls objects.Object, code []byte, instr int, loadMethod bool) {
	// `super` must still resolve to the builtin super type. Anything
	// else is a shadowed binding and CPython refuses to specialize.
	if globalSuper != objects.Object(objects.SuperType) {
		Unspecialize(code, instr)
		return
	}
	if _, ok := cls.(*objects.Type); !ok {
		Unspecialize(code, instr)
		return
	}
	if loadMethod {
		Specialize(code, instr, compile.LOAD_SUPER_ATTR_METHOD)
		return
	}
	Specialize(code, instr, compile.LOAD_SUPER_ATTR_ATTR)
}
