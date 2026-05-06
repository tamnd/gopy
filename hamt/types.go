// Type registrations for Hamt and its iterators. Mirrors the static
// type objects in cpython/Python/hamt.c (`_PyHamt_Type`,
// `_PyHamtKeys_Type`, `_PyHamtValues_Type`, `_PyHamtItems_Type`).
//
// CPython: Python/hamt.c

package hamt

import "github.com/tamnd/gopy/objects"

// HamtType is the runtime type for Hamt objects. The runtime never
// exposes Hamt to Python directly; the type exists so Hamt satisfies
// objects.Object.
//
// CPython: Python/hamt.c:2814 _PyHamt_Type
var HamtType = objects.NewType("hamt", []*objects.Type{objects.ObjectType()})

// HamtKeysType, HamtValuesType, HamtItemsType wrap the three iterator
// shapes CPython exposes through _PyHamt_NewIterKeys etc. They are
// runtime-private so we register only the bare type name.
//
// CPython: Python/hamt.c:2680 _PyHamtKeys_Type / 2723 _PyHamtValues_Type / 2766 _PyHamtItems_Type
var (
	HamtKeysType   = objects.NewType("hamt_keys", []*objects.Type{objects.ObjectType()})
	HamtValuesType = objects.NewType("hamt_values", []*objects.Type{objects.ObjectType()})
	HamtItemsType  = objects.NewType("hamt_items", []*objects.Type{objects.ObjectType()})
)
