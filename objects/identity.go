// Identity tests: Py_Is, Py_IsNone, Py_IsTrue, Py_IsFalse. These
// compare object pointers, never values. The runtime relies on the
// singleton invariants: there is exactly one None, one True, one
// False, one NotImplemented, one Ellipsis.
//
// CPython: Include/object.h:257 Py_Is etc.

package objects

// IsTrue reports whether o is the True singleton (Python `o is True`).
//
// CPython: Include/object.h:262 Py_IsTrue
func IsTrue(o Object) bool {
	return o == trueSingleton
}

// IsFalse reports whether o is the False singleton (Python `o is False`).
//
// CPython: Include/object.h:267 Py_IsFalse
func IsFalse(o Object) bool {
	return o == falseSingleton
}

// IsBool reports whether o is True or False. Mirrors PyBool_Check.
//
// CPython: Include/cpython/boolobject.h:11 PyBool_Check
func IsBool(o Object) bool {
	return IsTrue(o) || IsFalse(o)
}
