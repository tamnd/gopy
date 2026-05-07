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
			SetTypeDescr(t, s.v, v)
		}
	}
	return t
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
