// GenericAlias is the runtime representation of parameterized types
// such as list[int], dict[str, int], or tuple[int, ...]. It binds an
// origin (the underlying class) to an args tuple (the parameters) and
// proxies most attribute access back to the origin so user code can
// keep treating it as a class for introspection. types.GenericAlias is
// the public alias for this type; class subscription (list[int]) and
// types.GenericAlias(list, (int,)) both land here.
//
// CPython: Objects/genericaliasobject.c

package objects

import (
	"fmt"
	"strings"
)

// GenericAlias mirrors gaobject from CPython. The starred flag carries
// the *tuple[int] form so iteration over a generic alias can mark
// itself unpacked.
//
// CPython: Objects/genericaliasobject.c:15 gaobject
type GenericAlias struct {
	Header
	origin     Object
	args       *Tuple
	parameters *Tuple
	starred    bool
}

// GenericAliasType is the type singleton for types.GenericAlias.
//
// CPython: Objects/genericaliasobject.c:1014 Py_GenericAliasType
var GenericAliasType = NewType("types.GenericAlias", []*Type{objectType})

func init() {
	GenericAliasType.Repr = gaRepr
	GenericAliasType.Str = gaRepr
	GenericAliasType.Hash = gaHash
	GenericAliasType.Call = gaCall
	GenericAliasType.RichCmp = gaRichCompare
	GenericAliasType.Getattro = gaGetattro
	GenericAliasType.Iter = gaIter
	GenericAliasType.TpNew = gaNew
	GenericAliasType.Mapping = &MappingMethods{
		GetItem: gaSubscript,
	}
	// nb_or so a generic alias can compose with another type via |.
	//
	// CPython: Objects/genericaliasobject.c:917 ga_as_number
	GenericAliasType.Number = &NumberMethods{
		Or: unionTypeOr,
	}
	GenericAliasType.TpTraverse = gaTraverse
}

// NewGenericAlias constructs an alias for origin with the given args.
// If args is not already a tuple it is wrapped in a single-item tuple,
// matching setup_ga's behavior for the single-arg shortcut.
//
// CPython: Objects/genericaliasobject.c:1040 Py_GenericAlias
func NewGenericAlias(origin Object, args Object) *GenericAlias {
	ga := &GenericAlias{origin: origin}
	ga.init(GenericAliasType)
	gaSetupArgs(ga, args)
	return ga
}

// gaSetupArgs normalizes args into a tuple. CPython's setup_ga packs a
// non-tuple value in a one-tuple so callers that pass list[int] (not
// list[(int,)]) get the same shape.
//
// CPython: Objects/genericaliasobject.c:868 setup_ga
func gaSetupArgs(ga *GenericAlias, args Object) {
	if t, ok := args.(*Tuple); ok {
		ga.args = t
		return
	}
	ga.args = NewTuple([]Object{args})
}

// Origin returns the underlying class. Mirrors gaobject->origin.
//
// CPython: Objects/genericaliasobject.c:17 gaobject.origin
func (ga *GenericAlias) Origin() Object { return ga.origin }

// Args returns the parameter tuple. Mirrors gaobject->args.
//
// CPython: Objects/genericaliasobject.c:18 gaobject.args
func (ga *GenericAlias) Args() *Tuple { return ga.args }

// gaTraverse visits origin, args, and parameters so the cycle collector
// can walk through a generic alias.
//
// CPython: Objects/genericaliasobject.c:45 ga_traverse
func gaTraverse(o Object, visit Visitor) error {
	ga := o.(*GenericAlias)
	if ga.origin != nil {
		if err := visit(ga.origin); err != nil {
			return err
		}
	}
	if ga.args != nil {
		if err := visit(ga.args); err != nil {
			return err
		}
	}
	if ga.parameters != nil {
		if err := visit(ga.parameters); err != nil {
			return err
		}
	}
	return nil
}

