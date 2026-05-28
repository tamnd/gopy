package objects

import (
	"fmt"
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
	// Qualname mirrors __qualname__ on a type. Empty means "fall back
	// to Name", which is the right shape for built-ins and any user
	// class whose body did not stamp __qualname__ explicitly. The
	// __build_class__ path copies the namespace's __qualname__ here so
	// nested classes report their dotted path ("Outer.Inner") instead
	// of just the bare "Inner".
	//
	// CPython: Objects/typeobject.c:984 type_qualname
	Qualname string
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

	// TypeParams holds the PEP 695 type-parameter tuple set via
	// __type_params__ in the class body. nil means "not set" which
	// __type_params__ exposes as an empty tuple.
	//
	// CPython: Include/cpython/typeobject.h tp_typeparams
	TypeParams Object

	// TypingParameters holds the __parameters__ tuple set by
	// typing.Generic.__init_subclass__ for traditional-style generic
	// classes (class Foo(Generic[T])). nil means use TypeParams as fallback.
	//
	// CPython: Lib/typing.py:1209 cls.__parameters__ = tuple(tvars)
	TypingParameters *Tuple

	// Slots holds the resolved __slots__ names for this user type, in
	// declaration order. Empty when the class did not declare __slots__
	// or the class is a built-in. Each name has a fixed index into the
	// instance slots array; MemberDescr objects carry the index and act
	// as data descriptors so reads/writes go through DescrGet/DescrSet.
	//
	// CPython: Objects/typeobject.c:4401 type_new_descriptors
	Slots []string

	// SlotsBase is the cumulative count of slot fields contributed by
	// the layout base chain (CPython's ctx->base->tp_basicsize translated
	// to a slot-count offset). MemberDescr indices on this type start at
	// SlotsBase, so an inherited slot keeps reading the parent's index
	// while a freshly declared slot lands above it. NewInstance sizes
	// Instance.slots to SlotsBase + len(Slots).
	//
	// CPython: Objects/typeobject.c:4404 type_new_descriptors (slotoffset
	// = ctx->base->tp_basicsize)
	SlotsBase int

	// HasDict is true when instances of this type carry a per-instance
	// __dict__. False only when the class declares __slots__ without
	// __dict__ (and no base contributes one). Mirrors a non-zero
	// tp_dictoffset / Py_TPFLAGS_MANAGED_DICT.
	//
	// CPython: Include/cpython/typeobject.h tp_dictoffset
	HasDict bool

	// ClassAttrDict is the live attribute dict for user types, mirroring
	// CPython's tp_dict. SetTypeDescr writes through to this dict so that
	// PEP 695 type alias thunks using LOAD_FROM_DICT_OR_GLOBALS with the
	// __classdict__ closure cell always see the current attribute values.
	// nil for built-in types (they use typeDescrTable only).
	//
	// CPython: Objects/typeobject.c:4500 type_new_set_classdictcell
	// (CPython sets the __classdictcell__ to tp_dict, not to ns)
	ClassAttrDict *Dict

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

	// cachedKeys mirrors PyHeapTypeObject.ht_cached_keys: the union of
	// attribute names ever observed in an instance's __dict__. The
	// LOAD_ATTR_*_WITH_VALUES specializer arms refuse to fire for any
	// name in this set, because such names can shadow class-level
	// descriptors. Populated lazily by instanceSetAttr.
	//
	// CPython: Include/cpython/typeobject.h ht_cached_keys
	cachedKeys map[string]bool

	// sharedKeys is the real PyDictKeysObject shared by every split
	// __dict__ allocated for an instance of this type. AddCachedKey
	// lazily allocates it and extends it in place via AddKey; the
	// table never resizes (NewSplitDict copies sk.entries's slice
	// header into d.entries, so a reallocation would orphan attached
	// dicts). When the table fills, AddCachedKey stops extending and
	// new keys fall through the dictInsertSplit materialize path.
	//
	// CPython: Include/cpython/typeobject.h ht_cached_keys
	sharedKeys *SharedKeys

	// cachedKeysVersion is the dk_version of cachedKeys. Bumps on
	// every additive change so caches stamped before the change
	// invalidate. 0 means "not yet allocated or invalidated".
	//
	// CPython: Include/internal/pycore_dict.h dk_version
	cachedKeysVersion uint32

	// tpWatched mirrors CPython's PyTypeObject.tp_watched: an 8-bit
	// mask, one bit per registered type watcher. PyType_Watch sets
	// the bit, PyType_Unwatch clears it, type_modified_unlocked
	// scans it on every mutation. Keeping the subscription on the
	// type (rather than on a per-watcher pointer set) puts the
	// "anyone watching?" check on a single load and bitwise-and.
	//
	// CPython: Include/cpython/object.h:234 tp_watched
	tpWatched uint8

	// specCacheInit mirrors PyHeapTypeObject._spec_cache.init: the
	// __init__ Function pointer the adaptive specializer caches when
	// it picks CALL_ALLOC_AND_ENTER_INIT for the class. The fast arm
	// dereferences this directly instead of re-walking the MRO for
	// "__init__" on every call. Cleared by InvalidateVersionTag along
	// with specCacheInitVersion so a class mutation forces a re-cache.
	//
	// CPython: Include/internal/pycore_typeobject.h _spec_cache.init
	specCacheInit *Function

	// specCacheInitVersion mirrors PyHeapTypeObject._spec_cache.init_version:
	// the tp_version_tag captured at the moment specCacheInit was
	// stashed. The fast arm compares this against the live version
	// tag in cache cells 2-3; mismatch deopts. Cleared with the
	// version tag on type mutation.
	//
	// CPython: Include/internal/pycore_typeobject.h _spec_cache.init_version
	specCacheInitVersion uint32
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
	// TpFlagInlineValues mirrors Py_TPFLAGS_INLINE_VALUES. Set on heap
	// types whose instances begin life with a PyDictValues block stored
	// inline at tp_basicsize. The adaptive specializer keys its
	// LOAD_ATTR_METHOD_WITH_VALUES / NONDESCRIPTOR_WITH_VALUES arms on
	// this bit.
	//
	// CPython: Include/object.h:518 Py_TPFLAGS_INLINE_VALUES
	TpFlagInlineValues uint64 = 1 << 2
	// TpFlagManagedDict mirrors Py_TPFLAGS_MANAGED_DICT. Set on heap
	// types whose instances carry their __dict__ in a managed slot at
	// MANAGED_DICT_OFFSET rather than via tp_dictoffset. Gates the
	// LOAD_ATTR_METHOD_LAZY_DICT arm.
	//
	// CPython: Include/object.h:528 Py_TPFLAGS_MANAGED_DICT
	TpFlagManagedDict uint64 = 1 << 4
	// TpFlagMatchSelf marks the ten built-in types (bool/bytearray/
	// bytes/dict/float/frozenset/int/list/set/str/tuple) that bind the
	// subject itself when MATCH_CLASS receives exactly one positional
	// sub-pattern and the type has no __match_args__. Subclasses
	// inherit the flag through the type system.
	//
	// CPython: Include/object.h:588 _Py_TPFLAGS_MATCH_SELF
	TpFlagMatchSelf uint64 = 1 << 22
	// TpFlagImmutable mirrors Py_TPFLAGS_IMMUTABLETYPE. Set on every
	// static built-in type so SetFlagsRecursive (called from
	// Sequence.register(str) and friends in collections.abc) refuses
	// to paint sequence/mapping bits onto str/bytes/int/etc. Without
	// the guard MATCH_SEQUENCE on a plain string returns True.
	//
	// CPython: Include/object.h:289 Py_TPFLAGS_IMMUTABLETYPE
	TpFlagImmutable uint64 = 1 << 8
	// TpFlagBasetype mirrors Py_TPFLAGS_BASETYPE. When clear, the type
	// cannot be used as a base class and type.__new__ raises
	// "type '...' is not an acceptable base type". Set by default on
	// every type created via NewType; explicitly cleared for types like
	// TypeAliasType that prohibit subclassing.
	//
	// CPython: Include/object.h:264 Py_TPFLAGS_BASETYPE
	TpFlagBasetype uint64 = 1 << 10
)

