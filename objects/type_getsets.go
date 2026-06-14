// Type-level getset descriptors: __name__, __qualname__, __bases__,
// __mro__, __module__, __dict__. CPython registers these on PyType_Type
// via type_getsets so every class exposes them; gopy mirrors that by
// installing GetSetDescr entries in typeType's descriptor table.
//
// CPython: Objects/typeobject.c:1145 type_getsets

package objects

import (
	"fmt"
	"strings"
)

func init() {
	register := func(name string, get func(o Object) (Object, error), set func(o Object, v Object) error) {
		SetTypeDescr(typeType, name, NewGetSetDescr(name, get, set))
	}
	register("__name__", typeGetName, typeSetName)
	register("__qualname__", typeGetQualname, typeSetQualname)
	register("__module__", typeGetModule, typeSetModule)
	register("__bases__", typeGetBases, typeSetBases)
	register("__base__", typeGetBase, nil)
	register("__mro__", typeGetMRO, nil)
	register("__doc__", typeGetDoc, typeSetDoc)
	register("__parameters__", typeGetParameters, typeSetParameters)
	// CPython: Objects/typeobject.c:5915 type___subclasses___impl
	SetTypeDescr(typeType, "__subclasses__", NewMethodDescr(typeType, "__subclasses__", typeSubclassesMeth))
	// CPython: Objects/typeobject.c:1254 type_mro — returns __mro__ as a list.
	SetTypeDescr(typeType, "mro", NewMethodDescr(typeType, "mro", typeMroMeth))
	// CPython: Objects/typeobject.c:5862 type___dir___impl
	SetTypeDescr(typeType, "__dir__", NewMethodDescr(typeType, "__dir__", typeDirMeth))
	// type.__subclasscheck__ / type.__instancecheck__ run the "real" check
	// (recursive_issubclass / recursive_isinstance) without re-dispatching
	// through a metaclass __subclasscheck__, so subclasses like typing's
	// _ProtocolMeta can delegate to them via super()/type.__subclasscheck__.
	//
	// CPython: Objects/typeobject.c:5995 type___subclasscheck___impl
	// CPython: Objects/typeobject.c:5982 type___instancecheck___impl
	SetTypeDescr(typeType, "__subclasscheck__", NewMethodDescr(typeType, "__subclasscheck__", typeSubclasscheckMeth))
	SetTypeDescr(typeType, "__instancecheck__", NewMethodDescr(typeType, "__instancecheck__", typeInstancecheckMeth))
}

// typeSubclasscheckMeth implements type.__subclasscheck__(cls, subclass):
// the structural (MRO / __bases__) subclass test, bypassing any metaclass
// __subclasscheck__ override so it can serve as the base implementation.
//
// CPython: Objects/typeobject.c:5995 type___subclasscheck___impl
//          (_PyObject_RealIsSubclass -> recursive_issubclass)
func typeSubclasscheckMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __subclasscheck__() takes exactly one argument (%d given)", len(args)-1)
	}
	cls, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__subclasscheck__' requires a 'type' object")
	}
	ok2, err := realIsSubclass(args[1], cls)
	if err != nil {
		return nil, err
	}
	return NewBool(ok2), nil
}

// typeInstancecheckMeth implements type.__instancecheck__(cls, instance):
// the structural isinstance test (type(instance) is a subclass of cls),
// bypassing any metaclass __instancecheck__ override.
//
// CPython: Objects/typeobject.c:5982 type___instancecheck___impl
//          (_PyObject_RealIsInstance -> recursive_isinstance)
func typeInstancecheckMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __instancecheck__() takes exactly one argument (%d given)", len(args)-1)
	}
	cls, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__instancecheck__' requires a 'type' object")
	}
	ok2, err := realIsSubclass(args[1].Type(), cls)
	if err != nil {
		return nil, err
	}
	return NewBool(ok2), nil
}