// typingTypeRepr is the shared helper that prints types the way
// genericalias / union reprs want them: a builtin class shows its bare
// name, Ellipsis shows "...", NoneType shows "None", everything else
// falls back to repr. Mirrors _Py_typing_type_repr.
//
// CPython: Objects/typevarobject.c:262 _Py_typing_type_repr
func typingTypeRepr(p Object) (string, error) {
	if p == Ellipsis() {
		return "...", nil
	}
	if t, ok := p.(*Type); ok {
		// NoneType prints as "None" (not the bare type repr) so that
		// list[None] / int | None reads naturally.
		if t == None().Type() {
			return "None", nil
		}
		mod := t.Module
		if mod == "" || mod == "builtins" {
			return t.Name, nil
		}
		return mod + "." + t.Name, nil
	}
	return Repr(p)
}

// gaRepr formats list[int] as "list[int]", tuple[()] as "tuple[()]",
// and *tuple[int] as "*tuple[int]". A list argument is printed in
// square brackets, mirroring the ParamSpec-style display.
//
// CPython: Objects/genericaliasobject.c:90 ga_repr
func gaRepr(o Object) (string, error) {
	ga := o.(*GenericAlias)
	var b strings.Builder
	if ga.starred {
		b.WriteByte('*')
	}
	originRepr, err := typingTypeRepr(ga.origin)
	if err != nil {
		return "", err
	}
	b.WriteString(originRepr)
	b.WriteByte('[')
	n := ga.args.Len()
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		item := ga.args.Item(i)
		if l, ok := item.(*List); ok {
			s, err := gaReprItemsList(l)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			continue
		}
		s, err := typingTypeRepr(item)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	if n == 0 {
		b.WriteString("()")
	}
	b.WriteByte(']')
	return b.String(), nil
}

// gaReprItemsList formats a list inside an alias args tuple as
// [a, b, c]. The list shape signals a ParamSpec; items are printed via
// typingTypeRepr the same way the outer tuple is.
//
// CPython: Objects/genericaliasobject.c:55 ga_repr_items_list
func gaReprItemsList(l *List) (string, error) {
	var b strings.Builder
	b.WriteByte('[')
	for i, item := range l.items {
		if i > 0 {
			b.WriteString(", ")
		}
		s, err := typingTypeRepr(item)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	b.WriteByte(']')
	return b.String(), nil
}

// gaHash combines the hashes of origin and args with xor, matching
// ga_hash. CPython notes the args tuple already folds its members'
// hashes, so this is sufficient for collision behavior.
//
// CPython: Objects/genericaliasobject.c:604 ga_hash
func gaHash(o Object) (int64, error) {
	ga := o.(*GenericAlias)
	h0, err := Hash(ga.origin)
	if err != nil {
		return 0, err
	}
	h1, err := Hash(ga.args)
	if err != nil {
		return 0, err
	}
	return h0 ^ h1, nil
}

// gaCall forwards a parameterized class call to its origin: list[int]()
// builds an empty list, dict[str, int]() builds an empty dict. The
// runtime drops __orig_class__ tagging since gopy does not yet support
// per-instance attribute injection on built-ins.
//
// CPython: Objects/genericaliasobject.c:637 ga_call
func gaCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	ga := o.(*GenericAlias)
	return Call(ga.origin, NewTuple(args), kwargsToDict(kwargs))
}

// kwargsToDict turns a map[string]Object kwargs into a *Dict, or nil
// when empty, so calls funnel through the single Call entry point.
func kwargsToDict(kwargs map[string]Object) *Dict {
	if len(kwargs) == 0 {
		return nil
	}
	d := NewDict()
	for k, v := range kwargs {
		_ = d.SetItem(NewStr(k), v)
	}
	return d
}

// gaRichCompare implements __eq__ / __ne__ for generic aliases: two
// aliases are equal when their starred flag, origin, and args tuple
// match. Other comparison operators return NotImplemented.
//
// CPython: Objects/genericaliasobject.c:705 ga_richcompare
func gaRichCompare(a, b Object, op CompareOp) (Object, error) {
	bb, ok := b.(*GenericAlias)
	if !ok || (op != CompareEQ && op != CompareNE) {
		return NotImplemented(), nil
	}
	aa := a.(*GenericAlias)
	if op == CompareNE {
		eq, err := gaRichCompare(a, b, CompareEQ)
		if err != nil {
			return nil, err
		}
		t, err := IsTruthy(eq)
		if err != nil {
			return nil, err
		}
		return NewBool(!t), nil
	}
	if aa.starred != bb.starred {
		return False(), nil
	}
	eqOrigin, err := RichCmpBool(aa.origin, bb.origin, CompareEQ)
	if err != nil {
		return nil, err
	}
	if !eqOrigin {
		return False(), nil
	}
	return RichCmp(aa.args, bb.args, CompareEQ)
}

