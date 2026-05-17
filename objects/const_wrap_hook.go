// ConstWrapHook lets higher layers (vm) plug compile-package value
// types into objects' const-attribute path. wrapConstAttr consults the
// hook after exhausting the scalar cases and before falling back to
// the %v stringifier, so dis.py / inspect / pdb see a real *objects.Code
// when they read co_consts on a function that nests a sub-scope.
//
// The hook avoids an import cycle: objects/ cannot reach
// compile.Code / compile.ConstTuple directly, so vm.init() installs the
// conversion at startup. When no hook is set (e.g. an objects/ unit
// test that never spins up the VM), the fallback string format keeps
// the older behavior so tests that only care about scalar consts
// continue to pass.

package objects

// ConstWrapHook converts raw compile-pipeline values into Objects so
// they round-trip through co_consts inspection. Returns ok=false to
// signal that the hook does not know about this concrete type, in
// which case wrapConstAttr falls back to its default behavior.
var ConstWrapHook func(any) (Object, bool)
