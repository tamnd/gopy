// UnionType is the runtime representation of int | str (PEP 604).
// Calling `|` on any combination of types, None, GenericAliases, or
// existing unions folds the operands into a single UnionType,
// deduplicating identical entries. types.UnionType is the public
// alias for this type.
//
// CPython: Objects/unionobject.c

package objects

import (
	"fmt"
	"strings"
)

// UnionType mirrors unionobject from CPython. args holds every member
// in declaration order; hashableArgs holds the same set as a
// frozenset for equality and hashing (CPython treats unions as
// order-preserving sets); parameters is the lazy typevar tuple, kept
// for parity with __parameters__.
//
// CPython: Objects/unionobject.c:10 unionobject
type UnionType struct {
	Header
	args           *Tuple
	hashableArgs   *Set
	unhashableArgs *Tuple
	parameters     *Tuple
}

// UnionTypeType is the type singleton for types.UnionType. Mirrors
// _PyUnion_Type.
//
// CPython: Objects/unionobject.c:524 _PyUnion_Type
var UnionTypeType = NewType("types.UnionType", []*Type{objectType})

func init() {
	UnionTypeType.Repr = unionRepr
	UnionTypeType.Str = unionRepr
	UnionTypeType.Hash = unionHash
	UnionTypeType.RichCmp = unionRichCompare
	UnionTypeType.Getattro = unionGetattro
	UnionTypeType.Mapping = &MappingMethods{
		GetItem: unionGetitem,
	}
	UnionTypeType.Number = &NumberMethods{
		Or: unionNbOr,
	}
	UnionTypeType.TpTraverse = unionTraverse
}

// Args returns the tuple of union members. Mirrors unionobject->args.
//
// CPython: Objects/unionobject.c:12 unionobject.args
func (u *UnionType) Args() *Tuple { return u.args }

// unionTraverse visits args, hashableArgs, unhashableArgs, parameters
// so the cycle collector can follow a union.
//
// CPython: Objects/unionobject.c:35 union_traverse
func unionTraverse(o Object, visit Visitor) error {
	u := o.(*UnionType)
	if u.args != nil {
		if err := visit(u.args); err != nil {
			return err
		}
	}
	if u.hashableArgs != nil {
		if err := visit(u.hashableArgs); err != nil {
			return err
		}
	}
	if u.unhashableArgs != nil {
		if err := visit(u.unhashableArgs); err != nil {
			return err
		}
	}
	if u.parameters != nil {
		if err := visit(u.parameters); err != nil {
			return err
		}
	}
	return nil
}

// unionRepr formats a union as "A | B | C" using typingTypeRepr to
// shape each member. Empty unions cannot happen (make_union rejects
// length zero) so the loop always emits at least one entry.
//
// CPython: Objects/unionobject.c:279 union_repr
func unionRepr(o Object) (string, error) {
	u := o.(*UnionType)
	var b strings.Builder
	n := u.args.Len()
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(" | ")
		}
		s, err := typingTypeRepr(u.args.Item(i))
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// unionHash hashes a union via its frozenset of hashable args. When
// the union holds unhashable members, the call raises TypeError just
// like CPython's union_hash does after re-checking each unhashable
// arg.
//
// CPython: Objects/unionobject.c:46 union_hash
func unionHash(o Object) (int64, error) {
	u := o.(*UnionType)
	if u.unhashableArgs != nil {
		return 0, fmt.Errorf("TypeError: union contains %d unhashable elements", u.unhashableArgs.Len())
	}
	return Hash(u.hashableArgs)
}

