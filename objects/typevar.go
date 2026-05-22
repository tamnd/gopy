// TypeVar / ParamSpec / TypeVarTuple / Generic are the runtime
// objects PEP 695 generic syntax produces. CPython implements them in
// Objects/typevarobject.c as full PyTypeObjects with bound/constraints
// thunks, default thunks, __mro_entries__, and substitution behavior.
//
// The gopy port here covers the surface that the codegen path needs
// to actually run a `class C[T]: ...` / `def f[T](...)` / `type X[T] = ...`
// statement end-to-end. Subscription (the bound/constraints thunks)
// and substitution land alongside typing.py support; for now the
// values are inert placeholders that carry __name__ and behave well
// as bases (Generic[T].__mro_entries__ returns (Generic,) so
// __build_class__ picks up the right base).
//
// CPython: Objects/typevarobject.c

package objects

import (
	"fmt"
)

// TypeVar mirrors CPython's typevarobject. The bound and constraints
// fields are nil unless the caller supplied them via the binary
// intrinsics (TYPEVAR_WITH_BOUND / TYPEVAR_WITH_CONSTRAINTS).
//
// CPython: Objects/typevarobject.c:30 typevarobject
type TypeVar struct {
	Header
	NameStr     string
	Bound       Object
	Constraints Object
	Default     Object
	HasDefault  bool
}

// TypeVarType is the type singleton.
//
// CPython: Objects/typevarobject.c PyTypeVar_Type
var TypeVarType = NewType("typing.TypeVar", []*Type{objectType})

// NewTypeVar returns a fresh TypeVar with the given name.
//
// CPython: Objects/typevarobject.c:1813 _Py_make_typevar
func NewTypeVar(name string, bound, constraints Object) *TypeVar {
	tv := &TypeVar{NameStr: name, Bound: bound, Constraints: constraints}
	tv.Init(TypeVarType)
	return tv
}

// ParamSpec is PEP 612's parameter-specification variable.
//
// CPython: Objects/typevarobject.c:760 paramspecobject
type ParamSpec struct {
	Header
	NameStr    string
	Default    Object
	HasDefault bool
}

// ParamSpecType is the type singleton.
//
// CPython: Objects/typevarobject.c PyParamSpec_Type
var ParamSpecType = NewType("typing.ParamSpec", []*Type{objectType})

// NewParamSpec builds a fresh ParamSpec.
//
// CPython: Objects/typevarobject.c:1844 _Py_make_paramspec
func NewParamSpec(name string) *ParamSpec {
	ps := &ParamSpec{NameStr: name}
	ps.Init(ParamSpecType)
	return ps
}

// TypeVarTuple is PEP 646's variadic type variable.
//
// CPython: Objects/typevarobject.c:1180 typevartupleobject
type TypeVarTuple struct {
	Header
	NameStr    string
	Default    Object
	HasDefault bool
}

// TypeVarTupleType is the type singleton.
//
// CPython: Objects/typevarobject.c PyTypeVarTuple_Type
var TypeVarTupleType = NewType("typing.TypeVarTuple", []*Type{objectType})

// NewTypeVarTuple builds a fresh TypeVarTuple.
//
// CPython: Objects/typevarobject.c:1875 _Py_make_typevartuple
func NewTypeVarTuple(name string) *TypeVarTuple {
	tvt := &TypeVarTuple{NameStr: name}
	tvt.Init(TypeVarTupleType)
	return tvt
}

// GenericType is the runtime stand-in for typing.Generic. CPython
// exposes it as a real PyTypeObject whose __class_getitem__ produces a
// _GenericAlias; the simpler stand-in here is just a subclassable type
// that __build_class__ accepts as a base. Generic[T,...] from the
// SUBSCRIPT_GENERIC intrinsic becomes a GenericAlias whose origin is
// this type, so PEP 560 __mro_entries__ pulls Generic itself into the
// new class's bases.
//
// CPython: Objects/typevarobject.c PyGeneric_Type
var GenericType = NewType("typing.Generic", []*Type{objectType})

