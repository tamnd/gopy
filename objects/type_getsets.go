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

// typeMroMeth implements type.mro() -> list. Returns the same content as
// __mro__ but as a Python list rather than a tuple.
//
// CPython: Objects/typeobject.c:1254 type_mro
func typeMroMeth(args []Object, _ map[string]Object) (Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: descriptor 'mro' of 'type' object needs an argument")
	}
	t, ok := args[0].(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'mro' for 'type' objects doesn't apply to a '%s' object", typeNameOf(args[0]))
	}
	items := make([]Object, len(t.MRO))
	for i, b := range t.MRO {
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
// differently. CPython compares tp_basicsize and tp_itemsize.
//
// CPython: Objects/typeobject.c:2962 shape_differs
func shapeDiffers(t1, t2 *Type) bool {
	return t1.BaseSize != t2.BaseSize || t1.ItemSize != t2.ItemSize
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
	newBases := make([]*Type, 0, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		b, ok := tup.Item(i).(*Type)
		if !ok {
			return fmt.Errorf("TypeError: %s.__bases__ must be tuple of classes, not '%s'", t.Name, typeNameOf(tup.Item(i)))
		}
		if b == t {
			return fmt.Errorf("TypeError: a __bases__ item causes an inheritance cycle")
		}
		newBases = append(newBases, b)
	}
	t.Bases = newBases
	mro, err := c3Linearize(t)
	if err != nil {
		return err
	}
	t.MRO = mro
	t.InvalidateVersionTag()
	return nil
}

// typeGetMRO returns a tuple of t.MRO. Mirrors type_mro.
//
// CPython: Objects/typeobject.c:1183 type_get_mro
func typeGetMRO(o Object) (Object, error) {
	t, ok := o.(*Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__mro__' for 'type' objects doesn't apply to a '%s' object", typeNameOf(o))
	}
	items := make([]Object, len(t.MRO))
	for i, b := range t.MRO {
		items[i] = b
	}
	return NewTuple(items), nil
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
	if t.TypingParameters != nil {
		return t.TypingParameters, nil
	}
	if t.TypeParams != nil {
		return t.TypeParams, nil
	}
	return NewTuple(nil), nil
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
		t.TypingParameters = tup
	} else if v == None() {
		t.TypingParameters = nil
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
	SetTypeDescr(t, "__doc__", v)
	return nil
}
