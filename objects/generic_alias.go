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
	return newGenericAliasOfType(GenericAliasType, origin, args)
}

// newGenericAliasOfType builds an alias whose type is cls. ga_new allocates
// via type->tp_alloc(type, 0), so a GenericAlias subclass (e.g.
// _collections_abc._CallableGenericAlias) gets an instance of itself.
//
// CPython: Objects/genericaliasobject.c:896 ga_new
func newGenericAliasOfType(cls *Type, origin Object, args Object) *GenericAlias {
	// setup_ga keeps a counted reference to origin (Py_NewRef) so the alias
	// owns it independently of the caller's transient reference.
	Incref(origin)
	ga := &GenericAlias{origin: origin}
	ga.init(cls)
	gaSetupArgs(ga, args)
	return ga
}

// gaSetupArgs normalizes args into a tuple. CPython's setup_ga packs a
// non-tuple value in a one-tuple so callers that pass list[int] (not
// list[(int,)]) get the same shape, and increfs an already-tuple args so
// the alias keeps it alive past the caller's reference.
//
// CPython: Objects/genericaliasobject.c:868 setup_ga
func gaSetupArgs(ga *GenericAlias, args Object) {
	if t, ok := args.(*Tuple); ok {
		Incref(t)
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
	if p == None().Type() {
		return "None", nil
	}
	// Anything that looks like a GenericAlias (has both __origin__ and
	// __args__) prints via its own repr rather than as a class.
	if origin, _ := LookupAttr(p, NewStr("__origin__")); origin != nil {
		if argsAttr, _ := LookupAttr(p, NewStr("__args__")); argsAttr != nil {
			return Repr(p)
		}
	}
	// A class is rendered from its __qualname__ (so nested/local classes
	// keep their dotted path) prefixed by __module__ unless that is
	// "builtins". Anything without both falls back to repr.
	qualname, err := LookupAttr(p, NewStr("__qualname__"))
	if err != nil {
		return "", err
	}
	if qualname == nil {
		return Repr(p)
	}
	qn, err := Str(qualname)
	if err != nil {
		return "", err
	}
	module, err := LookupAttr(p, NewStr("__module__"))
	if err != nil {
		return "", err
	}
	if module == nil || IsNone(module) {
		return Repr(p)
	}
	if m, ok := module.(*Unicode); ok && m.Value() == "builtins" {
		return qn, nil
	}
	ms, err := Str(module)
	if err != nil {
		return "", err
	}
	return ms + "." + qn, nil
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
	// Capture the length up front but read each element through a live
	// bounds check: a member's __repr__ can mutate (shrink) the list, in
	// which case CPython's PyList_GetItemRef raises IndexError.
	length := len(l.items)
	for i := 0; i < length; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		if i >= len(l.items) {
			return "", fmt.Errorf("IndexError: list index out of range")
		}
		s, err := typingTypeRepr(l.items[i])
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
// aliases are equal when their origin and args match. Handles both
// Go *GenericAlias (types.GenericAlias) and Python _GenericAlias
// objects (from typing.py) by reading __origin__ and __args__ via
// GetAttr when the other side is not a Go GenericAlias.
//
// CPython: Objects/genericaliasobject.c:705 ga_richcompare
func gaRichCompare(a, b Object, op CompareOp) (Object, error) {
	if op != CompareEQ && op != CompareNE {
		return NotImplemented(), nil
	}
	aa := a.(*GenericAlias)

	var bOrigin, bArgs Object
	if bb, ok := b.(*GenericAlias); ok {
		// Fast path: both are Go GenericAlias.
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
		bOrigin = bb.origin
		bArgs = bb.args
	} else {
		// Slow path: b might be a Python-level _GenericAlias from typing.py.
		// Read __origin__ and __args__ via attribute lookup.
		//
		// CPython: Objects/genericaliasobject.c:706 ga_richcompare (handles
		// both ga_type and _GenericAlias via __origin__/__args__ duck-typing)
		var err error
		bOrigin, err = GetAttr(b, NewStr("__origin__"))
		if err != nil {
			return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return on missing attr
		}
		bArgs, err = GetAttr(b, NewStr("__args__"))
		if err != nil {
			return NotImplemented(), nil //nolint:nilerr // mirrors Py_NotImplemented return on missing attr
		}
	}

	eqOrigin, err := RichCmpBool(aa.origin, bOrigin, CompareEQ)
	if err != nil {
		return nil, err
	}
	if !eqOrigin {
		if op == CompareNE {
			return True(), nil
		}
		return False(), nil
	}
	eq, err := RichCmp(aa.args, bArgs, CompareEQ)
	if err != nil {
		return nil, err
	}
	if op == CompareNE {
		t, err := IsTruthy(eq)
		if err != nil {
			return nil, err
		}
		return NewBool(!t), nil
	}
	return eq, nil
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

// gaAttrOwnOrder is the same set as gaAttrOwn but in CPython's declaration
// order, used by __dir__ to append the alias-owned names to dir(origin).
//
// CPython: Objects/genericaliasobject.c:653 attr_exceptions
var gaAttrOwnOrder = []string{
	"__class__",
	"__origin__",
	"__args__",
	"__unpacked__",
	"__parameters__",
	"__typing_unpacked_tuple_args__",
	"__mro_entries__",
	"__reduce_ex__",
	"__reduce__",
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

// gaSubscript implements alias[args]: it substitutes the alias's
// type parameters with the supplied arguments, then rebuilds a new
// GenericAlias around origin carrying the substituted args.
//
// CPython: Objects/genericaliasobject.c:572 ga_getitem
func gaSubscript(o, item Object) (Object, error) {
	ga := o.(*GenericAlias)
	if ga.parameters == nil {
		ga.parameters = makeParameters(ga.args)
	}
	newargs, err := subsParameters(o, ga.args, ga.parameters, item)
	if err != nil {
		return nil, err
	}
	res := NewGenericAlias(ga.origin, newargs)
	res.starred = ga.starred
	return res, nil
}

// gaNew is the tp_new slot for types.GenericAlias(origin, args).
//
// CPython: Objects/genericaliasobject.c:896 ga_new
func gaNew(cls *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: GenericAlias() takes no keyword arguments")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: GenericAlias expected 2 arguments, got %d", len(args))
	}
	if cls == nil {
		cls = GenericAliasType
	}
	return newGenericAliasOfType(cls, args[0], args[1]), nil
}

// gaIterObject is the gopy port of CPython's gaiterobject. It yields the
// alias once (as a starred copy) then exhausts. obj is set to nil after
// the single item is returned, matching CPython's sentinel pattern.
//
// CPython: Objects/genericaliasobject.c:27 gaiterobject
type gaIterObject struct {
	Header
	obj *GenericAlias // set to nil once exhausted
}

var gaIterType = NewType("generic_alias_iterator", []*Type{objectType})

func init() {
	gaIterType.Iter = SelfIter
	// CPython: Objects/genericaliasobject.c:922 ga_iternext
	gaIterType.IterNext = func(o Object) (Object, error) {
		gi := o.(*gaIterObject)
		if gi.obj == nil {
			return nil, ErrStopIteration
		}
		ga := gi.obj
		starred := NewGenericAlias(ga.origin, ga.args)
		starred.starred = true
		gi.obj = nil
		return starred, nil
	}
	// CPython: Objects/genericaliasobject.c:965 ga_iter_reduce
	SetTypeDescr(gaIterType, "__reduce__", NewMethodDescrConv(gaIterType, "__reduce__", MethNoArgs,
		func(args []Object, _ map[string]Object) (Object, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("TypeError: __reduce__() takes no arguments")
			}
			if BuiltinLookup == nil {
				return nil, fmt.Errorf("PicklingError: builtins not loaded")
			}
			iterFn, err := BuiltinLookup("iter")
			if err != nil {
				return nil, err
			}
			gi := args[0].(*gaIterObject)
			if gi.obj == nil {
				return NewTuple([]Object{iterFn, NewTuple([]Object{NewTuple(nil)})}), nil
			}
			return NewTuple([]Object{iterFn, NewTuple([]Object{gi.obj})}), nil
		},
	))
	AddIterSlotWrappers(gaIterType)
}

// gaIter implements iter(alias). CPython uses a dedicated gaiterobject
// that yields the alias once as a starred copy then exhausts.
//
// CPython: Objects/genericaliasobject.c:1001 ga_iter
func gaIter(o Object) (Object, error) {
	ga := o.(*GenericAlias)
	gi := &gaIterObject{obj: ga}
	gi.init(gaIterType)
	return gi, nil
}

// seqItems returns the elements of a tuple or list and whether arg was a
// list. Used by the parameter-collection and substitution helpers, which
// treat tuples and lists (ParamSpec arg lists) the same way.
func seqItems(arg Object) (items []Object, isList, ok bool) {
	switch v := arg.(type) {
	case *Tuple:
		out := make([]Object, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = v.Item(i)
		}
		return out, false, true
	case *List:
		out := make([]Object, len(v.items))
		copy(out, v.items)
		return out, true, true
	}
	return nil, false, false
}

// hasTypingSubst reports whether obj exposes a __typing_subst__ attribute,
// the marker CPython uses to recognize a type parameter (TypeVar, ParamSpec,
// TypeVarTuple) as opposed to an ordinary nested generic.
//
// CPython: Objects/genericaliasobject.c:210 PyObject_HasAttrWithError
func hasTypingSubst(obj Object) bool {
	m, err := LookupAttr(obj, NewStr("__typing_subst__"))
	return err == nil && m != nil
}

// tupleIndex returns the index of item in params[:len] by identity, or -1.
//
// CPython: Objects/genericaliasobject.c:148 tuple_index
func tupleIndex(params *Tuple, length int, item Object) int {
	for i := 0; i < length; i++ {
		if params.Item(i) == item {
			return i
		}
	}
	return -1
}

// makeParameters walks args collecting type-parameter-like entries. An
// entry qualifies if it has a __typing_subst__ attribute (TypeVar,
// ParamSpec, TypeVarTuple); otherwise its own __parameters__ tuple (a
// nested generic alias), or, for a bare tuple/list, the parameters of its
// members, are folded in. Deduplication preserves first-appearance order.
//
// CPython: Objects/genericaliasobject.c:186 _Py_make_parameters
func makeParameters(args *Tuple) *Tuple {
	items, _, _ := seqItems(args)
	var params []Object
	add := func(t Object) {
		for _, p := range params {
			if p == t {
				return
			}
		}
		params = append(params, t)
	}
	for _, t := range items {
		// We don't want the __parameters__ descriptor of a bare class.
		if _, ok := t.(*Type); ok {
			continue
		}
		if hasTypingSubst(t) {
			add(t)
			continue
		}
		var subparams *Tuple
		if p, err := LookupAttr(t, NewStr("__parameters__")); err == nil && p != nil {
			if tup, ok := p.(*Tuple); ok {
				subparams = tup
			}
		} else if _, _, isSeq := seqItems(t); isSeq {
			// Recurse into bare tuples/lists (ParamSpec arg lists).
			subparams = makeParameters(toTuple(t))
		}
		if subparams != nil {
			for j := 0; j < subparams.Len(); j++ {
				add(subparams.Item(j))
			}
		}
	}
	return NewTuple(params)
}

// toTuple coerces a tuple or list to a *Tuple for the recursive helpers.
func toTuple(arg Object) *Tuple {
	if t, ok := arg.(*Tuple); ok {
		return t
	}
	items, _, _ := seqItems(arg)
	return NewTuple(items)
}

// subsTvars handles a nested generic alias inside the args being
// substituted: list[dict[T, S]][int, str] rebuilds the inner dict with the
// outer substitutions threaded through its own __parameters__.
//
// CPython: Objects/genericaliasobject.c:274 subs_tvars
func subsTvars(obj Object, params *Tuple, argitems []Object) (Object, error) {
	var subparams *Tuple
	if p, err := LookupAttr(obj, NewStr("__parameters__")); err != nil {
		return nil, err
	} else if p != nil {
		if tup, ok := p.(*Tuple); ok {
			subparams = tup
		}
	}
	if subparams == nil || subparams.Len() == 0 {
		return obj, nil
	}
	nparams := params.Len()
	var subargs []Object
	for i := 0; i < subparams.Len(); i++ {
		arg := subparams.Item(i)
		iparam := tupleIndex(params, nparams, arg)
		if iparam >= 0 {
			param := params.Item(iparam)
			arg = argitems[iparam]
			// TypeVarTuple slot: splice its tuple of replacements in.
			if _, ok := param.(*TypeVarTuple); ok {
				if argTup, ok2 := arg.(*Tuple); ok2 {
					for k := 0; k < argTup.Len(); k++ {
						subargs = append(subargs, argTup.Item(k))
					}
					continue
				}
			}
		}
		subargs = append(subargs, arg)
	}
	return GetItem(obj, NewTuple(subargs))
}

// isUnpackedTypevartuple reports whether arg is an *Ts / Unpack[Ts] form,
// which expands in place during substitution.
//
// CPython: Objects/genericaliasobject.c:321 _is_unpacked_typevartuple
func isUnpackedTypevartuple(arg Object) (bool, error) {
	if _, ok := arg.(*Type); ok {
		return false, nil
	}
	tmp, err := LookupAttr(arg, NewStr("__typing_is_unpacked_typevartuple__"))
	if err != nil {
		return false, err
	}
	if tmp == nil {
		return false, nil
	}
	return IsTruthy(tmp)
}

// unpackedTupleArgs returns the args of a starred tuple alias (*tuple[int,
// str]) so they can be spliced into the surrounding subscript, or nil.
//
// CPython: Objects/genericaliasobject.c:336 _unpacked_tuple_args
func unpackedTupleArgs(arg Object) (*Tuple, error) {
	if ga, ok := arg.(*GenericAlias); ok && ga.starred && ga.origin == Object(TupleType) {
		return ga.args, nil
	}
	res, err := LookupAttr(arg, NewStr("__typing_unpacked_tuple_args__"))
	if err != nil {
		return nil, err
	}
	if res != nil && !IsNone(res) {
		if tup, ok := res.(*Tuple); ok {
			return tup, nil
		}
	}
	return nil, nil
}

// unpackArgs flattens any starred tuple aliases in item into a flat tuple of
// arguments, matching CPython's _unpack_args. A trailing Ellipsis (tuple[int,
// ...]) blocks flattening so the variadic shape is preserved.
//
// CPython: Objects/genericaliasobject.c:358 _unpack_args
func unpackArgs(item Object) (*Tuple, error) {
	var items []Object
	if t, ok := item.(*Tuple); ok {
		for i := 0; i < t.Len(); i++ {
			items = append(items, t.Item(i))
		}
	} else {
		items = []Object{item}
	}
	var newargs []Object
	for _, it := range items {
		if _, isType := it.(*Type); !isType {
			subargs, err := unpackedTupleArgs(it)
			if err != nil {
				return nil, err
			}
			if subargs != nil {
				n := subargs.Len()
				if n == 0 || subargs.Item(n-1) != Ellipsis() {
					for k := 0; k < n; k++ {
						newargs = append(newargs, subargs.Item(k))
					}
					continue
				}
			}
		}
		newargs = append(newargs, it)
	}
	return NewTuple(newargs), nil
}

// subsParameters substitutes the type parameters in args with the values in
// item, returning the new args tuple for the rebuilt alias. This is the full
// port of _Py_subs_parameters: it unpacks starred arguments, runs each
// parameter's __typing_prepare_subst__ hook, checks arity, then walks args
// replacing TypeVars via __typing_subst__ and recursing into nested
// generics, tuples and lists.
//
// CPython: Objects/genericaliasobject.c:404 _Py_subs_parameters
func subsParameters(self Object, args Object, parameters *Tuple, item Object) (*Tuple, error) {
	nparams := parameters.Len()
	if nparams == 0 {
		repr, err := Repr(self)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("TypeError: %s is not a generic class", repr)
	}
	preparedItem, argitems, err := prepareSubstItems(self, parameters, item)
	if err != nil {
		return nil, err
	}

	srcItems, _, _ := seqItems(args)
	var newargs []Object
	for _, arg := range srcItems {
		out, serr := substArg(self, arg, parameters, argitems, preparedItem)
		if serr != nil {
			return nil, serr
		}
		newargs = append(newargs, out...)
	}
	return NewTuple(newargs), nil
}

// prepareSubstItems runs each parameter's __typing_prepare_subst__ hook over
// the (unpacked) substitution item, then flattens the result into argitems
// and validates the arity against nparams. Returns the prepared item (used
// when recursing into nested sequences) and the per-parameter argitems.
//
// CPython: Objects/genericaliasobject.c:404 _Py_subs_parameters (prepare loop)
func prepareSubstItems(self Object, parameters *Tuple, item Object) (Object, []Object, error) {
	nparams := parameters.Len()
	prepared, err := unpackArgs(item)
	if err != nil {
		return nil, nil, err
	}
	var preparedItem Object = prepared
	for i := 0; i < nparams; i++ {
		prepare, lerr := LookupAttr(parameters.Item(i), NewStr("__typing_prepare_subst__"))
		if lerr != nil {
			return nil, nil, lerr
		}
		if prepare != nil && !IsNone(prepare) {
			tmp, cerr := Call(prepare, NewTuple([]Object{self, preparedItem}), nil)
			if cerr != nil {
				return nil, nil, cerr
			}
			preparedItem = tmp
		}
	}
	var argitems []Object
	if t, ok := preparedItem.(*Tuple); ok {
		argitems = make([]Object, t.Len())
		for i := 0; i < t.Len(); i++ {
			argitems[i] = t.Item(i)
		}
	} else {
		argitems = []Object{preparedItem}
	}
	if len(argitems) != nparams {
		repr, rerr := Repr(self)
		if rerr != nil {
			return nil, nil, rerr
		}
		kind := "few"
		if len(argitems) > nparams {
			kind = "many"
		}
		return nil, nil, fmt.Errorf("TypeError: Too %s arguments for %s; actual %d, expected %d",
			kind, repr, len(argitems), nparams)
	}
	return preparedItem, argitems, nil
}

// substArg substitutes the parameters of a single arg, returning the values
// it contributes to the rebuilt args tuple. A bare class passes through; a
// nested list/tuple recurses; a TypeVar is replaced via __typing_subst__ and
// an unpacked TypeVarTuple splices its tuple result inline.
//
// CPython: Objects/genericaliasobject.c:404 _Py_subs_parameters (arg loop)
func substArg(self, arg Object, parameters *Tuple, argitems []Object, preparedItem Object) ([]Object, error) {
	if _, ok := arg.(*Type); ok {
		return []Object{arg}, nil
	}
	// Recursively substitute params in lists/tuples.
	if subItems, isList, isSeq := seqItems(arg); isSeq {
		subres, serr := subsParameters(self, NewTuple(subItems), parameters, preparedItem)
		if serr != nil {
			return nil, serr
		}
		if isList {
			lst := make([]Object, subres.Len())
			for i := 0; i < subres.Len(); i++ {
				lst[i] = subres.Item(i)
			}
			return []Object{NewList(lst)}, nil
		}
		return []Object{subres}, nil
	}
	unpack, uerr := isUnpackedTypevartuple(arg)
	if uerr != nil {
		return nil, uerr
	}
	subst, lerr := LookupAttr(arg, NewStr("__typing_subst__"))
	if lerr != nil {
		return nil, lerr
	}
	var repl Object
	var err error
	if subst != nil && !IsNone(subst) {
		iparam := tupleIndex(parameters, parameters.Len(), arg)
		repl, err = Call(subst, NewTuple([]Object{argitems[iparam]}), nil)
	} else {
		repl, err = subsTvars(arg, parameters, argitems)
	}
	if err != nil {
		return nil, err
	}
	if unpack {
		tup, ok := repl.(*Tuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected __typing_subst__ to return a tuple, not %s", typeNameOf(repl))
		}
		out := make([]Object, tup.Len())
		for k := 0; k < tup.Len(); k++ {
			out[k] = tup.Item(k)
		}
		return out, nil
	}
	return []Object{repl}, nil
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
	// __origin__/__args__/__parameters__ hand back stored references, so they
	// must return an owned reference (Py_NewRef) the way CPython's getset
	// getters do. Without the Incref the caller's arg-drop decrefs the stored
	// tuple to zero and tupleDealloc empties it under the alias's feet.
	//
	// CPython: Objects/genericaliasobject.c:790 ga_members (Py_NewRef contract)
	// Expose ga_new as __new__ so a Python subclass can reach it through
	// super().__new__(cls, origin, args) instead of falling through to
	// object.__new__ (which rejects the extra arguments).
	//
	// CPython: Objects/genericaliasobject.c:896 ga_new
	SetTypeDescr(GenericAliasType, "__new__", NewBuiltinFunction("types.GenericAlias.__new__", func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: GenericAlias.__new__(): not enough arguments")
		}
		cls, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: GenericAlias.__new__(X): X is not a type object (%s)", typeNameOf(args[0]))
		}
		return gaNew(cls, args[1:], kwargs)
	}))
	// Expose the tp_repr/tp_hash slots as dunders so a Python subclass of
	// types.GenericAlias inherits them. Without a __repr__ in the dict, a
	// subclass's slot fixup finds object.__repr__ first and renders the
	// default <object at 0x..> form instead of "list[int]".
	//
	// CPython: Objects/typeobject.c add_operators (slot wrappers)
	SetTypeDescr(GenericAliasType, "__repr__", NewMethodDescrConv(GenericAliasType, "__repr__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __repr__() missing self argument")
		}
		s, err := gaRepr(args[0])
		if err != nil {
			return nil, err
		}
		return NewStr(s), nil
	}))
	SetTypeDescr(GenericAliasType, "__hash__", NewMethodDescrConv(GenericAliasType, "__hash__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __hash__() missing self argument")
		}
		h, err := gaHash(args[0])
		if err != nil {
			return nil, err
		}
		return NewInt(h), nil
	}))
	SetTypeDescr(GenericAliasType, "__origin__", NewGetSetDescr("__origin__", gaGetOrigin, nil))
	SetTypeDescr(GenericAliasType, "__args__", NewGetSetDescr("__args__", gaGetArgs, nil))
	SetTypeDescr(GenericAliasType, "__unpacked__", NewGetSetDescr("__unpacked__", gaGetUnpacked, nil))
	SetTypeDescr(GenericAliasType, "__parameters__", NewGetSetDescr("__parameters__", gaGetParameters, nil))
	SetTypeDescr(GenericAliasType, "__typing_unpacked_tuple_args__", NewGetSetDescr("__typing_unpacked_tuple_args__", gaGetUnpackedTupleArgs, nil))
	SetTypeDescr(GenericAliasType, "__mro_entries__", NewMethodDescr(GenericAliasType, "__mro_entries__", gaMroEntriesMethod))
	SetTypeDescr(GenericAliasType, "__instancecheck__", NewMethodDescr(GenericAliasType, "__instancecheck__", gaInstanceCheck))
	SetTypeDescr(GenericAliasType, "__subclasscheck__", NewMethodDescr(GenericAliasType, "__subclasscheck__", gaSubclassCheck))
	SetTypeDescr(GenericAliasType, "__reduce__", NewMethodDescrConv(GenericAliasType, "__reduce__", MethNoArgs, gaReduce))
	SetTypeDescr(GenericAliasType, "__dir__", NewMethodDescr(GenericAliasType, "__dir__", gaDir))
	SetTypeDescr(GenericAliasType, "__or__", NewMethodDescr(GenericAliasType, "__or__", gaOr))
	SetTypeDescr(GenericAliasType, "__ror__", NewMethodDescr(GenericAliasType, "__ror__", gaRor))
}

