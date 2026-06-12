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
	"strings"
)

// SysModulesGetter is wired by the imp package after its init completes.
// It returns the live sys.modules dict so that objects code can perform
// lazy module lookups without importing imp (which would be circular).
var SysModulesGetter func() *Dict

// TypeVar mirrors CPython's typevarobject. The bound and constraints
// fields are nil unless the caller supplied them via the binary
// intrinsics (TYPEVAR_WITH_BOUND / TYPEVAR_WITH_CONSTRAINTS).
//
// CPython: Objects/typevarobject.c:30 typevarobject
type TypeVar struct {
	Header
	NameStr             string
	Module              string // __module__, set from the calling frame's module name
	Bound               Object
	EvaluateBound       Object
	Constraints         Object
	EvaluateConstraints Object
	Default             Object
	EvaluateDefault     Object
	HasDefault          bool
	Covariant           bool
	Contravariant       bool
	InferVariance       bool
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
	NameStr         string
	Module          string // __module__, set from the calling frame's module name
	Default         Object
	EvaluateDefault Object
	HasDefault      bool
	Covariant       bool
	Contravariant   bool
	InferVariance   bool
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
	NameStr         string
	Module          string // __module__, set from the calling frame's module name
	Default         Object
	EvaluateDefault Object
	HasDefault      bool
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

// ConstEvaluator wraps either a constant value (constructor path) or a
// no-arg thunk (PEP 695 path) and exposes the evaluate-function protocol:
// callable with a format integer from annotationlib.Format.
//
// CPython: Objects/typevarobject.c constevaluatorobject
type ConstEvaluator struct {
	Header
	Value Object // set for constructor path (TypeVar("T", bound=int))
	Thunk Object // set for PEP 695 path (no-arg closure)
}

// ConstEvaluatorType is the type singleton for _typing._ConstEvaluator.
//
// CPython: Objects/typevarobject.c constevaluator_spec
var ConstEvaluatorType = NewType("_typing._ConstEvaluator", []*Type{objectType})

// NewConstEvaluator wraps a constant value for the constructor path.
//
// CPython: Objects/typevarobject.c constevaluator_alloc
func NewConstEvaluator(value Object) *ConstEvaluator {
	ce := &ConstEvaluator{Value: value}
	ce.Init(ConstEvaluatorType)
	return ce
}

// NewThunkEvaluator wraps a no-arg thunk for the PEP 695 path.
//
// CPython: Objects/typevarobject.c typevar_evaluate_bound (returns thunk directly)
func NewThunkEvaluator(thunk Object) *ConstEvaluator {
	ce := &ConstEvaluator{Thunk: thunk}
	ce.Init(ConstEvaluatorType)
	return ce
}

func init() {
	// The type itself is immutable: ConstEvaluator.attribute = 1 raises TypeError.
	// CPython: Objects/typevarobject.c constevaluator_spec Py_TPFLAGS_IMMUTABLETYPE
	ConstEvaluatorType.TpFlags |= TpFlagImmutable

	// Instantiation from Python is forbidden.
	// CPython: Objects/typevarobject.c Py_TPFLAGS_DISALLOW_INSTANTIATION
	ConstEvaluatorType.TpNew = func(_ *Type, _ []Object, _ map[string]Object) (Object, error) {
		return nil, fmt.Errorf("TypeError: cannot create '_typing._ConstEvaluator' instances")
	}

	// CPython: Objects/typevarobject.c constevaluator_repr uses %R (repr)
	ConstEvaluatorType.Repr = func(o Object) (string, error) {
		ce := o.(*ConstEvaluator)
		var val Object
		if ce.Value != nil {
			val = ce.Value
		} else {
			v, err := Call(ce.Thunk, nil, nil)
			if err != nil {
				return "", err
			}
			val = v
		}
		r, err := Repr(val)
		if err != nil {
			return "", err
		}
		return "<constevaluator " + r + ">", nil
	}

	// CPython: Objects/typevarobject.c constevaluator_call
	ConstEvaluatorType.Call = func(self Object, args []Object, _ map[string]Object) (Object, error) {
		ce := self.(*ConstEvaluator)
		if len(args) != 1 {
			return nil, fmt.Errorf("TypeError: constevaluator.__call__ takes exactly 1 argument (%d given)", len(args))
		}
		format, err := indexAsInt(args[0])
		if err != nil {
			return nil, fmt.Errorf("TypeError: constevaluator.__call__ format must be int")
		}
		const formatSTRING = 4
		if ce.Value != nil {
			if format == formatSTRING {
				return constEvaluatorString(ce.Value)
			}
			return ce.Value, nil
		}
		// thunk path
		val, err2 := Call(ce.Thunk, nil, nil)
		if err2 != nil {
			return nil, err2
		}
		if format == formatSTRING {
			return constEvaluatorString(val)
		}
		return val, nil
	}
}

// constEvaluatorString produces the STRING-format output for a value:
// tuples are formatted as "(T1, T2, ...)"; other values use typingTypeRepr.
//
// CPython: Objects/typevarobject.c:162 constevaluator_call (STRING branch)
func constEvaluatorString(value Object) (Object, error) {
	if t, ok := value.(*Tuple); ok {
		var b strings.Builder
		b.WriteByte('(')
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			r, err := typingTypeRepr(t.Item(i))
			if err != nil {
				return nil, err
			}
			b.WriteString(r)
		}
		b.WriteByte(')')
		return NewStr(b.String()), nil
	}
	r, err := typingTypeRepr(value)
	if err != nil {
		return nil, err
	}
	return NewStr(r), nil
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
	// CPython: Objects/typevarobject.c:687 typevar_tp_hash (id-based)
	TypeVarType.Hash = identityHash
	ParamSpecType.Repr = func(o Object) (string, error) {
		return "~" + o.(*ParamSpec).NameStr, nil
	}
	ParamSpecType.Str = ParamSpecType.Repr
	// CPython: Objects/typevarobject.c:1247 paramspec_tp_hash (id-based)
	ParamSpecType.Hash = identityHash
	TypeVarTupleType.Repr = func(o Object) (string, error) {
		return "~" + o.(*TypeVarTuple).NameStr, nil
	}
	TypeVarTupleType.Str = TypeVarTupleType.Repr
	// CPython: Objects/typevarobject.c:1567 typevartuple_tp_hash (id-based)
	TypeVarTupleType.Hash = identityHash

	SetTypeDescr(TypeVarType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*TypeVar).NameStr), nil
	}, nil))
	// CPython: Objects/typevarobject.c:663 typevar_new_impl (sets __module__ from caller frame)
	SetTypeDescr(TypeVarType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			tv := o.(*TypeVar)
			if tv.Module == "" {
				return None(), nil
			}
			return NewStr(tv.Module), nil
		},
		func(o Object, v Object) error {
			if s, ok := v.(*Unicode); ok {
				o.(*TypeVar).Module = s.Value()
			} else if v == None() {
				o.(*TypeVar).Module = ""
			}
			return nil
		},
	))
	// CPython: Objects/typevarobject.c:829 typevar_reduce_impl
	SetTypeDescr(TypeVarType, "__reduce__", NewMethodDescrConv(TypeVarType, "__reduce__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() missing self")
		}
		tv, ok := args[0].(*TypeVar)
		if !ok {
			return nil, fmt.Errorf("TypeError: __reduce__() requires TypeVar")
		}
		return NewStr(tv.NameStr), nil
	}))
	SetTypeDescr(TypeVarType, "__bound__", NewGetSetDescr("__bound__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.Bound != nil {
			return tv.Bound, nil
		}
		if tv.EvaluateBound != nil {
			v, err := Call(tv.EvaluateBound, nil, nil)
			if err != nil {
				return nil, err
			}
			tv.Bound = v
			return v, nil
		}
		return None(), nil
	}, nil))
	SetTypeDescr(TypeVarType, "__constraints__", NewGetSetDescr("__constraints__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.Constraints != nil {
			return tv.Constraints, nil
		}
		if tv.EvaluateConstraints != nil {
			v, err := Call(tv.EvaluateConstraints, nil, nil)
			if err != nil {
				return nil, err
			}
			// constraints thunk returns a tuple; wrap in outer tuple like eager path.
			// CPython: typevar_constraints calls PyObject_CallNoArgs and wraps if needed.
			if t, ok := v.(*Tuple); ok {
				tv.Constraints = t
			} else {
				tv.Constraints = NewTuple([]Object{v})
			}
			return tv.Constraints, nil
		}
		return NewTuple(nil), nil
	}, nil))
	SetTypeDescr(TypeVarType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if !tv.HasDefault {
			return NoDefault(), nil
		}
		if tv.Default == nil && tv.EvaluateDefault != nil {
			v, err := Call(tv.EvaluateDefault, nil, nil)
			if err != nil {
				return nil, err
			}
			tv.Default = v
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
	SetTypeDescr(TypeVarType, "__covariant__", NewGetSetDescr("__covariant__", func(o Object) (Object, error) {
		return NewBool(o.(*TypeVar).Covariant), nil
	}, nil))
	SetTypeDescr(TypeVarType, "__contravariant__", NewGetSetDescr("__contravariant__", func(o Object) (Object, error) {
		return NewBool(o.(*TypeVar).Contravariant), nil
	}, nil))
	// CPython: Objects/typevarobject.c:624 typevar_infer_variance
	SetTypeDescr(TypeVarType, "__infer_variance__", NewGetSetDescr("__infer_variance__", func(o Object) (Object, error) {
		return NewBool(o.(*TypeVar).InferVariance), nil
	}, nil))

	// CPython: Objects/typevarobject.c:586 typevar_evaluate_bound
	SetTypeDescr(TypeVarType, "evaluate_bound", NewGetSetDescr("evaluate_bound", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.EvaluateBound != nil {
			return NewThunkEvaluator(tv.EvaluateBound), nil
		}
		if tv.Bound != nil {
			return NewConstEvaluator(tv.Bound), nil
		}
		return None(), nil
	}, nil))
	// CPython: Objects/typevarobject.c:599 typevar_evaluate_constraints
	SetTypeDescr(TypeVarType, "evaluate_constraints", NewGetSetDescr("evaluate_constraints", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.EvaluateConstraints != nil {
			return NewThunkEvaluator(tv.EvaluateConstraints), nil
		}
		if tv.Constraints != nil {
			return NewConstEvaluator(tv.Constraints), nil
		}
		return None(), nil
	}, nil))
	// CPython: Objects/typevarobject.c:612 typevar_evaluate_default
	SetTypeDescr(TypeVarType, "evaluate_default", NewGetSetDescr("evaluate_default", func(o Object) (Object, error) {
		tv := o.(*TypeVar)
		if tv.EvaluateDefault != nil {
			return NewThunkEvaluator(tv.EvaluateDefault), nil
		}
		if tv.HasDefault && tv.Default != nil {
			return NewConstEvaluator(tv.Default), nil
		}
		return None(), nil
	}, nil))

	// CPython: Objects/typevarobject.c:755 typevar_typing_subst_impl
	SetTypeDescr(TypeVarType, "__typing_subst__", NewMethodDescr(TypeVarType, "__typing_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: __typing_subst__() missing argument")
		}
		return args[1], nil
	}))
	// CPython: Objects/typevarobject.c:780 typevar_typing_prepare_subst_impl
	SetTypeDescr(TypeVarType, "__typing_prepare_subst__", NewMethodDescr(TypeVarType, "__typing_prepare_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 3 {
			return nil, fmt.Errorf("TypeError: __typing_prepare_subst__() missing arguments")
		}
		// alias is args[1], subst_args is args[2]; just return the args tuple unchanged
		return args[2], nil
	}))

	SetTypeDescr(ParamSpecType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*ParamSpec).NameStr), nil
	}, nil))
	// CPython: Objects/typevarobject.c:1300 paramspec_new_impl (sets __module__ from caller frame)
	SetTypeDescr(ParamSpecType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			ps := o.(*ParamSpec)
			if ps.Module == "" {
				return None(), nil
			}
			return NewStr(ps.Module), nil
		},
		func(o Object, v Object) error {
			if s, ok := v.(*Unicode); ok {
				o.(*ParamSpec).Module = s.Value()
			} else if v == None() {
				o.(*ParamSpec).Module = ""
			}
			return nil
		},
	))
	// CPython: Objects/typevarobject.c:1394 paramspec_reduce
	SetTypeDescr(ParamSpecType, "__reduce__", NewMethodDescrConv(ParamSpecType, "__reduce__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() missing self")
		}
		ps, ok := args[0].(*ParamSpec)
		if !ok {
			return nil, fmt.Errorf("TypeError: __reduce__() requires ParamSpec")
		}
		return NewStr(ps.NameStr), nil
	}))
	SetTypeDescr(ParamSpecType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		ps := o.(*ParamSpec)
		if !ps.HasDefault {
			return NoDefault(), nil
		}
		if ps.Default == nil && ps.EvaluateDefault != nil {
			v, err := Call(ps.EvaluateDefault, nil, nil)
			if err != nil {
				return nil, err
			}
			ps.Default = v
		}
		return ps.Default, nil
	}, nil))
	// CPython: Objects/typevarobject.c:1262 paramspec_evaluate_default
	SetTypeDescr(ParamSpecType, "evaluate_default", NewGetSetDescr("evaluate_default", func(o Object) (Object, error) {
		ps := o.(*ParamSpec)
		if ps.EvaluateDefault != nil {
			return NewThunkEvaluator(ps.EvaluateDefault), nil
		}
		if ps.HasDefault && ps.Default != nil {
			return NewConstEvaluator(ps.Default), nil
		}
		return None(), nil
	}, nil))
	// CPython: Objects/typevarobject.c:1258 paramspec_has_default_impl
	SetTypeDescr(ParamSpecType, "has_default", NewMethodDescr(ParamSpecType, "has_default", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: has_default() missing self")
		}
		ps, ok := args[0].(*ParamSpec)
		if !ok {
			return nil, fmt.Errorf("TypeError: has_default() requires ParamSpec")
		}
		return NewBool(ps.HasDefault), nil
	}))
	SetTypeDescr(ParamSpecType, "__covariant__", NewGetSetDescr("__covariant__", func(o Object) (Object, error) {
		return NewBool(o.(*ParamSpec).Covariant), nil
	}, nil))
	SetTypeDescr(ParamSpecType, "__contravariant__", NewGetSetDescr("__contravariant__", func(o Object) (Object, error) {
		return NewBool(o.(*ParamSpec).Contravariant), nil
	}, nil))
	// CPython: Objects/typevarobject.c paramspec_getset __infer_variance__
	SetTypeDescr(ParamSpecType, "__infer_variance__", NewGetSetDescr("__infer_variance__", func(o Object) (Object, error) {
		return NewBool(o.(*ParamSpec).InferVariance), nil
	}, nil))

	// CPython: Objects/typevarobject.c:1365 paramspec_typing_subst_impl
	SetTypeDescr(ParamSpecType, "__typing_subst__", NewMethodDescr(ParamSpecType, "__typing_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: __typing_subst__() missing argument")
		}
		return args[1], nil
	}))
	// CPython: Objects/typevarobject.c:1385 paramspec_typing_prepare_subst_impl
	SetTypeDescr(ParamSpecType, "__typing_prepare_subst__", NewMethodDescr(ParamSpecType, "__typing_prepare_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 3 {
			return nil, fmt.Errorf("TypeError: __typing_prepare_subst__() missing arguments")
		}
		return args[2], nil
	}))

	SetTypeDescr(TypeVarTupleType, "__name__", NewGetSetDescr("__name__", func(o Object) (Object, error) {
		return NewStr(o.(*TypeVarTuple).NameStr), nil
	}, nil))
	// CPython: Objects/typevarobject.c:1577 typevartuple_impl (sets __module__ from caller frame)
	SetTypeDescr(TypeVarTupleType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			tvt := o.(*TypeVarTuple)
			if tvt.Module == "" {
				return None(), nil
			}
			return NewStr(tvt.Module), nil
		},
		func(o Object, v Object) error {
			if s, ok := v.(*Unicode); ok {
				o.(*TypeVarTuple).Module = s.Value()
			} else if v == None() {
				o.(*TypeVarTuple).Module = ""
			}
			return nil
		},
	))
	// CPython: Objects/typevarobject.c:1647 typevartuple_reduce
	SetTypeDescr(TypeVarTupleType, "__reduce__", NewMethodDescrConv(TypeVarTupleType, "__reduce__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() missing self")
		}
		tvt, ok := args[0].(*TypeVarTuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: __reduce__() requires TypeVarTuple")
		}
		return NewStr(tvt.NameStr), nil
	}))
	SetTypeDescr(TypeVarTupleType, "__default__", NewGetSetDescr("__default__", func(o Object) (Object, error) {
		tvt := o.(*TypeVarTuple)
		if !tvt.HasDefault {
			return NoDefault(), nil
		}
		if tvt.Default == nil && tvt.EvaluateDefault != nil {
			v, err := Call(tvt.EvaluateDefault, nil, nil)
			if err != nil {
				return nil, err
			}
			tvt.Default = v
		}
		return tvt.Default, nil
	}, nil))
	// CPython: Objects/typevarobject.c typevartuple_evaluate_default
	SetTypeDescr(TypeVarTupleType, "evaluate_default", NewGetSetDescr("evaluate_default", func(o Object) (Object, error) {
		tvt := o.(*TypeVarTuple)
		if tvt.EvaluateDefault != nil {
			return NewThunkEvaluator(tvt.EvaluateDefault), nil
		}
		if tvt.HasDefault && tvt.Default != nil {
			return NewConstEvaluator(tvt.Default), nil
		}
		return None(), nil
	}, nil))
	// CPython: Objects/typevarobject.c:1615 typevartuple_has_default_impl
	SetTypeDescr(TypeVarTupleType, "has_default", NewMethodDescr(TypeVarTupleType, "has_default", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: has_default() missing self")
		}
		tvt, ok := args[0].(*TypeVarTuple)
		if !ok {
			return nil, fmt.Errorf("TypeError: has_default() requires TypeVarTuple")
		}
		return NewBool(tvt.HasDefault), nil
	}))
	// CPython: Objects/typevarobject.c:1619 typevartuple_typing_subst_impl
	SetTypeDescr(TypeVarTupleType, "__typing_subst__", NewMethodDescr(TypeVarTupleType, "__typing_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 2 {
			return nil, fmt.Errorf("TypeError: __typing_subst__() missing argument")
		}
		return args[1], nil
	}))
	// CPython: Objects/typevarobject.c:1640 typevartuple_typing_prepare_subst_impl
	// Delegates to typing._typevartuple_prepare_subst(self, alias, args) which
	// groups variadic positional args into a single replacement tuple for the
	// TypeVarTuple slot. Returning args unchanged caused "Too many arguments"
	// because the expected_len check fires before the variadic grouping.
	SetTypeDescr(TypeVarTupleType, "__typing_prepare_subst__", NewMethodDescr(TypeVarTupleType, "__typing_prepare_subst__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 3 {
			return nil, fmt.Errorf("TypeError: __typing_prepare_subst__() missing arguments")
		}
		self, alias, subArgs := args[0], args[1], args[2]
		if SysModulesGetter != nil {
			sysmod := SysModulesGetter()
			if sysmod != nil {
				if typingMod, err := sysmod.GetItem(NewStr("typing")); err == nil && typingMod != nil {
					if fn, err2 := GetAttr(typingMod, NewStr("_typevartuple_prepare_subst")); err2 == nil && fn != nil {
						result, err3 := Call(fn, NewTuple([]Object{self, alias, subArgs}), nil)
						if err3 == nil {
							return result, nil
						}
						return nil, err3
					}
				}
			}
		}
		return subArgs, nil
	}))

	// TypeVarTuple.__iter__: yields Unpack[self] so that *Ts in a subscript
	// expands to Unpack[Ts]. Mirrors CPython's unpack_iter which calls
	// typing.Unpack[self] and returns an iterator over a one-tuple.
	//
	// CPython: Objects/typevarobject.c:1535 unpack_iter
	SetTypeDescr(TypeVarTupleType, "__iter__", NewMethodDescr(TypeVarTupleType, "__iter__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __iter__() missing self")
		}
		self := args[0]
		if SysModulesGetter != nil {
			sysmod := SysModulesGetter()
			if sysmod != nil {
				if typingMod, err := sysmod.GetItem(NewStr("typing")); err == nil && typingMod != nil {
					if unpack, err2 := GetAttr(typingMod, NewStr("Unpack")); err2 == nil && unpack != nil {
						// CPython: Objects/typevarobject.c:1535 unpack_iter
						if unpacked, err3 := GetItem(unpack, self); err3 == nil {
							return listIter(NewList([]Object{unpacked}))
						}
					}
				}
			}
		}
		// Fallback: yield self so *Ts at least expands to something.
		return listIter(NewList([]Object{self}))
	}))

	// Install Generic.__init_subclass__ so subclasses (class Foo(Generic[T]))
	// get their __parameters__ populated from __orig_bases__.
	// This is the Go port of _generic_init_subclass in typing.py.
	//
	// CPython: Lib/typing.py:1174 _generic_init_subclass
	SetTypeDescr(GenericType, "__init_subclass__", NewClassMethod(
		NewBuiltinFunction("__init_subclass__", genericInitSubclass),
	))
}

