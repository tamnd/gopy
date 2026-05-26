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

import "fmt"

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
		// CPython: Objects/typeobject.c:7521 inherit_slots (tp_new slot)
		t.TpNew = DictType.TpNew
	case IsSubtype(t, strType):
		t.Getattro = strSubclassGetAttr
		t.Setattro = strSubclassSetAttr
		t.TpNew = strType.TpNew
	case IsSubtype(t, IntType):
		t.Getattro = intSubclassGetAttr
		t.Setattro = intSubclassSetAttr
		t.TpNew = IntType.TpNew
	default:
		t.Getattro = instanceGetAttr
		t.Setattro = instanceSetAttr
	}
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
	if len(bases) == 0 {
		bases = []*Type{objectType}
	}
	t, err := newTypeE(name, bases)
	if err != nil {
		return nil, err
	}
	t.IsUser = true
	// Heap (user) types are mutable: drop the IMMUTABLETYPE flag that
	// NewType stamps on by default so collections.abc registrations
	// can still paint sequence/mapping bits onto user subclasses.
	//
	// CPython: Objects/typeobject.c:4153 type_new (heap types lack
	// Py_TPFLAGS_IMMUTABLETYPE)
	t.TpFlags &^= TpFlagImmutable
	stampMetaclass(t, meta)
	if err := applyMetaclassMRO(t, meta); err != nil {
		return nil, err
	}
	installSubclassAttrSlots(t)
	noSlotsDeclared := hasNoSlotsDeclared(ns)
	configureManagedDict(t, bases, noSlotsDeclared)
	processClassNamespace(t, ns)
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

