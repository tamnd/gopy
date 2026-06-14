// Bridge variables that let the object.__reduce_ex__ port reach
// helpers defined in the vendored copyreg.py module. objects/ cannot
// import the import machinery directly without creating a cycle, so
// vm.init() installs CopyregLookup to forward attribute lookups
// through sys.modules.

package objects

// CopyregLookup retrieves an attribute from the copyreg module. The
// hook is wired by the vm package at startup so the pickle reducer in
// objects can reach copyreg.__newobj__, copyreg.__newobj_ex__ and
// copyreg._reduce_ex without depending on the import package.
//
// CPython: Objects/typeobject.c:7747 _common_reduce (import_copyreg)
var CopyregLookup func(name string) (Object, error)

// BuiltinLookup retrieves a named object from the builtins module.
// Wired by vm.init() so set iterator __reduce__ can reference iter()
// without importing builtins directly.
//
// CPython: Python/bltinmodule.c:_PyEval_GetBuiltin
var BuiltinLookup func(name string) (Object, error)

// CurrentBuiltinsHook returns the builtins namespace a freshly built
// function should inherit when its globals dict carries no
// __builtins__ key. Wired by vm.init() to the running frame's
// f_builtins, falling back to the builtins module dict.
//
// CPython: Objects/dictobject.c _PyDict_LoadBuiltinsFromGlobals
var CurrentBuiltinsHook func() Object

// ImportModuleHook imports a module by its absolute name and returns
// the module object, importing it through the path/finder machinery
// when it is not yet in sys.modules. Wired by vm.init() so the _pickle
// decoder can resolve GLOBAL references and load _compat_pickle for the
// proto < 3 two-to-three name mapping without depending on the import
// package's Executor wiring.
//
// CPython: Python/import.c:1450 PyImport_ImportModule
var ImportModuleHook func(name string) (Object, error)

// ModuleReprHook formats a module's repr by delegating to the vendored
// importlib._bootstrap._module_repr, exactly as CPython's C
// module_repr forwards to _PyImport_ImportlibModuleRepr. Wired by
// vm.init(); nil during early bootstrap, where moduleRepr falls back to
// a minimal Go rendering.
//
// CPython: Python/import.c:3346 _PyImport_ImportlibModuleRepr
var ModuleReprHook func(m Object) (string, error)
