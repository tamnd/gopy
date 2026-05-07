package objects

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
	return o
}()

func init() {
	ellipsisType.Repr = func(_ Object) (string, error) { return "Ellipsis", nil }
	ellipsisType.Str = ellipsisType.Repr
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