// realIsSubclass is the metaclass-bypassing subclass test used by
// type.__subclasscheck__/__instancecheck__: direct PyType_IsSubtype when
// both operands are real types, otherwise the abstract __bases__ walk.
//
// CPython: Objects/abstract.c:2742 recursive_issubclass
func realIsSubclass(sub Object, cls *Type) (bool, error) {
	if st, ok := sub.(*Type); ok {
		return IsSubtype(st, cls), nil
	}
	if err := checkClass(sub, "issubclass() arg 1 must be a class"); err != nil {
		return false, err
	}
	return abstractIsSubclass(sub, cls, 1)
}

// typeDirMeth implements type.__dir__(): the names reachable by merging
// the class's own dict with every base's dict. Unlike object.__dir__ it
// does not chase the metaclass, so dir(str) lists str's and object's
// members but not type-level names like __mro__ or mro.
//
// CPython: Objects/typeobject.c:5862 type___dir___impl
func typeDirMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '__dir__' of 'type' object needs an argument")
	}
	t, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__dir__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	// merge_class_dict(dict, self): self's dict plus the recursive merge
	// of every base. Walking the MRO covers the same set.
	seen := map[string]struct{}{}
	for _, base := range t.MRO {
		for _, n := range descriptorNames(base) {
			seen[n] = struct{}{}
		}
	}
	items := make([]Object, 0, len(seen))
	for n := range seen {
		items = append(items, NewStr(n))
	}
	return NewList(items), nil
}

// typeSubclassesMeth implements type.__subclasses__() -> list.
//
// CPython: Objects/typeobject.c:5915 type___subclasses___impl
func typeSubclassesMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor '__subclasses__' of 'type' object needs an argument")
	}
	t, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__subclasses__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	subs := t.Subclasses()
	items := make([]Object, len(subs))
	for i, s := range subs {
		items[i] = s
	}
	return NewList(items), nil
}

// typeMroMeth implements type.mro() -> list. Like mro_implementation, it
// recomputes the C3 linearization from tp_bases rather than echoing the
// stored tp_mro: a custom metaclass mro() calls type.mro(cls) while the
// type is mid-creation and tp_mro is still NULL, so reading t.MRO would
// hand back an empty list. Recomputing keeps it correct in that window.
//
// CPython: Objects/typeobject.c:3434 type_mro_impl (mro_implementation)
func typeMroMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor 'mro' of 'type' object needs an argument")
	}
	t, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'mro' for 'type' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	mro, err := c3Linearize(t)
	if err != nil {
		return nil, err
	}
	items := make([]Object, len(mro))
	for i, b := range mro {
		items[i] = b
	}
	return NewList(items), nil
}

// typeGetName mirrors type_name. Heap types return ht_name verbatim;
// static types take the tail after the last dot of tp_name so a
// "datetime.date" tp_name reports __name__ == "date".
//
// CPython: Objects/typeobject.c:1457 type_name
func typeGetName(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__name__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		return NewStr(t.Name), nil
	}
	return NewStr(tailName(t.Name)), nil
}

// checkTypeName mirrors the PyUnicode_AsUTF8AndSize step that both
// type_new_set_name and type_set_name run on a type's name. A lone
// surrogate cannot encode to UTF-8, so CPython surfaces the codec's
// UnicodeEncodeError; an embedded null passes the encode but fails the
// strlen != size guard with ValueError. The test only inspects the
// exception type, so the messages stay close to CPython without
// reproducing the full codec arg tuple.
//
// CPython: Objects/typeobject.c:4233 type_new_set_name
func checkTypeName(name string) error {
	for i, r := range strLenientRunes(name) {
		if r >= 0xD800 && r <= 0xDFFF {
			return fmt.Errorf("UnicodeEncodeError: 'utf-8' codec can't encode character '\\u%04x' in position %d: surrogates not allowed", r, i)
		}
	}
	if strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("ValueError: type name must not contain null characters")
	}
	return nil
}

// typeSetName writes t.Name. Only allowed on user-defined types.
//
// CPython: Objects/typeobject.c:1024 type_set_name
func typeSetName(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__name__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__name__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__name__' attribute")
	}
	s, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: can only assign string to %s.__name__, not '%s'", t.Name, typeNameOf(v))
	}
	if err := checkTypeName(s.v); err != nil {
		return err
	}
	t.Name = s.v
	t.InvalidateVersionTag()
	return nil
}

