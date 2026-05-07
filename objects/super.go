// Cooperative super: walks the MRO of an instance starting after a
// named class, so a subclass can chain into the next implementation
// without naming the next class itself. The two-arg form
// `super(C, x)` records the (type, instance) pair; attribute access
// then resolves through `obj_type.MRO` skipping past `type`. The
// one-arg form `super(C)` defers binding until the resulting object
// goes through __get__; instance-mode super (no obj) and the zero-arg
// `super()` shorthand stay unimplemented in v0.10.1 since the latter
// requires the compiler to capture __class__ as a free var.
//
// CPython: Objects/typeobject.c:11769 superobject

package objects

import "fmt"

// Super carries the three slots PyTypeObject's superobject exposes:
// the explicit class, the instance (or class) the lookup is bound
// to, and the cached type to walk for the MRO.
//
// CPython: Objects/typeobject.c:11771 superobject
type Super struct {
	Header

	typ     *Type // su_type: the class super was constructed from
	obj     Object
	objType *Type // su_obj_type: cached Type for obj
}

// SuperType is the type singleton for the cooperative super helper.
//
// CPython: Objects/typeobject.c:12313 PySuper_Type
var SuperType = NewType("super", []*Type{objectType})

func init() {
	SuperType.Repr = superRepr
	SuperType.Str = superRepr
	SuperType.Getattro = superGetAttr
	SuperType.DescrGet = superDescrGet
	SuperType.Call = superCall
}

// NewSuper builds a Super tied to (typ, obj). obj==nil produces an
// unbound super (the form bound later via tp_descr_get). When obj is
// not nil supercheck must validate it; an error here surfaces the
// TypeError CPython raises in super_init.
//
// CPython: Objects/typeobject.c:12148 super_init_impl
func NewSuper(typ *Type, obj Object) (*Super, error) {
	if typ == nil {
		return nil, fmt.Errorf("TypeError: super() argument 1 must be type")
	}
	su := &Super{typ: typ, obj: obj}
	if obj != nil && obj != None() {
		ot, err := supercheck(typ, obj)
		if err != nil {
			return nil, err
		}
		su.objType = ot
	} else {
		su.obj = nil
	}
	su.init(SuperType)
	return su, nil
}

// supercheck mirrors the CPython helper of the same name. It validates
// that obj is an instance or subtype of typ and returns the type to
// walk for the MRO. Classes (the @classmethod path) return obj
// directly; ordinary instances return obj.__class__.
//
// CPython: Objects/typeobject.c:11937 supercheck
func supercheck(typ *Type, obj Object) (*Type, error) {
	if t, ok := obj.(*Type); ok {
		if IsSubtype(t, typ) {
			return t, nil
		}
	}
	ot := obj.Type()
	if IsSubtype(ot, typ) {
		return ot, nil
	}
	kind := "instance of"
	name := ot.Name
	if t, ok := obj.(*Type); ok {
		kind = "type"
		name = t.Name
	}
	return nil, fmt.Errorf(
		"TypeError: super(type, obj): obj (%s %s) is not an instance or subtype of type (%s).",
		kind, name, typ.Name,
	)
}

// superRepr matches CPython's super_repr.
//
// CPython: Objects/typeobject.c:11802 super_repr
func superRepr(o Object) (string, error) {
	su := o.(*Super)
	thisName := "NULL"
	if su.typ != nil {
		thisName = su.typ.Name
	}
	if su.objType != nil {
		return fmt.Sprintf("<super: <class '%s'>, <%s object>>", thisName, su.objType.Name), nil
	}
	return fmt.Sprintf("<super: <class '%s'>, NULL>", thisName), nil
}

// superGetAttr is the tp_getattro slot. It walks su.objType.MRO
// past su.typ and returns the first matching attribute, applying
// tp_descr_get with the instance so methods rebind. __class__ is
// special-cased to return SuperType (the class of the super proxy
// itself), matching CPython.
//
// CPython: Objects/typeobject.c:11922 super_getattro
func superGetAttr(o Object, name Object) (Object, error) {
	su := o.(*Super)
	if name == nil || name.Type() != strType {
		return nil, fmt.Errorf("TypeError: attribute name must be string, not '%s'", typeNameOf(name))
	}
	n := attrNameStr(name)
	if n == "__class__" {
		return GenericGetAttr(o, name)
	}
	if su.objType != nil {
		if res := lookupSuperDescr(su.typ, su.objType, n); res != nil {
			dt := res.Type()
			if dt.DescrGet != nil {
				bindTo := su.obj
				if su.obj == su.objType {
					bindTo = nil
				}
				return dt.DescrGet(res, bindTo, su.objType)
			}
			return res, nil
		}
	}
	return nil, fmt.Errorf("AttributeError: 'super' object has no attribute '%s'", n)
}

// lookupSuperDescr returns the attribute named `name` from the first
// type in objType.MRO strictly after typ. Returns nil when typ is not
// in the MRO or no class past it defines the attribute.
//
// CPython: Objects/typeobject.c:11822 _super_lookup_descr
func lookupSuperDescr(typ, objType *Type, name string) Object {
	mro := objType.MRO
	i := 0
	for ; i < len(mro); i++ {
		if mro[i] == typ {
			break
		}
	}
	i++
	for ; i < len(mro); i++ {
		if d := lookupTypeMember(mro[i], name); d != nil {
			return d
		}
	}
	return nil
}

// superDescrGet implements the tp_descr_get slot for super itself:
// `super(C).__get__(obj)` returns a fresh super bound to obj. Already
// bound supers and class-level access return self unchanged. This is
// the path that lets `super(C)` written at class-body time become an
// instance-bound super when the descriptor lookup hits it.
//
// CPython: Objects/typeobject.c:12015 super_descr_get
func superDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	su := descr.(*Super)
	if owner == nil || owner == None() || su.obj != nil {
		return descr, nil
	}
	return NewSuper(su.typ, owner)
}

// superCall handles `super(...)` invocation: zero-arg form is left to
// the future compiler-driven path; the one- and two-arg forms route
// through NewSuper. Keyword arguments are rejected to match
// _PyArg_NoKeywords in super_init.
//
// CPython: Objects/typeobject.c:12132 super_init
func superCall(callable Object, args []Object, kwargs map[string]Object) (Object, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("TypeError: super() takes no keyword arguments")
	}
	switch len(args) {
	case 0:
		return nil, fmt.Errorf("RuntimeError: super(): no arguments (zero-arg form not yet supported)")
	case 1:
		t, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: super() argument 1 must be type, not %s", typeNameOf(args[0]))
		}
		return NewSuper(t, nil)
	case 2:
		t, ok := args[0].(*Type)
		if !ok {
			return nil, fmt.Errorf("TypeError: super() argument 1 must be type, not %s", typeNameOf(args[0]))
		}
		return NewSuper(t, args[1])
	default:
		return nil, fmt.Errorf("TypeError: super() takes at most 2 arguments (%d given)", len(args))
	}
}
