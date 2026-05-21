// Package objects ports the gating subset of cpython/Objects/. v0.2
// covers the object protocol, the type slot table, and the concrete
// builtins needed to construct a dict, hash a tuple, and iterate a
// list. Strings, bytes, set, exceptions, and the cycle collector
// arrive in later phases (see notes/Spec/1700/).
package objects

// Header is the per-object header. Mirrors struct _object in
// cpython/Include/object.h. The zero value is invalid; constructors
// set refcount to 1 and install the type pointer.
//
// refcnt is a plain int64 to match CPython's GIL-build ob_refcnt,
// where Py_INCREF / Py_DECREF expand to non-atomic ++/--. Atomic
// access would cost a memory barrier per op on the hot path. The
// free-threaded build (v0.14) will introduce a tagged biased
// refcount instead of a global atomic.
//
// CPython: Include/object.h:L107 _object
type Header struct {
	refcnt int64
	typ    *Type

	// weakrefs is the per-object doubly-linked list of weak references
	// pointing at this object. CPython stores the head pointer at the
	// type-configured tp_weaklistoffset inside the instance; gopy keeps
	// the head + its lock together in a heap-allocated weakrefList so
	// types do not have to opt-in: every Object can be weakref'd. The
	// pointer stays nil for the common case where nothing ever creates
	// a weak reference, so the cost is one word per object.
	//
	// CPython: Include/cpython/typeobject.h tp_weaklistoffset
	weakrefs *weakrefList
}

// VarHeader extends Header with ob_size for variable-length builtins
// such as tuple, str, bytes, and int.
//
// CPython: Include/object.h:L121 PyVarObject
type VarHeader struct {
	Header
	size int64
}

// Object is the interface every Python value satisfies. Concrete
// types embed *Header (or *VarHeader) and add their own fields.
//
// CPython: Include/object.h:L107 PyObject (interface analog)
type Object interface {
	// Type returns the object's type. Mirrors Py_TYPE.
	Type() *Type
	// Hdr returns a pointer to the embedded Header so generic
	// runtime code can reach the refcount without knowing the
	// concrete shape. CPython reaches the same field by casting to
	// PyObject*.
	Hdr() *Header
}

// Type returns the type pointer stored in the header.
//
// CPython: Include/object.h:L249 Py_TYPE
func (h *Header) Type() *Type {
	return h.typ
}

// Hdr returns the receiver. Implements Object for any value that
// embeds *Header.
func (h *Header) Hdr() *Header {
	return h
}

// init wires up a freshly allocated header. Called by every type's
// constructor. Refcount starts at 1 to match Py_NewRef on a freshly
// allocated PyObject.
//
// CPython: Objects/object.c:L184 _PyObject_Init
func (h *Header) init(t *Type) {
	h.typ = t
	h.refcnt = 1
}

// Init is the cross-package entry point to init. Out-of-package types
// such as the exception in errors/ embed Header and need to bind their
// Header.typ to a *Type without re-implementing the refcount dance.
//
// CPython: Objects/object.c:L184 _PyObject_Init
func (h *Header) Init(t *Type) {
	h.init(t)
}

// Refcnt returns the current refcount. Test-only; production code
// should not depend on the exact value because Go's GC reclaims
// memory independently.
//
// CPython: Include/object.h:L246 Py_REFCNT
func (h *Header) Refcnt() int64 {
	return h.refcnt
}

// Size returns ob_size. Only meaningful for VarHeader-backed types.
//
// CPython: Include/object.h:L252 Py_SIZE
func (v *VarHeader) Size() int64 {
	return v.size
}
