package objects

import "fmt"

// noneObject is the singleton for None.
//
// CPython: Objects/object.c:L1881 _Py_NoneStruct
type noneObject struct {
	Header
}

var noneType = NewType("NoneType", []*Type{objectType})

var noneSingleton = func() *noneObject {
	o := &noneObject{}
	o.init(noneType)
	o.MakeImmortal()
	return o
}()

func init() {
	noneType.Repr = func(_ Object) (string, error) { return "None", nil }
	noneType.Str = noneType.Repr
	noneType.Hash = func(_ Object) (int64, error) { return 0, nil }
	noneType.Getattro = GenericGetAttr
	noneType.Setattro = GenericSetAttr
	// NoneType is not subclassable. CPython: Objects/object.c PyNone_Type
	// does not set Py_TPFLAGS_BASETYPE.
	noneType.TpFlags &^= TpFlagBasetype
	// NoneType() returns the singleton and rejects any argument.
	//
	// CPython: Objects/object.c:2218 none_new
	noneType.TpNew = func(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 0 || len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: NoneType takes no arguments")
		}
		return noneSingleton, nil
	}
}

// None returns the singleton None value. Mirrors Py_None.
//
// CPython: Include/object.h:L820 Py_None
func None() Object {
	return noneSingleton
}

// NoneType returns the type singleton for None.
//
// CPython: Objects/object.c:L1893 _PyNone_Type
func NoneType() *Type {
	return noneType
}

// IsNone reports whether o is the None singleton. Mirrors Py_IsNone.
//
// CPython: Include/object.h:L827 Py_IsNone
func IsNone(o Object) bool {
	return o == noneSingleton
}
