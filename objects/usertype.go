// NewUserType builds a Type from the (name, bases, namespace) triple
// __build_class__ produces. Each entry in ns becomes a type-level
// descriptor reachable through LookupDescriptor; instance attribute
// access is wired to instanceGetAttr/instanceSetAttr so the dict is
// the per-instance backing store.
//
// CPython lays the same path through type.__call__ -> type_new ->
// type_init, which copies the body dict into tp_dict and stamps
// tp_getattro / tp_setattro to the generic slots. gopy's v0.10.1 cut
// keeps the sub-pieces small: the type call slot dispatches here and
// here we install the namespace.
//
// CPython: Objects/typeobject.c:4153 type_new

package objects

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
)

// NewUserType builds a Python-defined class. bases default to
// [object] when empty; ns must be non-nil and is iterated for type
// members. __slots__ in ns triggers the slot layout machinery
// (CPython type_new_slots + type_new_descriptors): each slot becomes a
// MemberDescr at a fixed instance index, and the resulting class has
// no per-instance __dict__ unless a base contributes one or the slots
// list explicitly includes "__dict__".
//
// CPython: Objects/typeobject.c:4153 type_new
func NewUserType(name string, bases []*Type, ns *Dict) *Type {
	return NewUserTypeKwargs(name, bases, ns, nil)
}

// NewUserTypeKwargs is the kwargs-aware variant of NewUserType. The
// extra kwargs (metaclass-strip already done by the caller) flow
// through to __init_subclass__ so PEP 487 hooks see them.
//
// CPython: Objects/typeobject.c:4153 type_new (the mkw kwargs path)
func NewUserTypeKwargs(name string, bases []*Type, ns *Dict, kwargs map[string]Object) *Type {
	return NewUserTypeMeta(name, bases, ns, kwargs, nil)
}

// installSubclassAttrSlots stamps the right Getattro / Setattro pair
// (plus TpNew for the C-port subclass cases) on t. Metaclasses route
// through typeGetAttr because their instances are *Type. dict / str /
// int subclasses keep the C-port TpNew so instances are *Dict / *Unicode
// / *Int instead of *Instance, and use their type-specific attr slots
// so descriptor lookups hit the right vtable. Everything else lands on
// the generic instance attr slots.
//
// CPython: Objects/typeobject.c inherit_slots (type_getattro inheritance
// + tp_new copy)
func installSubclassAttrSlots(t *Type) {
	switch {
	case IsSubtype(t, typeType):
		t.Getattro = typeGetAttr
		t.Setattro = typeSetAttr
	case IsSubtype(t, DictType):
		t.Getattro = dictSubclassGetAttr
		t.Setattro = dictSubclassSetAttr
		// Inherit TpNew from the most-derived non-user base (e.g.
		// defaultdict overrides DictType.TpNew with its own allocator).
		//
		// CPython: Objects/typeobject.c:7521 inherit_slots (tp_new slot)
		t.TpNew = DictType.TpNew
		for _, base := range t.MRO {
			if base == t || base.IsUser || base.TpNew == nil {
				continue
			}
			t.TpNew = base.TpNew
			break
		}
	case IsSubtype(t, strType):
		t.Getattro = strSubclassGetAttr
		t.Setattro = strSubclassSetAttr
		t.TpNew = strType.TpNew
	case IsSubtype(t, IntType):
		t.Getattro = intSubclassGetAttr
		t.Setattro = intSubclassSetAttr
		t.TpNew = IntType.TpNew
	case IsSubtype(t, SuperType):
		// super publishes a custom tp_getattro (the cooperative MRO walk)
		// and an allocation-only tp_new, but no tp_setattro, so the generic
		// customAttrBase heuristic (which keys off a custom setter) misses
		// it. A subclass like mysuper(super) must inherit super's getattro
		// and new or its instances are plain *Instance objects and the MRO
		// walk never runs.
		//
		// CPython: Objects/typeobject.c:7521 inherit_slots (tp_getattro/tp_new)
		t.Getattro = superGetAttr
		t.Setattro = instanceSetAttr
		t.TpNew = SuperType.TpNew
	case customAttrBase(t) != nil:
		// A built-in base on the MRO stores instance attributes through a
		// custom tp_setattro (for example _thread._local, which keeps a
		// per-thread dict). CPython inherits tp_getattro / tp_setattro /
		// tp_new from such a base in inherit_slots; stamping the generic
		// instance slots here would clobber that storage protocol and
		// strand the subclass with no usable attribute store. Adopt the
		// base's attr slots and its tp_new so the subclass keeps the base's
		// instance layout. Only non-nil base slots are copied so a base
		// that customizes one half of the protocol still leaves the other
		// half on the generic instance path.
		//
		// CPython: Objects/typeobject.c:7521 inherit_slots
		//          (tp_getattro / tp_setattro / tp_new inheritance)
		b := customAttrBase(t)
		if b.Getattro != nil {
			t.Getattro = b.Getattro
		} else {
			t.Getattro = instanceGetAttr
		}
		t.Setattro = b.Setattro
		if b.TpNew != nil {
			t.TpNew = b.TpNew
		}
	default:
		t.Getattro = instanceGetAttr
		t.Setattro = instanceSetAttr
	}
}

// customAttrBase returns the first built-in base on t's MRO that stores
// instance attributes through a custom tp_setattro. Subclasses of such a
// base must inherit its slots rather than have the generic instance slots
// stamped over them, otherwise attribute writes land in the wrong store.
// Returns nil when no base needs special treatment (the common
// object-subclass case), so the caller falls through to instanceGetAttr /
// instanceSetAttr.
//
// A base with a custom tp_getattro but the generic tp_setattro (gopy's
// _io.StringIO, which serves a fixed set of attrs and rejects the rest)
// is intentionally NOT matched: such a base holds no per-instance state of
// its own, so a subclass works correctly on the generic instance path,
// which is also what CPython produces for those types (they expose their
// attributes through getset descriptors and inherit object's generic
// tp_getattro).
//
// CPython: Objects/typeobject.c:7521 inherit_slots
func customAttrBase(t *Type) *Type {
	for _, base := range t.MRO {
		if base == t || base == objectType || base.IsUser {
			continue
		}
		if hasCustomAttrSlot(base) {
			return base
		}
	}
	return nil
}

// hasCustomAttrSlot reports whether b stores instance attributes through a
// custom tp_setattro, meaning a slot that is neither the generic built-in
// path (GenericSetAttr) nor the user-instance path (instanceSetAttr). A
// custom storage setter is the one piece of the attribute protocol that a
// subclass cannot reproduce via descriptors plus the generic instance
// dict, so it is the signal that the base's slots must be inherited.
// Function-pointer comparison mirrors CPython's slot-inheritance check.
func hasCustomAttrSlot(b *Type) bool {
	if b.Setattro != nil &&
		fnPtr(b.Setattro) != fnPtr(GenericSetAttr) &&
		fnPtr(b.Setattro) != fnPtr(instanceSetAttr) {
		return true
	}
	return false
}

// NewUserTypeMeta is the full-form constructor used by type.__new__.
// meta is the metaclass to stamp on the new type; nil means inherit
// the default typeType. Stamping happens before typeSetNames so PEP
// 487 hooks that call cls.<metaclass_method>(...) resolve through the
// real metatype, not the placeholder.
//
// Panics if typeSetNames or typeInitSubclass returns an error. Callers
// that need to surface those failures as Python exceptions should use
// NewUserTypeMetaE instead.
//
// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
func NewUserTypeMeta(name string, bases []*Type, ns *Dict, kwargs map[string]Object, meta *Type) *Type {
	t, err := NewUserTypeMetaE(name, bases, ns, kwargs, meta)
	if err != nil {
		panic(err)
	}
	return t
}

