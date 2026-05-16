// Descriptor classifier mirroring CPython's classify_descriptor enum.
// The LOAD_ATTR specializer fans out by descriptor kind to pick the
// right specialized opcode; classify is the single place that maps a
// resolved type-attribute (from objects.LookupDescriptor) into one of
// the enum values CPython's specialize_instance_load_attr switches on.
//
// gopy's object model is narrower than CPython's so several enum
// values are unreachable (no INLINE_VALUES, no managed dict offsets,
// no classmethod_descriptor / Py_TPFLAGS_METHOD_DESCRIPTOR fan-out).
// The classifier still returns the matching CPython kind where it can;
// the caller fails out on the kinds gopy cannot model and falls back to
// Unspecialize.
//
// CPython: Python/specialize.c:849 DescriptorClassification
// CPython: Python/specialize.c:867 classify_descriptor

package specialize

import "github.com/tamnd/gopy/objects"

// DescrKind enumerates the descriptor categories the LOAD_ATTR
// specializer cares about. Names track CPython's enum spelling so the
// cross-reference reads cleanly.
//
// CPython: Python/specialize.c:849 DescriptorClassification
type DescrKind int

const (
	KindAbsent             DescrKind = iota // no descriptor on the type
	KindOverriding                          // has __get__ AND (__set__ OR __delete__) and is not slot/property
	KindMethod                              // bound-method-style: function / builtin / METHOD_DESCRIPTOR
	KindProperty                            // property object with a getter
	KindObjectSlot                          // MemberDescr that stores PyObject*
	KindOtherSlot                           // MemberDescr that stores non-object (int, double, ...)
	KindNonOverriding                       // has __get__ only, immutable class
	KindBuiltinClassmethod                  // classmethod_descriptor (C-level @classmethod)
	KindPythonClassmethod                   // Python classmethod(func)
	KindNonDescriptor                       // plain class attribute (int / str / list / ...)
	KindMutable                             // descriptor of a user-defined (mutable) class
	KindGetsetOverridden                    // __getattribute__/__setattr__ overridden
)

// ClassifyDescriptor maps a resolved type attribute to a DescrKind.
// nil descriptors return KindAbsent. The classifier inspects the
// descriptor's *type* (not the value), matching CPython's
// classify_descriptor: a slot membership decides OBJECT_SLOT vs
// OTHER_SLOT, a Property type decides PROPERTY, callables flagged as
// method-like decide METHOD, everything else with __get__ but no
// __set__ decides NON_OVERRIDING, otherwise NON_DESCRIPTOR.
//
// CPython: Python/specialize.c:867 classify_descriptor
func ClassifyDescriptor(descr objects.Object) DescrKind {
	if descr == nil {
		return KindAbsent
	}
	descType := descr.Type()
	if descType.IsUser {
		// CPython: Py_TPFLAGS_IMMUTABLETYPE check.
		return KindMutable
	}
	switch d := descr.(type) {
	case *objects.MemberDescr:
		// CPython distinguishes Py_T_OBJECT_EX/_Py_T_OBJECT from
		// non-object slots; gopy's MemberDescr only models object
		// slots today, so the OTHER_SLOT branch is unreachable.
		_ = d
		return KindObjectSlot
	case *objects.Property:
		if d.Fget() == nil {
			return KindGetsetOverridden
		}
		return KindProperty
	case *objects.Function, *objects.BuiltinFunction, *objects.MethodDescr:
		// METHOD_DESCRIPTOR equivalents: callables that bind to a
		// bound method when accessed off an instance.
		return KindMethod
	case *objects.ClassMethod:
		return KindPythonClassmethod
	}
	if descType.DescrSet != nil {
		// Has __set__ but not one of the slot/property cases above:
		// CPython would call this OVERRIDING.
		return KindOverriding
	}
	if descType.DescrGet != nil {
		return KindNonOverriding
	}
	return KindNonDescriptor
}
