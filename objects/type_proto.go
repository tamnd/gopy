// Type-level public API ported from Objects/typeobject.c. These are
// the PyType_* helpers callers reach for when they need name /
// qualname / module / fully-qualified-name strings, flag bits,
// weakref support, generic alloc + new, and metaclass resolution.
// The other type_*.go files cover the slot machinery; this one
// exposes the introspection surface.

package objects

import (
	"fmt"
	"strings"
)

// TypeGetName mirrors PyType_GetName. For built-ins it returns the
// tail component of the dotted tp_name (matching CPython's
// _PyType_Name fallback); for user types it returns the heap-type
// ht_name which gopy stores directly on the Type.
//
// CPython: Objects/typeobject.c:5430 PyType_GetName
func TypeGetName(t *Type) Object {
	if t == nil {
		return NewStr("")
	}
	if t.IsUser {
		return NewStr(t.Name)
	}
	return NewStr(tailName(t.Name))
}

// TypeGetQualName mirrors PyType_GetQualName. Heap types use the
// stamped ht_qualname; static types fall through to the
// _PyType_Name tail.
//
// CPython: Objects/typeobject.c:5436 PyType_GetQualName
// CPython: Objects/typeobject.c:1469 type_qualname
func TypeGetQualName(t *Type) Object {
	if t == nil {
		return NewStr("")
	}
	if t.Qualname != "" {
		return NewStr(t.Qualname)
	}
	return TypeGetName(t)
}

// TypeGetModuleName mirrors PyType_GetModuleName. Heap types report
// their __module__; static types either return the leading dotted
// component of tp_name or the literal "builtins" sentinel.
//
// CPython: Objects/typeobject.c:5442 PyType_GetModuleName
// CPython: Objects/typeobject.c:1537 type_module
func TypeGetModuleName(t *Type) (Object, error) {
	if t == nil {
		return nil, fmt.Errorf("TypeError: TypeGetModuleName(nil)")
	}
	if t.IsUser {
		if t.Module != "" {
			return NewStr(t.Module), nil
		}
		return nil, fmt.Errorf("AttributeError: __module__")
	}
	if i := strings.LastIndexByte(t.Name, '.'); i >= 0 {
		return NewStr(t.Name[:i]), nil
	}
	return NewStr("builtins"), nil
}

// TypeGetFullyQualifiedName mirrors PyType_GetFullyQualifiedName:
// builds "<module>.<qualname>" when the module is neither "builtins"
// nor "__main__", and returns just the qualname otherwise. Built-in
// static types short-circuit to tp_name directly.
//
// CPython: Objects/typeobject.c:1622 PyType_GetFullyQualifiedName
// CPython: Objects/typeobject.c:1589 _PyType_GetFullyQualifiedName
func TypeGetFullyQualifiedName(t *Type) (Object, error) {
	if t == nil {
		return nil, fmt.Errorf("TypeError: TypeGetFullyQualifiedName(nil)")
	}
	if !t.IsUser {
		return NewStr(t.Name), nil
	}
	qual := TypeGetQualName(t)
	mod, err := TypeGetModuleName(t)
	if err != nil {
		return nil, err
	}
	m := unicodeOf(mod)
	q := unicodeOf(qual)
	if m == "" || m == "builtins" || m == "__main__" {
		return qual, nil
	}
	return NewStr(m + "." + q), nil
}

// TypeGetFlags mirrors PyType_GetFlags: returns the tp_flags bitset.
//
// CPython: Objects/typeobject.c:3906 PyType_GetFlags
func TypeGetFlags(t *Type) uint64 {
	if t == nil {
		return 0
	}
	return t.TpFlags
}

// TypeSupportsWeakrefs mirrors PyType_SUPPORTS_WEAKREFS. In CPython
// the answer is "tp_weaklistoffset != 0"; in gopy every object can
// be weakref'd because the Header carries the weakref list head
// directly, so this is always true for a non-nil type.
//
// CPython: Objects/typeobject.c:3913 PyType_SUPPORTS_WEAKREFS
func TypeSupportsWeakrefs(t *Type) bool {
	return t != nil
}

// TypeGenericAlloc mirrors PyType_GenericAlloc: the tp_alloc default
// that hands back a fresh, zero-initialized instance of type t.
// CPython sizes the allocation by tp_basicsize + nitems*tp_itemsize;
// gopy backs every user-class instance with *Instance and lets Go's
// allocator do the work, so nitems is informational (only matters
// for var-sized built-ins which override tp_alloc).
//
// CPython: Objects/typeobject.c:2457 PyType_GenericAlloc
func TypeGenericAlloc(t *Type, _ int) (Object, error) {
	if t == nil {
		return nil, fmt.Errorf("TypeError: TypeGenericAlloc(nil)")
	}
	return NewInstance(t), nil
}

// TypeGenericNew mirrors PyType_GenericNew: the tp_new default that
// just calls tp_alloc(type, 0) and ignores its args. Used by types
// that want allocation without a custom constructor.
//
// CPython: Objects/typeobject.c:2471 PyType_GenericNew
func TypeGenericNew(t *Type, _ []Object, _ map[string]Object) (Object, error) {
	return TypeGenericAlloc(t, 0)
}

// CalculateMetaclass mirrors _PyType_CalculateMetaclass: pick the
// most-derived metatype among the supplied metatype and the types
// of bases, or raise a metaclass-conflict TypeError when no winner
// exists. This is the exported entry point; the package-internal
// helper used by type_call.go forwards here.
//
// CPython: Objects/typeobject.c:3921 _PyType_CalculateMetaclass
func CalculateMetaclass(metatype *Type, bases []*Type) (*Type, error) {
	return calculateMetaclass(metatype, bases)
}

// tailName returns the substring after the last '.' in name, which
// matches _PyType_Name's dot-stripping behavior for tp_name.
//
// CPython: Objects/typeobject.c:1443 _PyType_Name
func tailName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// unicodeOf extracts the Go string out of a Unicode-typed Object.
// Returns "" when v is nil or not a string. Callers that need the
// underlying *Unicode should type-assert; this helper is just for
// quickly composing fully-qualified names.
func unicodeOf(v Object) string {
	if u, ok := v.(*Unicode); ok {
		return u.Value()
	}
	return ""
}