// HasInlineValues reports whether t carries Py_TPFLAGS_INLINE_VALUES.
//
// CPython: Include/object.h:518 Py_TPFLAGS_INLINE_VALUES
func (t *Type) HasInlineValues() bool {
	return t.TpFlags&TpFlagInlineValues != 0
}

// HasManagedDict reports whether t carries Py_TPFLAGS_MANAGED_DICT.
//
// CPython: Include/object.h:528 Py_TPFLAGS_MANAGED_DICT
func (t *Type) HasManagedDict() bool {
	return t.TpFlags&TpFlagManagedDict != 0
}

// CachedKeysVersion returns the cached_keys version, allocating a
// fresh value on first read. Mirrors PyHeapTypeObject.ht_cached_keys
// dk_version: the specializer stamps this into LOAD_ATTR_*_WITH_VALUES
// caches and the fast arm rejects on mismatch (i.e. when a new
// attribute name was added to any instance's dict).
//
// CPython: Include/internal/pycore_dict.h dk_version
func (t *Type) CachedKeysVersion() uint32 {
	if t.cachedKeysVersion != 0 {
		return t.cachedKeysVersion
	}
	v := allocDictKeysVersion()
	if v == 0 {
		return 0
	}
	t.cachedKeysVersion = v
	return v
}