// typeGetQualname mirrors type_qualname. Heap types return ht_qualname
// verbatim; static types fall back to _PyType_Name which strips the
// dotted module prefix from tp_name.
//
// CPython: Objects/typeobject.c:1470 type_qualname
func typeGetQualname(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__qualname__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		if t.Qualname != "" {
			return NewStr(t.Qualname), nil
		}
		return NewStr(t.Name), nil
	}
	return NewStr(tailName(t.Name)), nil
}

// typeSetQualname writes t.Qualname. Only allowed on heap (user) types,
// matching CPython's check against Py_TPFLAGS_HEAPTYPE.
//
// CPython: Objects/typeobject.c:1003 type_set_qualname
func typeSetQualname(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__qualname__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__qualname__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__qualname__' attribute")
	}
	s, ok := v.(*Unicode)
	if !ok {
		return fmt.Errorf("TypeError: can only assign string to %s.__qualname__, not '%s'", t.Name, typeNameOf(v))
	}
	t.Qualname = s.v
	t.InvalidateVersionTag()
	return nil
}

// typeGetModule mirrors type_module: heap (user) types return their
// __module__ field (raising AttributeError when unset), static types
// take the leading dotted component of tp_name or fall back to
// "builtins". CPython's strrchr lookup on tp_name is the convention
// that lets a C extension type pin its __module__ at registration via
// the "module.name" form.
//
// CPython: Objects/typeobject.c:1538 type_module
func typeGetModule(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__module__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.IsUser {
		// Check the descriptor dict first (non-string __module__ stored there by typeSetModule).
		if v := TypeOwnDescrs(t)["__module__"]; v != nil {
			return v, nil
		}
		if t.Module == "" {
			return nil, fmt.Errorf("AttributeError: __module__")
		}
		return NewStr(t.Module), nil
	}
	// Non-user (C/Go heap) types: honor an explicit t.Module when set,
	// then fall back to the dotted-prefix convention, then "builtins".
	// CPython heap types (Py_TPFLAGS_HEAPTYPE, e.g. functools.partial)
	// store __module__ in tp_dict; our equivalent is t.Module.
	//
	// CPython: Objects/typeobject.c:1538 type_module
	if t.Module != "" {
		return NewStr(t.Module), nil
	}
	if i := strings.LastIndexByte(t.Name, '.'); i >= 0 {
		return NewStr(t.Name[:i]), nil
	}
	return NewStr("builtins"), nil
}

// typeSetModule writes t.Module.
//
// CPython: Objects/typeobject.c:939 type_set_module
func typeSetModule(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__module__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__module__' attribute of immutable type '%s'", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__module__' attribute")
	}
	// CPython stores any object (not just str) in tp_dict["__module__"].
	// Update t.Module only when the value is a string so the Go field stays
	// consistent; non-string values are stored only in the descriptor dict.
	//
	// CPython: Objects/typeobject.c:1581 type_set_module
	if s, ok := v.(*Unicode); ok {
		t.Module = s.v
	} else {
		SetTypeDescr(t, "__module__", v)
	}
	// __firstlineno__ records the source line the class statement opened
	// on; once __module__ is reassigned the recorded line no longer
	// describes where the type lives, so CPython drops it from tp_dict.
	//
	// CPython: Objects/typeobject.c:1581 type_set_module (PyDict_Pop __firstlineno__)
	DelTypeDescr(t, "__firstlineno__")
	t.InvalidateVersionTag()
	return nil
}

// typeGetBases returns a tuple of t.Bases. Mirrors type_bases.
//
// CPython: Objects/typeobject.c:1077 type_get_bases
func typeGetBases(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__bases__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	// A type built via type(name, bases, ns) keeps the original bases
	// object so a tuple subclass survives as the type of __bases__.
	//
	// CPython: Objects/typeobject.c:1083 type_get_bases (returns tp_bases)
	if t.BasesObj != nil {
		Incref(t.BasesObj)
		return t.BasesObj, nil
	}
	items := make([]Object, len(t.Bases))
	for i, b := range t.Bases {
		items[i] = b
	}
	return NewTuple(items), nil
}

