package objects

import "fmt"

// ellipsisObject is the singleton for `...`, the Ellipsis literal.
// It surfaces in slice expressions (`a[..., 0]`), in stub function
// bodies (`def f(): ...`), and as a sentinel in typing.
//
// CPython: Objects/object.c:1980 _Py_EllipsisObject
type ellipsisObject struct {
	Header
}

// ellipsisType is the type singleton for `ellipsis`.
//
// CPython: Objects/object.c:1972 PyEllipsis_Type
var ellipsisType = NewType("ellipsis", []*Type{objectType})

var ellipsisSingleton = func() *ellipsisObject {
	o := &ellipsisObject{}
	o.init(ellipsisType)
	// _Py_EllipsisObject is a statically allocated immortal object in
	// CPython, so sys.getrefcount reports it above the immortal minimum.
	//
	// CPython: Objects/object.c:1980 _Py_EllipsisObject
	o.MakeImmortal()
	return o
}()

func init() {
	ellipsisType.Repr = func(_ Object) (string, error) { return "Ellipsis", nil }
	ellipsisType.Str = ellipsisType.Repr
	ellipsisType.Getattro = GenericGetAttr
	ellipsisType.Setattro = GenericSetAttr
	// ellipsis defines no __eq__, so it inherits object's default
	// pointer-identity hash. Without this it would surface as
	// unhashable, breaking Tuple[int, ...] cache keys and any set/dict
	// keyed on the singleton.
	//
	// CPython: Objects/sliceobject.c PyEllipsis_Type (tp_hash inherited)
	ellipsisType.Hash = identityHash
	// ellipsis is not subclassable. CPython: Objects/sliceobject.c
	// PyEllipsis_Type does not set Py_TPFLAGS_BASETYPE.
	ellipsisType.TpFlags &^= TpFlagBasetype
	// EllipsisType() returns the singleton and rejects any argument.
	//
	// CPython: Objects/sliceobject.c:28 ellipsis_new
	ellipsisType.TpNew = func(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 0 || len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: EllipsisType takes no arguments")
		}
		return ellipsisSingleton, nil
	}
	// CPython: Objects/object.c ellipsis_reduce - returning a string causes
	// pickle to serialize as "builtins.Ellipsis" (GLOBAL opcode) so loads
	// returns the singleton rather than creating a new instance.
	SetTypeDescr(ellipsisType, "__reduce__", NewMethodDescrConv(ellipsisType, "__reduce__", MethNoArgs,
		func(_ []Object, _ map[string]Object) (Object, error) {
			return NewStr("Ellipsis"), nil
		}))
}

// Ellipsis returns the singleton Ellipsis value. Mirrors Py_Ellipsis.
//
// CPython: Include/object.h Py_Ellipsis
func Ellipsis() Object {
	return ellipsisSingleton
}

// IsEllipsis reports whether o is the Ellipsis singleton.
//
// CPython: Objects/object.c Py_IsEllipsis (3.14+)
func IsEllipsis(o Object) bool {
	return o == ellipsisSingleton
}

// EllipsisType returns the type singleton for ellipsis. Mirrors PyEllipsis_Type.
//
// CPython: Objects/object.c:1972 PyEllipsis_Type
func EllipsisType() *Type {
	return ellipsisType
}