// InvalidateCachedKeysVersion zeroes t.cachedKeysVersion so the next
// CachedKeysVersion call allocates a fresh value. Called by
// AddCachedKey when a new attribute name enters the shared-keys set.
//
// CPython: Objects/dictobject.c:739 dictkeys_modified
func (t *Type) InvalidateCachedKeysVersion() {
	t.cachedKeysVersion = 0
}

// HasCachedKey reports whether name has ever been observed as an
// instance attribute on t (or any subclass that bubbled it up).
// The specializer uses this to refuse WITH_VALUES specialization for
// names that an instance might shadow.
//
// CPython: Objects/dictobject.c:5132 insert_split_key (shared-keys insertion)
func (t *Type) HasCachedKey(name string) bool {
	if t.cachedKeys == nil {
		return false
	}
	_, ok := t.cachedKeys[name]
	return ok
}

// AddCachedKey records name in the shared-keys set and bumps
// cached_keys_version. No-op when name is already known. Called from
// instanceSetAttr the first time a given attribute name is stored on
// any instance.
//
// Also extends t.sharedKeys in place via AddKey so future instances
// of t can allocate a split __dict__ whose splitValues array already
// includes this slot. When the shared table is full (or hashing the
// name fails) the extension silently fails and instances that need
// the new key will materialize through dictInsertSplit instead.
//
// CPython: Objects/dictobject.c:1832 insert_split_key
func (t *Type) AddCachedKey(name string) {
	if t.cachedKeys == nil {
		t.cachedKeys = map[string]bool{}
	}
	if t.cachedKeys[name] {
		return
	}
	t.cachedKeys[name] = true
	if t.sharedKeys == nil {
		t.sharedKeys = NewEmptySharedKeys()
	}
	t.sharedKeys.AddKey(name)
	t.InvalidateCachedKeysVersion()
}

// SharedKeys returns the per-type shared keys table, or nil when no
// instance attribute has been observed yet. Used by NewInstance to
// decide whether a freshly built __dict__ can attach as a split dict
// or has to fall back to combined.
//
// CPython: Include/cpython/typeobject.h ht_cached_keys
func (t *Type) SharedKeys() *SharedKeys { return t.sharedKeys }

// FullyQualifiedName returns the display name used in error messages.
// For built-in types it is just Name. For user-defined (heap) types it
// follows CPython's _PyType_GetFullyQualifiedName: prepend the module
// unless the module is "", "builtins", or "__main__".
//
// CPython: Objects/typeobject.c:1589 _PyType_GetFullyQualifiedName
func (t *Type) FullyQualifiedName() string {
	qualname := t.Qualname
	if qualname == "" {
		qualname = t.Name
	}
	mod := t.Module
	if mod == "" || mod == "builtins" || mod == "__main__" {
		return qualname
	}
	return mod + "." + qualname
}

// typeType is the type of Type itself. Lazily initialized on first
// use to break the bootstrap cycle (Type.Header.typ == typeType).
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
var typeType = &Type{Name: "type", TpFlags: TpFlagImmutable | TpFlagBasetype}

func init() {
	typeType.typ = typeType
	typeType.refcnt = 1
	// type inherits from object. CPython: Objects/typeobject.c:6361
	// PyType_Type sets tp_base = &PyBaseObject_Type, which puts object
	// in type's MRO so metatype lookup of __class__ / __dict__ finds
	// the getset descriptors object owns.
	typeType.Bases = []*Type{objectType}
	typeType.MRO = []*Type{typeType, objectType}
	typeType.Hash = identityHash

	// CPython: Objects/typeobject.c type_type_params getset
	SetTypeDescr(typeType, "__type_params__", NewGetSetDescr("__type_params__",
		func(o Object) (Object, error) {
			t, ok := o.(*Type)
			if !ok {
				return NewTuple(nil), nil
			}
			if t.TypeParams == nil {
				return NewTuple(nil), nil
			}
			return t.TypeParams, nil
		},
		func(o Object, v Object) error {
			t, ok := o.(*Type)
			if !ok {
				return fmt.Errorf("TypeError: __type_params__ can only be set on types")
			}
			if v == None() || v == nil {
				t.TypeParams = nil
				return nil
			}
			if _, ok := v.(*Tuple); !ok {
				return fmt.Errorf("TypeError: __type_params__ must be a tuple")
			}
			t.TypeParams = v
			return nil
		},
	))
}

