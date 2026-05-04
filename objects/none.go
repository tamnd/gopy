package objects

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
	return o
}()

func init() {
	noneType.Repr = func(_ Object) (string, error) { return "None", nil }
	noneType.Str = noneType.Repr
	noneType.Hash = func(_ Object) (int64, error) { return 0, nil }
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