// __origin__/__args__/__parameters__ hand back stored references, so they
// must return an owned reference (Py_NewRef) the way CPython's getset getters
// do. Without the Incref the caller's arg-drop decrefs the stored tuple to
// zero and tupleDealloc empties it under the alias's feet.
//
// CPython: Objects/genericaliasobject.c:790 ga_members (Py_NewRef contract)
func gaGetOrigin(o Object) (Object, error) {
	origin := o.(*GenericAlias).origin
	Incref(origin)
	return origin, nil
}

func gaGetArgs(o Object) (Object, error) {
	args := o.(*GenericAlias).args
	Incref(args)
	return args, nil
}

func gaGetUnpacked(o Object) (Object, error) {
	return NewBool(o.(*GenericAlias).starred), nil
}

func gaGetParameters(o Object) (Object, error) {
	ga := o.(*GenericAlias)
	if ga.parameters == nil {
		ga.parameters = makeParameters(ga.args)
	}
	Incref(ga.parameters)
	return ga.parameters, nil
}

func gaGetUnpackedTupleArgs(o Object) (Object, error) {
	ga := o.(*GenericAlias)
	if ga.starred && ga.origin == Object(TupleType) {
		Incref(ga.args)
		return ga.args, nil
	}
	return None(), nil
}

func gaMroEntriesMethod(args []Object, _ map[string]Object) (Object, error) {
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
}