// gaAttrBlocked is the set of names that must never proxy to origin.
// CPython lists __bases__, __copy__, __deepcopy__; routing them
// through origin would make list[int].__bases__ return (object,) and
// pickle would treat the alias as the class.
//
// CPython: Objects/genericaliasobject.c:666 attr_blocked
var gaAttrBlocked = map[string]struct{}{
	"__bases__":    {},
	"__copy__":     {},
	"__deepcopy__": {},
}

// gaAttrOwn is the set of names handled by the alias itself rather
// than proxied to origin. Anything outside this set and outside
// gaAttrBlocked routes to origin's getattribute.
//
// CPython: Objects/genericaliasobject.c:653 attr_exceptions
var gaAttrOwn = map[string]struct{}{
	"__class__":                      {},
	"__origin__":                     {},
	"__args__":                       {},
	"__unpacked__":                   {},
	"__parameters__":                 {},
	"__typing_unpacked_tuple_args__": {},
	"__mro_entries__":                {},
	"__reduce_ex__":                  {},
	"__reduce__":                     {},
}

// gaGetattro fulfills the attr-exceptions / attr-blocked dispatch: own
// attrs and blocked names go through GenericGetAttr (which finds the
// member / getset / method descriptors registered below), everything
// else proxies to origin so list[int].append still works.
//
// CPython: Objects/genericaliasobject.c:674 ga_getattro
func gaGetattro(o Object, name Object) (Object, error) {
	ga := o.(*GenericAlias)
	if s, ok := name.(*Unicode); ok {
		n := s.v
		if _, isBlocked := gaAttrBlocked[n]; isBlocked {
			return GenericGetAttr(o, name)
		}
		if _, isOwn := gaAttrOwn[n]; isOwn {
			return GenericGetAttr(o, name)
		}
		return GetAttr(ga.origin, name)
	}
	return GenericGetAttr(o, name)
}

// gaSubscript implements alias[args]: it propagates parameter
// substitution from the alias's typevars into args, then rebuilds a
// new GenericAlias around origin. The typevar machinery (TypeVar,
// ParamSpec) is not ported in this slice, so the v0.12.1 cut accepts
// substitution only when the alias has no parameters left to bind
// (the no-op case) and otherwise raises a TypeError matching
// _Py_subs_parameters' "is not a generic class" branch.
//
// CPython: Objects/genericaliasobject.c:572 ga_getitem
func gaSubscript(o, item Object) (Object, error) {
	ga := o.(*GenericAlias)
	if ga.parameters == nil {
		ga.parameters = makeParameters(ga.args)
	}
	if err := subsParameters(o, ga.parameters); err != nil {
		return nil, err
	}
	res := NewGenericAlias(ga.origin, ga.args)
	res.starred = ga.starred
	return res, nil
}

// gaNew is the tp_new slot for types.GenericAlias(origin, args).
//
// CPython: Objects/genericaliasobject.c:896 ga_new
func gaNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: GenericAlias() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: GenericAlias expected 2 arguments, got %d", len(args))
	}
	return NewGenericAlias(args[0], args[1]), nil
}

// gaIter implements iter(alias). Each call yields a starred copy of
// the alias once, then raises StopIteration. CPython uses a separate
// gaiterobject for this; gopy collapses to a one-shot iterator built
// from the existing seqiter helper.
//
// CPython: Objects/genericaliasobject.c:1001 ga_iter
func gaIter(o Object) (Object, error) {
	ga := o.(*GenericAlias)
	starred := NewGenericAlias(ga.origin, ga.args)
	starred.starred = true
	return NewList([]Object{starred}).Type().Iter(NewList([]Object{starred}))
}