// typeGetBase returns the type's single "best base", the base whose
// instance layout the type inherits. object has no base and reports
// None; every other type reports the winner of best_base over its
// explicit bases.
//
// CPython: Objects/typeobject.c:1095 type_get_base
func typeGetBase(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__base__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if len(t.Bases) == 0 {
		return None(), nil
	}
	base, err := bestBase(t.Bases)
	if err != nil {
		return nil, err
	}
	if base == nil {
		return None(), nil
	}
	return base, nil
}

// shapeDiffers reports whether two types lay their instances out
// differently. CPython compares tp_basicsize and tp_itemsize, which are
// always concrete on a finished type. gopy leaves the raw BaseSize /
// ItemSize fields at zero on user types and resolves the inherited size
// by walking the MRO, so the comparison must go through typeBasicSize /
// typeItemSize too. Comparing the raw fields would report a spurious
// difference (0 vs object's 16) for a plain user class against object
// and wrongly make it its own solid base.
//
// CPython: Objects/typeobject.c:2962 shape_differs
func shapeDiffers(t1, t2 *Type) bool {
	return typeBasicSize(t1) != typeBasicSize(t2) || typeItemSize(t1) != typeItemSize(t2)
}

// solidBase returns the most-derived ancestor of t whose instance
// layout differs from its own base, walking the primary base chain.
//
// CPython: Objects/typeobject.c:2971 solid_base
func solidBase(t *Type) *Type {
	var base *Type
	if len(t.Bases) > 0 {
		base = solidBase(t.Bases[0])
	} else {
		base = objectType
	}
	if shapeDiffers(t, base) {
		return t
	}
	return base
}

// bestBase mirrors best_base: of the explicit bases it picks the one
// whose solid base is the most derived, raising on an instance-layout
// conflict between two unrelated solid bases.
//
// CPython: Objects/typeobject.c:2998 best_base
func bestBase(bases []*Type) (*Type, error) {
	var base, winner *Type
	for _, bi := range bases {
		if bi.TpFlags&TpFlagBasetype == 0 {
			return nil, fmt.Errorf("TypeError: type '%s' is not an acceptable base type", bi.Name)
		}
		candidate := solidBase(bi)
		switch {
		case winner == nil:
			winner = candidate
			base = bi
		case IsSubtype(winner, candidate):
			// winner already dominates; keep it.
		case IsSubtype(candidate, winner):
			winner = candidate
			base = bi
		default:
			return nil, fmt.Errorf("TypeError: multiple bases have instance lay-out conflict")
		}
	}
	return base, nil
}

