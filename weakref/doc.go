// Package weakref ports the pure-Python weak collection classes from
// CPython's Lib/weakref.py and Lib/_weakrefset.py: WeakSet,
// WeakValueDictionary, and WeakKeyDictionary. They sit on top of the
// objects.Weakref scaffolding and rely on the gc package to fire the
// per-weakref callback when a referent goes unreachable.
//
// CPython models these as Python classes that subclass set / dict and
// install a tiny "_remove" closure as the weakref callback so dead
// referents drop out of the underlying container automatically. gopy
// implements them as Go types that wrap an internal map keyed by
// *objects.Weakref; the weakref callback is an objects.BuiltinFunction
// whose Fn closure captures the container and removes the matching
// entry when the referent dies.
//
// Lifetime caveat: each container's remover closure captures the
// container strongly, and every weakref it issues holds the closure
// strongly through its callback slot. That cycle is invisible to the
// gopy cycle collector (Go closures are opaque to TpTraverse), so an
// abandoned WeakSet / WeakValueDictionary / WeakKeyDictionary leaks
// until the items it weakly referenced have all died and dropped their
// weakrefs. CPython sidesteps this by capturing weakref(self) in the
// closure instead of self; matching that requires a TpTraverse-aware
// closure shape and lands with 1613-S (collector-visible Go closures).
//
// CPython: Lib/_weakrefset.py:11 WeakSet
// CPython: Lib/weakref.py:92 WeakValueDictionary
// CPython: Lib/weakref.py:298 WeakKeyDictionary
package weakref