// NewUserTypeMetaE is the error-returning sibling of NewUserTypeMeta.
// typeNewBuiltin uses this variant so a __init_subclass__ hook that
// raises TypeError surfaces through the normal exception path instead
// of crashing the interpreter.
//
// CPython: Objects/typeobject.c:4153 type_new
func NewUserTypeMetaE(name string, bases []*Type, ns *Dict, kwargs map[string]Object, meta *Type) (*Type, error) {
	// type_new_set_name runs PyUnicode_AsUTF8AndSize on the name: a lone
	// surrogate cannot encode to UTF-8 (UnicodeEncodeError) and an
	// embedded null trips the strlen != size guard (ValueError). Validate
	// up front so type('A\x00B', ...) / type('A\udcdcB', ...) raise here
	// instead of silently building a malformed type.
	//
	// CPython: Objects/typeobject.c:4233 type_new_set_name
	if err := checkTypeName(name); err != nil {
		return nil, err
	}
	if len(bases) == 0 {
		bases = []*Type{objectType}
	}
	// A base listed twice (type('X', (A, A), {})) is rejected up front with a
	// dedicated message, before the C3 linearization would otherwise report a
	// generic MRO conflict.
	//
	// CPython: Objects/typeobject.c:3168 check_duplicates
	for i := range bases {
		for j := i + 1; j < len(bases); j++ {
			if bases[j] == bases[i] {
				return nil, fmt.Errorf("TypeError: duplicate base class %s", bases[i].Name)
			}
		}
	}
	// best_base validates the instance layout before anything else: two
	// bases whose solid bases differ (int and str each own a distinct C
	// struct) cannot be combined, so type('A', (int, str), {}) raises a
	// lay-out conflict here rather than producing a broken type.
	//
	// CPython: Objects/typeobject.c:2998 best_base
	solidWinner, err := bestBase(bases)
	if err != nil {
		return nil, err
	}
	t, err := newTypeE(name, bases)
	if err != nil {
		return nil, err
	}
	// type_new_alloc seeds the new type's layout from its best base, so a
	// plain user class inherits object's tp_basicsize (16) rather than
	// reporting 0. gopy does not model the dict/weakref/slot growth on top,
	// but inheriting the base size keeps shape_differs false for a plain
	// subclass, so solid_base still resolves to the base (no spurious
	// instance-layout conflicts) while object.__basicsize__ stays nonzero.
	//
	// CPython: Objects/typeobject.c:4438 type_new_alloc (tp_basicsize = base->tp_basicsize)
	if solidWinner != nil {
		t.BaseSize = solidWinner.BaseSize
		t.ItemSize = solidWinner.ItemSize
	}
	t.IsUser = true
	// Heap (user) types are mutable: drop the IMMUTABLETYPE flag that
	// NewType stamps on by default so collections.abc registrations
	// can still paint sequence/mapping bits onto user subclasses.
	//
	// CPython: Objects/typeobject.c:4153 type_new (heap types lack
	// Py_TPFLAGS_IMMUTABLETYPE)
	t.TpFlags &^= TpFlagImmutable
	// NewType stamps the type immortal so static built-in types never
	// deallocate. Heap types ARE refcounted (they die with the last
	// reference, e.g. when a class falls out of its defining scope), so
	// reset the refcount to a freshly-allocated value.
	//
	// CPython: Objects/typeobject.c:4153 type_new (heap types start at
	// refcount 1 from PyObject_GC_NewVar)
	atomic.StoreInt64(&t.Hdr().refcnt, 1)
	stampMetaclass(t, meta)
	installSubclassAttrSlots(t)
	noSlotsDeclared := hasNoSlotsDeclared(ns)
	configureManagedDict(t, bases, noSlotsDeclared)
	// type_new_set_attrs copies the namespace into tp_dict (slots, classcell,
	// plain attributes) BEFORE type_ready -> mro_internal invokes the
	// metaclass mro(). A metaclass that overrides mro() can therefore read
	// self.__dict__["f"] during MRO computation (test___class___mro), so the
	// dict must be populated first; applyMetaclassMRO runs last.
	//
	// CPython: Objects/typeobject.c:4526 type_new_set_attrs (before type_ready)
	if err := processClassNamespace(t, ns); err != nil {
		return nil, err
	}
	if err := applyMetaclassMRO(t, meta); err != nil {
		return nil, err
	}
	// When the namespace did not carry __module__, inherit it from the
	// calling frame's globals __name__. The compiler injects __module__
	// into every class-statement namespace, so this only fires for the
	// type(name, bases, {}) constructor form, matching CPython where
	// type_new_set_module pulls globals __name__ for the bare builtin call.
	//
	// CPython: Objects/typeobject.c:4382 type_new_set_module
	if t.Module == "" {
		has := false
		if ns != nil {
			has, _ = ns.Contains(NewStr("__module__"))
		}
		if !has {
			t.Module = CallerModuleName()
		}
	}
	// type_new_set_ht_name defaults ht_qualname to ht_name when the
	// namespace omits __qualname__. Stamping it now (rather than letting
	// typeGetQualname fall back to t.Name on every read) keeps qualname
	// independent of later __name__ assignments, matching CPython where
	// type_set_name touches ht_name only.
	//
	// CPython: Objects/typeobject.c:4300 type_new_set_ht_name
	if t.Qualname == "" {
		t.Qualname = name
	}
	// NewType already ran inheritSlotsAllMRO when the namespace was not
	// yet populated, so typeOverridesHash could not see __hash__. If the
	// just-copied namespace declares __hash__, drop any inherited Hash /
	// RichCmp pair so the second inheritSlotsAllMRO inside
	// fixupSlotDispatchers honors the override and the per-type fixup
	// installs the correct dispatcher (None namespace entry => identityHash
	// fallback, real callable => slotTpHash).
	//
	// CPython: Objects/typeobject.c:8366 (richcompare/hash dance)
	if typeOverridesHash(t) {
		t.Hash = nil
		t.RichCmp = nil
	}
	fixupSlotDispatchers(t)
	// User-class instances are *Instance carrying a per-instance dict
	// and/or slots, so the cycle collector needs a tp_traverse that
	// walks both. If no base contributed one (the common object-only
	// case), install instanceTraverse. Subclasses of built-ins
	// (list/dict/str/int) inherit their base's traverse via
	// inheritSlotsAllMRO and keep that wiring.
	//
	// CPython: Objects/typeobject.c:1356 subtype_traverse
	if t.TpTraverse == nil {
		t.TpTraverse = instanceTraverse
	}
	// Pure user classes (object-only storage base) have no inherited
	// tp_dealloc, so wire instanceDealloc to release the type reference
	// each instance takes in NewInstance. Subclasses of built-ins keep the
	// dealloc they inherited from their storage base (set, etc.); their
	// instances never route through NewInstance and so never took the
	// reference instanceDealloc would release. This mirrors CPython giving
	// every heap type subtype_dealloc, whose _Py_DECREF_TYPE balances the
	// _Py_INCREF_TYPE in _PyObject_Init.
	//
	// CPython: Objects/typeobject.c:4213 type_new (tp_dealloc = subtype_dealloc)
	if t.Dealloc == nil {
		t.Dealloc = instanceDealloc
	}
	// type_new warns once if the finished class dict carries a non-string
	// key (type('MyClass', (), {1: 2}) or a metaclass that injects ns[1]=2).
	// The namespace is the source of those keys; the special cells were
	// already stripped above.
	//
	// CPython: Objects/typeobject.c:4665 type_new (RuntimeWarning)
	if RuntimeWarnHook != nil && ns != nil {
		for _, k := range ns.Keys() {
			if _, isStr := k.(*Unicode); !isStr {
				if err := RuntimeWarnHook(fmt.Sprintf("non-string key in the __dict__ of class %s", name)); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	// PEP 487: after the class is built, walk the namespace and call
	// __set_name__ on every value that defines it. enum._proto_member
	// uses this hook to turn each placeholder into a real enum member
	// during class creation, so skipping it leaves _member_map_ empty.
	//
	// CPython: Objects/typeobject.c:4549 type_new_set_names
	if err := typeSetNames(t, ns); err != nil {
		return nil, err
	}
	if err := typeInitSubclass(t, kwargs); err != nil {
		return nil, err
	}
	return t, nil
}

// typeUserDealloc releases the descriptor-table entries for a user-defined
// heap type. CPython's tp_dict is a regular dict whose dealloc Decrefs all
// values; in gopy the equivalent is typeDescrTable plus the optional
// ClassAttrDict.
//
// CPython: Objects/typeobject.c:6434 type_dealloc (Py_XDECREF(type->tp_dict))
func typeUserDealloc(o Object) {
	t, ok := o.(*Type)
	if !ok {
		return
	}
	if m, ok := typeDescrTable[t]; ok {
		for _, v := range m {
			Decref(v)
		}
		delete(typeDescrTable, t)
		delete(typeDescrOrder, t)
	}
	if t.ClassAttrDict != nil {
		// ClassAttrDict.SetItem Incref'd every value it holds. Dict.Dealloc is
		// nil so those refs would leak unless we release them here explicitly.
		// CPython: Objects/typeobject.c:6434 type_dealloc (tp_dict Py_DECREF
		// calls through to dict_dealloc which calls PyDict_Clear).
		t.ClassAttrDict.clearContents()
		Decref(t.ClassAttrDict)
		t.ClassAttrDict = nil
	}
	// Release the references this type took on its bases in newTypeE,
	// mirroring type_dealloc decref'ing tp_base / tp_bases / tp_mro. The
	// basesReleased latch keeps this one-to-one with the per-base Incref
	// even if gopy re-enters dealloc on a type whose refcount oscillated
	// through zero.
	//
	// CPython: Objects/typeobject.c:6434 type_dealloc
	//          (Py_XDECREF(type->tp_bases / tp_base / tp_mro))
	if !t.basesReleased {
		t.basesReleased = true
		// Drop this type from each base's tp_subclasses before releasing the
		// base reference, so __subclasses__() stops reporting a type that is
		// being torn down (bpo-46417).
		//
		// CPython: Objects/typeobject.c type_dealloc (remove_all_subclasses)
		for _, b := range t.Bases {
			if b != nil {
				b.removeSubclass(t)
			}
		}
		for _, b := range t.Bases {
			if b != nil {
				Decref(b)
			}
		}
	}
	// Release the metatype reference stampMetaclass took, mirroring
	// type_dealloc's Py_DECREF(Py_TYPE(type)). The latch keeps this paired
	// one-to-one with the single incref even if dealloc re-enters on a type
	// whose refcount oscillated through zero. typeType is immortal, so a
	// type whose metatype is the default decrefs harmlessly here.
	//
	// CPython: Objects/typeobject.c:6434 type_dealloc (Py_DECREF(Py_TYPE(type)))
	if !t.metatypeReleased {
		t.metatypeReleased = true
		Decref(t.Type())
	}
}

// stampMetaclass writes meta onto t so PEP 487 hooks see Py_TYPE(t) ==
// meta. Skips a nil or typeType meta because NewType already wired the
// default.
//
// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
func stampMetaclass(t *Type, meta *Type) {
	if meta != nil && meta != typeType {
		t.Init(meta)
		// A type holds a strong reference to its metatype, exactly as
		// every object holds one to ob_type. CPython allocates the new
		// type through metatype->tp_alloc, which routes through
		// _PyObject_Init and increfs the (heap) metatype; type_dealloc
		// balances it with Py_DECREF(Py_TYPE(type)). Without this incref a
		// metaclass defined in a temporary scope (six.with_metaclass) is
		// freed the moment its defining frame returns even though a class
		// built from it still names it as ob_type, so its tp_dict is
		// cleared out from under the next construction.
		//
		// CPython: Objects/typeobject.c:6434 type_dealloc
		//          (Py_DECREF(Py_TYPE(type)))
		Incref(meta)
	}
}

// applyMetaclassMRO replaces t.MRO with the tuple returned by the
// metaclass's mro() method when the metaclass overrides it. NewType
// already filled t.MRO via c3Linearize, which matches type.mro(); only
// a user override changes the result. Ports mro_invoke's custom branch.
//
// CPython: Objects/typeobject.c:2228 mro_invoke
func applyMetaclassMRO(t *Type, meta *Type) error {
	if meta == nil || meta == typeType {
		return nil
	}
	descr, _ := LookupDescriptor(meta, "mro")
	if descr == nil {
		return nil
	}
	if owner, ok := mroDescrOwner(descr); ok && owner == typeType {
		return nil
	}
	// mro_internal stamps tp_mro only after mro_invoke returns, so the
	// custom mro() sees tp_mro still at its pre-creation value (NULL for a
	// freshly built type). NewType has already filled t.MRO with the C3
	// default; drop it back to nil across the call so cls.__mro__ reports
	// None inside the override, matching CPython. A reentrant __bases__
	// assignment from within mro() may repopulate t.MRO before we return.
	//
	// CPython: Objects/typeobject.c:3552 mro_internal_unlocked (set_tp_mro
	// runs after mro_invoke)
	t.MRO = nil
	bound := bindDescr(descr, t, meta)
	res, err := callBound(bound, nil, nil)
	if err != nil {
		return err
	}
	newMRO, err := mroResultToTypes(res)
	if err != nil {
		return err
	}
	t.MRO = newMRO
	return nil
}

// mroDescrOwner returns the type that owns a descriptor lookup result,
// when the descriptor records its owning class (MethodDescr does). This
// lets applyMetaclassMRO short-circuit when the lookup returned the
// built-in type.mro descriptor unchanged from typeType.
func mroDescrOwner(o Object) (*Type, bool) {
	if md, ok := o.(*MethodDescr); ok {
		return md.owner, true
	}
	return nil, false
}

// hasNoSlotsDeclared reports whether ns lacks a __slots__ entry. object
// itself does not advertise HasDict, but every gopy user class without
// __slots__ has historically carried a dict; the result feeds back into
// configureManagedDict so omitting __slots__ keeps the prior behavior.
func hasNoSlotsDeclared(ns *Dict) bool {
	if ns == nil {
		return true
	}
	has, _ := ns.Contains(NewStr("__slots__"))
	return !has
}

// configureManagedDict inherits HasDict from any base that exposes one
// and stamps MANAGED_DICT / INLINE_VALUES on t. INLINE_VALUES rides
// along only when every base supports it; CPython's type_new gates the
// flag on basicsize fit (no inline-values slot inside int / list / str /
// etc.), so heap subclasses of built-ins drop into the LAZY_DICT shape
// where the dict slot is null until the first store.
//
// CPython: Objects/typeobject.c:4153 type_new (sets
// Py_TPFLAGS_INLINE_VALUES + Py_TPFLAGS_MANAGED_DICT on heap types with
// a managed dict)
func configureManagedDict(t *Type, bases []*Type, noSlotsDeclared bool) {
	inheritedDict := false
	for _, b := range bases {
		if b != nil && b.HasDict {
			t.HasDict = true
			inheritedDict = true
			break
		}
	}
	if noSlotsDeclared {
		t.HasDict = true
	}
	// The __dict__ getset lands on the class that first introduces the
	// managed dict; a class that merely inherits one already sees the
	// descriptor through its MRO. This mirrors dir(C) listing __dict__ for
	// `class C: pass` but not for a subclass of C.
	//
	// CPython: Objects/typeobject.c type_new_descriptors (add_dict gate)
	if t.HasDict && !inheritedDict {
		installInstanceDictDescr(t)
	}
	// HasWeakref tracks tp_weaklistoffset. It inherits from any base that
	// provides weak-reference support, and the no-__slots__ case adds it
	// for the new type whenever the solid base is not a variable-size
	// built-in (the may_add_weak gate). A class declaring __slots__ only
	// gains weakref support when the list names __weakref__, which
	// installSlots handles.
	//
	// CPython: Objects/typeobject.c:4160 type_new_slots (may_add_weak)
	inheritedWeakref := false
	for _, b := range bases {
		if b != nil && mroHasWeakref(b) {
			t.HasWeakref = true
			inheritedWeakref = true
			break
		}
	}
	if !t.HasWeakref && noSlotsDeclared {
		if base, err := bestBase(bases); err == nil && base != nil && typeItemSize(base) == 0 {
			t.HasWeakref = true
		}
	}
	// The __weakref__ getset lands on the class that first introduces
	// weakref support, exactly like __dict__; an inheriting subclass
	// already sees it through the MRO.
	//
	// CPython: Objects/typeobject.c type_new_descriptors (add_weak gate)
	if t.HasWeakref && !inheritedWeakref {
		installInstanceWeakrefDescr(t)
	}
	if !t.HasDict {
		return
	}
	t.TpFlags |= TpFlagManagedDict
	if basesAllowInlineValues(bases, noSlotsDeclared) {
		t.TpFlags |= TpFlagInlineValues
	}
}

// mroHasDict reports whether any class in b's MRO carries a per-instance
// __dict__ (a non-zero tp_dictoffset), the signal type_new reads to gate
// may_add_dict.
//
// CPython: Objects/typeobject.c:4160 type_new_slots (base->tp_dictoffset)
func mroHasDict(b *Type) bool {
	for _, cls := range b.MRO {
		if cls.HasDict {
			return true
		}
	}
	return false
}

// mroHasWeakref is the tp_weaklistoffset companion of mroHasDict.
//
// CPython: Objects/typeobject.c:4161 type_new_slots (base->tp_weaklistoffset)
func mroHasWeakref(b *Type) bool {
	for _, cls := range b.MRO {
		if cls.HasWeakref {
			return true
		}
	}
	return false
}

// basesAllowInlineValues reports whether every non-object base on bases
// still carries INLINE_VALUES, which is the gate for the new type
// keeping the flag.
func basesAllowInlineValues(bases []*Type, noSlotsDeclared bool) bool {
	if !noSlotsDeclared {
		return false
	}
	for _, b := range bases {
		if b == nil || b == objectType {
			continue
		}
		if !b.HasInlineValues() {
			return false
		}
	}
	return true
}

// processClassNamespace patches __classcell__, installs __slots__
// descriptors, and copies the rest of ns onto t.
func processClassNamespace(t *Type, ns *Dict) error {
	if ns == nil {
		return nil
	}
	// type_new operates on dict = PyDict_Copy(orig_dict), never mutating
	// the caller's namespace. The DelItem calls below (__classcell__,
	// __classdictcell__) must therefore land on a copy, so a metaclass that
	// reuses the same namespace dict for a second type() call still sees
	// those keys (test___classcell___wrong_cell).
	//
	// CPython: Objects/typeobject.c:4612 type_new (dict = PyDict_Copy)
	ns = copyClassNamespace(ns)
	// __classcell__ is the cell __build_class__ left in the namespace so
	// we can patch it with the new class. It is not a real attribute,
	// so install it before walking the rest of the namespace and skip
	// it during the descriptor copy.
	classCellKey := NewStr("__classcell__")
	if cellObj, err := ns.GetItem(classCellKey); err == nil {
		// At least one method requires a reference to its defining class.
		// A non-cell __classcell__ is rejected outright.
		// CPython: Objects/typeobject.c:4485 type_new_set_classcell
		cell, ok := cellObj.(*Cell)
		if !ok {
			r, rerr := Repr(cellObj.Type())
			if rerr != nil {
				return rerr
			}
			return fmt.Errorf("TypeError: __classcell__ must be a nonlocal cell, not %s", r)
		}
		cell.Contents = t
		_ = ns.DelItem(classCellKey)
	}
	// __classdictcell__ is the closure cell that PEP 695 type-alias
	// thunks hold as their __classdict__ free variable. CPython sets the
	// cell to tp_dict (the live attribute dict), not to ns. gopy mirrors
	// this by keeping t.ClassAttrDict as the authoritative store and
	// making SetTypeDescr write through to it. The cell is pointed at
	// ClassAttrDict so subsequent typeSetAttr calls (X.T = float) are
	// visible when the thunk later reads X.Alias.__value__.
	//
	// CPython: Objects/typeobject.c:4500 type_new_set_classdictcell
	classDictCellKey := NewStr("__classdictcell__")
	if cellObj, err := ns.GetItem(classDictCellKey); err == nil {
		if cell, ok := cellObj.(*Cell); ok {
			t.ClassAttrDict = NewDict()
			cell.Contents = t.ClassAttrDict
		}
		_ = ns.DelItem(classDictCellKey)
	}
	// __slots__ processing runs before the descriptor copy so the
	// MemberDescr entries land in typeDescrTable before any class body
	// assignments could overwrite them. Errors here are programming
	// bugs in the class body (non-string slot, conflict with class
	// variable, etc.).
	//
	// CPython: Objects/typeobject.c:4397 type_new_slots — raises TypeError/ValueError
	if err := installSlots(t, ns); err != nil {
		return err
	}
	return copyNamespaceToType(t, ns)
}

// copyClassNamespace returns a plain-dict copy of ns in the order
// type_new copies entries into tp_dict, mirroring PyDict_Copy(orig_dict).
// The copy keeps the caller's namespace dict unmutated when type_new
// strips __classcell__ / __classdictcell__.
//
// CPython: Objects/typeobject.c:4612 type_new (dict = PyDict_Copy)
func copyClassNamespace(ns *Dict) *Dict {
	out := NewDict()
	for _, k := range namespaceKeyOrder(ns) {
		v, err := ns.GetItem(k)
		if err != nil || v == nil {
			continue
		}
		if err := out.SetItem(k, v); err != nil {
			continue
		}
	}
	return out
}

// copyNamespaceToType walks ns and installs each entry as a type
// descriptor on t, with the same special-casing CPython performs in
// type_new_set_attrs: __init_subclass__ and __class_getitem__ become
// classmethods when they are plain functions, and __module__ propagates
// onto t.Module so type_repr can render qualified names. __prepare__
// is NOT auto-wrapped; CPython leaves it alone so the user controls
// the binding via @classmethod / @staticmethod / plain function.
//
// CPython: Objects/typeobject.c:4526 type_new_set_attrs
// CPython: Objects/typeobject.c:4372 type_new_classmethod
// namespaceKeyOrder returns ns's keys in the order type_new should copy
// them into tp_dict. CPython does dict = PyDict_Copy(orig_dict), and
// PyDict_Copy/PyDict_Merge walk the source via its Python iterator when
// the source overrides dict.__iter__ (an OrderedDict namespace, per
// bpo-34320). For an exact dict the storage order is already correct, so
// the common class-statement path keeps the cheap ns.Keys() walk.
//
// CPython: Objects/typeobject.c:4612 type_new (dict = PyDict_Copy)
func namespaceKeyOrder(ns *Dict) []Object {
	if !dictIterOverridden(ns.Type()) {
		return ns.Keys()
	}
	keysAttr, err := GetAttr(ns, NewStr("keys"))
	if err != nil {
		return ns.Keys()
	}
	keysObj, err := Call(keysAttr, NewTuple(nil), nil)
	if err != nil {
		return ns.Keys()
	}
	it, err := Iter(keysObj)
	if err != nil {
		return ns.Keys()
	}
	out := make([]Object, 0, ns.Len())
	for {
		k, err := IterNext(it)
		if err != nil {
			if errors.Is(err, ErrStopIteration) {
				return out
			}
			return ns.Keys()
		}
		out = append(out, k)
	}
}

func copyNamespaceToType(t *Type, ns *Dict) error {
	sawDoc := false
	for _, k := range namespaceKeyOrder(ns) {
		s, ok := k.(*Unicode)
		if !ok || s.v == "__slots__" {
			continue
		}
		if s.v == "__doc__" {
			sawDoc = true
		}
		v, err := ns.GetItem(k)
		if err != nil {
			continue
		}
		switch s.v {
		case "__init_subclass__", "__class_getitem__":
			if _, isFn := v.(*Function); isFn {
				v = NewClassMethod(v)
			}
		case "__new__":
			// A __new__ defined as a plain function in the class body is
			// implicitly wrapped in staticmethod, so type.__dict__['__new__']
			// is a staticmethod and the unbound first parameter stays cls.
			// Already-wrapped (@staticmethod / @classmethod) and builtin
			// __new__ are left untouched.
			//
			// CPython: Objects/typeobject.c:4345 type_new_staticmethod
			if _, isFn := v.(*Function); isFn {
				v = NewStaticMethod(v)
			}
		case "__doc__":
			// type_new_set_doc runs PyUnicode_AsUTF8 on a string __doc__;
			// a lone surrogate cannot encode and surfaces the codec's
			// UnicodeEncodeError, aborting the whole class creation. A null
			// byte encodes fine (the C truncation only clips tp_doc, not the
			// __doc__ attribute), and non-string docs are stored verbatim.
			//
			// CPython: Objects/typeobject.c:4496 type_new_set_doc
			if u, ok := v.(*Unicode); ok {
				for i, r := range strLenientRunes(u.v) {
					if r >= 0xD800 && r <= 0xDFFF {
						return fmt.Errorf("UnicodeEncodeError: 'utf-8' codec can't encode character '\\u%04x' in position %d: surrogates not allowed", r, i)
					}
				}
			}
		case "__module__":
			if u, ok := v.(*Unicode); ok {
				t.Module = u.v
			}
		case "__qualname__":
			// type_new_set_ht_name rejects a non-str __qualname__ in the
			// namespace with TypeError before it ever reaches tp_dict.
			//
			// CPython: Objects/typeobject.c:4289 type_new_set_ht_name
			u, ok := v.(*Unicode)
			if !ok {
				return fmt.Errorf("TypeError: type __qualname__ must be a str, not %s", typeNameOf(v))
			}
			t.Qualname = u.v
			// __qualname__ is also stored on the type via the getset
			// path (typeSetQualname), so do not also stash a raw descr
			// for it: the descr table would shadow the getset.
			continue
		case "__type_params__":
			// CPython: Objects/typeobject.c type_new_impl extracts
			// __type_params__ from the namespace into tp_typeparams.
			if tp, ok := v.(*Tuple); ok {
				t.TypeParams = tp
			}
			// Do not install __type_params__ as a regular descr: the
			// getset on typeType serves all lookups via the MRO.
			continue
		case "__annotations__":
			// If the value is a descriptor (e.g. a property), install it
			// directly under "__annotations__" so GenericGetAttr can find
			// and invoke it for instance-level access. A plain dict/mapping
			// goes through typeSetAnnotations so it lands under
			// "__annotations_cache__" as the lazy-evaluation cache.
			//
			// CPython: Objects/typeobject.c:4526 type_new_set_attrs calls
			// PyObject_SetAttr -> type_setattro -> type_set_annotations, which
			// stores the value in tp_dict["__annotations__"] unchanged.
			if v.Type().DescrGet != nil {
				SetTypeDescr(t, "__annotations__", v)
			} else {
				_ = typeSetAnnotations(t, v)
			}
			continue
		case "__annotate__":
			// User-defined __annotate__ from the class body: store directly
			// under __annotate__ so typeGetAnnotate's priority check
			// (user-defined __annotate__ beats compiler-generated
			// __annotate_func__) works correctly. CPython type_new_impl copies
			// the class namespace directly into tp_dict without special-casing
			// __annotate__, so the user's function survives alongside the
			// synthetic __annotate_func__ the compiler emits at end-of-body.
			//
			// CPython: Objects/typeobject.c:4618 type_new_init (PyDict_Copy)
			SetTypeDescr(t, "__annotate__", v)
			continue
		}
		SetTypeDescr(t, s.v, v)
	}
	// type_dict_set_doc: when the type dictionary lacks a __doc__ entry, set it
	// from tp_doc when present and to None otherwise. A user class has no
	// tp_doc, so it defaults to None. This is why every class (docstring or
	// not) has __doc__ in __dict__, and it lets instance __doc__ lookups
	// resolve to the class value instead of falling through to __getattr__.
	//
	// The check is against the type's own dict, not just the namespace: a
	// __slots__ entry named "__doc__" installs a member descriptor under
	// "__doc__" (handled by installSlots above), and that entry must not be
	// clobbered with None.
	//
	// CPython: Objects/typeobject.c:8551 type_dict_set_doc
	if !sawDoc && lookupTypeMember(t, "__doc__") == nil {
		SetTypeDescr(t, "__doc__", None())
	}
	return nil
}

// FormatNoteHook lets the errors package attach a __notes__ string to
// the live exception on the thread state. Installed from vm/eval_call
// so objects does not depend on errors or state.
//
// CPython: Python/errors.c:1567 _PyErr_FormatNote
var FormatNoteHook func(string)

// typeSetNames invokes __set_name__(cls, name) on every namespace
// value that defines it. Mirrors CPython's __set_name__ pass; this
// is what gives PEP 487 descriptors (and enum's _proto_member) a
// chance to rewrite themselves once the owning class is known. On
// failure CPython attaches a note that names the offending instance,
// key, and owner type.
//
// CPython: Objects/typeobject.c:11514 type_new_set_names
func typeSetNames(t *Type, ns *Dict) error {
	if ns == nil {
		return nil
	}
	for _, k := range ns.Keys() {
		s, ok := k.(*Unicode)
		if !ok {
			continue
		}
		v, err := ns.GetItem(k)
		if err != nil {
			continue
		}
		// Look up __set_name__ on the type, not the instance, because
		// it's a descriptor protocol method like __init__.
		setName, _ := LookupDescriptor(v.Type(), "__set_name__")
		if setName == nil {
			continue
		}
		dt := setName.Type()
		var callable Object
		setNameIsNew := dt.DescrGet != nil
		if setNameIsNew {
			bound, err := dt.DescrGet(setName, v, v.Type())
			if err != nil {
				return err
			}
			callable = bound
		} else {
			callable = setName
		}
		_, callErr := Call(callable, NewTuple([]Object{t, s}), nil)
		// CPython: Objects/typeobject.c:6046 type_set_names Py_DECREF(set_name) after call
		if setNameIsNew {
			Decref(callable)
		}
		if callErr != nil {
			if FormatNoteHook != nil {
				keyRepr, rerr := Repr(s)
				keyText := s.Value()
				if rerr == nil {
					keyText = keyRepr
				}
				FormatNoteHook(fmt.Sprintf("Error calling __set_name__ on '%s' instance %s in '%s'",
					typeNameOf(v), keyText, t.Name))
			}
			return callErr
		}
	}
	return nil
}

// typeInitSubclass invokes the parent's __init_subclass__ hook on the
// freshly built subclass. CPython builds super(t, t) and looks
// __init_subclass__ up on it, so the subclass's own override is skipped
// and the bound parent hook runs. Routing through super (rather than a
// hand-rolled MRO walk past index 1) also reproduces supercheck: a type
// whose custom mro() returned an MRO that omits the type itself fails the
// super(t, t) construction with a TypeError, matching gh-92112. kwargs is
// the leftover class-creation kwargs after the metaclass has been pulled
// out, so subclass hooks see `class C(Base, foo=1):` as
// init_subclass(cls, foo=1).
//
// CPython: Objects/typeobject.c:11560 type_new_init_subclass
func typeInitSubclass(t *Type, kwargs map[string]Object) error {
	su, err := NewSuper(t, t)
	if err != nil {
		return err
	}
	// LookupAttrString hands back a new reference: the classmethod bind
	// increfs t into the bound __init_subclass__. CPython Py_DECREFs both
	// super and func once the hook returns; mirror that so the bound method
	// does not keep a refcounted edge to t alive, which would block
	// type_dealloc's remove_all_subclasses for a discarded subclass.
	callable, err := LookupAttrString(su, "__init_subclass__")
	if err != nil {
		return err
	}
	if callable == nil {
		return nil
	}
	defer Decref(callable)
	var kwd *Dict
	if len(kwargs) > 0 {
		kwd = NewDict()
		for k, v := range kwargs {
			if err := kwd.SetItem(NewStr(k), v); err != nil {
				return err
			}
		}
	}
	// CPython: Objects/typeobject.c:11575 PyObject_VectorcallDict(func, NULL, 0, kwds)
	_, callErr := Call(callable, NewTuple(nil), kwd)
	return callErr
}

// fixupSlotDispatchers wires the type's C-level slots to Python-level
// dunders when the class body or any base provides them. Mirrors
// CPython's fixup_slot_dispatchers: each slot's dispatcher does a
// type-MRO lookup of the matching dunder and calls it. Without this
// pass, `class C: def __call__(self): ...` instances would raise
// TypeError on call because the type's Call slot would stay nil.
//
// Order matters: inherit_slots runs first so the subclass picks up the
// base's slot table, then the fixup steps override individual slots
// when the user namespace supplies an overriding dunder. Mirrors the
// PyType_Ready -> inherit_slots -> fixup_slot_dispatchers order in
// CPython.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers
func fixupSlotDispatchers(t *Type) {
	inheritSlotsAllMRO(t)
	for _, base := range t.Bases {
		if base != nil {
			inheritDirectBaseScalars(t, base)
		}
	}
	inheritTpNewFromBestBase(t)
	fixupCallReprStr(t)
	fixupHashAndIter(t)
	fixupRichCmpAndBool(t)
	fixupSubscriptSlots(t)
	fixupDescriptorSlots(t)
	fixupGetattroSlot(t)
	fixupSetattroSlot(t)
	fixupAsyncSlots(t)
	fixupNumberSlots(t)
	fixupTpNew(t)
	fixupFinalize(t)
}

// fixupAsyncSlots wires tp_as_async (am_aiter / am_anext / am_await)
// when the class body (or any user-defined base on the MRO) provides
// __aiter__ / __anext__ / __await__. Without this, async for / async
// with / await on a user class can find the Python-level methods via
// LookupDescriptor but the C-level Async slot stays nil, so the
// dispatcher panel rejects the call as "no __aiter__ method".
//
// CPython: Objects/typeobject.c:10336 update_one_slot (am_aiter /
// am_anext / am_await entries), Objects/typeobject.c slot_am_aiter etc.
func fixupAsyncSlots(t *Type) {
	hasAiter := lookupDunderCallable(t, "__aiter__")
	hasAnext := lookupDunderCallable(t, "__anext__")
	hasAwait := lookupDunderCallable(t, "__await__")
	if !hasAiter && !hasAnext && !hasAwait {
		return
	}
	if t.Async == nil {
		t.Async = &AsyncMethods{}
	}
	if hasAiter {
		t.Async.Aiter = slotAmAiter
	}
	if hasAnext {
		t.Async.Anext = slotAmAnext
	}
	if hasAwait {
		t.Async.Await = slotAmAwait
	}
}

// slotAmAiter dispatches to __aiter__.
//
// CPython: Objects/typeobject.c slot_am_aiter
func slotAmAiter(o Object) (Object, error) {
	// Route through slotDunderNoArg so the looked-up callable is released
	// the way slot_am_aiter's Py_DECREF(func) does. The earlier
	// lookupMethodOnSelf path built a bound method via __get__ and never
	// released it, so the method object pinned self forever; a failed
	// async-for setup then leaked the iterable (test_coroutines test_for_3).
	//
	// CPython: Objects/typeobject.c:9090 slot_am_aiter (call_unbound_noarg + Py_DECREF)
	return slotDunderNoArg(o, "__aiter__")
}

// slotAmAnext dispatches to __anext__.
//
// CPython: Objects/typeobject.c:9112 slot_am_anext
func slotAmAnext(o Object) (Object, error) {
	return slotDunderNoArg(o, "__anext__")
}

// slotAmAwait dispatches to __await__.
//
// CPython: Objects/typeobject.c:9068 slot_am_await
func slotAmAwait(o Object) (Object, error) {
	return slotDunderNoArg(o, "__await__")
}

// fixupFinalize wires tp_finalize when the class body (or any base on
// the MRO) provides __del__. Without this, user __del__ never fires
// during cycle collection because the cycle collector calls Type.Finalize
// directly. Mirrors CPython's slotdefs entry for tp_finalize, which
// update_one_slot resolves to slot_tp_finalize when __del__ is present.
//
// CPython: Objects/typeobject.c:10336 update_one_slot (tp_finalize entry)
// CPython: Objects/typeobject.c:10585 slot_tp_finalize
func fixupFinalize(t *Type) {
	if lookupDunderCallable(t, "__del__") {
		t.Finalize = slotTpFinalize
	}
}

// fixupGetattroSlot wires tp_getattro to a slot dispatcher that calls
// the user's __getattribute__ when the class body (or any user-defined
// base) overrides it. Without this, LookupDescriptor finds the user
// function but the C-level Getattro slot still points at instanceGetAttr
// so attribute access bypasses the override. CPython installs
// _Py_slot_tp_getattr_hook in the same situation; the hook degrades to
// _Py_slot_tp_getattro when no __getattr__ is present, but gopy folds
// the two into one entry point that consults __getattr__ on
// AttributeError.
//
// CPython: Objects/typeobject.c:10336 update_one_slot tp_getattro path
// CPython: Objects/typeobject.c:10341 _Py_slot_tp_getattro
// CPython: Objects/typeobject.c:10373 _Py_slot_tp_getattr_hook
func fixupGetattroSlot(t *Type) {
	descr, owner := LookupDescriptor(t, "__getattribute__")
	if descr == nil || owner == nil {
		return
	}
	if owner == objectType {
		return
	}
	t.Getattro = slotTpGetattroHook
}

// slotTpGetattroHook calls the user's __getattribute__ and falls back
// to __getattr__ on AttributeError. CPython routes both
// _Py_slot_tp_getattro (no __getattr__) and _Py_slot_tp_getattr_hook
// (with __getattr__) through the same call_attribute helper; gopy
// keeps a single dispatcher and consults __getattr__ only when the
// primary call raises AttributeError.
//
// CPython: Objects/typeobject.c:10373 _Py_slot_tp_getattr_hook
func slotTpGetattroHook(o Object, name Object) (Object, error) {
	tp := o.Type()
	getattribute, _ := LookupDescriptor(tp, "__getattribute__")
	if getattribute == nil {
		return GenericGetAttr(o, name)
	}
	fn, err := bindAttrCallable(getattribute, o, tp)
	if err != nil {
		return nil, err
	}
	res, err := Call(fn, NewTuple([]Object{name}), nil)
	if err == nil {
		return res, nil
	}
	if !isAttributeError(err) {
		return nil, err
	}
	getattr, _ := LookupDescriptor(tp, "__getattr__")
	if getattr == nil {
		return nil, err
	}
	fb, err2 := bindAttrCallable(getattr, o, tp)
	if err2 != nil {
		return nil, err2
	}
	return Call(fb, NewTuple([]Object{name}), nil)
}

// fixupSetattroSlot wires tp_setattro to a slot dispatcher that calls
// the user's __setattr__ (or __delattr__ for deletions) when the class
// body, or any user-defined base, overrides them. Without this, attribute
// assignment on an instance of such a class bypasses the Python-level
// override and writes directly to the instance dict.
//
// CPython: Objects/typeobject.c:10336 update_one_slot tp_setattro path
// CPython: Objects/typeobject.c:10406 slot_tp_setattro
func fixupSetattroSlot(t *Type) {
	_, saOwner := LookupDescriptor(t, "__setattr__")
	_, daOwner := LookupDescriptor(t, "__delattr__")
	hasSetattr := saOwner != nil && saOwner != objectType
	hasDelattr := daOwner != nil && daOwner != objectType
	if !hasSetattr && !hasDelattr {
		return
	}
	t.Setattro = slotTpSetattroHook
}

// slotTpSetattroHook dispatches attribute set (value != nil) through the
// type's __setattr__ and attribute delete (value == nil) through
// __delattr__. This is the gopy port of CPython's slot_tp_setattro.
//
// CPython: Objects/typeobject.c:10406 slot_tp_setattro
func slotTpSetattroHook(o Object, name Object, value Object) error {
	tp := o.Type()
	if value != nil {
		sa, _ := LookupDescriptor(tp, "__setattr__")
		if sa != nil {
			bound, err := bindAttrCallable(sa, o, tp)
			if err != nil {
				return err
			}
			_, err = Call(bound, NewTuple([]Object{name, value}), nil)
			return err
		}
		return GenericSetAttr(o, name, value)
	}
	da, _ := LookupDescriptor(tp, "__delattr__")
	if da != nil {
		bound, err := bindAttrCallable(da, o, tp)
		if err != nil {
			return err
		}
		_, err = Call(bound, NewTuple([]Object{name}), nil)
		return err
	}
	return GenericSetAttr(o, name, nil)
}

// bindAttrCallable applies tp_descr_get to attr with (o, tp) so the
// resulting callable already has self bound, mirroring CPython's
// call_attribute helper. Unbound objects (plain functions found on a
// class with no DescrGet wrapping) pass through unchanged so the call
// site can supply self explicitly.
//
// CPython: Objects/typeobject.c:10347 call_attribute
func bindAttrCallable(attr Object, o Object, tp *Type) (Object, error) {
	dt := attr.Type()
	if dt.DescrGet != nil {
		return dt.DescrGet(attr, o, tp)
	}
	return attr, nil
}

// inheritTpNewFromBestBase fixes tp_new to come from best_base rather than
// whichever base happens to be first in the bases list. CPython's
// inherit_special copies tp_new (and the instance layout) from tp_base, the
// solid base chosen by best_base. inheritDirectBaseScalars runs per base in
// list order, so a multiple-inheritance class whose layout-fixing base is not
// first (class C(PyMixin, CStruct) where CStruct carries the instance struct)
// would otherwise inherit the first base's generic object tp_new and allocate
// the wrong struct. Override with the best base's tp_new so the right
// allocator runs; fixupTpNew still routes to slotTpNew when a Python __new__
// is in play.
//
// CPython: Objects/typeobject.c:7019 inherit_special (tp_new from tp_base)
func inheritTpNewFromBestBase(t *Type) {
	base, err := bestBase(t.Bases)
	if err != nil || base == nil {
		return
	}
	if base.TpNew != nil {
		t.TpNew = base.TpNew
	}
}

// fixupTpNew installs slotTpNew when the class body defines its own
// __new__. Without this, typeCallViaTpNew would call the inherited
// C-level tp_new (e.g. int's intTpNew) directly and skip the user's
// Python __new__, so super().__new__(cls, value) shapes are never
// reached.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers
//
//	(slotdefs entry for tp_new)
func fixupTpNew(t *Type) {
	if lookupTypeMember(t, "__new__") != nil {
		t.TpNew = slotTpNew
		t.TpNewWithDict = slotTpNewWithDict
		return
	}
	// inheritDirectBaseScalars only copies TpNew from the primary base
	// (Bases[0]). When a Python-level __new__ lives on a secondary base
	// of a multiple-inheritance class (e.g. class Mock(CallableMixin,
	// NonCallableMock) where NonCallableMock defines __new__), the slot
	// is missed and construction falls through to the built-in tp_new,
	// skipping the user __new__. CPython's update_one_slot walks the
	// full MRO and installs slot_tp_new whenever the resolved __new__ is
	// a Python object rather than the C tp_new_wrapper. Mirror that: walk
	// the MRO and route through slotTpNew when __new__ resolves to a
	// Python function (built-in __new__ stays a BuiltinFunction and keeps
	// the inherited C tp_new).
	//
	// CPython: Objects/typeobject.c:10336 update_one_slot (tp_new entry)
	if fnPtr(t.TpNew) == fnPtr(slotTpNew) {
		return
	}
	newFn, _ := LookupDescriptor(t, "__new__")
	if sm, ok := newFn.(*StaticMethod); ok {
		newFn = sm.smCallable
	}
	if _, ok := newFn.(*Function); ok {
		t.TpNew = slotTpNew
		t.TpNewWithDict = slotTpNewWithDict
	}
}

// fixupCallReprStr wires tp_call, tp_repr, and tp_str.
func fixupCallReprStr(t *Type) {
	if callOverridesVectorcall(t) {
		t.Call = slotTpCall
		t.Vectorcall = nil
		// A generic __call__ is incompatible with vectorcall, so a heap
		// type that defines its own __call__ does not advertise the flag.
		//
		// CPython: Objects/typeobject.c:11392 update_one_slot
		t.TpFlags &^= TpFlagHaveVectorcall
	} else {
		// Inherit Py_TPFLAGS_HAVE_VECTORCALL from the nearest base in the
		// MRO that advertises it, since tp_call was not overridden.
		//
		// CPython: Objects/typeobject.c:8354 inherit_slots
		for _, b := range t.MRO {
			if b != t && b.TpFlags&TpFlagHaveVectorcall != 0 {
				t.TpFlags |= TpFlagHaveVectorcall
				break
			}
		}
	}
	if lookupDunderCallable(t, "__repr__") {
		t.Repr = slotTpRepr
	}
	if lookupDunderCallable(t, "__str__") {
		t.Str = slotTpStr
	} else if t.Repr != nil && t.Str == nil {
		t.Str = t.Repr
	}
}

// unhashableTypeHash is the explicit-unhashable sentinel wired to any
// type (or its subclass) that defines __hash__ = None.
//
// CPython: Objects/typeobject.c:7975 PyObject_HashNotImplemented
func unhashableTypeHash(o Object) (int64, error) {
	return 0, fmt.Errorf("TypeError: unhashable type: '%s'", o.Type().Name)
}

// fixupHashAndIter wires tp_hash, tp_iter, and tp_iternext.
func fixupHashAndIter(t *Type) {
	// A class that defines its own __eq__ but not its own __hash__ does
	// not inherit the base's hash: tp_hash is left NULL during slot
	// inheritance and type_ready then pins __hash__ to None, making the
	// class unhashable. Without this, a str subclass that overrides
	// __eq__ would wrongly keep str.__hash__ (gh-132002).
	//
	// CPython: Objects/typeobject.c:8214 overrides_hash
	// CPython: Objects/typeobject.c:8363 inherit_slots (comparison slots)
	// CPython: Objects/typeobject.c:8758 type_ready_set_hash
	if isOwnDescriptor(t, "__eq__") && !isOwnDescriptor(t, "__hash__") {
		t.Hash = unhashableTypeHash
		SetTypeDescr(t, "__hash__", None())
		if isOwnDescriptor(t, "__iter__") {
			t.Iter = slotTpIter
		}
		if isOwnDescriptor(t, "__next__") {
			t.IterNext = slotTpIterNext
		}
		return
	}
	hashDescr, _ := LookupDescriptor(t, "__hash__")
	switch {
	case hashDescr != nil && hashDescr != None():
		t.Hash = slotTpHash
	case hashDescr == None():
		// __hash__ = None anywhere in MRO signals explicitly unhashable.
		// CPython: Objects/typeobject.c:7975 PyObject_HashNotImplemented
		t.Hash = unhashableTypeHash
	case t.Hash == nil:
		t.Hash = identityHash
	}
	// Only swap to the generic dispatcher when __iter__/__next__ is
	// defined directly on t. A purely inherited descriptor means the
	// C-level Iter slot that inheritSlotsAllMRO already copied (e.g.
	// list's listIter) is the right one; installing slotTpIter in that
	// case routes every iter(subclass) back through a Python-level
	// __iter__ round-trip that over-increfs the source.
	//
	// CPython: Objects/typeobject.c:9874 update_one_slot keeps the
	// inherited wrapper's C function (d_wrapped) when the resolved
	// descriptor is the base wrapper_descriptor, only installing the
	// generic slot for a real override.
	if isOwnDescriptor(t, "__iter__") {
		t.Iter = slotTpIter
	}
	if isOwnDescriptor(t, "__next__") {
		t.IterNext = slotTpIterNext
	}
}

// fixupRichCmpAndBool wires tp_richcompare and nb_bool. The dispatcher
// is only installed when one of the comparison dunders lives directly
// on t. A purely inherited descriptor means the C-level RichCmp slot
// that inheritSlotsAllMRO already copied is the right one; installing
// slotTpRichCompare in that case would route back through object's
// __eq__ slot wrapper and lose the base's tp_richcompare.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers /
// update_one_slot performs the same discrimination via the
// wrapper-vs-method check (PyWrapperDescr_Type whose d_base->wrapper
// matches p->wrapper keeps the C function; anything else swaps in
// p->function).
func fixupRichCmpAndBool(t *Type) {
	if isOwnDescriptor(t, "__eq__") || isOwnDescriptor(t, "__ne__") ||
		isOwnDescriptor(t, "__lt__") || isOwnDescriptor(t, "__le__") ||
		isOwnDescriptor(t, "__gt__") || isOwnDescriptor(t, "__ge__") {
		t.RichCmp = slotTpRichCompare
	}
	if isOwnDescriptor(t, "__bool__") {
		ensureNumberMethods(t).Bool = slotNbBool
	} else if isOwnDescriptor(t, "__len__") {
		ensureNumberMethods(t).Bool = slotNbBoolFromLen
	}
}

// fixupSubscriptSlots wires the mapping/sequence subscription slots
// (length, getitem, setitem, delitem, contains). For each dunder, the
// dispatcher is only installed when the descriptor lives directly on
// t. A descriptor inherited from a base type means the subclass is
// reusing the base's slot wrapper, so the C-level slot that
// inheritSlotsAllMRO already copied stays in place. Installing the
// dispatcher in that case turns into infinite recursion because the
// dispatcher looks up __getitem__ which routes back through the
// dispatcher.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers handles
// the same discrimination through update_one_slot's wrapper-vs-method
// check (PyWrapperDescr_Type with d_base->wrapper == p->wrapper picks
// the underlying C function, anything else picks p->function).
func fixupSubscriptSlots(t *Type) {
	if isOwnDescriptor(t, "__len__") {
		ensureMappingMethods(t).Length = slotMpLength
		ensureSequenceMethods(t).Length = slotMpLength
	}
	if isOwnDescriptor(t, "__getitem__") {
		ensureMappingMethods(t).GetItem = slotMpSubscript
		ensureSequenceMethods(t).GetItem = slotSqGetItem
	}
	if isOwnDescriptor(t, "__setitem__") {
		ensureMappingMethods(t).SetItem = slotMpSubscriptSet
		ensureSequenceMethods(t).SetItem = slotSqSetItem
	}
	if isOwnDescriptor(t, "__delitem__") {
		ensureMappingMethods(t).DelItem = slotMpSubscriptDel
	}
	if isOwnDescriptor(t, "__contains__") {
		ensureSequenceMethods(t).Contains = slotSqContains
	}
}

// isOwnDescriptor reports whether t's namespace itself supplies the
// named dunder. Inherited descriptors come back from LookupDescriptor
// with a different providingType; we treat those as "no override" so
// the inherited slot stays in place. None is also considered an own
// descriptor when it lives directly on t (explicit slot blocker).
//
// CPython: Objects/typeobject.c:10336 update_one_slot (None as blocker)
func isOwnDescriptor(t *Type, name string) bool {
	d, providing := LookupDescriptor(t, name)
	if d == nil {
		return false
	}
	return providing == t
}

// IsOwnDescriptor reports whether name is defined directly on t (not
// just inherited). Exported so builtins can detect user-defined slots.
func IsOwnDescriptor(t *Type, name string) bool {
	return isOwnDescriptor(t, name)
}

// fixupDescriptorSlots wires DescrGet when __get__ exists, DescrSet
// when __set__ or __delete__ exists.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers
//
//	(slotdefs entries for tp_descr_get / tp_descr_set)
func fixupDescriptorSlots(t *Type) {
	if lookupDunderCallable(t, "__get__") {
		t.DescrGet = slotTpDescrGet
	}
	if lookupDunderCallable(t, "__set__") || lookupDunderCallable(t, "__delete__") {
		t.DescrSet = slotTpDescrSet
	}
}

// fixupNumberSlots wires the Number protocol slots (nb_add, nb_subtract,
// nb_multiply, nb_matmul, nb_remainder, nb_divmod, nb_power, nb_lshift,
// nb_rshift, nb_and, nb_xor, nb_or, nb_floordiv, nb_truediv, and inplace
// variants) when the class body or any direct base provides the matching
// dunder. Only wires slots where isOwnDescriptor(t, dunder) is true, so
// inherited slot wrappers from C built-ins are left in place. Mirrors
// CPython's fixup_slot_dispatchers / update_one_slot for the nb_* group.
//
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers
// CPython: Objects/typeobject.c:11291 update_one_slot
// CPython: Objects/typeobject.c:9997 SLOT1BIN / SLOT1BINFULL
func fixupNumberSlots(t *Type) {
	type nbEntry struct {
		dunder  string
		rdunder string
		set     func(*NumberMethods, func(a, b Object) (Object, error))
	}
	entries := []nbEntry{
		{"__add__", "__radd__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Add = f }},
		{"__sub__", "__rsub__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Subtract = f }},
		{"__mul__", "__rmul__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Multiply = f }},
		{"__matmul__", "__rmatmul__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.MatrixMultiply = f }},
		{"__mod__", "__rmod__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Remainder = f }},
		{"__divmod__", "__rdivmod__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Divmod = f }},
		{"__lshift__", "__rlshift__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Lshift = f }},
		{"__rshift__", "__rrshift__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Rshift = f }},
		{"__and__", "__rand__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.And = f }},
		{"__xor__", "__rxor__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Xor = f }},
		{"__or__", "__ror__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.Or = f }},
		{"__floordiv__", "__rfloordiv__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.FloorDivide = f }},
		{"__truediv__", "__rtruediv__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.TrueDivide = f }},
		// Inplace variants — the slot dispatcher always calls the dunder by
		// name so forward and inplace use the same SLOT1BINFULL shape.
		// update_one_slot uses the same slot function for normal and inplace.
		{"__iadd__", "__add__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceAdd = f }},
		{"__isub__", "__sub__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceSubtract = f }},
		{"__imul__", "__mul__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceMultiply = f }},
		{"__imatmul__", "__matmul__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceMatrixMultiply = f }},
		{"__imod__", "__mod__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceRemainder = f }},
		{"__ifloordiv__", "__floordiv__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceFloorDivide = f }},
		{"__itruediv__", "__truediv__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceTrueDivide = f }},
		{"__ilshift__", "__lshift__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceLshift = f }},
		{"__irshift__", "__rshift__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceRshift = f }},
		{"__iand__", "__and__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceAnd = f }},
		{"__ixor__", "__xor__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceXor = f }},
		{"__ior__", "__or__", func(n *NumberMethods, f func(a, b Object) (Object, error)) { n.InPlaceOr = f }},
	}
	for _, e := range entries {
		// A binary number slot (nb_add, nb_pow, ...) is shared by its forward
		// and reflected dunders, so update_one_slot installs it when EITHER is
		// defined: a class that supplies only __radd__ still gets nb_add. The
		// in-place slots (nb_inplace_*) map to the single __i*__ dunder only;
		// their rdunder field names the non-inplace fallback used inside the
		// dispatch, not a second trigger.
		//
		// CPython: Objects/typeobject.c:11291 update_one_slot
		inplace := strings.HasPrefix(e.dunder, "__i")
		if !isOwnDescriptor(t, e.dunder) && (inplace || !isOwnDescriptor(t, e.rdunder)) {
			continue
		}
		dunder := e.dunder
		rdunder := e.rdunder
		fn := makeSlot1Bin(dunder, rdunder)
		e.set(ensureNumberMethods(t), fn)
	}
	// Power and InPlacePower have a 3-arg signature (mod for 3-arg pow()).
	// Wrap slot1BinFull ignoring the mod argument for the 2-arg bytecode form.
	//
	// CPython: Objects/typeobject.c:10129 SLOT1BINFULL(slot_nb_power_binary, ...)
	if isOwnDescriptor(t, "__pow__") || isOwnDescriptor(t, "__rpow__") {
		ensureNumberMethods(t).Power = slotPowerFull
	}
	if isOwnDescriptor(t, "__ipow__") {
		ensureNumberMethods(t).InPlacePower = func(a, b, _ Object) (Object, error) {
			return slot1BinFull(a, b, "__ipow__", "__pow__")
		}
	}
	// Unary number ops.
	if isOwnDescriptor(t, "__neg__") {
		ensureNumberMethods(t).Negative = makeSlotNbUnary("__neg__")
	}
	if isOwnDescriptor(t, "__pos__") {
		ensureNumberMethods(t).Positive = makeSlotNbUnary("__pos__")
	}
	if isOwnDescriptor(t, "__abs__") {
		ensureNumberMethods(t).Absolute = makeSlotNbUnary("__abs__")
	}
	if isOwnDescriptor(t, "__invert__") {
		ensureNumberMethods(t).Invert = makeSlotNbUnary("__invert__")
	}
	if isOwnDescriptor(t, "__int__") {
		ensureNumberMethods(t).Int = makeSlotNbUnary("__int__")
	}
	if isOwnDescriptor(t, "__float__") {
		ensureNumberMethods(t).Float = makeSlotNbUnary("__float__")
	}
	if isOwnDescriptor(t, "__index__") {
		ensureNumberMethods(t).Index = makeSlotNbUnaryIndex("__index__")
	}
}

// makeSlot1Bin returns a Number slot function that implements CPython's
// SLOT1BINFULL dispatch: try dunder on self, handle subtype __rdunder__
// priority, fall back to rdunder on other.
//
// CPython: Objects/typeobject.c:9954 SLOT1BINFULL
func makeSlot1Bin(dunder, rdunder string) func(a, b Object) (Object, error) {
	return func(a, b Object) (Object, error) {
		return slot1BinFull(a, b, dunder, rdunder)
	}
}

// slot1BinFull implements SLOT1BINFULL: call dunder(a, b); if a's type is a
// superclass of b's type and b overloads rdunder, try rdunder first; fall
// through to rdunder(b, a) when dunder returns NotImplemented.
//
// CPython: Objects/typeobject.c:9954 SLOT1BINFULL
func slot1BinFull(a, b Object, dunder, rdunder string) (Object, error) {
	aType := a.Type()
	bType := b.Type()

	// If b's type is a strict subtype of a's type, and b overloads rdunder
	// differently from a, give b a chance to handle it first.
	//
	// CPython: Objects/typeobject.c:9961 (do_other && issubtype check)
	if aType != bType && IsSubtype(bType, aType) {
		if methodIsOverloaded(a, b, rdunder) {
			rd, err := callBinaryDunder(b, a, rdunder)
			if err != nil {
				return nil, err
			}
			if rd != nil && !IsNotImplemented(rd) {
				return rd, nil
			}
		}
	}

	// Try dunder on a.
	//
	// CPython: Objects/typeobject.c:9973 (self dunder call)
	r, err := callBinaryDunder(a, b, dunder)
	if err != nil {
		return nil, err
	}
	if r != nil && !IsNotImplemented(r) {
		return r, nil
	}
	if aType == bType {
		return notImplemented(), nil
	}

	// If b's type also defines rdunder, try it.
	//
	// CPython: Objects/typeobject.c:9985 (do_other rdunder call)
	if IsSubtype(bType, aType) || methodIsOverloaded(a, b, rdunder) {
		rd, err := callBinaryDunder(b, a, rdunder)
		if err != nil {
			return nil, err
		}
		if rd != nil {
			return rd, nil
		}
	}
	return notImplemented(), nil
}

// methodIsOverloaded reports whether b's type defines rdunder differently
// from a's type. Matches CPython's method_is_overloaded: if the descriptor
// is the same object on both types, there is no override.
//
// CPython: Objects/typeobject.c:9921 method_is_overloaded
func methodIsOverloaded(a, b Object, rdunder string) bool {
	da, _ := LookupDescriptor(a.Type(), rdunder)
	db, _ := LookupDescriptor(b.Type(), rdunder)
	if db == nil {
		return false
	}
	return da != db
}

// callBinaryDunder looks up dunder on self.Type() via the MRO and calls
// it with (self, other). Returns (nil, nil) if the dunder is not found.
// If the dunder is explicitly set to None (slot blocker), returns TypeError,
// mirroring CPython's vectorcall_maybe which calls None and gets TypeError.
//
// CPython: Objects/typeobject.c:9960 vectorcall_maybe
// CPython: Objects/typeobject.c:9997 SLOT1BINFULL
func callBinaryDunder(self, other Object, dunder string) (Object, error) {
	descr, _ := LookupDescriptor(self.Type(), dunder)
	if descr == nil {
		return nil, nil
	}
	if IsNone(descr) {
		return nil, fmt.Errorf("TypeError: 'NoneType' object is not callable")
	}
	var bound Object
	if dg := descr.Type().DescrGet; dg != nil {
		var err error
		bound, err = dg(descr, self, self.Type())
		if err != nil {
			return nil, err
		}
	} else {
		bound = descr
	}
	return Call(bound, NewTuple([]Object{other}), nil)
}

// callDunderArgs is callBinaryDunder generalized to any positional arg
// count, so the three-argument power slot can pass the modulus through.
func callDunderArgs(self Object, dunder string, args ...Object) (Object, error) {
	descr, _ := LookupDescriptor(self.Type(), dunder)
	if descr == nil {
		return nil, nil
	}
	if IsNone(descr) {
		return nil, fmt.Errorf("TypeError: 'NoneType' object is not callable")
	}
	bound := descr
	if dg := descr.Type().DescrGet; dg != nil {
		b, err := dg(descr, self, self.Type())
		if err != nil {
			return nil, err
		}
		bound = b
	}
	return Call(bound, NewTuple(args), nil)
}

// slotPowerFull implements slot_nb_power: the two-argument form delegates
// to slot1BinFull, while the three-argument form (pow(a, b, mod) with a
// non-None modulus) threads the modulus into __pow__ / __rpow__ rather
// than dropping it.
//
// CPython: Objects/typeobject.c:10131 slot_nb_power
func slotPowerFull(a, b, mod Object) (Object, error) {
	if mod == nil || IsNone(mod) {
		return slot1BinFull(a, b, "__pow__", "__rpow__")
	}
	aType := a.Type()
	bType := b.Type()
	doOther := aType != bType
	if doOther && IsSubtype(bType, aType) && methodIsOverloaded(a, b, "__rpow__") {
		r, err := callDunderArgs(b, "__rpow__", a, mod)
		if err != nil {
			return nil, err
		}
		if r != nil && !IsNotImplemented(r) {
			return r, nil
		}
		doOther = false
	}
	r, err := callDunderArgs(a, "__pow__", b, mod)
	if err != nil {
		return nil, err
	}
	if r != nil && !IsNotImplemented(r) {
		return r, nil
	}
	if aType == bType {
		return notImplemented(), nil
	}
	if doOther {
		rr, err := callDunderArgs(b, "__rpow__", a, mod)
		if err != nil {
			return nil, err
		}
		if rr != nil {
			return rr, nil
		}
	}
	return notImplemented(), nil
}

// makeSlotNbUnary returns a unary Number slot function that looks up the
// named dunder on self via the MRO and calls it with no arguments.
//
// CPython: Objects/typeobject.c SLOT0 macro
func makeSlotNbUnary(dunder string) func(Object) (Object, error) {
	return func(self Object) (Object, error) {
		return vectorcallMethod(self, dunder)
	}
}

// makeSlotNbUnaryIndex is the __index__ variant: same as unary but the
// result must be an int.
//
// CPython: Objects/typeobject.c slot_nb_index
func makeSlotNbUnaryIndex(dunder string) func(Object) (Object, error) {
	return makeSlotNbUnary(dunder)
}

// ensureNumberMethods allocates t.Number on demand. Built-in types
// share a NumberMethods table; user types start with nil and gain one
// only when fixup wires a numeric slot.
func ensureNumberMethods(t *Type) *NumberMethods {
	if t.Number == nil {
		t.Number = &NumberMethods{}
	}
	return t.Number
}

// ensureMappingMethods allocates t.Mapping on demand.
func ensureMappingMethods(t *Type) *MappingMethods {
	if t.Mapping == nil {
		t.Mapping = &MappingMethods{}
	}
	return t.Mapping
}

// ensureSequenceMethods allocates t.Sequence on demand.
func ensureSequenceMethods(t *Type) *SequenceMethods {
	if t.Sequence == nil {
		t.Sequence = &SequenceMethods{}
	}
	return t.Sequence
}

// lookupDunderCallable returns the named dunder if it is bound on t's
// MRO via a real descriptor (Function, BuiltinFunction, etc.). Plain
// data attributes are ignored: `__hash__ = None` on the class means
// the type is explicitly unhashable.
// callOverridesVectorcall reports whether t resolves __call__ to a real
// override (a Python __call__ or any non-slot-wrapper callable) rather than
// the auto-generated tp_call slot wrapper that a HAVE_VECTORCALL base
// installs. Only a real override forces the generic slot_tp_call path and
// clears Py_TPFLAGS_HAVE_VECTORCALL; inheriting the base's vectorcall wrapper
// keeps the fast path, so MethodDescriptorHeap(Base) and DerivedType(Super)
// stay vectorcall-capable.
//
// CPython: Objects/typeobject.c:11392 update_one_slot (tp_call / vectorcall)
func callOverridesVectorcall(t *Type) bool {
	d, _ := LookupDescriptor(t, "__call__")
	if d == nil || d == None() {
		return false
	}
	// The wrapper AddCallSlotWrapper installs is a method_descriptor named
	// "__call__"; it routes through the owning type's tp_call/vectorcall and
	// is therefore vectorcall-compatible.
	if md, ok := d.(*MethodDescr); ok && md.Name() == "__call__" {
		return false
	}
	return true
}

func lookupDunderCallable(t *Type, name string) bool {
	d, _ := LookupDescriptor(t, name)
	if d == nil {
		return false
	}
	if d == None() {
		return false
	}
	return true
}

// lookupMaybeMethod is the CPython-faithful port of lookup_maybe_method.
// It returns the descriptor plus an unbound flag matching CPython's
// out-parameter contract: when the descriptor's type carries
// METHOD_DESCRIPTOR semantics (gopy's BuiltinFunction / Function pair,
// covered by isMethodLike), the returned object is the raw descriptor
// and unbound=true so the slot dispatcher prepends self before
// invoking it. Otherwise the descriptor is bound through descr_get and
// unbound=false. This preserves the no-temporary-PyMethodObject
// optimization the CPython routine was written to enable.
//
// CPython: Objects/typeobject.c:2255 lookup_maybe_method
func lookupMaybeMethod(self Object, name string) (Object, bool, error) {
	descr, _ := LookupDescriptor(self.Type(), name)
	if descr == nil {
		return nil, false, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", self.Type().Name, name)
	}
	if isMethodLike(descr) {
		return descr, true, nil
	}
	if dg := descr.Type().DescrGet; dg != nil {
		bound, err := dg(descr, self, self.Type())
		return bound, false, err
	}
	return descr, false, nil
}

// callUnboundNoArg invokes fn under the (unbound, self) shape returned
// by lookupMaybeMethod. When unbound is true, self is passed as the
// sole positional argument; when false, fn is already bound and the
// call carries no args.
//
// CPython's call_unbound_noarg passes self on a borrowed vectorcall
// stack, so nothing pins self past the call. gopy threads arguments
// through a real argument tuple, and NewTuple takes a counted reference
// on every item it stores (so self.refs goes up by one for the unbound
// path). That reference must be released once the call returns or it
// leaks: a single d[o]=1 whose key hashes through slot_tp_hash would
// otherwise strand a permanent +1 on the key, and the cycle collector
// would never see the instance go unreachable. Decref on the throwaway
// tuple runs tupleDealloc, which both untracks it from the collector and
// drops the per-item reference, so self is balanced exactly as CPython's
// borrowed stack leaves it.
//
// CPython: Objects/typeobject.c:2308 call_unbound_noarg
func callUnboundNoArg(unbound bool, fn Object, self Object) (Object, error) {
	var args *Tuple
	if unbound {
		args = NewTuple([]Object{self})
	} else {
		args = NewTuple(nil)
	}
	result, err := Call(fn, args, nil)
	Decref(args)
	return result, err
}

// vectorcallMethod ports CPython's vectorcall_method: look name up through
// lookup_maybe_method, invoke it with self prepended when the lookup handed
// back an unbound method-like descriptor (the no-temporary-PyMethodObject
// path) or with the already-bound object otherwise, then release the
// looked-up callable. The trailing Decref is the discipline that keeps a
// __getitem__/__len__/__contains__ dispatch from leaking one reference to
// self on every invocation: without it the temporary bound method pins self
// forever, so the cycle collector never sees the instance go unreachable and
// __del__ never fires. extra carries the positional arguments after self.
//
// CPython: Objects/typeobject.c:2334 vectorcall_method
func vectorcallMethod(o Object, name string, extra ...Object) (Object, error) {
	fn, unbound, err := lookupMaybeMethod(o, name)
	if err != nil {
		return nil, err
	}
	var callArgs []Object
	if unbound {
		callArgs = make([]Object, 0, len(extra)+1)
		callArgs = append(callArgs, o)
		callArgs = append(callArgs, extra...)
	} else {
		callArgs = extra
	}
	args := NewTuple(callArgs)
	res, callErr := Call(fn, args, nil)
	// NewTuple counted a reference on every argument (self plus extra);
	// release the throwaway tuple so tupleDealloc untracks it and drops
	// those per-item references rather than stranding them.
	Decref(args)
	if !unbound {
		Decref(fn)
	}
	return res, callErr
}

// slotTpCall is the generic tp_call dispatcher: look up __call__ via
// the descriptor protocol (so the instance is bound) and call it.
//
// CPython: Objects/typeobject.c:8174 slot_tp_call
func slotTpCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	// A __call__ rebound to another callable instance (A.__call__ = A())
	// recurses through tp_call without ever pushing a Python frame, so the
	// per-frame recursion limit never sees it. Guard the C-level nesting the
	// way CPython's call machinery does so the loop raises RecursionError
	// instead of overflowing the goroutine stack.
	//
	// CPython: Objects/call.c:242 _PyObject_MakeTpCall (Py_EnterRecursiveCall)
	if err := enterRecursiveCall(" while calling a Python object"); err != nil {
		return nil, err
	}
	defer leaveRecursiveCall()
	fn, unbound, err := lookupMaybeMethod(callable, "__call__")
	if err != nil {
		return nil, err
	}
	// When the descriptor is method-like, lookup_maybe_method skips the
	// temporary bound method and we prepend self ourselves
	// (_PyObject_Call_Prepend). Otherwise __get__ already produced a bound
	// object that owns one reference, released by Decref below exactly as
	// CPython's Py_DECREF(meth) does so the callable does not pin self.
	//
	// CPython: Objects/typeobject.c:8174 slot_tp_call
	callArgs := args
	if unbound {
		callArgs = append([]Object{callable}, args...)
	}
	posArgs := NewTuple(callArgs)
	var kwDict *Dict
	if len(kwargs) > 0 {
		kwDict = NewDict()
		for k, v := range kwargs {
			_ = kwDict.SetItem(NewStr(k), v)
		}
	}
	res, callErr := Call(fn, posArgs, kwDict)
	// NewTuple counted a reference on each positional; release the
	// throwaway tuple so tupleDealloc untracks it and drops those
	// per-item references rather than stranding them.
	Decref(posArgs)
	if !unbound {
		Decref(fn)
	}
	return res, callErr
}

// slotDunderNoArg is the shared port of the slot_tp_* dispatchers that
// call a no-argument dunder: look the method up through
// lookup_maybe_method, invoke it through call_unbound_noarg, then
// release the looked-up callable. The unbound flag carries CPython's
// no-temporary-PyMethodObject optimization: when the descriptor is
// method-like (a method_descriptor or plain function), no bound method
// is built and self is threaded in as the lone positional argument, so
// nothing needs releasing. When the descriptor is a non-method (a
// data descriptor whose __get__ returns a fresh bound object), that
// object was created with one reference, so Decref balances it exactly
// as CPython's Py_DECREF(func) does. Skipping the Decref here is the
// leak that kept set subclasses pinned: the bound method held a strong
// reference to self forever, so the cycle collector never saw the
// instance go unreachable.
//
// CPython: Objects/typeobject.c:8235 slot_tp_repr (Py_DECREF(func) after call)
func slotDunderNoArg(o Object, name string) (Object, error) {
	fn, unbound, err := lookupMaybeMethod(o, name)
	if err != nil {
		return nil, err
	}
	res, callErr := callUnboundNoArg(unbound, fn, o)
	if !unbound {
		Decref(fn)
	}
	return res, callErr
}

// slotTpRepr is the generic tp_repr dispatcher: __repr__(self) and
// require the result is a string.
//
// CPython: Objects/typeobject.c:8235 slot_tp_repr
func slotTpRepr(o Object) (string, error) {
	r, err := slotDunderNoArg(o, "__repr__")
	if err != nil {
		return "", err
	}
	s, ok := r.(*Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: __repr__ returned non-string (type %s)", r.Type().Name)
	}
	return s.v, nil
}

// slotTpStr mirrors slotTpRepr for __str__.
//
// CPython: Objects/typeobject.c:8252 slot_tp_str
func slotTpStr(o Object) (string, error) {
	r, err := slotDunderNoArg(o, "__str__")
	if err != nil {
		return "", err
	}
	s, ok := r.(*Unicode)
	if !ok {
		return "", fmt.Errorf("TypeError: __str__ returned non-string (type %s)", r.Type().Name)
	}
	return s.v, nil
}

// slotTpHash dispatches to __hash__. Truncates the returned int to 64
// bits to match CPython's Py_hash_t.
//
// CPython: Objects/typeobject.c:8266 slot_tp_hash
func slotTpHash(o Object) (int64, error) {
	r, err := slotDunderNoArg(o, "__hash__")
	if err != nil {
		return 0, err
	}
	i, ok := r.(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __hash__ method should return an integer")
	}
	v, _ := i.Int64()
	return v, nil
}

// slotTpIter dispatches to __iter__.
//
// CPython: Objects/typeobject.c:8400 slot_tp_iter
func slotTpIter(o Object) (Object, error) {
	return slotDunderNoArg(o, "__iter__")
}

// slotTpIterNext dispatches to __next__.
//
// CPython: Objects/typeobject.c:8421 slot_tp_iternext
func slotTpIterNext(o Object) (Object, error) {
	fn, unbound, err := lookupMaybeMethod(o, "__next__")
	if err != nil {
		// CPython: Objects/typeobject.c:8421 slot_tp_iternext — when
		// __next__ is absent (deleted after class creation), the slot
		// still fires but the lookup fails. CPython returns NULL which
		// FOR_ITER treats as end-of-iteration; raise TypeError so callers
		// that bypassed FOR_ITER also see a sensible error.
		return nil, fmt.Errorf("TypeError: '%s' object is not an iterator", o.Type().Name)
	}
	res, callErr := callUnboundNoArg(unbound, fn, o)
	if !unbound {
		Decref(fn)
	}
	return res, callErr
}

// slotTpFinalize dispatches to the user's __del__. Errors raised by
// __del__ are routed through sys.unraisablehook via WriteUnraisableHook,
// matching CPython's PyErr_FormatUnraisable. The instance's __del__
// repr is the "calling deallocator" suffix.
//
// CPython: Objects/typeobject.c:10585 slot_tp_finalize
// CPython: Python/errors.c:1380 _PyErr_WriteUnraisable
func slotTpFinalize(o Object) {
	fn, unbound, err := lookupMaybeMethod(o, "__del__")
	if err != nil {
		return
	}
	_, callErr := callUnboundNoArg(unbound, fn, o)
	if callErr == nil {
		return
	}
	if h := WriteUnraisableHook; h != nil {
		msg := "Exception ignored while calling deallocator"
		if fn != nil {
			if s, reprErr := Repr(fn); reprErr == nil {
				msg = "Exception ignored while calling deallocator " + s
			}
		}
		h(o, msg, callErr)
	}
}

// slotTpRichCompare looks up the dunder that matches op and calls it,
// returning NotImplemented when the dunder is absent so the protocol
// can try the reflected operator on the other operand. When the dunder
// is explicitly None (slot blocker), raises TypeError as CPython does.
//
// CPython: Objects/typeobject.c:8347 slot_tp_richcompare
// CPython: Objects/typeobject.c:3037 vectorcall_maybe (None raises TypeError)
func slotTpRichCompare(a, b Object, op CompareOp) (Object, error) {
	name := richCompareDunderName(op)
	fn, unbound, err := lookupMaybeMethod(a, name)
	if err != nil {
		// A missing special method, or one whose descriptor __get__ raises
		// AttributeError, both read as "no such comparison" and fall back
		// to NotImplemented; only descr_get's AttributeError is cleared,
		// any other error propagates.
		//
		// CPython: Objects/typeobject.c:2914 lookup_method_ex (AttributeError cleared)
		// CPython: Objects/typeobject.c:10454 slot_tp_richcompare
		if isAttributeError(err) {
			return notImplemented(), nil
		}
		return nil, err
	}
	if IsNone(fn) {
		return nil, fmt.Errorf("TypeError: 'NoneType' object is not callable")
	}
	callArgs := []Object{b}
	if unbound {
		callArgs = []Object{a, b}
	}
	args := NewTuple(callArgs)
	res, callErr := Call(fn, args, nil)
	Decref(args)
	if !unbound {
		Decref(fn)
	}
	return res, callErr
}

// richCompareDunderName maps CompareOp to the dunder method name.
func richCompareDunderName(op CompareOp) string {
	switch op {
	case CompareLT:
		return "__lt__"
	case CompareLE:
		return "__le__"
	case CompareEQ:
		return "__eq__"
	case CompareNE:
		return "__ne__"
	case CompareGT:
		return "__gt__"
	case CompareGE:
		return "__ge__"
	}
	return ""
}

// slotNbBool dispatches to __bool__, requiring a bool result.
//
// CPython: Objects/typeobject.c:7869 slot_nb_bool
func slotNbBool(o Object) (bool, error) {
	r, err := vectorcallMethod(o, "__bool__")
	if err != nil {
		return false, err
	}
	switch r {
	case trueSingleton:
		return true, nil
	case falseSingleton:
		return false, nil
	}
	return false, fmt.Errorf("TypeError: __bool__ should return bool, returned %s", r.Type().Name)
}

// slotNbBoolFromLen falls back to __len__ when no __bool__ exists.
//
// CPython: Objects/typeobject.c:7891 slot_nb_bool (PyObject_Size path)
func slotNbBoolFromLen(o Object) (bool, error) {
	n, err := slotMpLength(o)
	if err != nil {
		return false, err
	}
	return n != 0, nil
}

// slotMpLength dispatches to __len__ and validates the result.
//
// CPython: Objects/typeobject.c:7948 slot_mp_length / slot_sq_length
func slotMpLength(o Object) (int, error) {
	r, err := vectorcallMethod(o, "__len__")
	if err != nil {
		return 0, err
	}
	// _PyNumber_Index coerces the result through __index__, so a
	// non-integer (e.g. 4.5) raises TypeError before any range check.
	idx, err := NumberIndex(r)
	if err != nil {
		return 0, err
	}
	i, ok := idx.(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __len__ should return int, returned %s", r.Type().Name)
	}
	// The negative test runs before the ssize_t range check, matching
	// slot_sq_length: a huge negative __len__ is reported as ValueError,
	// not OverflowError.
	if i.BigInt().Sign() < 0 {
		return 0, fmt.Errorf("ValueError: __len__() should return >= 0")
	}
	v, fits := i.Int64()
	if !fits {
		return 0, fmt.Errorf("OverflowError: cannot fit 'int' into an index-sized integer")
	}
	return int(v), nil
}

// slotMpSubscript dispatches to __getitem__ for mapping-style access.
//
// CPython: Objects/typeobject.c:7989 slot_mp_subscript
func slotMpSubscript(o Object, key Object) (Object, error) {
	return vectorcallMethod(o, "__getitem__", key)
}

// slotSqGetItem dispatches __getitem__ for sequence-style int indexing.
// Boxes the index into an Int so the user method sees the same type
// CPython hands to PyObject_GetItem.
//
// CPython: Objects/typeobject.c:7964 slot_sq_item
func slotSqGetItem(o Object, idx int) (Object, error) {
	return vectorcallMethod(o, "__getitem__", NewInt(int64(idx)))
}

// slotMpSubscriptSet dispatches __setitem__.
//
// CPython: Objects/typeobject.c:8004 slot_mp_ass_subscript (set branch)
func slotMpSubscriptSet(o, key, value Object) error {
	_, err := vectorcallMethod(o, "__setitem__", key, value)
	return err
}

// slotMpSubscriptDel dispatches __delitem__.
//
// CPython: Objects/typeobject.c:8004 slot_mp_ass_subscript (del branch)
func slotMpSubscriptDel(o, key Object) error {
	_, err := vectorcallMethod(o, "__delitem__", key)
	return err
}

// slotSqSetItem dispatches __setitem__ for sequence-style int indexing.
// value == nil routes through __delitem__.
//
// CPython: Objects/typeobject.c:7977 slot_sq_ass_item
func slotSqSetItem(o Object, idx int, value Object) error {
	key := NewInt(int64(idx))
	if value == nil {
		return slotMpSubscriptDel(o, key)
	}
	return slotMpSubscriptSet(o, key, value)
}

// slotTpNew dispatches tp_new for Python-defined classes. Looks up
// __new__ via MRO, unwraps the implicit staticmethod, prepends cls to
// the positional args, and calls the underlying function. Mirrors
// CPython's slot_tp_new: the dispatcher is installed by
// fixup_slot_dispatchers whenever a class body defines __new__.
//
// CPython: Objects/typeobject.c:9395 slot_tp_new
func slotTpNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	// Bind through getattr so a __new__ defined as a classmethod or
	// staticmethod is unwrapped by its own __get__ (classmethod yields a
	// method already bound to cls, which is why C(1) reaches the body as
	// __new__(cls, cls, 1)). slot_tp_new uses GetAttr for exactly this.
	//
	// CPython: Objects/typeobject.c:9395 slot_tp_new
	newFn, err := GetAttr(cls, NewStr("__new__"))
	if err != nil {
		return nil, err
	}
	if newFn == nil {
		return nil, fmt.Errorf("TypeError: object.__new__: cannot find __new__ for '%s'", cls.Name)
	}
	posArgs := make([]Object, 0, len(args)+1)
	posArgs = append(posArgs, cls)
	posArgs = append(posArgs, args...)
	var kwDict *Dict
	if len(kwargs) > 0 {
		kwDict = NewDict()
		for k, v := range kwargs {
			_ = kwDict.SetItem(NewStr(k), v)
		}
	}
	return Call(newFn, NewTuple(posArgs), kwDict)
}

// slotTpNewWithDict is slotTpNew with the keyword arguments preserved as an
// insertion-ordered *Dict rather than a Go map. typeCallViaTpNewWithDict calls
// this in preference to slotTpNew so the __new__ implementation observes the
// keyword order supplied at the call site. This matters for types like
// functools.partial / partialmethod whose __new__ stores **keywords verbatim:
// rebuilding the dict from a Go map (slotTpNew) shuffles the keys, so
// partialmethod(capture, self=1, func=2) reprs in random order. CPython's
// slot_tp_new receives the original ordered kwds dict, so mirror that.
//
// CPython: Objects/typeobject.c:9395 slot_tp_new
func slotTpNewWithDict(cls *Type, args []Object, kwargs *Dict) (Object, error) {
	// Bind through getattr (see slotTpNew) so classmethod/staticmethod
	// __new__ are unwrapped by their own descriptor protocol.
	//
	// CPython: Objects/typeobject.c:9395 slot_tp_new
	newFn, err := GetAttr(cls, NewStr("__new__"))
	if err != nil {
		return nil, err
	}
	if newFn == nil {
		return nil, fmt.Errorf("TypeError: object.__new__: cannot find __new__ for '%s'", cls.Name)
	}
	posArgs := make([]Object, 0, len(args)+1)
	posArgs = append(posArgs, cls)
	posArgs = append(posArgs, args...)
	return Call(newFn, NewTuple(posArgs), kwargs)
}

// slotTpDescrGet dispatches __get__(self, obj, type). obj is None when
// the descriptor is accessed through the class rather than an instance.
//
// CPython: Objects/typeobject.c:8444 slot_tp_descr_get
func slotTpDescrGet(descr Object, obj Object, tp *Type) (Object, error) {
	// CPython looks __get__ up raw (no descriptor binding) and calls it with
	// the explicit (self, obj, type) stack. Binding self here would drop a
	// positional argument when __get__ is a plain built-in function rather
	// than a method-like descriptor (bpo-25750's bad_get is exactly that).
	get, _ := LookupDescriptor(descr.Type(), "__get__")
	if get == nil {
		return descr, nil
	}
	objArg := None()
	if obj != nil {
		objArg = obj
	}
	typeArg := None()
	if tp != nil {
		typeArg = tp
	}
	return Call(get, NewTuple([]Object{descr, objArg, typeArg}), nil)
}

// slotTpDescrSet dispatches __set__(self, obj, value) or
// __delete__(self, obj) when value is nil.
//
// CPython: Objects/typeobject.c:8456 slot_tp_descr_set
func slotTpDescrSet(descr Object, obj Object, value Object) error {
	var err error
	if value == nil {
		_, err = vectorcallMethod(descr, "__delete__", obj)
	} else {
		_, err = vectorcallMethod(descr, "__set__", obj, value)
	}
	return err
}

// slotSqContains dispatches __contains__.
//
// CPython: Objects/typeobject.c:8064 slot_sq_contains
func slotSqContains(o Object, key Object) (bool, error) {
	r, err := vectorcallMethod(o, "__contains__", key)
	if err != nil {
		return false, err
	}
	return IsTruthy(r)
}

// mangleSlotName applies PEP 8 private name mangling to a slot name.
// Slot names starting with "__" (but not ending with "__") are rewritten to
// "_<stripped-class-name><name>", mirroring _Py_Mangle.
//
// CPython: Python/symtable.c:3207 _Py_Mangle
func mangleSlotName(className, name string) string {
	if len(name) < 2 || name[0] != '_' || name[1] != '_' || strings.HasSuffix(name, "__") {
		return name
	}
	stripped := strings.TrimLeft(className, "_")
	if stripped == "" {
		return name
	}
	return "_" + stripped + name
}

// installSlots reads __slots__ from ns, validates it, and registers a
// MemberDescr per slot on t. Mirrors the slice of CPython's type_new
// pipeline that runs type_new_slots + type_new_descriptors:
//   - __slots__ may be a string (treated as a single name) or any
//     iterable of strings;
//   - "__dict__" enables the per-instance dict (HasDict);
//   - "__weakref__" is recognized and skipped (gopy weakref support
//     does not yet plumb a per-instance offset);
//   - other names must be valid identifiers and must not collide with
//     names already bound in the class body.
//
// CPython: Objects/typeobject.c:4155 type_new_slots /
//
//	Objects/typeobject.c:4401 type_new_descriptors
func installSlots(t *Type, ns *Dict) error {
	t.SlotsBase = layoutSlotBase(t)
	slotsKey := NewStr("__slots__")
	has, err := ns.Contains(slotsKey)
	if err != nil || !has {
		return err
	}
	raw, err := ns.GetItem(slotsKey)
	if err != nil {
		return err
	}
	names, err := slotsToNames(raw)
	if err != nil {
		return err
	}
	// type_new_slots: a non-empty __slots__ is rejected outright when the
	// solid base is a variable-size built-in (int, tuple, bytes, str),
	// whose instances cannot grow a fixed slot array. may_add_dict /
	// may_add_weak then gate the two special slot names against a base
	// that already supplies that storage.
	//
	// CPython: Objects/typeobject.c:4124 type_new_slots_impl
	base, _ := bestBase(t.Bases)
	if base == nil {
		base = objectType
	}
	itemSize := typeItemSize(base)
	if len(names) > 0 && itemSize != 0 {
		return fmt.Errorf("TypeError: nonempty __slots__ not supported for subtype of '%s'", base.Name)
	}
	mayAddDict := !mroHasDict(base)
	mayAddWeak := !mroHasWeakref(base) && itemSize == 0
	addDict, addWeak := 0, 0
	resolved := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		switch n {
		case "__dict__":
			// CPython: Objects/typeobject.c:3989 type_new_visit_slots
			if !mayAddDict || addDict != 0 {
				return fmt.Errorf("TypeError: __dict__ slot disallowed: we already got one")
			}
			addDict++
			t.HasDict = true
			// configureManagedDict ran before __slots__ was parsed, so the
			// managed-dict flag and the __dict__ getset it would have stamped
			// for a no-__slots__ class were skipped. A class that names
			// __dict__ in __slots__ still gets a per-instance dict
			// (tp_dictoffset), so wire both here. Inline values stay off:
			// they ride only on classes that omit __slots__ entirely.
			//
			// CPython: Objects/typeobject.c:4153 type_new (managed dict gate)
			t.TpFlags |= TpFlagManagedDict
			installInstanceDictDescr(t)
			continue
		case "__weakref__":
			// CPython: Objects/typeobject.c:3998 type_new_visit_slots
			if !mayAddWeak || addWeak != 0 {
				return fmt.Errorf("TypeError: __weakref__ slot disallowed: we already got one")
			}
			addWeak++
			t.HasWeakref = true
			// configureManagedDict ran before __slots__ was parsed, so the
			// __weakref__ getset it stamps for the no-__slots__ case was
			// skipped. A class naming __weakref__ in __slots__ still gets
			// weakref support, so wire the descriptor here.
			installInstanceWeakrefDescr(t)
			continue
		}
		if !StrIsIdentifier(n) {
			return fmt.Errorf("TypeError: __slots__ must be identifiers")
		}
		// Apply PEP 8 private name mangling: "__x" in class "Foo" → "_Foo__x".
		// Mirrors _Py_Mangle which type_new_copy_slots calls on every slot name.
		//
		// CPython: Objects/typeobject.c:4037 type_new_copy_slots (_Py_Mangle call)
		n = mangleSlotName(t.Name, n)
		if seen[n] {
			continue
		}
		seen[n] = true
		// Conflict with a class body assignment of the same name. The
		// __slots__ entry itself lives under the "__slots__" key so it
		// does not appear in this check.
		// __qualname__ and __classcell__ are inserted into the namespace by
		// the class-creation machinery and deleted before the type's dict is
		// finalized, so they never act as class variables. Exempt them from
		// the conflict check (a bad type for either still raises TypeError
		// later, during qualname/classcell validation).
		//
		// CPython: Objects/typeobject.c:4015 type_new_slots_impl
		if has, _ := ns.Contains(NewStr(n)); has && n != "__qualname__" && n != "__classcell__" {
			return fmt.Errorf("ValueError: '%s' in __slots__ conflicts with class variable", n)
		}
		resolved = append(resolved, n)
	}
	// Sort the mangled slot names before assigning member offsets. The
	// sorted order makes two classes that declare the same slots (in any
	// order) lay their instances out identically, which is what lets
	// __class__ be reassigned between them with the slot values intact.
	//
	// CPython: Objects/typeobject.c:4046 type_new_slots_impl (PyList_Sort)
	sort.Strings(resolved)
	for i, n := range resolved {
		SetTypeDescr(t, n, NewMemberDescr(t, n, t.SlotsBase+i))
	}
	t.Slots = resolved
	// Keep __slots__ accessible as a class attribute. CPython normalizes
	// __slots__ into a tuple only for its internal et->ht_slots store; the
	// value visible from Python (tp_dict['__slots__']) is the user's
	// ORIGINAL object, copied verbatim from the namespace by PyDict_Copy.
	// So a list stays a list, a str stays a str, and the names are left
	// unmangled (the mangling applies only to the member descriptors). This
	// is what copyreg._reduce_ex inspects via getattr(inst, '__slots__').
	// Storing the raw value directly (a non-data descriptor) keeps instance
	// assignments like `self.__slots__ = None` landing in __dict__.
	//
	// CPython: Objects/typeobject.c:4401 type_new_descriptors / 4618 PyDict_Copy
	SetTypeDescr(t, "__slots__", raw)
	_ = ns.DelItem(slotsKey)
	return nil
}

// layoutSlotBase picks the layout base from t.Bases and returns the
// cumulative slot count up to (but not including) the current class.
// CPython's type_new walks the MRO looking for a "solid base" whose
// PyMemberDef offsets the child must respect; gopy mirrors the same
// behavior by taking the first non-object base's SlotsBase + len(Slots)
// as the new layout offset.
//
// CPython: Objects/typeobject.c:4086 type_new_slots_bases (best_base
// selection feeds slotoffset)
func layoutSlotBase(t *Type) int {
	for _, b := range t.Bases {
		if b == nil || b == objectType {
			continue
		}
		return b.SlotsBase + len(b.Slots)
	}
	return 0
}

// slotsToNames flattens the value of __slots__ into a list of strings.
// Accepts a single str, a tuple, or a list. Anything else raises.
//
// CPython: Objects/typeobject.c:3977 type_new_slots (PySequence_Tuple)
func slotsToNames(v Object) ([]string, error) {
	if s, ok := v.(*Unicode); ok {
		return []string{s.v}, nil
	}
	switch seq := v.(type) {
	case *Tuple:
		out := make([]string, 0, seq.Len())
		for i := 0; i < seq.Len(); i++ {
			s, ok := seq.Item(i).(*Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: __slots__ items must be strings, not '%s'", typeNameOf(seq.Item(i)))
			}
			out = append(out, s.v)
		}
		return out, nil
	case *List:
		out := make([]string, 0, seq.Len())
		for i := 0; i < seq.Len(); i++ {
			s, ok := seq.Item(i).(*Unicode)
			if !ok {
				return nil, fmt.Errorf("TypeError: __slots__ items must be strings, not '%s'", typeNameOf(seq.Item(i)))
			}
			out = append(out, s.v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("TypeError: __slots__ must be a string or iterable of strings, not '%s'", typeNameOf(v))
}
