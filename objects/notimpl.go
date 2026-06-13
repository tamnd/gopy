package objects

import "fmt"

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
	// _Py_NotImplementedStruct is a statically allocated immortal object in
	// CPython, so sys.getrefcount reports it above the immortal minimum.
	//
	// CPython: Objects/object.c:1953 _Py_NotImplementedStruct
	o.MakeImmortal()
	return o
}()

func init() {
	notImplementedType.Repr = func(_ Object) (string, error) { return "NotImplemented", nil }
	notImplementedType.Str = notImplementedType.Repr
	notImplementedType.Getattro = GenericGetAttr
	notImplementedType.Setattro = GenericSetAttr
	// NotImplemented must never be evaluated in a boolean context; the
	// nb_bool slot raises rather than returning a truth value.
	//
	// CPython: Objects/object.c:2364 notimplemented_bool
	notImplementedType.Number = &NumberMethods{
		Bool: func(_ Object) (bool, error) {
			return false, fmt.Errorf("TypeError: NotImplemented should not be used in a boolean context")
		},
	}
	// NotImplementedType() returns the singleton and rejects any argument.
	//
	// CPython: Objects/object.c:2344 notimplemented_new
	notImplementedType.TpNew = func(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) != 0 || len(kwargs) != 0 {
			return nil, fmt.Errorf("TypeError: NotImplementedType takes no arguments")
		}
		return notImplementedSingleton, nil
	}
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