// genericInitSubclass implements Generic.__init_subclass__(cls). It
// collects type parameters from cls.__orig_bases__ and stores them as
// cls.__parameters__, mirroring typing._generic_init_subclass.
//
// CPython: Lib/typing.py:1174 _generic_init_subclass
func genericInitSubclass(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 1 {
		return None(), nil
	}
	cls, ok := args[0].(*Type)
	if !ok {
		return None(), nil
	}
	origBases, _ := GetAttr(cls, NewStr("__orig_bases__"))
	if origBases == nil {
		cls.TypingParameters = NewTuple(nil)
		return None(), nil
	}
	ob, ok := origBases.(*Tuple)
	if !ok {
		cls.TypingParameters = NewTuple(nil)
		return None(), nil
	}
	// Collect all type parameters from __orig_bases__.
	allParams := makeParameters(ob)
	// Find the explicit Generic[T,...] base (if any) and enforce single-use.
	// Handles both Go *GenericAlias and Python _GenericAlias instances whose
	// __origin__ is typing.Generic.
	//
	// CPython: Lib/typing.py:1192 gvars loop
	var gvars *Tuple
	for i := 0; i < ob.Len(); i++ {
		base := ob.Item(i)
		if ga, ok2 := base.(*GenericAlias); ok2 {
			if ga.origin != Object(GenericType) {
				continue
			}
			if gvars != nil {
				return nil, fmt.Errorf("TypeError: Cannot inherit from Generic[...] multiple times.")
			}
			if ga.parameters == nil {
				ga.parameters = makeParameters(ga.args)
			}
			gvars = ga.parameters
			continue
		}
		// Python-level _GenericAlias: check __origin__ is Generic.
		origin, err := GetAttr(base, NewStr("__origin__"))
		if err != nil || origin != Object(GenericType) {
			continue
		}
		if gvars != nil {
			return nil, fmt.Errorf("TypeError: Cannot inherit from Generic[...] multiple times.")
		}
		if p, err2 := GetAttr(base, NewStr("__parameters__")); err2 == nil {
			if tup, ok2 := p.(*Tuple); ok2 {
				gvars = tup
			}
		}
	}
	if gvars != nil {
		// Validate: every param in allParams must appear in gvars.
		// CPython: Lib/typing.py:1200 subset check
		gvarSet := map[Object]bool{}
		for i := 0; i < gvars.Len(); i++ {
			gvarSet[gvars.Item(i)] = true
		}
		var extra []string
		for i := 0; i < allParams.Len(); i++ {
			p := allParams.Item(i)
			if !gvarSet[p] {
				extra = append(extra, fmt.Sprintf("%v", p))
			}
		}
		if len(extra) > 0 {
			s := fmt.Sprintf("%v", gvars)
			return nil, fmt.Errorf("TypeError: Some type variables (%s) are not listed in Generic[%s]",
				joinStrings(extra, ", "), s)
		}
		cls.TypingParameters = gvars
	} else {
		cls.TypingParameters = allParams
	}
	return None(), nil
}

