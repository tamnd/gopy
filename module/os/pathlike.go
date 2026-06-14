// PathLike: the abstract base class for the os.fspath protocol.
//
// CPython ships PathLike in Lib/os.py as `class PathLike(abc.ABC)` whose
// metaclass is therefore abc.ABCMeta and whose __subclasshook__ recognises
// any class exposing __fspath__. gopy's os module is a Go inittab module, so
// Lib/os.py never runs on top of it. We reproduce the class faithfully by
// constructing it through the real abc.ABCMeta the first time os.PathLike is
// read, which is the earliest point abc is importable (the os module is built
// while importlib's path-based finder is still half-installed, so abc cannot
// be imported during os module construction).
//
// Building it on the genuine ABCMeta matters for metaclass resolution:
// typing.Protocol's metaclass _ProtocolMeta is an ABCMeta subclass, so
// `class C(os.PathLike, Protocol)` resolves its metaclass to _ProtocolMeta.
// A bespoke metaclass unrelated to ABCMeta would make that a metaclass
// conflict.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
package os

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// builtPathLike caches the PathLike class once constructed through ABCMeta.
var builtPathLike objects.Object

// buildPathLike constructs os.PathLike through the real abc.ABCMeta, matching
// the Lib/os.py definition. Cached after the first call.
//
// CPython: Lib/os.py:1123 class PathLike(abc.ABC)
func buildPathLike() (objects.Object, error) {
	if builtPathLike != nil {
		return builtPathLike, nil
	}
	if objects.ImportModuleHook == nil {
		return nil, fmt.Errorf("ImportError: cannot import abc to build os.PathLike")
	}
	abcMod, err := objects.ImportModuleHook("abc")
	if err != nil {
		return nil, err
	}
	abcMeta, err := objects.LookupAttrString(abcMod, "ABCMeta")
	if err != nil {
		return nil, err
	}

	emptyTuple := objects.NewTuple(nil)

	ns := objects.NewDict()
	if err := ns.SetItem(objects.NewStr("__module__"), objects.NewStr("os")); err != nil {
		return nil, err
	}
	if err := ns.SetItem(objects.NewStr("__qualname__"), objects.NewStr("PathLike")); err != nil {
		return nil, err
	}
	if err := ns.SetItem(objects.NewStr("__slots__"), emptyTuple); err != nil {
		return nil, err
	}
	if err := ns.SetItem(objects.NewStr("__doc__"),
		objects.NewStr("Abstract base class for implementing the file system path protocol.")); err != nil {
		return nil, err
	}
	// __fspath__: the abstract path-protocol method. CPython marks it with
	// @abc.abstractmethod; a Go builtin function cannot carry
	// __isabstractmethod__, so the namespace entry is a plain method whose
	// body raises NotImplementedError, matching the upstream body.
	//
	// CPython: Lib/os.py:1129 @abc.abstractmethod def __fspath__
	fspathDescr := objects.NewMethodDescr(objects.ObjectType(), "__fspath__", pathLikeFspath)
	if err := ns.SetItem(objects.NewStr("__fspath__"), fspathDescr); err != nil {
		return nil, err
	}
	// __subclasshook__: recognise any class exposing __fspath__ so
	// isinstance/issubclass work like CPython's _check_methods-based hook.
	//
	// CPython: Lib/os.py:1134 def __subclasshook__
	hook := objects.NewClassMethod(objects.NewBuiltinFunction("__subclasshook__", pathLikeSubclasshook))
	if err := ns.SetItem(objects.NewStr("__subclasshook__"), hook); err != nil {
		return nil, err
	}

	bases := objects.NewTuple([]objects.Object{objects.ObjectType()})
	args := objects.NewTuple([]objects.Object{objects.NewStr("PathLike"), bases, ns})
	cls, err := objects.Call(abcMeta, args, nil)
	if err != nil {
		return nil, err
	}
	builtPathLike = cls
	return cls, nil
}

// pathLikeSubclasshook backs PathLike.__subclasshook__(cls, subclass): it
// returns True when subclass exposes __fspath__ anywhere in its MRO,
// otherwise NotImplemented so normal subclass logic continues. Mirrors the
// _check_methods(subclass, '__fspath__') result in CPython.
//
// CPython: Lib/os.py:1134 PathLike.__subclasshook__ / _collections_abc._check_methods
func pathLikeSubclasshook(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: __subclasshook__() takes 2 arguments")
	}
	sub, ok := args[1].(*objects.Type)
	if !ok {
		return objects.NotImplemented(), nil
	}
	// _check_methods(subclass, '__fspath__'): look for '__fspath__' across the
	// candidate's MRO __dict__ entries. A bound getattr is the wrong test here
	// because gopy's LookupAttr reports a missing attribute as (nil, nil),
	// which would make every class match; an explicit None entry must
	// short-circuit to NotImplemented just as in CPython.
	//
	// CPython: Lib/_collections_abc.py:83 _check_methods
	descr, _ := objects.LookupDescriptor(sub, "__fspath__")
	if descr == nil {
		return objects.NotImplemented(), nil
	}
	if descr == objects.None() {
		return objects.NotImplemented(), nil
	}
	return objects.True(), nil
}

// pathLikeFspath is the abstract method body. CPython raises
// NotImplementedError; we follow.
//
// CPython: Lib/os.py:1131 PathLike.__fspath__
func pathLikeFspath(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return nil, fmt.Errorf("NotImplementedError")
}
