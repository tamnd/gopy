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