func joinStrings(ss []string, sep string) string {
	return strings.Join(ss, sep)
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
	tv.Covariant = covariant
	tv.Contravariant = contravariant
	tv.InferVariance = inferVariance
	tv.Module = typevarCallerModule()
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
	ps.Covariant = covariant
	ps.Contravariant = contravariant
	ps.InferVariance = inferVariance
	ps.Module = typevarCallerModule()
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
	tvt.Module = typevarCallerModule()
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
	Module        Object // __module__: str or None
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
	// TypeAliasType does not allow subclassing, matching CPython's
	// _PyTypeAlias_Type which carries no Py_TPFLAGS_BASETYPE.
	//
	// CPython: Objects/typevarobject.c:2159 _PyTypeAlias_Type (tp_flags)
	TypeAliasObjType.TpFlags &^= TpFlagBasetype

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
	// CPython: Objects/typevarobject.c typealias_members __module__
	SetTypeDescr(TypeAliasObjType, "__module__", NewGetSetDescr("__module__",
		func(o Object) (Object, error) {
			a := o.(*TypeAliasObj)
			if a.Module == nil {
				return None(), nil
			}
			return a.Module, nil
		},
		func(o Object, v Object) error {
			o.(*TypeAliasObj).Module = v
			return nil
		},
	))

	// CPython: Objects/typevarobject.c:1900 typealias_evaluate_value
	SetTypeDescr(TypeAliasObjType, "evaluate_value", NewGetSetDescr("evaluate_value", func(o Object) (Object, error) {
		a := o.(*TypeAliasObj)
		if a.ComputeValue != nil {
			return NewThunkEvaluator(a.ComputeValue), nil
		}
		if a.Cached != nil {
			return NewConstEvaluator(a.Cached), nil
		}
		return NewConstEvaluator(None()), nil
	}, nil))

	// TypeAliasType.__iter__: yields Unpack[self] so that (*Alias,) works.
	// Unpack is fetched lazily from sys.modules['typing'] via SysModulesGetter.
	//
	// CPython: Objects/typevarobject.c typealias_iter
	SetTypeDescr(TypeAliasObjType, "__iter__", NewMethodDescr(TypeAliasObjType, "__iter__", func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __iter__() missing self")
		}
		self := args[0]
		if SysModulesGetter != nil {
			sysmod := SysModulesGetter()
			if sysmod != nil {
				if typingMod, err := sysmod.GetItem(NewStr("typing")); err == nil && typingMod != nil {
					if unpack, err2 := GetAttr(typingMod, NewStr("Unpack")); err2 == nil && unpack != nil {
						// GetItem only routes through Go Mapping/Sequence slots.
						// CPython: Objects/typevarobject.c:2038 typealias_iter
						if unpacked, err3 := GetItem(unpack, self); err3 == nil {
							return listIter(NewList([]Object{unpacked}))
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("TypeError: 'typing.TypeAliasType' object is not iterable")
	}))

	// CPython: Objects/typevarobject.c:2059 typealias_parameters
	// _Py_make_parameters wraps every TypeVarTuple entry in Unpack[tvt]
	// so that __parameters__ contains Unpack[Ts] rather than Ts.
	//
	// CPython: Objects/typevarobject.c:2059 typealias_parameters -> _Py_make_parameters
	SetTypeDescr(TypeAliasObjType, "__parameters__", NewGetSetDescr("__parameters__", func(o Object) (Object, error) {
		a := o.(*TypeAliasObj)
		if a.TypeParamsObj == nil || a.TypeParamsObj == None() {
			return NewTuple(nil), nil
		}
		tp, ok := a.TypeParamsObj.(*Tuple)
		if !ok {
			return a.TypeParamsObj, nil
		}
		// Wrap TypeVarTuple entries in Unpack[...]. Get the Unpack _SpecialForm
		// from typing; if unavailable, return params as-is.
		var unpackObj Object
		if SysModulesGetter != nil {
			if sysmod := SysModulesGetter(); sysmod != nil {
				if typingMod, err := sysmod.GetItem(NewStr("typing")); err == nil && typingMod != nil {
					if u, err2 := GetAttr(typingMod, NewStr("Unpack")); err2 == nil {
						unpackObj = u
					}
				}
			}
		}
		out := make([]Object, tp.Len())
		for i := 0; i < tp.Len(); i++ {
			param := tp.Item(i)
			if _, isTvt := param.(*TypeVarTuple); isTvt && unpackObj != nil {
				if wrapped, err := GetItem(unpackObj, param); err == nil {
					out[i] = wrapped
					continue
				}
			}
			out[i] = param
		}
		return NewTuple(out), nil
	}, nil))

	// CPython: Objects/typevarobject.c:2071 typealias_subscript
	TypeAliasObjType.Mapping = &MappingMethods{
		GetItem: func(self, item Object) (Object, error) {
			a := self.(*TypeAliasObj)
			hasParams := a.TypeParamsObj != nil && a.TypeParamsObj != None()
			if hasParams {
				if t, ok := a.TypeParamsObj.(*Tuple); ok && t.Len() == 0 {
					hasParams = false
				}
			}
			if !hasParams {
				return nil, fmt.Errorf("TypeError: Only generic type aliases are subscriptable")
			}
			return NewGenericAlias(self, item), nil
		},
	}

	// CPython: Objects/typevarobject.c:2080 typealias_or
	TypeAliasObjType.Number = &NumberMethods{Or: unionTypeOr}

	// CPython: Objects/typevarobject.c:2059 typealias_reduce_impl
	SetTypeDescr(TypeAliasObjType, "__reduce__", NewMethodDescrConv(TypeAliasObjType, "__reduce__", MethNoArgs, func(args []Object, _ map[string]Object) (Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("TypeError: __reduce__() missing self")
		}
		a, ok := args[0].(*TypeAliasObj)
		if !ok {
			return nil, fmt.Errorf("TypeError: __reduce__() requires TypeAliasType")
		}
		return NewStr(a.NameStr), nil
	}))

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
	// Accept name and value as positional or keyword arguments.
	// CPython: Objects/typevarobject.c:2090 typealias_new (clinic generated)
	var nameObj *Unicode
	var value Object
	switch len(args) {
	case 0:
		if n, ok := kwargs["name"]; ok {
			if u, ok2 := n.(*Unicode); ok2 {
				nameObj = u
			} else {
				return nil, fmt.Errorf("TypeError: TypeAliasType() argument 'name' must be str")
			}
			delete(kwargs, "name")
		}
		if v, ok := kwargs["value"]; ok {
			value = v
			delete(kwargs, "value")
		}
		if nameObj == nil || value == nil {
			if nameObj == nil && value == nil {
				return nil, fmt.Errorf("TypeError: TypeAliasType() missing required arguments: 'name' and 'value'")
			} else if nameObj == nil {
				return nil, fmt.Errorf("TypeError: TypeAliasType() missing required argument: 'name'")
			}
			return nil, fmt.Errorf("TypeError: TypeAliasType() missing required argument: 'value'")
		}
	case 1:
		u, ok := args[0].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: TypeAliasType() argument 'name' must be str")
		}
		nameObj = u
		if v, ok := kwargs["value"]; ok {
			value = v
			delete(kwargs, "value")
		} else {
			return nil, fmt.Errorf("TypeError: TypeAliasType() missing required argument: 'value'")
		}
	case 2:
		u, ok := args[0].(*Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: TypeAliasType() argument 'name' must be str")
		}
		nameObj = u
		value = args[1]
	default:
		return nil, fmt.Errorf("TypeError: TypeAliasType() takes 2 positional arguments (%d given)", len(args))
	}
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
	if typeParams != nil {
		if err := typealiasCheckTypeParams(typeParams.(*Tuple)); err != nil {
			return nil, err
		}
	}
	a := NewTypeAlias(nameObj.Value(), typeParams, nil)
	a.Cached = value
	a.Module = typealiasModule()
	return a, nil
}

// typealiasCheckTypeParams validates that no non-default type parameter
// follows a default one. Mirrors CPython's typealias_check_type_params.
//
// CPython: Objects/typevarobject.c:1958 typealias_check_type_params
func typealiasCheckTypeParams(tp *Tuple) error {
	defaultSeen := false
	for i := 0; i < tp.Len(); i++ {
		p := tp.Item(i)
		var hasDefault bool
		switch v := p.(type) {
		case *TypeVar:
			hasDefault = v.HasDefault
		case *ParamSpec:
			hasDefault = v.HasDefault
		case *TypeVarTuple:
			hasDefault = v.HasDefault
		default:
			return fmt.Errorf("TypeError: Expected a type param, got %s", typeNameOf(p))
		}
		if !hasDefault {
			if defaultSeen {
				repr, _ := Repr(p)
				return fmt.Errorf("TypeError: non-default type parameter '%s' follows default type parameter", repr)
			}
		} else {
			defaultSeen = true
		}
	}
	return nil
}

// TypealiasModule reads __name__ from the currently executing frame's globals.
// Used by both typealiasTpNew and the MAKE_TYPEALIAS intrinsic.
//
// CPython: Objects/typevarobject.c:2152 typealias_new_impl (ta_module = ...)
func TypealiasModule() Object {
	return typealiasModule()
}

func typealiasModule() Object {
	if CurrentFrameHook == nil {
		return None()
	}
	f := CurrentFrameHook()
	if f == nil {
		return None()
	}
	g := f.FrameGlobals()
	if g == nil {
		return None()
	}
	d, ok := g.(*Dict)
	if !ok {
		return None()
	}
	v, err := d.GetItem(NewStr("__name__"))
	if err != nil || v == nil {
		return None()
	}
	return v
}

// CallerModuleName returns the module name from the currently executing
// frame's globals dict (__name__ key), mirroring CPython's caller()
// helper used in typevar_new_impl / paramspec_new_impl / typevartuple_impl.
// Returns "" when no frame is live or globals has no __name__.
//
// CPython: Objects/typevarobject.c:387 caller
func CallerModuleName() string {
	return typevarCallerModule()
}

// typevarCallerModule is the unexported implementation.
func typevarCallerModule() string {
	if CurrentFrameHook == nil {
		return ""
	}
	f := CurrentFrameHook()
	if f == nil {
		return ""
	}
	g := f.FrameGlobals()
	if g == nil {
		return ""
	}
	d, ok := g.(*Dict)
	if !ok {
		return ""
	}
	v, err := d.GetItem(NewStr("__name__"))
	if err != nil || v == nil {
		return ""
	}
	if s, ok := v.(*Unicode); ok {
		return s.Value()
	}
	return ""
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