// typeSetBases reassigns t.Bases and recomputes the MRO. Only allowed
// on heap (user) types. typing.NamedTuple does this so the resulting
// class can claim the user-supplied bases tuple (which may include
// Generic) after _make_nmtuple has already produced a plain tuple
// subclass.
//
// CPython: Objects/typeobject.c:1109 type_set_bases
func typeSetBases(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__bases__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: can't set %s.__bases__", t.Name)
	}
	if v == nil {
		return fmt.Errorf("TypeError: can't delete %s.__bases__", t.Name)
	}
	tup, ok := v.(*Tuple)
	if !ok {
		return fmt.Errorf("TypeError: can only assign tuple to %s.__bases__, not '%s'", t.Name, typeNameOf(v))
	}
	if tup.Len() == 0 {
		return fmt.Errorf("TypeError: can only assign non-empty tuple to %s.__bases__", t.Name)
	}
	newBases, err := validateNewBases(t, tup)
	if err != nil {
		return err
	}
	// The new best base must lay its instances out compatibly with the old
	// one, the same gate object.__class__ assignment uses.
	//
	// CPython: Objects/typeobject.c:1847 type_set_bases_unlocked
	newBase, err := bestBase(newBases)
	if err != nil {
		return err
	}
	oldBase, _ := bestBase(t.Bases)
	if oldBase != nil && newBase != nil && !compatibleForAssignment(oldBase, newBase) {
		return fmt.Errorf("TypeError: __bases__ assignment: '%s' object layout differs from '%s'", newBase.Name, oldBase.Name)
	}

	oldBases := t.Bases
	oldBasesObj := t.BasesObj
	t.Bases = newBases
	// BasesObj is the identity token for reentrancy detection below: a
	// custom mro() invoked while we recompute the hierarchy may assign
	// __bases__ again, which overwrites this field with its own tuple.
	t.BasesObj = tup
	// Recompute the MRO for t and every transitive subclass, recording the
	// prior MROs so a C3 conflict deeper in the tree can be rolled back.
	//
	// CPython: Objects/typeobject.c:1724 mro_hierarchy_for_complete_type
	var saved []mroSnapshot
	if err := mroHierarchy(t, &saved, map[*Type]bool{}); err != nil {
		// Roll the MROs back, but skip any type a reentrant assignment
		// recomputed in the meantime: that newer MRO must survive.
		//
		// CPython: Objects/typeobject.c:1895 type_set_bases_unlocked (undo)
		for i := len(saved) - 1; i >= 0; i-- {
			if sameMRO(saved[i].cls.MRO, saved[i].newMRO) {
				saved[i].cls.MRO = saved[i].oldMRO
			}
		}
		// Only restore __bases__ if a reentrant assignment has not already
		// replaced it; otherwise the reentrant result is the live one.
		//
		// CPython: Objects/typeobject.c:1912 type_set_bases_unlocked (bail)
		if t.BasesObj == tup {
			t.Bases = oldBases
			t.BasesObj = oldBasesObj
		}
		return err
	}
	// Take no action if tp_bases was replaced through reentrance: the
	// reentrant call already moved t between the subclass lists and
	// updated slots, and redoing it here would re-add t to bases it no
	// longer has.
	//
	// CPython: Objects/typeobject.c:1869 type_set_bases_unlocked
	//
	//	(if (lookup_tp_bases(type) == new_bases))
	if t.BasesObj != tup {
		return nil
	}
	// Move t between the old and new bases' subclass lists, then re-derive
	// the inherited slots for t and its subclasses.
	//
	// CPython: Objects/typeobject.c:1878 remove_all_subclasses / add_all_subclasses / update_all_slots
	for _, b := range oldBases {
		b.removeSubclass(t)
	}
	for _, b := range newBases {
		b.addSubclass(t)
	}
	refixupSlotDispatchers(t)
	t.InvalidateVersionTag()
	return nil
}