// unionsEqual compares two unions: order is irrelevant for hashable
// args (they're held in a frozenset), unhashable args must match
// set-wise via PySequence_Contains.
//
// CPython: Objects/unionobject.c:71 unions_equal
func unionsEqual(a, b *UnionType) (bool, error) {
	eq, err := RichCmpBool(a.hashableArgs, b.hashableArgs, CompareEQ)
	if err != nil || !eq {
		return false, err
	}
	if a.unhashableArgs != nil && b.unhashableArgs != nil {
		if a.unhashableArgs.Len() != b.unhashableArgs.Len() {
			return false, nil
		}
		for i := 0; i < a.unhashableArgs.Len(); i++ {
			found := false
			for j := 0; j < b.unhashableArgs.Len(); j++ {
				ok, err := RichCmpBool(a.unhashableArgs.Item(i), b.unhashableArgs.Item(j), CompareEQ)
				if err != nil {
					return false, err
				}
				if ok {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	}
	if a.unhashableArgs != nil || b.unhashableArgs != nil {
		return false, nil
	}
	return true, nil
}

// unionRichCompare implements __eq__/__ne__: only union vs union
// triggers a real comparison; everything else returns NotImplemented.
//
// CPython: Objects/unionobject.c:113 union_richcompare
func unionRichCompare(a, b Object, op CompareOp) (Object, error) {
	bb, ok := b.(*UnionType)
	if !ok || (op != CompareEQ && op != CompareNE) {
		return NotImplemented(), nil
	}
	aa := a.(*UnionType)
	eq, err := unionsEqual(aa, bb)
	if err != nil {
		return nil, err
	}
	if op == CompareEQ {
		return NewBool(eq), nil
	}
	return NewBool(!eq), nil
}

// unionBuilder mirrors CPython's unionbuilder struct: a working list
// of args plus a hashable / unhashable bucket so dedup runs in O(1).
//
// CPython: Objects/unionobject.c:131 unionbuilder
type unionBuilder struct {
	args           []Object
	hashableArgs   *Set
	unhashableArgs []Object
	isChecked      bool
}

// newUnionBuilder seeds an empty builder. isChecked controls whether
// every added arg gets routed through type_check; the C entry points
// pass true (constructed via __class_getitem__) or false (from |).
//
// CPython: Objects/unionobject.c:142 unionbuilder_init
func newUnionBuilder(isChecked bool) *unionBuilder {
	return &unionBuilder{
		hashableArgs: NewSet(),
		isChecked:    isChecked,
	}
}

// addSingle adds one arg with CPython's normalization: None becomes
// NoneType, an existing union gets flattened, and type_check applies
// when the builder was opened in checked mode.
//
// CPython: Objects/unionobject.c:208 unionbuilder_add_single
func (ub *unionBuilder) addSingle(arg Object) error {
	if IsNone(arg) {
		arg = None().Type()
	} else if existing, ok := arg.(*UnionType); ok {
		return ub.addTuple(existing.args)
	}
	if ub.isChecked {
		checked, err := unionTypeCheck(arg, "Union[arg, ...]: each arg must be a type.")
		if err != nil {
			return err
		}
		arg = checked
	}
	return ub.addSingleUnchecked(arg)
}

// addTuple folds every entry of t into the builder.
//
// CPython: Objects/unionobject.c:232 unionbuilder_add_tuple
func (ub *unionBuilder) addTuple(t *Tuple) error {
	for i := 0; i < t.Len(); i++ {
		if err := ub.addSingle(t.Item(i)); err != nil {
			return err
		}
	}
	return nil
}

// addSingleUnchecked is the post-normalization insert. Hashable args
// go through a frozenset for O(1) dedup; unhashable ones get a linear
// scan against the unhashable bucket.
//
// CPython: Objects/unionobject.c:168 unionbuilder_add_single_unchecked
func (ub *unionBuilder) addSingleUnchecked(arg Object) error {
	if _, err := Hash(arg); err != nil {
		// Unhashable: check against existing unhashables.
		for _, existing := range ub.unhashableArgs {
			eq, cerr := RichCmpBool(existing, arg, CompareEQ)
			if cerr != nil {
				return cerr
			}
			if eq {
				return nil
			}
		}
		ub.unhashableArgs = append(ub.unhashableArgs, arg)
		ub.args = append(ub.args, arg)
		return nil
	}
	contains, err := ub.hashableArgs.Contains(arg)
	if err != nil {
		return err
	}
	if contains {
		return nil
	}
	if err := ub.hashableArgs.Add(arg); err != nil {
		return err
	}
	ub.args = append(ub.args, arg)
	return nil
}

// makeUnion materializes the builder into a UnionType. A single-arg
// union collapses back to the bare type (matching CPython's "a | a
// == a" simplification); an empty builder is a TypeError.
//
// CPython: Objects/unionobject.c:549 make_union
func (ub *unionBuilder) makeUnion() (Object, error) {
	if len(ub.args) == 0 {
		return nil, fmt.Errorf("TypeError: Cannot take a Union of no types")
	}
	if len(ub.args) == 1 {
		return ub.args[0], nil
	}
	args := NewTuple(ub.args)
	hashable, err := NewFrozenset(ub.hashableArgs.Items())
	if err != nil {
		return nil, err
	}
	u := &UnionType{
		args:         args,
		hashableArgs: hashable,
	}
	if ub.unhashableArgs != nil {
		u.unhashableArgs = NewTuple(ub.unhashableArgs)
	}
	u.init(UnionTypeType)
	return u, nil
}

// isUnionable reports whether obj can sit inside a union. CPython
// accepts None, types, GenericAliases, existing unions, and
// TypeAlias instances.
//
// CPython: Objects/unionobject.c:244 is_unionable
func isUnionable(obj Object) bool {
	if IsNone(obj) {
		return true
	}
	if _, ok := obj.(*Type); ok {
		return true
	}
	if _, ok := obj.(*GenericAlias); ok {
		return true
	}
	if _, ok := obj.(*UnionType); ok {
		return true
	}
	if _, ok := obj.(*TypeAliasObj); ok {
		return true
	}
	return false
}

// unionTypeOr is _Py_union_type_or: combine self and other into a
// single union. Used by GenericAlias' nb_or and by the type binding
// for `int | str`.
//
// CPython: Objects/unionobject.c:257 _Py_union_type_or
func unionTypeOr(self, other Object) (Object, error) {
	if !isUnionable(self) || !isUnionable(other) {
		return NotImplemented(), nil
	}
	ub := newUnionBuilder(false)
	if err := ub.addSingle(self); err != nil {
		return nil, err
	}
	if err := ub.addSingle(other); err != nil {
		return nil, err
	}
	return ub.makeUnion()
}

// unionNbOr is the nb_or slot for UnionType itself: every operand is
// type-checked before going in. Mirrors union_nb_or.
//
// CPython: Objects/unionobject.c:397 union_nb_or
func unionNbOr(self, other Object) (Object, error) {
	if !isUnionable(self) || !isUnionable(other) {
		return NotImplemented(), nil
	}
	ub := newUnionBuilder(true)
	if err := ub.addSingle(self); err != nil {
		return nil, err
	}
	if err := ub.addSingle(other); err != nil {
		return nil, err
	}
	return ub.makeUnion()
}

// unionFromTuple is _Py_union_from_tuple: the entry point typing.Union
// uses internally. Public so future imports of the typing module can
// reach the same fast path.
//
// CPython: Objects/unionobject.c:484 _Py_union_from_tuple
func unionFromTuple(args Object) (Object, error) {
	ub := newUnionBuilder(true)
	if t, ok := args.(*Tuple); ok {
		if err := ub.addTuple(t); err != nil {
			return nil, err
		}
	} else {
		if err := ub.addSingle(args); err != nil {
			return nil, err
		}
	}
	return ub.makeUnion()
}

// unionTypeCheck is the analog of type_check in unionobject.c. The
// gopy slice does not call into typing._type_check (typing.py is not
// runnable yet), so anything that is not directly unionable raises
// the same TypeError CPython would surface.
//
// CPython: Objects/unionobject.c:463 type_check
func unionTypeCheck(arg Object, msg string) (Object, error) {
	if IsNone(arg) {
		return None().Type(), nil
	}
	if isUnionable(arg) {
		return arg, nil
	}
	return nil, fmt.Errorf("TypeError: %s Got %s", msg, typeNameOf(arg))
}

// unionGetitem implements `union[args]` (the typevar-substitution
// surface). The gopy slice does not implement typevars, so empty
// parameters short-circuit through the same "is not a generic class"
// path as GenericAlias.
//
// CPython: Objects/unionobject.c:341 union_getitem
func unionGetitem(o, item Object) (Object, error) {
	u := o.(*UnionType)
	if u.parameters == nil {
		u.parameters = makeParameters(u.args)
	}
	if u.parameters.Len() == 0 {
		repr, err := Repr(o)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("TypeError: %s is not a generic class", repr)
	}
	return nil, fmt.Errorf("TypeError: parameterized generic substitution is not supported")
}

// unionClsAttrs is the set of names union_getattro proxies back to
// type(self) rather than serving from the union instance.
//
// CPython: Objects/unionobject.c:415 cls_attrs
var unionClsAttrs = map[string]struct{}{
	"__module__": {},
}

// unionGetattro proxies __module__ to the union type itself so user
// code can read typing-compatible metadata; everything else routes
// through GenericGetAttr which finds the descriptors registered below.
//
// CPython: Objects/unionobject.c:421 union_getattro
func unionGetattro(o Object, name Object) (Object, error) {
	if s, ok := name.(*Unicode); ok {
		if _, isCls := unionClsAttrs[s.v]; isCls {
			return GetAttr(o.Type(), name)
		}
	}
	return GenericGetAttr(o, name)
}

// init wires the property and method descriptors UnionType exposes:
// __args__, __parameters__, __origin__, __name__, __qualname__, and
// __class_getitem__ / __mro_entries__.
//
// CPython: Objects/unionobject.c:518 union_methods + 319 union_members +
//
//	384 union_properties
func init() {
	SetTypeDescr(UnionTypeType, "__args__", NewGetSetDescr("__args__", func(o Object) (Object, error) {
		return o.(*UnionType).args, nil
	}, nil))
	SetTypeDescr(UnionTypeType, "__parameters__", NewGetSetDescr("__parameters__", func(o Object) (Object, error) {
		u := o.(*UnionType)
		if u.parameters == nil {
			u.parameters = makeParameters(u.args)
		}
		return u.parameters, nil
	}, nil))
	SetTypeDescr(UnionTypeType, "__name__", NewGetSetDescr("__name__", func(_ Object) (Object, error) {
		return NewStr("Union"), nil
	}, nil))
	SetTypeDescr(UnionTypeType, "__qualname__", NewGetSetDescr("__qualname__", func(_ Object) (Object, error) {
		return NewStr("Union"), nil
	}, nil))
	SetTypeDescr(UnionTypeType, "__origin__", NewGetSetDescr("__origin__", func(_ Object) (Object, error) {
		return UnionTypeType, nil
	}, nil))
	SetTypeDescr(UnionTypeType, "__mro_entries__", NewMethodDescr(UnionTypeType, "__mro_entries__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __mro_entries__() missing self argument")
		}
		repr, err := Repr(args[0])
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("TypeError: Cannot subclass %s", repr)
	}))
	SetTypeDescr(UnionTypeType, "__class_getitem__", NewClassMethod(NewBuiltinFunction("__class_getitem__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: __class_getitem__() missing arguments")
		}
		return unionFromTuple(args[1])
	})))
	SetTypeDescr(UnionTypeType, "__or__", NewMethodDescr(UnionTypeType, "__or__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __or__() takes exactly one argument")
		}
		return unionNbOr(args[0], args[1])
	}))
	SetTypeDescr(UnionTypeType, "__ror__", NewMethodDescr(UnionTypeType, "__ror__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __ror__() takes exactly one argument")
		}
		return unionNbOr(args[1], args[0])
	}))
}