// makeParameters walks args collecting type-parameter-like entries.
// An entry qualifies if it has a __typing_subst__ attribute (TypeVar,
// ParamSpec, TypeVarTuple) or itself carries a non-empty __parameters__
// tuple (a nested generic alias). Deduplication preserves first-appearance
// order, matching CPython's _Py_make_parameters.
//
// CPython: Objects/genericaliasobject.c:186 _Py_make_parameters
func makeParameters(args *Tuple) *Tuple {
	seen := map[Object]bool{}
	var params []Object
	for i := 0; i < args.Len(); i++ {
		arg := args.Item(i)
		collectTypeParams(arg, seen, &params)
	}
	return NewTuple(params)
}

// collectTypeParams adds type-parameter-like objects from arg into params,
// deduplicating by pointer identity.
//
// CPython: Objects/genericaliasobject.c:147 collect_parameters
func collectTypeParams(arg Object, seen map[Object]bool, params *[]Object) {
	// TypeVar / ParamSpec / TypeVarTuple: has __typing_subst__
	if _, ok := arg.(*TypeVar); ok {
		if !seen[arg] {
			seen[arg] = true
			*params = append(*params, arg)
		}
		return
	}
	if _, ok := arg.(*ParamSpec); ok {
		if !seen[arg] {
			seen[arg] = true
			*params = append(*params, arg)
		}
		return
	}
	if _, ok := arg.(*TypeVarTuple); ok {
		if !seen[arg] {
			seen[arg] = true
			*params = append(*params, arg)
		}
		return
	}
	// Nested generic alias: recurse into its __parameters__.
	if ga, ok := arg.(*GenericAlias); ok {
		if ga.parameters == nil {
			ga.parameters = makeParameters(ga.args)
		}
		for i := 0; i < ga.parameters.Len(); i++ {
			collectTypeParams(ga.parameters.Item(i), seen, params)
		}
		return
	}
	// Python-level generic aliases (_GenericAlias, _UnionGenericAlias, etc.)
	// expose __parameters__ as a tuple; collect its elements.
	//
	// CPython: Objects/genericaliasobject.c:147 collect_parameters
	// (the Py_GenericAlias branch then the __parameters__ fallback)
	if p, err := GetAttr(arg, NewStr("__parameters__")); err == nil && p != nil {
		if tup, ok := p.(*Tuple); ok {
			for i := 0; i < tup.Len(); i++ {
				collectTypeParams(tup.Item(i), seen, params)
			}
		}
	}
}

// subsParameters substitutes typevars in args with the values in item.
// gopy does not implement typevars; the only callable case is "no
// parameters left to bind", which matches the empty-parameters branch
// of _Py_subs_parameters and raises the same TypeError CPython does.
//
// CPython: Objects/genericaliasobject.c:404 _Py_subs_parameters
func subsParameters(self Object, parameters *Tuple) error {
	if parameters.Len() == 0 {
		repr, err := Repr(self)
		if err != nil {
			return err
		}
		return fmt.Errorf("TypeError: %s is not a generic class", repr)
	}
	// Typevar substitution path is not implemented in this slice. We
	// keep the function so the dispatcher's shape matches CPython and
	// later TypeVar work can fill it in without touching the call
	// sites.
	return fmt.Errorf("TypeError: parameterized generic substitution is not supported")
}

// gaMroEntries returns (origin,) so a class statement that subclasses
// list[int] picks up `list` as its real base class. PEP 560 calls this
// during type construction.
//
// When origin is Generic, mirror typing._GenericAlias.__mro_entries__'s
// dedup: return () if any other base in the sibling tuple is a Protocol
// or another GenericAlias. Without this, classes like
// `class Foo[T](Protocol)` end up with both Generic and Protocol in
// their bases, producing an inconsistent C3 linearization.
//
// CPython: Objects/genericaliasobject.c:742 ga_mro_entries
// CPython: Lib/typing.py _GenericAlias.__mro_entries__
func gaMroEntries(ga *GenericAlias, bases *Tuple) *Tuple {
	if ga.origin == Object(GenericType) && bases != nil {
		selfIdx := -1
		for i := 0; i < bases.Len(); i++ {
			b := bases.Item(i)
			if b == Object(ga) {
				selfIdx = i
			}
			if t, ok := b.(*Type); ok && t.Name == "Protocol" {
				return NewTuple(nil)
			}
		}
		if selfIdx >= 0 {
			for i := selfIdx + 1; i < bases.Len(); i++ {
				if other, ok := bases.Item(i).(*GenericAlias); ok && other != ga {
					return NewTuple(nil)
				}
			}
		}
	}
	return NewTuple([]Object{ga.origin})
}

