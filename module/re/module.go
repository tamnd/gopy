// Package re was the Go-backed shim for the Python re module. The
// module is now resolved from stdlib/re/__init__.py via PathFinder;
// the _sre C extension is backed by module/_sre/module.go.
//
// This package is kept compilable so existing blank-import sites do
// not need updating before stdlibinit/registry.go is updated.
//
// CPython: Lib/re/__init__.py the public surface
package re
