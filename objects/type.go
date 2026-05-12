package objects

import (
	"reflect"
	"unsafe"
)

// CompareOp identifies the six comparison operators Python exposes
// through __lt__/__le__/__eq__/__ne__/__gt__/__ge__. Numeric values
// match the C macros Py_LT..Py_GE so direct ports of slot
// implementations can pass the integer through.
//
// CPython: Include/object.h:L375 Py_LT
type CompareOp int

const (
	// CompareLT is `<`.
	CompareLT CompareOp = iota
	// CompareLE is `<=`.
	CompareLE
	// CompareEQ is `==`.
	CompareEQ
	// CompareNE is `!=`.
	CompareNE
	// CompareGT is `>`.
	CompareGT
	// CompareGE is `>=`.
	CompareGE
)

// Type is the meta-class for every Python value. Slots not yet
// implemented in v0.2 are nil; the runtime checks for nil before
// dispatching.
//
// CPython: Include/cpython/typeobject.h:L139 _typeobject
type Type struct {
	Header

	Name string
	// Module mirrors __module__ on a type. Empty (treated as
	// "builtins") for the built-in types ported in objects/.
	//
	// CPython: Objects/typeobject.c:907 type_module
	Module   string
	BaseSize int
	ItemSize int

	Bases []*Type
	MRO   []*Type

	Repr    func(o Object) (string, error)
	Str     func(o Object) (string, error)
	Hash    func(o Object) (int64, error)
	RichCmp func(a, b Object, op CompareOp) (Object, error)
	// Format is the tp_format slot. Receives the object and the
	// format-spec string (the part after the colon in str.format).
	// When nil, the protocol-level Format helper falls back to Str
	// for empty specs and raises TypeError otherwise.
	//
	// CPython: Objects/typeobject.c:L8260 slot_tp_format
	Format   func(o Object, spec string) (string, error)
	Iter     func(o Object) (Object, error)
	IterNext func(o Object) (Object, error)
	Call     func(o Object, args []Object, kwargs map[string]Object) (Object, error)
	// TpNew is the tp_new slot: the constructor invoked when the type
	// is called (e.g. int(x)). typeCall reaches this through the
	// metaclass tp_call. Built-in primitive types set TpNew rather
	// than Call so that calling instances of the type stays a TypeError
	// and callable(obj) keeps returning False; CPython encodes the same
	// distinction by leaving PyLong_Type.tp_call NULL while wiring
	// long_new through PyType_Type.tp_call.
	//
	// CPython: Include/cpython/typeobject.h tp_new
	TpNew func(cls *Type, args []Object, kwargs map[string]Object) (Object, error)
	// Vectorcall is the PEP 590 fast-call slot. When non-nil, the call
	// machinery uses this instead of going through Call. args is a flat
	// array of positional values followed by keyword values; nargsf is
	// the positional count optionally combined with the
	// VectorcallArgumentsOffset flag; kwnames is a tuple of keyword
	// name strings (or nil).
	//
	// CPython: Include/cpython/object.h tp_vectorcall_offset + Py_TPFLAGS_HAVE_VECTORCALL
	Vectorcall func(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error)
	// Getattro is the tp_getattro slot. Receives the owner and the
	// attribute name (as a Str), returns the attribute value or an
	// error. Mirrors getattrofunc.
	//
	// CPython: Include/cpython/object.h tp_getattro
	Getattro func(o Object, name Object) (Object, error)
	// Setattro is the tp_setattro slot. value==nil signals a delete
	// (PyObject_DelAttr forwards there). Mirrors setattrofunc.
	//
	// CPython: Include/cpython/object.h tp_setattro
	Setattro func(o Object, name Object, value Object) error

	// DescrGet is the tp_descr_get slot. When a class attribute that
	// implements this slot is found during instance attribute lookup,
	// the lookup machinery hands it to DescrGet so the descriptor can
	// produce the bound value. owner is the instance the lookup
	// started from (nil when accessed on the class itself); ownerType
	// is the type whose __mro__ supplied the descriptor.
	//
	// CPython: Include/cpython/object.h tp_descr_get
	DescrGet func(descr Object, owner Object, ownerType *Type) (Object, error)
	// DescrSet is the tp_descr_set slot. value==nil signals a delete.
	//
	// CPython: Include/cpython/object.h tp_descr_set
	DescrSet func(descr Object, owner Object, value Object) error

	Dealloc func(o Object)

	// Finalize is the tp_finalize slot. The cycle collector invokes
	// it once on each unreachable object before reclaiming, and the
	// destruction path fires it from PyObject_CallFinalizer. User
	// classes get this slot populated with a wrapper that calls
	// __del__; built-in types fill it in directly when they need
	// cleanup that runs before memory goes back.
	//
	// CPython: Include/cpython/object.h:237 tp_finalize
	// CPython: Objects/typeobject.c slot_tp_finalize
	Finalize func(o Object)

	// TpTraverse mirrors tp_traverse. It calls visit on every Object
	// reachable directly from o so the cycle collector can walk the
	// reference graph. Returning a non-nil error short-circuits the
	// traversal.
	//
	// CPython: Include/cpython/object.h tp_traverse
	TpTraverse func(o Object, visit Visitor) error

	Number   *NumberMethods
	Sequence *SequenceMethods
	Mapping  *MappingMethods
	Async    *AsyncMethods

	// TpFlags mirrors CPython's tp_flags bitset for the subset of flags
	// that affect VM dispatch (MATCH_MAPPING / MATCH_SEQUENCE).
	//
	// CPython: Include/object.h Py_TPFLAGS_*
	TpFlags uint64

	// IsUser is the gopy stand-in for Py_TPFLAGS_HEAPTYPE: true for
	// classes built via NewUserType (and thus through __build_class__
	// or type(name, bases, dict)). The type-call path consults this
	// to decide between class construction and instance allocation.
	//
	// CPython: Include/object.h Py_TPFLAGS_HEAPTYPE
	IsUser bool

	// Slots holds the resolved __slots__ names for this user type, in
	// declaration order. Empty when the class did not declare __slots__
	// or the class is a built-in. Each name has a fixed index into the
	// instance slots array; MemberDescr objects carry the index and act
	// as data descriptors so reads/writes go through DescrGet/DescrSet.
	//
	// CPython: Objects/typeobject.c:4401 type_new_descriptors
	Slots []string

	// HasDict is true when instances of this type carry a per-instance
	// __dict__. False only when the class declares __slots__ without
	// __dict__ (and no base contributes one). Mirrors a non-zero
	// tp_dictoffset / Py_TPFLAGS_MANAGED_DICT.
	//
	// CPython: Include/cpython/typeobject.h tp_dictoffset
	HasDict bool

	// subclasses tracks the direct subclasses of this type in
	// registration order. CPython stores a dict of weak references in
	// tp_subclasses so the cycle collector can drop dead entries; gopy
	// uses plain pointers because the Go GC keeps these alive as long
	// as the parent type does, which matches the static lifetime of
	// almost every gopy type (built-ins are package-globals and user
	// classes outlive their use sites). Adding weakref support here
	// would trigger an initialization cycle through the NewType /
	// NewWeakref path.
	//
	// CPython: Include/cpython/typeobject.h tp_subclasses
	subclasses []*Type

	// versionTag is the tp_version_tag the adaptive specializer
	// stamps into LOAD_ATTR / STORE_ATTR / CALL inline caches and
	// the dispatch loop checks on every hit. 0 means "not yet
	// allocated or already invalidated"; VersionTag lazily
	// allocates a fresh value from the global counter, mutations
	// that change observable type state reset it back to 0.
	//
	// CPython: Include/internal/pycore_typeobject.h tp_version_tag
	versionTag uint32
}