func gaInstanceCheck(_ []Object, _ map[string]Object) (Object, error) {
	return nil, fmt.Errorf("TypeError: isinstance() argument 2 cannot be a parameterized generic")
}

func gaSubclassCheck(_ []Object, _ map[string]Object) (Object, error) {
	return nil, fmt.Errorf("TypeError: issubclass() argument 2 cannot be a parameterized generic")
}

func gaReduce(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __reduce__() missing self argument")
	}
	ga, ok := args[0].(*GenericAlias)
	if !ok {
		return nil, fmt.Errorf("TypeError: __reduce__ requires a GenericAlias, not %s", typeNameOf(args[0]))
	}
	// A starred alias (*tuple[int]) reduces to next(iter(unstarred)),
	// because iterating a generic alias yields one starred copy. That
	// is how CPython round-trips the starred flag through pickle.
	//
	// CPython: Objects/genericaliasobject.c:765 ga_reduce
	if ga.starred {
		if BuiltinLookup == nil {
			return nil, fmt.Errorf("PicklingError: builtins not loaded")
		}
		nextFn, err := BuiltinLookup("next")
		if err != nil {
			return nil, err
		}
		it, err := gaIter(NewGenericAlias(ga.origin, ga.args))
		if err != nil {
			return nil, err
		}
		return NewTuple([]Object{nextFn, NewTuple([]Object{it})}), nil
	}
	return NewTuple([]Object{
		GenericAliasType,
		NewTuple([]Object{ga.origin, ga.args}),
	}), nil
}

func gaDir(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: __dir__() missing self argument")
	}
	ga, ok := args[0].(*GenericAlias)
	if !ok {
		return nil, fmt.Errorf("TypeError: __dir__ requires a GenericAlias, not %s", typeNameOf(args[0]))
	}
	// dir(alias) is dir(origin) plus the alias-owned attributes not
	// already present, in CPython's declaration order.
	//
	// CPython: Objects/genericaliasobject.c:836 ga_dir
	dir, err := Dir(ga.origin)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, it := range dir.items {
		if s, ok := it.(*Unicode); ok {
			present[s.Value()] = true
		}
	}
	for _, name := range gaAttrOwnOrder {
		if !present[name] {
			dir.items = append(dir.items, NewStr(name))
		}
	}
	return dir, nil
}

func gaOr(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __or__() takes exactly one argument")
	}
	return unionTypeOr(args[0], args[1])
}

func gaRor(args []Object, _ map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __ror__() takes exactly one argument")
	}
	return unionTypeOr(args[1], args[0])
}