// sameMRO reports whether two MRO slices hold the same types in the same
// order. type_set_bases_unlocked compares tp_mro by pointer identity to
// decide whether a reentrant assignment recomputed it; gopy stores the
// MRO as a fresh slice each pass, so element-wise equality stands in for
// that identity check.
func sameMRO(a, b []*Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validateNewBases converts the assigned __bases__ tuple into a slice of
// types, rejecting non-type entries and any base that would close an
// inheritance cycle through t.
//
// CPython: Objects/typeobject.c:1812 type_set_bases_unlocked (argument loop)
func validateNewBases(t *Type, tup *Tuple) ([]*Type, error) {
	newBases := make([]*Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		b, ok := tup.Item(i).(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: %s.__bases__ must be tuple of classes, not '%s'", t.Name, typeNameOf(tup.Item(i)))
		}
		// A base that already has t in its ancestry (or is t itself) would
		// close an inheritance cycle. The MRO scan alone misses a cycle that
		// a reentrant custom mro() forms through the primary base before the
		// MRO is refreshed, so basesCauseCycle also walks the tp_base chain.
		//
		// CPython: Objects/typeobject.c:1823 type_set_bases_unlocked
		if b == t || basesCauseCycle(b, t) {
			return nil, fmt.Errorf("TypeError: a __bases__ item causes an inheritance cycle")
		}
		newBases = append(newBases, b)
	}
	return newBases, nil
}

// mroSnapshot pairs a type with the MRO it had before a __bases__
// assignment (oldMRO) and the MRO this pass installed (newMRO), so the
// whole hierarchy can be rolled back on a later failure. The rollback
// only restores a type whose MRO is still the one this pass set: a
// reentrant __bases__ assignment fired from a custom mro() may have
// recomputed it, and that fresher MRO must survive.
//
// CPython: Objects/typeobject.c:1895 type_set_bases_unlocked
//
//	(undo loop: "Do not rollback if cls has a newer version of MRO")
type mroSnapshot struct {
	cls    *Type
	oldMRO []*Type
	newMRO []*Type
}

// mroHierarchy recomputes t's MRO and then recurses into every direct
// subclass, appending each (type, old MRO) pair to saved as it goes.
//
// CPython: Objects/typeobject.c:1724 mro_hierarchy_for_complete_type
func mroHierarchy(t *Type, saved *[]mroSnapshot, visited map[*Type]bool) error {
	if visited[t] {
		return nil
	}
	visited[t] = true
	old := t.MRO
	// mro_invoke computes the new MRO, dispatching to a metaclass mro()
	// override (e.g. test_descr's WorkOnce/DebugHelperMeta). It does not
	// install the result; a custom mro() may reassign t.__bases__
	// reentrantly, which installs a fresh t.MRO deeper in the stack.
	//
	// CPython: Objects/typeobject.c:3563 mro_internal_unlocked
	newMRO, err := mroInvoke(t)
	// Reentrancy check against the value captured before the call: a
	// changed slice header means a reentrant assignment already recomputed
	// t and its whole subtree.
	//
	// CPython: Objects/typeobject.c:3564 mro_internal_unlocked (reent)
	reent := !sameSliceIdentity(t.MRO, old)
	if err != nil {
		// On error mro_internal returns -1 without touching tp_mro, leaving
		// whatever a reentrant assignment installed. Do not restore old.
		//
		// CPython: Objects/typeobject.c:3569 mro_internal_unlocked (return -1)
		return err
	}
	if reent {
		// The reentrant assignment already recomputed t and every subclass;
		// installing newMRO here would clobber it. Stop without recording a
		// snapshot, exactly as mro_internal returning 0 short-circuits
		// mro_hierarchy_for_complete_type.
		//
		// CPython: Objects/typeobject.c:3573 mro_internal_unlocked (return 0)
		return nil
	}
	t.MRO = newMRO
	*saved = append(*saved, mroSnapshot{cls: t, oldMRO: old, newMRO: newMRO})
	for _, sub := range t.Subclasses() {
		if err := mroHierarchy(sub, saved, visited); err != nil {
			return err
		}
	}
	return nil
}

// typeGetMRO returns a tuple of t.MRO, or None when tp_mro has not been
// set yet. A custom metaclass mro() runs while the type is mid-creation
// with tp_mro still NULL, and test code reads cls.__mro__ to detect that
// window, so an unset MRO must surface as None rather than an empty tuple.
//
// CPython: Objects/typeobject.c:1697 type_get_mro
func typeGetMRO(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__mro__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if t.MRO == nil {
		return None(), nil
	}
	items := make([]Object, len(t.MRO))
	for i, b := range t.MRO {
		items[i] = b
	}
	return NewTuple(items), nil
}

// SetTypingParameters pins the __parameters__ tuple as a counted reference
// on the type, dropping any previously pinned tuple. TypingParameters is an
// un-counted Go field, so without this pin the tuple would sit at refcount 0
// while still reachable through cls.__parameters__ and tuple_dealloc would
// nil its items the first time the VM transiently decreffed it. CPython holds
// cls.__parameters__ as a normal counted instance-dict entry.
//
// CPython: Lib/typing.py:1222 cls.__parameters__ = tuple(tvars)
func SetTypingParameters(t *Type, tup *Tuple) {
	if t.TypingParameters == tup {
		return
	}
	if tup != nil {
		Incref(tup)
	}
	if t.TypingParameters != nil {
		Decref(t.TypingParameters)
	}
	t.TypingParameters = tup
}

// typeGetParameters returns the class's __parameters__ tuple. Priority:
// 1. TypingParameters set by typing.Generic.__init_subclass__ (traditional generics)
// 2. TypeParams set from the PEP 695 class body (__type_params__)
// 3. Empty tuple fallback
//
// CPython: Lib/typing.py:1209 Generic.__init_subclass__ sets cls.__parameters__
// CPython: Include/cpython/typeobject.h tp_typeparams (PEP 695 source)
func typeGetParameters(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__parameters__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	// __parameters__ hands back a stored tuple, so it must return an owned
	// reference the way CPython's getset getters do (a dict-entry read goes
	// through _PyDict_GetItemRef which increfs). Without the Incref the
	// caller's arg-drop decrefs the pinned tuple toward zero and tuple_dealloc
	// empties it, so the second cls[...] subscription reads an empty tuple and
	// raises "is not a generic class".
	//
	// CPython: Lib/typing.py:1209 cls.__parameters__ as a counted dict entry
	if t.TypingParameters != nil {
		Incref(t.TypingParameters)
		return t.TypingParameters, nil
	}
	// CPython stores __parameters__ as a plain instance attribute set only by
	// typing.Generic.__init_subclass__. A class where that hook never ran (a
	// non-generic class, or one whose __init_subclass__ forgot to call super)
	// has no __parameters__, so reading it must raise AttributeError. typing's
	// _generic_class_getitem relies on this: it adds the "did you forget
	// super().__init_subclass__()" note to that AttributeError. __type_params__
	// (PEP 695) is a separate attribute and must not stand in for it.
	//
	// CPython: Lib/typing.py:1151 parameters = cls.__parameters__ (AttributeError path)
	return nil, fmt.Errorf("AttributeError: type object '%s' has no attribute '__parameters__'", t.Name)
}

// typeSetParameters stores a __parameters__ tuple assigned by user code
// (typically typing.Generic.__init_subclass__).
//
// CPython: Lib/typing.py:1209 cls.__parameters__ = tuple(tvars)
func typeSetParameters(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__parameters__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if tup, ok2 := v.(*Tuple); ok2 {
		SetTypingParameters(t, tup)
	} else if v == None() {
		SetTypingParameters(t, nil)
	} else {
		return fmt.Errorf("TypeError: __parameters__ must be a tuple")
	}
	return nil
}

// typeGetDoc returns the type's __doc__. Looks in the type's own
// descriptor table first (user classes store their docstring there
// via STORE_NAME "__doc__"); returns None when absent.
//
// CPython: Objects/typeobject.c:1200 type_get_doc
func typeGetDoc(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__doc__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if v, ok2 := typeDescrTable[t]["__doc__"]; ok2 {
		// A docstring stored as a descriptor (e.g. a class that sets
		// __doc__ = SomeDescriptor()) is fetched through its __get__
		// with a NULL instance, exactly as type_get_doc does before
		// handing the result back.
		//
		// CPython: Objects/typeobject.c:1213 type_get_doc (tp_descr_get)
		if dg := v.Type().DescrGet; dg != nil {
			return dg(v, nil, t)
		}
		return v, nil
	}
	return None(), nil
}

// typeSetDoc sets __doc__ on a user-defined type.
//
// CPython: Objects/typeobject.c:1227 type_set_doc
func typeSetDoc(o Object, v Object) error {
	t, ok := o.(*Type)
	if !ok {
		return fmt.Errorf("TypeError: descriptor '__doc__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	if !t.IsUser {
		return fmt.Errorf("TypeError: cannot set '__doc__' attribute of immutable type '%s'", t.Name)
	}
	// check_set_special_type_attr rejects deletion (NULL value) with a
	// message that, by CPython's own wording, still says "immutable type".
	if v == nil {
		return fmt.Errorf("TypeError: cannot delete '__doc__' attribute of immutable type '%s'", t.Name)
	}
	SetTypeDescr(t, "__doc__", v)
	return nil
}
