// Fast-path arms for the LOAD_SUPER_ATTR family.
//
// LOAD_SUPER_ATTR's oparg packs three things: bit 0 = load_method
// (controls whether the trailing CALL sees an unbound-method pair),
// bit 1 = has_self (super was constructed with two args), bits 2+ =
// name index into co.Names. Both fast arms still expect the
// (global_super, class, self) tuple on the stack; the specializer
// only fires when global_super is the unshadowed builtin super and
// class is an actual type, so the arms re-check those invariants
// and deopt to the generic body if they shift.
//
// CPython: Python/bytecodes.c:2221 LOAD_SUPER_ATTR_ATTR
// CPython: Python/bytecodes.c:2237 LOAD_SUPER_ATTR_METHOD

package vm

import (
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
)

// superAttrPrelude is the shared guard/setup for the two LOAD_SUPER_ATTR
// fast arms. It walks the (global_super, class, self) tuple, verifies
// the specialize-time invariants still hold, and returns the resolved
// name from co.Names[oparg>>2]. ok=false means the arm must deopt.
//
// CPython: Python/bytecodes.c LOAD_SUPER_ATTR_<x> DEOPT_IF block.
func (e *evalState) superAttrPrelude(oparg uint32) (cls, self objects.Object, name string, ok bool) {
	if e.peek(2).AsObject() != objects.Object(objects.SuperType) {
		return nil, nil, "", false
	}
	clsObj := e.peek(1).AsObject()
	clsType, isType := clsObj.(*objects.Type)
	if !isType {
		return nil, nil, "", false
	}
	self = e.peek(0).AsObject()
	co := e.f.Code
	idx := int(oparg >> 2)
	if idx < 0 || idx >= len(co.Names) {
		return nil, nil, "", false
	}
	return clsType, self, co.Names[idx], true
}

// fastLoadSuperAttrAttr implements LOAD_SUPER_ATTR_ATTR. Pops the
// (super, class, self) tuple, runs _PySuper_Lookup without the
// method-found probe, and pushes the resolved attribute. On
// AttributeError we surface the error so the loop unwinds; on guard
// miss we return ok=false so the dispatcher falls through to the
// generic body.
//
// CPython: Python/bytecodes.c:2221 LOAD_SUPER_ATTR_ATTR
func (e *evalState) fastLoadSuperAttrAttr(oparg uint32) (int, bool, error) {
	// ATTR is only emitted when load_method (bit 0) is clear; refuse
	// to take the fast path otherwise, matching CPython's
	// `assert(!(oparg & 1))`.
	if oparg&1 != 0 {
		return 0, false, nil
	}
	cls, self, name, ok := e.superAttrPrelude(oparg)
	if !ok {
		return 0, false, nil
	}
	suType := cls.(*objects.Type)
	attr, err := objects.SuperLookup(suType, self, name, nil)
	if err != nil {
		return 0, true, err
	}
	e.pop()
	e.pop()
	e.pop()
	e.pushObject(attr)
	return e.cacheAdvance(compile.LOAD_SUPER_ATTR), true, nil
}

// fastLoadSuperAttrMethod implements LOAD_SUPER_ATTR_METHOD. Same
// shape as the ATTR arm but with the method_found probe: when the
// resolved descriptor is method-like, push (descr, self) so a
// following CALL sees the unbound-method pair; otherwise push
// (attr, NULL) so CALL falls through to the generic call path.
//
// CPython: Python/bytecodes.c:2237 LOAD_SUPER_ATTR_METHOD
func (e *evalState) fastLoadSuperAttrMethod(oparg uint32) (int, bool, error) {
	// METHOD is only emitted when load_method (bit 0) is set; refuse
	// to take the fast path otherwise, matching CPython's
	// `assert(oparg & 1)`.
	if oparg&1 == 0 {
		return 0, false, nil
	}
	cls, self, name, ok := e.superAttrPrelude(oparg)
	if !ok {
		return 0, false, nil
	}
	suType := cls.(*objects.Type)
	var methodFound bool
	// CPython only fills the method-found out-param when the self's
	// type uses the default tp_getattro: Objects/typeobject.c:12004
	// `_PySuper_Lookup(..., Py_TYPE(self)->tp_getattro == PyObject_GenericGetAttr ? &method_found : NULL)`.
	// gopy's equivalent test is that the type does not override
	// Getattro. When the override is present we resolve without the
	// probe and push (attr, NULL), matching the bound-method branch.
	var probe *bool
	if self != nil && self.Type() != nil && self.Type().Getattro == nil {
		probe = &methodFound
	}
	attr, err := objects.SuperLookup(suType, self, name, probe)
	if err != nil {
		return 0, true, err
	}
	// Stack layout entering the arm is (super, cls, self) with self at
	// TOS; pop self first so we can preserve its stackref for the
	// unbound-method pair below.
	selfRef := e.pop()
	e.pop()
	e.pop()
	e.pushObject(attr)
	if methodFound {
		// Unbound-method pair: push self above the attr so the
		// following CALL trampoline reads (callable=attr, self).
		e.push(selfRef)
	} else {
		e.push(stackref.Null)
	}
	return e.cacheAdvance(compile.LOAD_SUPER_ATTR), true, nil
}
