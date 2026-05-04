package objects

// notImplementedObject is the singleton for NotImplemented, returned
// by binary slots when they cannot handle the operand types. The
// abstract layer treats it as a signal to try the reflected slot.
//
// CPython: Objects/object.c:L1953 _Py_NotImplementedStruct
type notImplementedObject struct {
	Header
}

var notImplementedType = NewType("NotImplementedType", []*Type{objectType})

var notImplementedSingleton = func() *notImplementedObject {
	o := &notImplementedObject{}
	o.init(notImplementedType)
	return o
}()

func init() {
	notImplementedType.Repr = func(_ Object) (string, error) { return "NotImplemented", nil }
	notImplementedType.Str = notImplementedType.Repr
}

// NotImplemented returns the singleton NotImplemented value. Mirrors
// Py_NotImplemented.
//
// CPython: Include/object.h:L863 Py_NotImplemented
func NotImplemented() Object {
	return notImplementedSingleton
}

// IsNotImplemented reports whether o is the NotImplemented singleton.
//
// CPython: Objects/object.c:L1965 Py_IsNotImplemented
func IsNotImplemented(o Object) bool {
	return o == notImplementedSingleton
}

// notImplemented is an internal alias used by slot implementations.
func notImplemented() Object { return notImplementedSingleton }
