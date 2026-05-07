// String interning. Ports the intern_common / PyUnicode_InternInPlace
// machinery from unicodeobject.c. CPython keys its intern dict on a
// per-interpreter PyDict; gopy uses a package-level map[string]*Unicode
// guarded by a mutex because sub-interpreter isolation is not yet
// split out. When that work lands the table moves to Interpreter.
//
// CPython: Objects/unicodeobject.c:16005 intern_common
// CPython: Objects/unicodeobject.c:16135 PyUnicode_InternInPlace

package objects

import "sync"

var (
	internedMu sync.RWMutex
	interned   = map[string]*Unicode{}
)

// InternInPlace returns the canonical interned *Unicode for *p,
// rewriting *p so callers see the canonical pointer. Equivalent
// strings share the same *Unicode after this call. The pointer-to-
// interface signature is the contract CPython exposes; callers rely
// on the rewrite to swap in the canonical pointer.
//
// CPython: Objects/unicodeobject.c:16135 PyUnicode_InternInPlace
//
//nolint:gocritic // ptrToRefParam: the pointer-rewrite is the API contract.
func InternInPlace(p *Object) {
	if p == nil || *p == nil {
		return
	}
	u, ok := (*p).(*Unicode)
	if !ok {
		return
	}
	*p = internUnicode(u)
}

// InternFromString returns the canonical *Unicode for s, allocating a
// new one if the table didn't have one already. Mirrors
// PyUnicode_InternFromString.
//
// CPython: Objects/unicodeobject.c:16151 PyUnicode_InternFromString
func InternFromString(s string) Object {
	internedMu.RLock()
	if u, ok := interned[s]; ok {
		internedMu.RUnlock()
		return u
	}
	internedMu.RUnlock()
	return internUnicode(NewStr(s).(*Unicode))
}

// internUnicode is the dict_setdefault step: if the table already has
// a string equal to u, return that. Otherwise insert u and return it.
//
// CPython: Objects/unicodeobject.c:16060 intern_common (LOCK_INTERNED branch)
func internUnicode(u *Unicode) *Unicode {
	internedMu.Lock()
	defer internedMu.Unlock()
	if existing, ok := interned[u.v]; ok {
		return existing
	}
	interned[u.v] = u
	return u
}

// InternedCount returns the number of interned strings. Used by
// tests; CPython exposes this via _PyUnicode_InternedSize.
//
// CPython: Objects/unicodeobject.c:277 _PyUnicode_InternedSize
func InternedCount() int {
	internedMu.RLock()
	defer internedMu.RUnlock()
	return len(interned)
}

// ClearInterned drops every entry from the intern table. CPython runs
// this at interpreter shutdown to release the references it dropped
// when interning.
//
// CPython: Objects/unicodeobject.c:16164 _PyUnicode_ClearInterned
func ClearInterned() {
	internedMu.Lock()
	defer internedMu.Unlock()
	interned = map[string]*Unicode{}
}