// identityHash hashes an object by its pointer address. Mirrors
// CPython's _Py_HashPointer, which is what type / object inherit
// when no user-defined __hash__ exists.
//
// CPython: Python/pyhash.c:152 _Py_HashPointer
func identityHash(o Object) (int64, error) {
	return int64(uintptr(unsafe.Pointer(reflect.ValueOf(o).Pointer()))), nil
}

// IdentityHash is the exported alias for identityHash so built-in
// modules can install pointer-identity hashing on types that inherit
// object's default hash semantics. Mirrors CPython's behavior where any
// type that does not override __hash__ shares object's _Py_HashPointer.
//
// CPython: Python/pyhash.c:152 _Py_HashPointer
func IdentityHash(o Object) (int64, error) { return identityHash(o) }

// TypeType returns the type singleton for `type` itself. Mirrors
// PyType_Type.
//
// CPython: Objects/typeobject.c:L6361 PyType_Type
func TypeType() *Type {
	return typeType
}

// NewType constructs a built-in type with the given name and bases.
// Bases must be non-empty for everything except `object`. The MRO
// is computed via C3 linearization. Panics if C3 fails; use newTypeE
// when the caller must surface MRO errors as Python exceptions.
//
// CPython: Objects/typeobject.c:4153 type_new (adapted from)
func NewType(name string, bases []*Type) *Type {
	t, err := newTypeE(name, bases)
	if err != nil {
		panic(err)
	}
	return t
}

// newTypeE is the error-returning variant of NewType used by
// NewUserTypeMetaE so an inconsistent MRO raises TypeError instead of
// crashing.
//
// CPython: Objects/typeobject.c:4153 type_new (adapted from)
func newTypeE(name string, bases []*Type) (*Type, error) {
	// Check that every base allows subclassing.
	//
	// CPython: Objects/typeobject.c:3638 type_new_set_base
	for _, b := range bases {
		if b == nil {
			continue
		}
		if b.TpFlags&TpFlagBasetype == 0 {
			return nil, fmt.Errorf("TypeError: type '%s' is not an acceptable base type", b.Name)
		}
	}
	t := &Type{Name: name, Bases: bases, TpFlags: TpFlagImmutable | TpFlagBasetype}
	t.init(typeType)
	mro, err := c3Linearize(t)
	if err != nil {
		return nil, err
	}
	t.MRO = mro
	for _, b := range bases {
		if b == nil {
			continue
		}
		b.addSubclass(t)
		// MATCH_SELF carries from every base independently: bool is a
		// self-matching int subclass, and we want that bit to ride
		// down through any multiple-inheritance combination.
		//
		// CPython: Objects/typeobject.c:8204 inherit_flags
		t.TpFlags |= b.TpFlags & TpFlagMatchSelf
	}
	inheritPatmaFlagsAllMRO(t)
	// inherit slots from every ancestor so dispatch can resolve in
	// one field load. Built-in types that set their own Number /
	// Sequence / Mapping / Async bundle after NewType returns will
	// wholesale replace what was inherited here; user types built
	// through NewUserType run a second pass after their namespace
	// has populated additional dunders.
	//
	// CPython: Objects/typeobject.c:8712 type_ready_inherit
	inheritSlotsAllMRO(t)
	return t, nil
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
// Immutable types are skipped (and stop the walk into their
// subclasses) so collections.abc registrations like
// Sequence.register(str) don't repaint built-in flag bits.
//
// CPython: Objects/typeobject.c:6042 set_flags_recursive
func SetFlagsRecursive(t *Type, mask, add uint64) {
	if t.TpFlags&TpFlagImmutable != 0 || (t.TpFlags&mask) == add {
		return
	}
	t.TpFlags = (t.TpFlags &^ mask) | add
	for _, sub := range t.Subclasses() {
		SetFlagsRecursive(sub, mask, add)
	}
}
