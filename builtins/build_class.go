// __build_class__ is the builtin every `class C(...): ...` statement
// invokes. The implementation has to push a frame and drive the eval
// loop, so it lives in vm/build_class.go and registers itself here on
// init via SetBuildClass. Init() then stamps the registered callable
// into the builtins dict alongside print, len, ... .
//
// CPython: Python/bltinmodule.c:131 builtin___build_class__

package builtins

import "github.com/tamnd/gopy/objects"

// BuildClass is the signature __build_class__ takes when registered
// from the vm side. args = (body_fn, name, *bases); kwargs may carry
// metaclass and any other keyword arguments forwarded to the
// metaclass call.
type BuildClass func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error)

var buildClass BuildClass

// SetBuildClass installs the implementation of __build_class__. vm
// calls this from init() so builtins.Init can find a callable to
// register under "__build_class__". Tests may override the hook.
//
// CPython: equivalent of registering builtin___build_class__ in
// Python/bltinmodule.c:3450 _PyBuiltin_Init
func SetBuildClass(fn BuildClass) {
	buildClass = fn
}