func init() {
	TypeVarType.Repr = func(o Object) (string, error) {
		return "~" + o.(*TypeVar).NameStr, nil
	}
	TypeVarType.Str = TypeVarType.Repr
	ParamSpecType.Repr = func(o Object) (string, error) {
		return "~" + o.(*ParamSpec).NameStr, nil
	}
	ParamSpecType.Str = ParamSpecType.Repr
	TypeVarTupleType.Repr = func(o Object) (string, error) {
		return "~" + o.(*TypeVarTuple).NameStr, nil
	}
	TypeVarTupleType.Str = TypeVarTupleType.Repr

	SetTypeDescr(TypeVarType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*TypeVar).NameStr), nil
	}, nil))
	SetTypeDescr(TypeVarType, "__bound__", NewGetSetDescr("__bound__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.Bound == nil {
			return None(), nil
		}
		return tv.Bound, nil
	}, nil))
	SetTypeDescr(TypeVarType, "__constraints__", NewGetSetDescr("__constraints__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.Constraints == nil {
			return NewTuple(nil), nil
		}
		return tv.Constraints, nil
	}, nil))
	SetTypeDescr(TypeVarType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if !tv.HasDefault {
			return NoDefault(), nil
		}
		return tv.Default, nil
	}, nil))
	SetTypeDescr(TypeVarType, "has_default", NewMethodDescr(TypeVarType, "has_default", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: has_default() missing self")
		}
		tv, ok := args[0].(*TypeVar)
		if !ok {
			return nil, fmt.Errorf("TypeError: has_default() requires TypeVar")
		}
		return NewBool(tv.HasDefault), nil
	}))

	SetTypeDescr(ParamSpecType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*ParamSpec).NameStr), nil
	}, nil))
	SetTypeDescr(ParamSpecType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		ps := o.(*ParamSpec)
		if !ps.HasDefault {
			return NoDefault(), nil
		}
		return ps.Default, nil
	}, nil))

	SetTypeDescr(TypeVarTupleType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*TypeVarTuple).NameStr), nil
	}, nil))
	SetTypeDescr(TypeVarTupleType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		tvt := o.(*TypeVarTuple)
		if !tvt.HasDefault {
			return NoDefault(), nil
		}
		return tvt.Default, nil
	}, nil))
}

// ParamSpecAttr is the shared object behind ParamSpec.args / .kwargs.
// CPython models both with a single paramspecattrobject carrying just
// the back-pointer to the originating ParamSpec.
//
// CPython: Objects/typevarobject.c:939 paramspecattrobject
type ParamSpecAttr struct {
	Header
	Origin Object
}

// ParamSpecArgsType is the typing.ParamSpecArgs type singleton.
//
// CPython: Objects/typevarobject.c:1079 paramspecargs_spec
var ParamSpecArgsType = NewType("typing.ParamSpecArgs", []*Type{objectType})

// ParamSpecKwargsType is the typing.ParamSpecKwargs type singleton.
//
// CPython: Objects/typevarobject.c:1136 paramspeckwargs_spec
var ParamSpecKwargsType = NewType("typing.ParamSpecKwargs", []*Type{objectType})

// NewParamSpecArgs builds the P.args attribute for ParamSpec P.
//
// CPython: Objects/typevarobject.c:1233 _Py_paramspecargs_new
func NewParamSpecArgs(origin Object) *ParamSpecAttr {
	a := &ParamSpecAttr{Origin: origin}
	a.Init(ParamSpecArgsType)
	return a
}

// NewParamSpecKwargs builds the P.kwargs attribute for ParamSpec P.
//
// CPython: Objects/typevarobject.c:1240 _Py_paramspeckwargs_new
func NewParamSpecKwargs(origin Object) *ParamSpecAttr {
	a := &ParamSpecAttr{Origin: origin}
	a.Init(ParamSpecKwargsType)
	return a
}

func init() {
	ParamSpecArgsType.Repr = func(o Object) (string, error) {
		psa := o.(*ParamSpecAttr)
		if ps, ok := psa.Origin.(*ParamSpec); ok {
			return ps.NameStr + ".args", nil
		}
		s, err := Repr(psa.Origin)
		if err != nil {
			return "", err
		}
		return s + ".args", nil
	}
	ParamSpecArgsType.Str = ParamSpecArgsType.Repr
	ParamSpecKwargsType.Repr = func(o Object) (string, error) {
		psa := o.(*ParamSpecAttr)
		if ps, ok := psa.Origin.(*ParamSpec); ok {
			return ps.NameStr + ".kwargs", nil
		}
		s, err := Repr(psa.Origin)
		if err != nil {
			return "", err
		}
		return s + ".kwargs", nil
	}
	ParamSpecKwargsType.Str = ParamSpecKwargsType.Repr

	originGet := func(o Object) (Object, error) {
		return o.(*ParamSpecAttr).Origin, nil
	}
	SetTypeDescr(ParamSpecArgsType, "__origin__", NewGetSetDescr("__origin__", originGet, nil))
	SetTypeDescr(ParamSpecKwargsType, "__origin__", NewGetSetDescr("__origin__", originGet, nil))

	SetTypeDescr(ParamSpecType, "args", NewGetSetDescr("args", func(o Object) (Object, error) {
		return NewParamSpecArgs(o), nil
	}, nil))
	SetTypeDescr(ParamSpecType, "kwargs", NewGetSetDescr("kwargs", func(o Object) (Object, error) {
		return NewParamSpecKwargs(o), nil
	}, nil))

	ParamSpecArgsType.TpNew = func(_ *Type, args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: ParamSpecArgs() takes exactly 1 argument (%d given)", len(args))
		}
		return NewParamSpecArgs(args[0]), nil
	}
	ParamSpecKwargsType.TpNew = func(_ *Type, args []Object, _ map[string]Object) (Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: ParamSpecKwargs() takes exactly 1 argument (%d given)", len(args))
		}
		return NewParamSpecKwargs(args[0]), nil
	}

	TypeVarType.TpNew = typevarTpNew
	ParamSpecType.TpNew = paramspecTpNew
	TypeVarTupleType.TpNew = typevartupleTpNew
}

