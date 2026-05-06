// Capsule is the opaque-pointer container CPython exposes to C
// extensions for sharing implementation pointers without leaking the
// underlying type. The Go port stores any value (typically a pointer
// to a Go-side struct) under a name. Lookups must present the exact
// name the capsule was created with.
//
// CPython: Objects/capsule.c

package objects

import "fmt"

// Capsule mirrors PyCapsule.
//
// CPython: Include/cpython/objimpl.h:80 PyCapsule
type Capsule struct {
	Header
	pointer    any
	name       string
	context    any
	destructor func(*Capsule)
}

// CapsuleType is the type singleton for capsules.
//
// CPython: Objects/capsule.c:354 PyCapsule_Type
var CapsuleType = NewType("PyCapsule", []*Type{objectType})

func init() {
	CapsuleType.Repr = capsuleRepr
	CapsuleType.Str = capsuleRepr
}

// NewCapsule creates a capsule wrapping pointer with the given name
// and optional destructor. Pointer must be non-nil; CPython rejects
// null pointers with ValueError.
//
// CPython: Objects/capsule.c:60 PyCapsule_New
func NewCapsule(pointer any, name string, destructor func(*Capsule)) (*Capsule, error) {
	if pointer == nil {
		return nil, fmt.Errorf("ValueError: PyCapsule_New called with null pointer")
	}
	c := &Capsule{
		pointer:    pointer,
		name:       name,
		destructor: destructor,
	}
	c.init(CapsuleType)
	return c, nil
}

// GetPointer returns the wrapped pointer, but only if name matches
// exactly. Mismatched names raise ValueError, just like CPython.
//
// CPython: Objects/capsule.c:99 PyCapsule_GetPointer
func (c *Capsule) GetPointer(name string) (any, error) {
	if !nameMatches(c.name, name) {
		return nil, fmt.Errorf("ValueError: PyCapsule_GetPointer called with incorrect name")
	}
	if c.pointer == nil {
		return nil, fmt.Errorf("ValueError: PyCapsule_GetPointer called with empty capsule")
	}
	return c.pointer, nil
}

// Name returns the capsule's name.
//
// CPython: Objects/capsule.c PyCapsule_GetName
func (c *Capsule) Name() string { return c.name }

// IsValid reports whether the capsule is non-empty and its name
// matches.
//
// CPython: Objects/capsule.c:87 PyCapsule_IsValid
func (c *Capsule) IsValid(name string) bool {
	return c != nil && c.pointer != nil && nameMatches(c.name, name)
}

// Context returns the user-supplied context value.
//
// CPython: Objects/capsule.c PyCapsule_GetContext
func (c *Capsule) Context() any { return c.context }

// SetContext attaches an opaque context value to the capsule.
//
// CPython: Objects/capsule.c PyCapsule_SetContext
func (c *Capsule) SetContext(ctx any) { c.context = ctx }

// CPython: Objects/capsule.c:48 name_matches
func nameMatches(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return a == b
}

// CPython: Objects/capsule.c:299 capsule_repr
func capsuleRepr(o Object) (string, error) {
	c := o.(*Capsule)
	if c.name == "" {
		return fmt.Sprintf("<capsule object at %p>", c), nil
	}
	return fmt.Sprintf("<capsule object %q at %p>", c.name, c), nil
}