// Visitor is the visitproc shape passed to TpTraverse. CPython's
// visitproc takes a void* arg; gopy closures already capture state
// so the second argument is unnecessary here.
//
// CPython: Include/cpython/object.h:L83 visitproc
type Visitor func(Object) error

// TpFlag values used by MATCH_MAPPING and MATCH_SEQUENCE.
//
// CPython: Include/object.h:L284 Py_TPFLAGS_MAPPING / Py_TPFLAGS_SEQUENCE
const (
	TpFlagMapping  uint64 = 1 << 6
	TpFlagSequence uint64 = 1 << 5
)

// typeType is the type of Type itself. Lazily initialized on first
// use to break the bootstrap cycle (Type.Header.typ == typeType).
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
var typeType = &Type{Name: "type"}

func init() {
	typeType.typ = typeType
	typeType.refcnt.Store(1)
	// type inherits from object. CPython: Objects/typeobject.c:6361
	// PyType_Type sets tp_base = &PyBaseObject_Type, which puts object
	// in type's MRO so metatype lookup of __class__ / __dict__ finds
	// the getset descriptors object owns.
	typeType.Bases = []*Type{objectType}
	typeType.MRO = []*Type{typeType, objectType}
	typeType.Hash = identityHash
}

// identityHash hashes an object by its pointer address. Mirrors
// CPython's _Py_HashPointer, which is what type / object inherit
// when no user-defined __hash__ exists.
//
// CPython: Python/pyhash.c:152 _Py_HashPointer
func identityHash(o Object) (int64, error) {
	return int64(uintptr(unsafe.Pointer(reflect.ValueOf(o).Pointer()))), nil
}

// TypeType returns the type singleton for `type` itself. Mirrors
// PyType_Type.
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
func TypeType() *Type {
	return typeType
}

// NewType constructs a built-in type with the given name and bases.
// Bases must be non-empty for everything except `object`. The MRO
// is computed via C3 linearization.
//
// CPython: Objects/typeobject.c:L4153 type_new (adapted from)
func NewType(name string, bases []*Type) *Type {
	t := &Type{Name: name, Bases: bases}
	t.init(typeType)
	t.MRO = c3Linearize(t)
	for _, b := range bases {
		if b == nil {
			continue
		}
		b.addSubclass(t)
	}
	return t
}

// addSubclass appends sub to t.subclasses via a weak reference. CPython
// stores a dict keyed by id(sub); gopy uses a slice because lookups are
// rare (only abc and __subclasses__) and Subclasses() filters dead
// entries on the fly.
//
// CPython: Objects/typeobject.c:5853 add_subclass
func (t *Type) addSubclass(sub *Type) {
	t.subclasses = append(t.subclasses, sub)
}

// Subclasses returns the direct subclasses of t in registration order.
// Mirrors type.__subclasses__().
//
// CPython: Objects/typeobject.c:5915 type___subclasses___impl
func (t *Type) Subclasses() []*Type {
	out := make([]*Type, len(t.subclasses))
	copy(out, t.subclasses)
	return out
}

// SetFlagsRecursive ORs add into t.TpFlags after clearing the bits in
// mask, then propagates the same edit to every transitive subclass.
// Mirrors _PyType_SetFlagsRecursive.
//
// CPython: Objects/typeobject.c:1340 _PyType_SetFlagsRecursive
func SetFlagsRecursive(t *Type, mask, add uint64) {
	t.TpFlags = (t.TpFlags &^ mask) | add
	for _, sub := range t.Subclasses() {
		SetFlagsRecursive(sub, mask, add)
	}
}