// stampMetaclass writes meta onto t so PEP 487 hooks see Py_TYPE(t) ==
// meta. Skips a nil or typeType meta because NewType already wired the
// default.
//
// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
func stampMetaclass(t *Type, meta *Type) {
	if meta != nil && meta != typeType {
		t.Init(meta)
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
	bound := bindDescr(descr, t, meta)
	res, err := callBound(bound, nil, nil)
	if err != nil {
		return err
	}
	tup, ok := res.(*Tuple)
	if !ok {
		lst, ok := res.(*List)
		if !ok {
			return fmt.Errorf("TypeError: mro() returned a non-tuple: %s", typeNameOf(res))
		}
		items := make([]Object, lst.Len())
		for i := 0; i < lst.Len(); i++ {
			items[i] = lst.Item(i)
		}
		tup = NewTuple(items)
	}
	newMRO := make([]*Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		entry, ok := tup.Item(i).(*Type)
		if !ok {
			return fmt.Errorf("TypeError: mro() returned a non-type at index %d: %s", i, typeNameOf(tup.Item(i)))
		}
		newMRO = append(newMRO, entry)
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
	for _, b := range bases {
		if b != nil && b.HasDict {
			t.HasDict = true
			break
		}
	}
	if noSlotsDeclared {
		t.HasDict = true
	}
	if !t.HasDict {
		return
	}
	t.TpFlags |= TpFlagManagedDict
	if basesAllowInlineValues(bases, noSlotsDeclared) {
		t.TpFlags |= TpFlagInlineValues
	}
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
func processClassNamespace(t *Type, ns *Dict) {
	if ns == nil {
		return
	}
	// __classcell__ is the cell __build_class__ left in the namespace so
	// we can patch it with the new class. It is not a real attribute,
	// so install it before walking the rest of the namespace and skip
	// it during the descriptor copy.
	classCellKey := NewStr("__classcell__")
	if cellObj, err := ns.GetItem(classCellKey); err == nil {
		if cell, ok := cellObj.(*Cell); ok {
			cell.Contents = t
		}
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
	// variable, etc.). CPython raises TypeError/ValueError; gopy's
	// NewUserType has no error channel yet, so panic with the same
	// text.
	if err := installSlots(t, ns); err != nil {
		panic(err)
	}
	copyNamespaceToType(t, ns)
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
func copyNamespaceToType(t *Type, ns *Dict) {
	for _, k := range ns.Keys() {
		s, ok := k.(*Unicode)
		if !ok || s.v == "__slots__" {
			continue
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
		case "__module__":
			if u, ok := v.(*Unicode); ok {
				t.Module = u.v
			}
		case "__qualname__":
			if u, ok := v.(*Unicode); ok {
				t.Qualname = u.v
			}
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
		}
		SetTypeDescr(t, s.v, v)
	}
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
		if dt.DescrGet != nil {
			bound, err := dt.DescrGet(setName, v, v.Type())
			if err != nil {
				return err
			}
			callable = bound
		} else {
			callable = setName
		}
		if _, err := Call(callable, NewTuple([]Object{t, s}), nil); err != nil {
			if FormatNoteHook != nil {
				keyRepr, rerr := Repr(s)
				keyText := s.Value()
				if rerr == nil {
					keyText = keyRepr
				}
				FormatNoteHook(fmt.Sprintf("Error calling __set_name__ on '%s' instance %s in '%s'",
					typeNameOf(v), keyText, t.Name))
			}
			return err
		}
	}
	return nil
}

// typeInitSubclass invokes the parent's __init_subclass__ hook on the
// freshly built subclass. CPython runs this from type_new after the
// type is fully constructed; it walks the MRO starting one position
// past `t` (via super(t, t)) so the subclass's own override does not
// recursively reapply. kwargs is the leftover class-creation kwargs
// after the metaclass has been pulled out, so subclass hooks see
// `class C(Base, foo=1):` as init_subclass(cls, foo=1).
//
// CPython: Objects/typeobject.c:4595 type_init_subclass
func typeInitSubclass(t *Type, kwargs map[string]Object) error {
	for i := 1; i < len(t.MRO); i++ {
		base := t.MRO[i]
		descr, _ := lookupOnType(base, "__init_subclass__")
		if descr == nil {
			continue
		}
		dt := descr.Type()
		var callable Object
		if dt.DescrGet != nil {
			bound, err := dt.DescrGet(descr, t, t)
			if err != nil {
				return err
			}
			callable = bound
		} else {
			callable = descr
		}
		var kwd *Dict
		if len(kwargs) > 0 {
			kwd = NewDict()
			for k, v := range kwargs {
				if err := kwd.SetItem(NewStr(k), v); err != nil {
					return err
				}
			}
		}
		_, err := Call(callable, NewTuple(nil), kwd)
		if err != nil {
			return err
		}
		return nil
	}
	return nil
}

// lookupOnType returns the descriptor stored directly in t's
// typeDescrTable (no MRO walk). Used by typeInitSubclass to find the
// first ancestor that owns __init_subclass__.
func lookupOnType(t *Type, name string) (Object, bool) {
	if descrs, ok := typeDescrTable[t]; ok {
		if v, ok := descrs[name]; ok {
			return v, true
		}
	}
	return nil, false
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
	fixupCallReprStr(t)
	fixupHashAndIter(t)
	fixupRichCmpAndBool(t)
	fixupSubscriptSlots(t)
	fixupDescriptorSlots(t)
	fixupGetattroSlot(t)
	fixupAsyncSlots(t)
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
	fn, err := lookupMethodOnSelf(o, "__aiter__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotAmAnext dispatches to __anext__.
//
// CPython: Objects/typeobject.c slot_am_anext
func slotAmAnext(o Object) (Object, error) {
	fn, err := lookupMethodOnSelf(o, "__anext__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotAmAwait dispatches to __await__.
//
// CPython: Objects/typeobject.c slot_am_await
func slotAmAwait(o Object) (Object, error) {
	fn, err := lookupMethodOnSelf(o, "__await__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
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
	}
}

// fixupCallReprStr wires tp_call, tp_repr, and tp_str.
func fixupCallReprStr(t *Type) {
	if lookupDunderCallable(t, "__call__") {
		t.Call = slotTpCall
		t.Vectorcall = nil
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

// fixupHashAndIter wires tp_hash, tp_iter, and tp_iternext.
func fixupHashAndIter(t *Type) {
	if lookupDunderCallable(t, "__hash__") {
		t.Hash = slotTpHash
	} else if t.Hash == nil {
		t.Hash = identityHash
	}
	if lookupDunderCallable(t, "__iter__") {
		t.Iter = slotTpIter
	}
	if lookupDunderCallable(t, "__next__") {
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
// named dunder via a real descriptor. Inherited descriptors come back
// from LookupDescriptor with a different providingType; we treat those
// as "no override" so the inherited slot stays in place.
func isOwnDescriptor(t *Type, name string) bool {
	d, providing := LookupDescriptor(t, name)
	if d == nil || d == None() {
		return false
	}
	return providing == t
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

// lookupMethodOnSelf finds name on type(o)'s MRO and applies descr_get
// with o as the bound instance. Mirrors CPython's lookup_maybe_method:
// slot dispatchers must look up via the *type* of self (not via self's
// own attribute path) so that, for example, hash(C) on a class C finds
// the metaclass's __hash__ entry rather than treating C as if it were
// an instance of itself. GetAttr returns the descriptor unbound when
// the receiver is a class, which breaks the no-arg call shape every
// slot dispatcher relies on.
//
// CPython: Objects/typeobject.c:2255 lookup_maybe_method
func lookupMethodOnSelf(o Object, name string) (Object, error) {
	descr, _ := LookupDescriptor(o.Type(), name)
	if descr == nil {
		return nil, fmt.Errorf("AttributeError: '%s' object has no attribute '%s'", o.Type().Name, name)
	}
	if dg := descr.Type().DescrGet; dg != nil {
		return dg(descr, o, o.Type())
	}
	return descr, nil
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
// CPython: Objects/typeobject.c:2308 call_unbound_noarg
func callUnboundNoArg(unbound bool, fn Object, self Object) (Object, error) {
	if unbound {
		return Call(fn, NewTuple([]Object{self}), nil)
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotTpCall is the generic tp_call dispatcher: look up __call__ via
// the descriptor protocol (so the instance is bound) and call it.
//
// CPython: Objects/typeobject.c:8174 slot_tp_call
func slotTpCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	fn, err := lookupMethodOnSelf(callable, "__call__")
	if err != nil {
		return nil, err
	}
	posArgs := NewTuple(args)
	var kwDict *Dict
	if len(kwargs) > 0 {
		kwDict = NewDict()
		for k, v := range kwargs {
			_ = kwDict.SetItem(NewStr(k), v)
		}
	}
	return Call(fn, posArgs, kwDict)
}

// slotTpRepr is the generic tp_repr dispatcher: __repr__(self) and
// require the result is a string.
//
// CPython: Objects/typeobject.c:8235 slot_tp_repr
func slotTpRepr(o Object) (string, error) {
	fn, err := lookupMethodOnSelf(o, "__repr__")
	if err != nil {
		return "", err
	}
	r, err := Call(fn, NewTuple(nil), nil)
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
	fn, err := lookupMethodOnSelf(o, "__str__")
	if err != nil {
		return "", err
	}
	r, err := Call(fn, NewTuple(nil), nil)
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
	fn, err := lookupMethodOnSelf(o, "__hash__")
	if err != nil {
		return 0, err
	}
	r, err := Call(fn, NewTuple(nil), nil)
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
	fn, err := lookupMethodOnSelf(o, "__iter__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotTpIterNext dispatches to __next__.
//
// CPython: Objects/typeobject.c:8421 slot_tp_iternext
func slotTpIterNext(o Object) (Object, error) {
	fn, err := lookupMethodOnSelf(o, "__next__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotTpFinalize dispatches to the user's __del__. Errors raised by
// __del__ are swallowed: CPython routes them through
// PyErr_FormatUnraisable so the collector can press on; gopy follows
// the same convention because re-raising mid-cycle-collection has no
// useful target.
//
// CPython: Objects/typeobject.c:10585 slot_tp_finalize
func slotTpFinalize(o Object) {
	fn, unbound, err := lookupMaybeMethod(o, "__del__")
	if err != nil {
		return
	}
	_, _ = callUnboundNoArg(unbound, fn, o)
}

// slotTpRichCompare looks up the dunder that matches op and calls it,
// returning NotImplemented when the dunder is absent so the protocol
// can try the reflected operator on the other operand.
//
// CPython: Objects/typeobject.c:8347 slot_tp_richcompare
func slotTpRichCompare(a, b Object, op CompareOp) (Object, error) {
	name := richCompareDunderName(op)
	if d, _ := LookupDescriptor(a.Type(), name); d == nil {
		return notImplemented(), nil
	}
	fn, err := lookupMethodOnSelf(a, name)
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple([]Object{b}), nil)
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
	fn, err := lookupMethodOnSelf(o, "__bool__")
	if err != nil {
		return false, err
	}
	r, err := Call(fn, NewTuple(nil), nil)
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
	fn, err := lookupMethodOnSelf(o, "__len__")
	if err != nil {
		return 0, err
	}
	r, err := Call(fn, NewTuple(nil), nil)
	if err != nil {
		return 0, err
	}
	i, ok := r.(*Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: __len__ should return int, returned %s", r.Type().Name)
	}
	v, _ := i.Int64()
	if v < 0 {
		return 0, fmt.Errorf("ValueError: __len__() should return >= 0")
	}
	return int(v), nil
}

// slotMpSubscript dispatches to __getitem__ for mapping-style access.
//
// CPython: Objects/typeobject.c:7989 slot_mp_subscript
func slotMpSubscript(o Object, key Object) (Object, error) {
	fn, err := lookupMethodOnSelf(o, "__getitem__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple([]Object{key}), nil)
}

// slotSqGetItem dispatches __getitem__ for sequence-style int indexing.
// Boxes the index into an Int so the user method sees the same type
// CPython hands to PyObject_GetItem.
//
// CPython: Objects/typeobject.c:7964 slot_sq_item
func slotSqGetItem(o Object, idx int) (Object, error) {
	fn, err := lookupMethodOnSelf(o, "__getitem__")
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple([]Object{NewInt(int64(idx))}), nil)
}

// slotMpSubscriptSet dispatches __setitem__.
//
// CPython: Objects/typeobject.c:8004 slot_mp_ass_subscript (set branch)
func slotMpSubscriptSet(o, key, value Object) error {
	fn, err := lookupMethodOnSelf(o, "__setitem__")
	if err != nil {
		return err
	}
	_, err = Call(fn, NewTuple([]Object{key, value}), nil)
	return err
}

// slotMpSubscriptDel dispatches __delitem__.
//
// CPython: Objects/typeobject.c:8004 slot_mp_ass_subscript (del branch)
func slotMpSubscriptDel(o, key Object) error {
	fn, err := lookupMethodOnSelf(o, "__delitem__")
	if err != nil {
		return err
	}
	_, err = Call(fn, NewTuple([]Object{key}), nil)
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
	newFn, _ := LookupDescriptor(cls, "__new__")
	if newFn == nil {
		return nil, fmt.Errorf("TypeError: object.__new__: cannot find __new__ for '%s'", cls.Name)
	}
	if sm, ok := newFn.(*StaticMethod); ok {
		newFn = sm.smCallable
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

// slotTpDescrGet dispatches __get__(self, obj, type). obj is None when
// the descriptor is accessed through the class rather than an instance.
//
// CPython: Objects/typeobject.c:8444 slot_tp_descr_get
func slotTpDescrGet(descr Object, obj Object, tp *Type) (Object, error) {
	fn, err := lookupMethodOnSelf(descr, "__get__")
	if err != nil {
		return nil, err
	}
	var objArg Object
	if obj == nil {
		objArg = None()
	} else {
		objArg = obj
	}
	var typeArg Object
	if tp == nil {
		typeArg = None()
	} else {
		typeArg = tp
	}
	return Call(fn, NewTuple([]Object{objArg, typeArg}), nil)
}

// slotTpDescrSet dispatches __set__(self, obj, value) or
// __delete__(self, obj) when value is nil.
//
// CPython: Objects/typeobject.c:8456 slot_tp_descr_set
func slotTpDescrSet(descr Object, obj Object, value Object) error {
	var fn Object
	var args []Object
	var err error
	if value == nil {
		fn, err = lookupMethodOnSelf(descr, "__delete__")
		if err != nil {
			return err
		}
		args = []Object{obj}
	} else {
		fn, err = lookupMethodOnSelf(descr, "__set__")
		if err != nil {
			return err
		}
		args = []Object{obj, value}
	}
	_, err = Call(fn, NewTuple(args), nil)
	return err
}

// slotSqContains dispatches __contains__.
//
// CPython: Objects/typeobject.c:8064 slot_sq_contains
func slotSqContains(o Object, key Object) (bool, error) {
	fn, err := lookupMethodOnSelf(o, "__contains__")
	if err != nil {
		return false, err
	}
	r, err := Call(fn, NewTuple([]Object{key}), nil)
	if err != nil {
		return false, err
	}
	return IsTruthy(r)
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
	resolved := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		switch n {
		case "__dict__":
			if t.HasDict {
				return fmt.Errorf("TypeError: __dict__ slot disallowed: we already got one")
			}
			t.HasDict = true
			continue
		case "__weakref__":
			// Recognized but no per-instance weakref offset yet.
			continue
		}
		if !StrIsIdentifier(n) {
			return fmt.Errorf("TypeError: __slots__ must be identifiers")
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		// Conflict with a class body assignment of the same name. The
		// __slots__ entry itself lives under the "__slots__" key so it
		// does not appear in this check.
		if has, _ := ns.Contains(NewStr(n)); has {
			return fmt.Errorf("ValueError: %q in __slots__ conflicts with class variable", n)
		}
		resolved = append(resolved, n)
	}
	for i, n := range resolved {
		SetTypeDescr(t, n, NewMemberDescr(n, t.SlotsBase+i))
	}
	t.Slots = resolved
	// Strip __slots__ from ns so it does not also become a stored
	// attribute on the type.
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