// typevarTpNew is the TypeVar(name, *constraints, bound=None,
// default=NoDefault, covariant=False, contravariant=False,
// infer_variance=False) constructor. Mirrors typevar_new_impl.
//
// CPython: Objects/typevarobject.c:687 typevar_new_impl
func typevarTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: TypeVar() missing required argument: 'name'")
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: TypeVar() argument 'name' must be str")
	}
	constraints := args[1:]
	var bound Object
	var defaultVal Object
	hasDefault := false
	covariant := false
	contravariant := false
	inferVariance := false
	for k, v := range kwargs {
		switch k {
		case "bound":
			if v != None() {
				bound = v
			}
		case "default":
			if v != NoDefault() {
				defaultVal = v
				hasDefault = true
			}
		case "covariant":
			covariant = IsTrue(v)
		case "contravariant":
			contravariant = IsTrue(v)
		case "infer_variance":
			inferVariance = IsTrue(v)
		default:
			return nil, fmt.Errorf("TypeError: TypeVar() got an unexpected keyword argument '%s'", k)
		}
	}
	if covariant && contravariant {
		return nil, fmt.Errorf("ValueError: Bivariant types are not supported.")
	}
	if inferVariance && (covariant || contravariant) {
		return nil, fmt.Errorf("ValueError: Variance cannot be specified with infer_variance.")
	}
	if len(constraints) == 1 {
		return nil, fmt.Errorf("TypeError: A single constraint is not allowed")
	}
	var constraintsTup Object
	if len(constraints) >= 2 {
		if bound != nil {
			return nil, fmt.Errorf("TypeError: Constraints cannot be combined with bound=...")
		}
		constraintsTup = NewTuple(constraints)
	}
	tv := NewTypeVar(nameObj.Value(), bound, constraintsTup)
	if hasDefault {
		tv.Default = defaultVal
		tv.HasDefault = true
	}
	_ = covariant
	_ = contravariant
	_ = inferVariance
	return tv, nil
}

// paramspecTpNew is the ParamSpec(name, *, bound=None, default=NoDefault,
// covariant=False, contravariant=False, infer_variance=False) constructor.
// Mirrors paramspec_new_impl.
//
// CPython: Objects/typevarobject.c:1323 paramspec_new_impl
func paramspecTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: ParamSpec() takes exactly 1 positional argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: ParamSpec() argument 'name' must be str")
	}
	covariant := false
	contravariant := false
	inferVariance := false
	var defaultVal Object
	hasDefault := false
	for k, v := range kwargs {
		switch k {
		case "bound":
			// gopy ParamSpec drops bound for now; ParamSpec.__bound__
			// is exposed but unused, matching the simplified port.
			_ = v
		case "default":
			if v != NoDefault() {
				defaultVal = v
				hasDefault = true
			}
		case "covariant":
			covariant = IsTrue(v)
		case "contravariant":
			contravariant = IsTrue(v)
		case "infer_variance":
			inferVariance = IsTrue(v)
		default:
			return nil, fmt.Errorf("TypeError: ParamSpec() got an unexpected keyword argument '%s'", k)
		}
	}
	if covariant && contravariant {
		return nil, fmt.Errorf("ValueError: Bivariant types are not supported.")
	}
	if inferVariance && (covariant || contravariant) {
		return nil, fmt.Errorf("ValueError: Variance cannot be specified with infer_variance.")
	}
	ps := NewParamSpec(nameObj.Value())
	if hasDefault {
		ps.Default = defaultVal
		ps.HasDefault = true
	}
	return ps, nil
}

