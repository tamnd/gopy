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

// pathLikeType is the os.PathLike type singleton.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
var pathLikeType = func() *objects.Type {
	t := objects.NewType("PathLike", []*objects.Type{objects.ObjectType()})
	objects.SetTypeDescr(t, "__fspath__",
		objects.NewMethodDescr(t, "__fspath__", pathLikeFspath))
	return t
}()

// pathLikeFspath is the @abstractmethod body. CPython raises
// NotImplementedError; we follow.
//
// CPython: Lib/os.py:1131 PathLike.__fspath__
func pathLikeFspath(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError")
}
