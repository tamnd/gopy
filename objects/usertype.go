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
	if len(bases) == 0 {
		bases = []*Type{objectType}
	}
	t := NewType(name, bases)
	t.IsUser = true
	t.Getattro = instanceGetAttr
	t.Setattro = instanceSetAttr
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
		for _, k := range ns.Keys() {
			s, ok := k.(*Unicode)
			if !ok {
				continue
			}
			if s.v == "__slots__" {
				continue
			}
			v, err := ns.GetItem(k)
			if err != nil {
				continue
			}
			// __init_subclass__ and __class_getitem__ defined in the
			// class body are implicitly classmethods. CPython does this
			// during type_new_set_attrs so user code can write a plain
			// def and still receive the class as the first argument.
			//
			// CPython: Objects/typeobject.c:4419 type_new_set_attrs
			if s.v == "__init_subclass__" || s.v == "__class_getitem__" {
				if _, isCM := v.(*ClassMethod); !isCM {
					v = NewClassMethod(v)
				}
			}
			SetTypeDescr(t, s.v, v)
		}
	}
	fixupSlotDispatchers(t)
	if err := typeInitSubclass(t); err != nil {
		panic(err)
	}
	return t
}

// typeInitSubclass invokes the parent's __init_subclass__ hook on the
// freshly built subclass. CPython runs this from type_new after the
// type is fully constructed; it walks the MRO starting one position
// past `t` (via super(t, t)) so the subclass's own override does not
// recursively reapply.
//
// CPython: Objects/typeobject.c:4595 type_init_subclass
func typeInitSubclass(t *Type) error {
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
		_, err := Call(callable, NewTuple(nil), nil)
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
// CPython: Objects/typeobject.c:9874 fixup_slot_dispatchers
func fixupSlotDispatchers(t *Type) {
	if _, ok := lookupDunderCallable(t, "__call__"); ok {
		t.Call = slotTpCall
		t.Vectorcall = nil
	}
	if _, ok := lookupDunderCallable(t, "__repr__"); ok {
		t.Repr = slotTpRepr
	}
	if _, ok := lookupDunderCallable(t, "__str__"); ok {
		t.Str = slotTpStr
	} else if t.Repr != nil && t.Str == nil {
		t.Str = t.Repr
	}
	if _, ok := lookupDunderCallable(t, "__hash__"); ok {
		t.Hash = slotTpHash
	} else if t.Hash == nil {
		t.Hash = identityHash
	}
	if _, ok := lookupDunderCallable(t, "__iter__"); ok {
		t.Iter = slotTpIter
	}
	if _, ok := lookupDunderCallable(t, "__next__"); ok {
		t.IterNext = slotTpIterNext
	}
	if hasAnyDunder(t, "__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__") {
		t.RichCmp = slotTpRichCompare
	}
	if _, ok := lookupDunderCallable(t, "__bool__"); ok {
		ensureNumberMethods(t).Bool = slotNbBool
	} else if _, ok := lookupDunderCallable(t, "__len__"); ok {
		ensureNumberMethods(t).Bool = slotNbBoolFromLen
	}
	if _, ok := lookupDunderCallable(t, "__len__"); ok {
		m := ensureMappingMethods(t)
		m.Length = slotMpLength
		s := ensureSequenceMethods(t)
		s.Length = slotMpLength
	}
	if _, ok := lookupDunderCallable(t, "__getitem__"); ok {
		ensureMappingMethods(t).GetItem = slotMpSubscript
		ensureSequenceMethods(t).GetItem = slotSqGetItem
	}
	if _, ok := lookupDunderCallable(t, "__setitem__"); ok {
		ensureMappingMethods(t).SetItem = slotMpSubscriptSet
		ensureSequenceMethods(t).SetItem = slotSqSetItem
	}
	if _, ok := lookupDunderCallable(t, "__delitem__"); ok {
		ensureMappingMethods(t).DelItem = slotMpSubscriptDel
	}
	if _, ok := lookupDunderCallable(t, "__contains__"); ok {
		ensureSequenceMethods(t).Contains = slotSqContains
	}
}

// hasAnyDunder reports whether t exposes any of the named dunders as a
// callable descriptor on its MRO. Used by RichCmp where we install one
// dispatcher that handles every operator and forwards to whichever
// dunder is defined.
func hasAnyDunder(t *Type, names ...string) bool {
	for _, n := range names {
		if _, ok := lookupDunderCallable(t, n); ok {
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
func lookupDunderCallable(t *Type, name string) (Object, bool) {
	d, _ := LookupDescriptor(t, name)
	if d == nil {
		return nil, false
	}
	if d == None() {
		return nil, false
	}
	return d, true
}

// slotTpCall is the generic tp_call dispatcher: look up __call__ via
// the descriptor protocol (so the instance is bound) and call it.
//
// CPython: Objects/typeobject.c:8174 slot_tp_call
func slotTpCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	fn, err := GetAttr(callable, NewStr("__call__"))
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
	fn, err := GetAttr(o, NewStr("__repr__"))
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
	fn, err := GetAttr(o, NewStr("__str__"))
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
	fn, err := GetAttr(o, NewStr("__hash__"))
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
	fn, err := GetAttr(o, NewStr("__iter__"))
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple(nil), nil)
}

// slotTpIterNext dispatches to __next__.
//
// CPython: Objects/typeobject.c:8421 slot_tp_iternext
func slotTpIterNext(o Object) (Object, error) {
	fn, err := GetAttr(o, NewStr("__next__"))
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
	d, _ := LookupDescriptor(a.Type(), name)
	if d == nil {
		return notImplemented(), nil
	}
	fn, err := GetAttr(a, NewStr(name))
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
	fn, err := GetAttr(o, NewStr("__bool__"))
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
	fn, err := GetAttr(o, NewStr("__len__"))
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
	fn, err := GetAttr(o, NewStr("__getitem__"))
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
	fn, err := GetAttr(o, NewStr("__getitem__"))
	if err != nil {
		return nil, err
	}
	return Call(fn, NewTuple([]Object{NewInt(int64(idx))}), nil)
}

// slotMpSubscriptSet dispatches __setitem__.
//
// CPython: Objects/typeobject.c:8004 slot_mp_ass_subscript (set branch)
func slotMpSubscriptSet(o, key, value Object) error {
	fn, err := GetAttr(o, NewStr("__setitem__"))
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
	fn, err := GetAttr(o, NewStr("__delitem__"))
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

// slotSqContains dispatches __contains__.
//
// CPython: Objects/typeobject.c:8064 slot_sq_contains
func slotSqContains(o Object, key Object) (bool, error) {
	fn, err := GetAttr(o, NewStr("__contains__"))
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
