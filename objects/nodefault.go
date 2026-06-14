package objects

import "fmt"

// noDefaultObject is the singleton typing.NoDefault, a marker that a TypeVar /
// ParamSpec / TypeVarTuple parameter has no default. Identity is the contract:
// callers compare against NoDefault() rather than inspecting state.
//
// CPython: Objects/typevarobject.c:124 _Py_NoDefaultStruct
type noDefaultObject struct {
	Header
}

var noDefaultType = NewType("NoDefaultType", []*Type{objectType})

var noDefaultSingleton = func() *noDefaultObject {
	o := &noDefaultObject{}
	o.init(noDefaultType)
	// _Py_NoDefaultStruct is a statically allocated immortal object, so its
	// dealloc re-immortalizes rather than freeing.
	//
	// CPython: Objects/typevarobject.c:98 nodefault_dealloc
	o.MakeImmortal()
	return o
}()

func init() {
	// repr is the fixed string "typing.NoDefault".
	//
	// CPython: Objects/typevarobject.c:72 NoDefault_repr
	noDefaultType.Repr = func(_ Object) (string, error) { return "typing.NoDefault", nil }
	noDefaultType.Str = noDefaultType.Repr
	noDefaultType.Getattro = GenericGetAttr
	noDefaultType.Setattro = GenericSetAttr
	SetTypeDescr(noDefaultType, "__doc__", NewStr("NoDefaultType()\n--\n\nThe type of the NoDefault singleton."))
	// NoDefaultType is not subclassable: the static type does not set
	// Py_TPFLAGS_BASETYPE.
	//
	// CPython: Objects/typevarobject.c:113 _PyNoDefault_Type
	noDefaultType.TpFlags &^= TpFlagBasetype
	// NoDefaultType() returns the singleton and rejects any argument.
	//
	// CPython: Objects/typevarobject.c:88 nodefault_new
	noDefaultType.TpNew = func(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 0 || len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: NoDefaultType takes no arguments")
		}
		return noDefaultSingleton, nil
	}
	// __reduce__ returns the bare name "NoDefault"; pickle resolves it as a
	// global, finding typing.NoDefault and pickling it by reference.
	//
	// CPython: Objects/typevarobject.c:78 NoDefault_reduce
	SetTypeDescr(noDefaultType, "__reduce__", NewMethodDescrConv(noDefaultType, "__reduce__", MethNoArgs,
		func(_ []Object, _ map[string]Object) (Object, error) {
			return NewStr("NoDefault"), nil
		}))
}

// NoDefault returns the singleton placeholder a TypeVar exposes via __default__
// when no default was supplied (typing.NoDefault). Identity comparison is the
// contract.
//
// CPython: Objects/typevarobject.c:124 _Py_NoDefaultStruct
func NoDefault() Object {
	return noDefaultSingleton
}
