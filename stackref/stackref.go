// Package stackref is the tagged stack-value representation the
// bytecode interpreter operates on. A Ref wraps an objects.Object
// with the same vocabulary CPython uses (FromObject, AsObject, Dup,
// Steal, Null, None/True/False sentinels), so the eval loop matches
// CPython's stackref calls one-for-one.
//
// In the GIL build, Ref is structurally a pointer with no tag bits.
// The wrapper exists so v0.14 can swap in the biased-refcount
// representation without touching dispatch arms.
//
// CPython: Include/internal/pycore_stackref.h _PyStackRef
// CPython: Python/stackrefs.c
package stackref

import "github.com/tamnd/gopy/objects"

// Ref is a tagged stack value. In the GIL build it is just an
// objects.Object wrapper; v0.14 will pack a deferred-refcount tag
// bit alongside the pointer for the free-threaded build.
//
// CPython: Include/internal/pycore_stackref.h _PyStackRef
type Ref struct {
	o objects.Object
}

// Null is the sentinel for absent values: unbound fast locals,
// cleared stack slots after a pop.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_NULL
var Null = Ref{}

// FromObject wraps a strong reference. The caller transfers
// ownership of the reference to the returned stackref. Mirrors the
// steal variant: the underlying refcount is unchanged.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_FromPyObjectSteal
func FromObject(o objects.Object) Ref {
	return Ref{o: o}
}

// FromObjectNew wraps a borrowed reference, bumping its refcount so
// the returned stackref owns its own strong reference.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_FromPyObjectNew
func FromObjectNew(o objects.Object) Ref {
	if o != nil {
		objects.Incref(o)
	}
	return Ref{o: o}
}

// FromObjectImmortal wraps an immortal singleton. In CPython this
// avoids touching the refcount at all; in gopy it is an alias for
// FromObject because Go's GC handles immortality differently.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_FromPyObjectImmortal
func FromObjectImmortal(o objects.Object) Ref {
	return Ref{o: o}
}

// AsObject extracts the underlying object as a borrowed reference.
// The caller must not retain the result past the lifetime of the
// stackref.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_AsPyObjectBorrow
func (r Ref) AsObject() objects.Object {
	return r.o
}

// AsObjectSteal extracts the underlying object and consumes the
// stackref. The caller takes ownership of the reference.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_AsPyObjectSteal
func (r Ref) AsObjectSteal() objects.Object {
	return r.o
}

// Dup returns a duplicate strong reference to the same object. The
// underlying object's refcount is bumped so the caller and the
// returned ref each own one strong reference.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_DUP
func (r Ref) Dup() Ref {
	if r.o != nil {
		objects.Incref(r.o)
	}
	return r
}

// IsNull reports whether the ref is the sentinel null.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_IsNull
func (r Ref) IsNull() bool {
	return r.o == nil
}

// Close releases a stackref by dropping the refcount on the
// underlying object. When the count hits zero the type's Dealloc
// fires, which is how the per-type freelists (currently Slice) get
// fed. Pairs with FromObject (steal) at construction and Dup
// (incref) at duplication. Null stackrefs are a no-op.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_CLOSE
func (r Ref) Close() {
	if r.o != nil {
		objects.Decref(r.o)
	}
}

// IsTrue reports whether the ref carries the True singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_IsTrue
func (r Ref) IsTrue() bool {
	return r.o == True.o
}

// IsFalse reports whether the ref carries the False singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_IsFalse
func (r Ref) IsFalse() bool {
	return r.o == False.o
}

// IsNone reports whether the ref carries the None singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_IsNone
func (r Ref) IsNone() bool {
	return r.o == None.o
}