// typevartupleTpNew is the TypeVarTuple(name, *, default=NoDefault)
// constructor. Mirrors typevartuple_impl.
//
// CPython: Objects/typevarobject.c:1596 typevartuple_impl
func typevartupleTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: TypeVarTuple() takes exactly 1 positional argument (%d given)", len(args))
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: TypeVarTuple() argument 'name' must be str")
	}
	var defaultVal Object
	hasDefault := false
	for k, v := range kwargs {
		switch k {
		case "default":
			if v != NoDefault() {
				defaultVal = v
				hasDefault = true
			}
		default:
			return nil, fmt.Errorf("TypeError: TypeVarTuple() got an unexpected keyword argument '%s'", k)
		}
	}
	tvt := NewTypeVarTuple(nameObj.Value())
	if hasDefault {
		tvt.Default = defaultVal
		tvt.HasDefault = true
	}
	return tvt, nil
}

// TypeAliasObj is the runtime object a `type X = ...` statement
// produces. It binds the alias name to a lazy compute_value callable
// (a function whose body evaluates the alias's RHS on first access)
// plus the type-param tuple from PEP 695 syntax.
//
// CPython: Objects/typevarobject.c:2090 typealiasobject
type TypeAliasObj struct {
	Header
	NameStr       string
	TypeParamsObj Object
	ComputeValue  Object
	Cached        Object
}

// TypeAliasObjType is the type singleton.
//
// CPython: Objects/typevarobject.c PyTypeAliasType
var TypeAliasObjType = NewType("typing.TypeAliasType", []*Type{objectType})

// NewTypeAlias builds the runtime alias object.
//
// CPython: Objects/typevarobject.c:2181 _Py_make_typealias
func NewTypeAlias(name string, typeParams, compute Object) *TypeAliasObj {
	a := &TypeAliasObj{NameStr: name, TypeParamsObj: typeParams, ComputeValue: compute}
	a.Init(TypeAliasObjType)
	return a
}

func init() {
	TypeAliasObjType.Repr = func(o Object) (string, error) {
		return o.(*TypeAliasObj).NameStr, nil
	}
	TypeAliasObjType.Str = TypeAliasObjType.Repr
	SetTypeDescr(TypeAliasObjType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*TypeAliasObj).NameStr), nil
	}, nil))
	SetTypeDescr(TypeAliasObjType, "__type_params__", NewGetSetDescr("__type_params__", func(o Object) (Object, error) {
		a := o.(*TypeAliasObj)
		if a.TypeParamsObj == nil {
			return NewTuple(nil), nil
		}
		if a.TypeParamsObj == None() {
			return NewTuple(nil), nil
		}
		return a.TypeParamsObj, nil
	}, nil))
	SetTypeDescr(TypeAliasObjType, "__value__", NewGetSetDescr("__value__", func(o Object) (Object, error) {
		return typealiasValue(o.(*TypeAliasObj))
	}, nil))

	TypeAliasObjType.TpNew = typealiasTpNew
}

// typealiasValue computes and caches the resolved RHS of a `type X =
// ...` alias. The compute_value callable is invoked the first time
// __value__ is read, then the result is memoized.
//
// CPython: Objects/typevarobject.c:1959 typealias_value
func typealiasValue(a *TypeAliasObj) (Object, error) {
	if a.Cached != nil {
		return a.Cached, nil
	}
	if a.ComputeValue == nil {
		return None(), nil
	}
	v, err := Call(a.ComputeValue, nil, nil)
	if err != nil {
		return nil, err
	}
	a.Cached = v
	return v, nil
}

// typealiasTpNew is the TypeAliasType(name, value, *, type_params=None)
// constructor. Mirrors typealias_new_impl.
//
// CPython: Objects/typevarobject.c:2100 typealias_new_impl
func typealiasTpNew(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: TypeAliasType() takes 2 positional arguments (%d given)", len(args))
	}
	nameObj, ok := args[0].(*Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: TypeAliasType() argument 'name' must be str")
	}
	value := args[1]
	var typeParams Object
	for k, v := range kwargs {
		switch k {
		case "type_params":
			if v != None() {
				if _, ok := v.(*Tuple); !ok {
					return nil, fmt.Errorf("TypeError: type_params must be a tuple")
				}
				typeParams = v
			}
		default:
			return nil, fmt.Errorf("TypeError: TypeAliasType() got an unexpected keyword argument '%s'", k)
		}
	}
	a := NewTypeAlias(nameObj.Value(), typeParams, nil)
	a.Cached = value
	return a, nil
}

var noDefaultSingleton Object

// NoDefault returns the singleton placeholder a TypeVar exposes via
// __default__ when no default was supplied (typing.NoDefault in
// CPython 3.13+). Identity comparison is the contract.
//
// CPython: Objects/typevarobject.c typing_NoDefault
func NoDefault() Object {
	if noDefaultSingleton == nil {
		noDefaultSingleton = newSingleton("typing.NoDefault")
	}
	return noDefaultSingleton
}

func newSingleton(name string) Object {
	t := NewType(name, []*Type{objectType})
	o := &simpleObject{}
	o.Init(t)
	return o
}

type simpleObject struct{ Header }
