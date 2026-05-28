// PathLike: the abstract base class for os.fspath protocol.
// CPython's os module ships PathLike as an abc.ABC subclass whose
// __subclasshook__ recognizes any class with __fspath__. In gopy,
// the os module is a Go inittab module so stdlib/os.py never runs
// on top of it. We wire the equivalent behavior by creating a custom
// metaclass (_PathLikeMeta) with __instancecheck__ / __subclasscheck__
// that check for __fspath__, then assign it as the metaclass of
// pathLikeType.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
package os

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// pathLikeRegistered holds virtual subclasses registered via
// os.PathLike.register().
//
// CPython: Lib/abc.py ABCMeta._abc_registry
var pathLikeRegistered []*objects.Type

// pathLikeMetaType is a custom metaclass that wires __instancecheck__
// / __subclasscheck__ to check for __fspath__, matching the ABCMeta
// __subclasshook__ in CPython's os.PathLike.
//
// CPython: Lib/abc.py ABCMeta.__instancecheck__
var pathLikeMetaType *objects.Type

// pathLikeType is the os.PathLike type singleton.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
var pathLikeType = objects.NewType("PathLike", []*objects.Type{objects.ObjectType()})

func init() {
	// Build the metaclass for PathLike. Cannot use a var-level func()
	// initializer because that would create an init cycle with
	// pathLikeType (meta references type, type references meta).
	pathLikeMetaType = objects.NewType("_PathLikeMeta", []*objects.Type{objects.TypeType()})

	objects.SetTypeDescr(pathLikeMetaType, "__instancecheck__",
		objects.NewMethodDescr(pathLikeMetaType, "__instancecheck__", pathLikeInstanceCheck))
	objects.SetTypeDescr(pathLikeMetaType, "__subclasscheck__",
		objects.NewMethodDescr(pathLikeMetaType, "__subclasscheck__", pathLikeSubclassCheck))

	// Wire pathLikeType's metaclass to _PathLikeMeta.
	// Header.Init re-stamps the ob_type field without touching MRO or
	// other type internals; it is the same pattern used by the bootstrap
	// cycle for typeType itself.
	//
	// CPython: Objects/typeobject.c:443 bootstrap: typeType.ob_type = typeType
	pathLikeType.Init(pathLikeMetaType)

	objects.SetTypeDescr(pathLikeType, "__fspath__",
		objects.NewMethodDescr(pathLikeType, "__fspath__", pathLikeFspath))
	// register() mirrors ABCMeta.register so pathlib can call
	// os.PathLike.register(PurePath).
	//
	// CPython: Lib/abc.py:40 ABCMeta.register
	objects.SetTypeDescr(pathLikeType, "register",
		objects.NewClassMethod(objects.NewBuiltinFunction("register", pathLikeRegister)))
}

// pathLikeInstanceCheck implements _PathLikeMeta.__instancecheck__:
// isinstance(obj, os.PathLike) returns True if the instance has __fspath__.
//
// CPython: Lib/os.py:1136 PathLike.__subclasshook__
func pathLikeInstanceCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __instancecheck__() takes 2 arguments")
	}
	instance := args[1]
	if objects.IsSubtype(instance.Type(), pathLikeType) {
		return objects.True(), nil
	}
	for _, t := range pathLikeRegistered {
		if objects.IsSubtype(instance.Type(), t) {
			return objects.True(), nil
		}
	}
	if _, err := objects.GetAttr(instance, objects.NewStr("__fspath__")); err == nil {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// pathLikeSubclassCheck implements _PathLikeMeta.__subclasscheck__:
// issubclass(cls, os.PathLike) returns True if cls defines __fspath__.
//
// CPython: Lib/os.py:1136 PathLike.__subclasshook__
func pathLikeSubclassCheck(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __subclasscheck__() takes 2 arguments")
	}
	sub, ok := args[1].(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: issubclass() arg 1 must be a class")
	}
	if objects.IsSubtype(sub, pathLikeType) {
		return objects.True(), nil
	}
	for _, t := range pathLikeRegistered {
		if objects.IsSubtype(sub, t) {
			return objects.True(), nil
		}
	}
	if _, err := objects.LookupAttrString(sub, "__fspath__"); err == nil {
		return objects.True(), nil
	}
	return objects.False(), nil
}

// pathLikeFspath is the abstract method body. CPython raises
// NotImplementedError; we follow.
//
// CPython: Lib/os.py:1131 PathLike.__fspath__
func pathLikeFspath(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError")
}

// pathLikeRegister records cls as a virtual subclass.
//
// CPython: Lib/abc.py:40 ABCMeta.register
func pathLikeRegister(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: register() takes at least 1 argument (0 given)")
	}
	subclass := args[len(args)-1]
	if t, ok := subclass.(*objects.Type); ok {
		pathLikeRegistered = append(pathLikeRegistered, t)
	}
	return subclass, nil
}