// init registers the alias-specific descriptors that ga_getattro
// routes through GenericGetAttr. __origin__ and __args__ expose the
// stored fields; __parameters__ lazily materializes through
// makeParameters; __mro_entries__, __instancecheck__,
// __subclasscheck__, __reduce__ are methods.
//
// CPython: Objects/genericaliasobject.c:820 ga_methods + ga_members +
//
//	ga_properties
func init() {
	SetTypeDescr(GenericAliasType, "__origin__", NewGetSetDescr("__origin__", func(o Object) (Object, error) {
		return o.(*GenericAlias).origin, nil
	}, nil))
	SetTypeDescr(GenericAliasType, "__args__", NewGetSetDescr("__args__", func(o Object) (Object, error) {
		return o.(*GenericAlias).args, nil
	}, nil))
	SetTypeDescr(GenericAliasType, "__unpacked__", NewGetSetDescr("__unpacked__", func(o Object) (Object, error) {
		return NewBool(o.(*GenericAlias).starred), nil
	}, nil))
	SetTypeDescr(GenericAliasType, "__parameters__", NewGetSetDescr("__parameters__", func(o Object) (Object, error) {
		ga := o.(*GenericAlias)
		if ga.parameters == nil {
			ga.parameters = makeParameters(ga.args)
		}
		return ga.parameters, nil
	}, nil))
	SetTypeDescr(GenericAliasType, "__typing_unpacked_tuple_args__", NewGetSetDescr("__typing_unpacked_tuple_args__", func(o Object) (Object, error) {
		ga := o.(*GenericAlias)
		if ga.starred && ga.origin == Object(TupleType) {
			return ga.args, nil
		}
		return None(), nil
	}, nil))
	SetTypeDescr(GenericAliasType, "__mro_entries__", NewMethodDescr(GenericAliasType, "__mro_entries__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __mro_entries__() missing self argument")
		}
		ga, ok := args[0].(*GenericAlias)
		if !ok {
			return nil, fmt.Errorf("TypeError: __mro_entries__ requires a GenericAlias, not %s", typeNameOf(args[0]))
		}
		var bases *Tuple
		if len(args) >= 2 {
			bases, _ = args[1].(*Tuple)
		}
		return gaMroEntries(ga, bases), nil
	}))
	SetTypeDescr(GenericAliasType, "__instancecheck__", NewMethodDescr(GenericAliasType, "__instancecheck__", func(_ []Object, _ map[string]Object) (Object, error) {
		return nil, fmt.Errorf("TypeError: isinstance() argument 2 cannot be a parameterized generic")
	}))
	SetTypeDescr(GenericAliasType, "__subclasscheck__", NewMethodDescr(GenericAliasType, "__subclasscheck__", func(_ []Object, _ map[string]Object) (Object, error) {
		return nil, fmt.Errorf("TypeError: issubclass() argument 2 cannot be a parameterized generic")
	}))
	SetTypeDescr(GenericAliasType, "__reduce__", NewMethodDescr(GenericAliasType, "__reduce__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() missing self argument")
		}
		ga, ok := args[0].(*GenericAlias)
		if !ok {
			return nil, fmt.Errorf("TypeError: __reduce__ requires a GenericAlias, not %s", typeNameOf(args[0]))
		}
		return NewTuple([]Object{
			GenericAliasType,
			NewTuple([]Object{ga.origin, ga.args}),
		}), nil
	}))
	SetTypeDescr(GenericAliasType, "__or__", NewMethodDescr(GenericAliasType, "__or__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __or__() takes exactly one argument")
		}
		return unionTypeOr(args[0], args[1])
	}))
	SetTypeDescr(GenericAliasType, "__ror__", NewMethodDescr(GenericAliasType, "__ror__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("TypeError: __ror__() takes exactly one argument")
		}
		return unionTypeOr(args[1], args[0])
	}))
}
