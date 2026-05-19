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
// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
func NewUserTypeMeta(name string, bases []*Type, ns *Dict, kwargs map[string]Object, meta *Type) *Type {
	if len(bases) == 0 {
		bases = []*Type{objectType}
	}
	t := NewType(name, bases)
	t.IsUser = true
	// Stamp the metaclass first so the upcoming namespace pass and
	// __set_name__ hooks see Py_TYPE(t) == meta, matching CPython where
	// type_new sets the metatype as part of allocation. Skip a nil or
	// typeType meta (NewType already wires the default).
	//
	// CPython: Objects/typeobject.c:4153 type_new (Py_TYPE(type) = metatype)
	if meta != nil && meta != typeType {
		t.Init(meta)
	}
	installSubclassAttrSlots(t)
	// Inherit a per-instance __dict__ from any base that has one, then
	// let __slots__ processing override it (e.g. the base contributes
	// dict, but the subclass's __slots__ also adds nothing new — still
	// inherits dict).
	for _, b := range bases {
		if b != nil && b.HasDict {
			t.HasDict = true
			break
		}
	}
	// object itself does not advertise HasDict, but every gopy user class
	// without __slots__ has historically carried a dict; preserve that
	// default so omitting __slots__ keeps the prior behavior.
	noSlotsDeclared := true
	if ns != nil {
		if has, _ := ns.Contains(NewStr("__slots__")); has {
			noSlotsDeclared = false
		}
	}
	if noSlotsDeclared {
		t.HasDict = true
	}
	// User classes with a per-instance __dict__ ship with the
	// INLINE_VALUES + MANAGED_DICT flag pair set, matching CPython
	// heap types built through type_new. The flags gate the
	// LOAD_ATTR_*_WITH_VALUES specializer arms.
	//
	// CPython: Objects/typeobject.c:4153 type_new (sets
	// Py_TPFLAGS_INLINE_VALUES + Py_TPFLAGS_MANAGED_DICT on heap types
	// with a managed dict)
	if t.HasDict {
		t.TpFlags |= TpFlagInlineValues | TpFlagManagedDict
	}
	if ns != nil {
		// __classcell__ is the cell __build_class__ left in the
		// namespace so we can patch it with the new class. It is not a
		// real attribute, so install it before walking the rest of the
		// namespace and skip it during the descriptor copy.
		classCellKey := NewStr("__classcell__")
		if cellObj, err := ns.GetItem(classCellKey); err == nil {
			if cell, ok := cellObj.(*Cell); ok {
				cell.Contents = t
			}
			_ = ns.DelItem(classCellKey)
		}
		// __slots__ processing runs before the descriptor copy so the
		// MemberDescr entries land in typeDescrTable before any class
		// body assignments could overwrite them.
		if err := installSlots(t, ns); err != nil {
			// Errors here are programming bugs in the class body
			// (non-string slot, conflict with class variable, etc.).
			// CPython raises TypeError/ValueError; gopy's NewUserType
			// has no error channel yet, so panic with the same text.
			panic(err)
		}
		copyNamespaceToType(t, ns)
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
	// PEP 487: after the class is built, walk the namespace and call
	// __set_name__ on every value that defines it. enum._proto_member
	// uses this hook to turn each placeholder into a real enum member
	// during class creation, so skipping it leaves _member_map_ empty.
	//
	// CPython: Objects/typeobject.c:4549 type_new_set_names
	if err := typeSetNames(t, ns); err != nil {
		panic(err)
	}
	if err := typeInitSubclass(t, kwargs); err != nil {
		panic(err)
	}
	return t
}

// copyNamespaceToType walks ns and installs each entry as a type
// descriptor on t, with the same special-casing CPython performs in
// type_new_set_attrs: __init_subclass__, __class_getitem__, and
// __prepare__ become classmethods, and __module__ propagates onto
// t.Module so type_repr can render qualified names.
//
// CPython: Objects/typeobject.c:4419 type_new_set_attrs
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
		case "__init_subclass__", "__class_getitem__", "__prepare__":
			if _, isCM := v.(*ClassMethod); !isCM {
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
		}
		SetTypeDescr(t, s.v, v)
	}
}

// typeSetNames invokes __set_name__(cls, name) on every namespace
// value that defines it. Mirrors CPython's __set_name__ pass; this
// is what gives PEP 487 descriptors (and enum's _proto_member) a
// chance to rewrite themselves once the owning class is known.
//
// CPython: Objects/typeobject.c:4549 type_new_set_names
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
	fixupTpNew(t)
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

// fixupRichCmpAndBool wires tp_richcompare and nb_bool.
func fixupRichCmpAndBool(t *Type) {
	if hasAnyDunder(t, "__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__") {
		t.RichCmp = slotTpRichCompare
	}
	if lookupDunderCallable(t, "__bool__") {
		ensureNumberMethods(t).Bool = slotNbBool
	} else if lookupDunderCallable(t, "__len__") {
		ensureNumberMethods(t).Bool = slotNbBoolFromLen
	}
}

// fixupSubscriptSlots wires the mapping/sequence subscription slots
// (length, getitem, setitem, delitem, contains).
func fixupSubscriptSlots(t *Type) {
	if lookupDunderCallable(t, "__len__") {
		ensureMappingMethods(t).Length = slotMpLength
		ensureSequenceMethods(t).Length = slotMpLength
	}
	if lookupDunderCallable(t, "__getitem__") {
		ensureMappingMethods(t).GetItem = slotMpSubscript
		ensureSequenceMethods(t).GetItem = slotSqGetItem
	}
	if lookupDunderCallable(t, "__setitem__") {
		ensureMappingMethods(t).SetItem = slotMpSubscriptSet
		ensureSequenceMethods(t).SetItem = slotSqSetItem
	}
	if lookupDunderCallable(t, "__delitem__") {
		ensureMappingMethods(t).DelItem = slotMpSubscriptDel
	}
	if lookupDunderCallable(t, "__contains__") {
		ensureSequenceMethods(t).Contains = slotSqContains
	}
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

// hasAnyDunder reports whether t exposes any of the named dunders as a
// callable descriptor on its MRO. Used by RichCmp where we install one
// dispatcher that handles every operator and forwards to whichever
// dunder is defined.
func hasAnyDunder(t *Type, names ...string) bool {
	for _, n := range names {
		if lookupDunderCallable(t, n) {
			return true
		}
	}
	return false
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
		SetTypeDescr(t, n, NewMemberDescr(n, i))
	}
	t.Slots = resolved
	// Strip __slots__ from ns so it does not also become a stored
	// attribute on the type.
	_ = ns.DelItem(slotsKey)
	return nil
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
