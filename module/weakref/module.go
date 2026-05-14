// weakref: redirected to stdlib/weakref.py + stdlib/_weakrefset.py.
// The Go _weakref backend (Modules/_weakref.c) lives in module/_weakref.
// Registering "weakref" or "_weakrefset" in inittab here would shadow
// the vendored Python files; we intentionally leave both out so
// PathFinder serves Lib/weakref.py and Lib/_weakrefset.py instead.
//
// CPython: Lib/weakref.py, Lib/_weakrefset.py
package weakref
