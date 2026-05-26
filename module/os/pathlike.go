// PathLike: the abstract base class that types implementing the
// file-system path protocol register against. Real CPython ships
// PathLike as an abc.ABC subclass with a __subclasshook__ that
// recognizes any class defining __fspath__ as a virtual subclass.
// gopy's isinstance does not yet consult __subclasshook__, so this
// port is the Type singleton + the abstract __fspath__ method body;
// the subclasshook arms will land alongside the broader ABC port.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
package os

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// pathLikeRegistered holds virtual subclasses registered via
// os.PathLike.register(). The ABC machinery's __subclasshook__
// would normally provide this; until gopy carries full ABC
// isinstance integration, the set is populated by register() and
// consulted by pathLikeIsInstance.
//
// CPython: Lib/abc.py ABCMeta._abc_registry
var pathLikeRegistered []*objects.Type

// pathLikeType is the os.PathLike type singleton.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
var pathLikeType = func() *objects.Type {
	t := objects.NewType("PathLike", []*objects.Type{objects.ObjectType()})
	objects.SetTypeDescr(t, "__fspath__",
		objects.NewMethodDescr(t, "__fspath__", pathLikeFspath))
	// register() is a classmethod on ABCMeta; expose it as a classmethod
	// so pathlib can call os.PathLike.register(PurePath).
	//
	// CPython: Lib/abc.py:40 ABCMeta.register
	objects.SetTypeDescr(t, "register",
		objects.NewClassMethod(objects.NewBuiltinFunction("register", pathLikeRegister)))
	return t
}()

// pathLikeFspath is the @abstractmethod body. CPython raises
// NotImplementedError; we follow.
//
// CPython: Lib/os.py:1131 PathLike.__fspath__
func pathLikeFspath(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError")
}

// pathLikeRegister implements os.PathLike.register(cls). Records the
// class as a virtual subclass so isinstance(x, os.PathLike) returns
// True for instances of cls even without __fspath__. Returns cls so
// it can be used as a decorator.
//
// CPython: Lib/abc.py:40 ABCMeta.register
func pathLikeRegister(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	// The first arg is the class object (cls arg from classmethod).
	// When called as os.PathLike.register(X), args = [os.PathLike, X].
	// When called via the classmethod descriptor, args = [X].
	// Accept both shapes.
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: register() takes at least 1 argument (0 given)")
	}
	subclass := args[len(args)-1]
	if t, ok := subclass.(*objects.Type); ok {
		pathLikeRegistered = append(pathLikeRegistered, t)
	}
	return subclass, nil
}
