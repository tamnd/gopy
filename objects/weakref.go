// Package objects port of CPython's weakref subsystem. This file is a
// strict translation of cpython/Objects/weakrefobject.c: it defines
// the PyWeakReference Go type (Weakref), the per-object doubly-linked
// list bookkeeping that CPython hangs off tp_weaklistoffset, and the
// PyWeakref_* C-API entry points that the _weakref builtin module
// re-exports. The proxy types live in weakref_proxy.go and share the
// same list machinery through weakrefEntry.
//
// Concurrency: CPython protects the list with a striped per-object
// mutex (LOCK_WEAKREFS in pycore_weakref.h) in free-threaded builds
// and relies on the GIL elsewhere. gopy has no GIL, so each
// weakrefList carries its own sync.Mutex. The lock is held only for
// list mutations (insert/remove/walk); callbacks run outside the
// lock, matching CPython's PyObject_ClearWeakRefs.
//
// Lifecycle: CPython clears the list from tp_dealloc when refcount
// hits zero (PyObject_ClearWeakRefs). gopy's Decref does the same in
// refcount.go. Because gopy lives in Go's GC world, we additionally
// install runtime.SetFinalizer on the referent's Header so a referent
// that drops out of Go's reachability graph (no Decref path, e.g.
// cyclic garbage cleared via the module/gc collector or plain Go GC)
// still gets its weakrefs cleared and callbacks fired.
//
// Structure split: CPython packs ref, proxy, and callable proxy into
// one PyWeakReference C struct because the layout is identical. gopy
// keeps Weakref and WeakProxy as two Go types because their slot
// tables differ; the linked list stores either through a weakrefEntry
// node so the bookkeeping (count, walk, clear) is uniform.
//
// CPython: Objects/weakrefobject.c:1 weakrefobject.c

package objects

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// weakrefEntry is one node in a referent's doubly-linked weakref
// list. CPython packs ref and proxy into a single PyWeakReference
// struct so its list is uniformly typed; gopy keeps Weakref and
// WeakProxy as two Go structs (different slot tables) and stores
// their pointers behind this small enum-tagged node so the list
// bookkeeping (count, walk, clear) stays uniform.
type weakrefEntry struct {
	ref   *Weakref   // nil when this entry holds a proxy
	proxy *WeakProxy // nil when this entry holds a ref
	prev  *weakrefEntry
	next  *weakrefEntry
}

// asObject returns the Object representation of this entry for
// getweakrefs().
func (e *weakrefEntry) asObject() Object {
	if e.ref != nil {
		return e.ref
	}
	return e.proxy
}

// hasCallback reports whether this entry's ref/proxy has a callback
// installed. Used by insert_weakref to keep callback-less basics at
// the head of the list.
//
// CPython: Objects/weakrefobject.c:357 is_basic_ref
func (e *weakrefEntry) hasCallback() bool {
	if e.ref != nil {
		return e.ref.callback != nil
	}
	if e.proxy != nil {
		return e.proxy.callback != nil
	}
	return false
}

// kindOf reports which CPython type singleton this entry belongs to.
func (e *weakrefEntry) kindOf() weakrefKind {
	if e.ref != nil {
		return e.ref.kind
	}
	if e.proxy != nil {
		if e.proxy.callable {
			return weakrefKindCallableProxy
		}
		return weakrefKindProxy
	}
	return weakrefKindRef
}

// weakrefList is the head + lock for one object's weakref list.
// CPython packs the head pointer inline at tp_weaklistoffset and
// uses a striped lock keyed on the referent address; gopy
// heap-allocates one weakrefList per object on first use so types
// pay zero footprint when no weakref ever appears.
//
// CPython: Include/internal/pycore_weakref.h:23 LOCK_WEAKREFS
type weakrefList struct {
	mu    sync.Mutex
	head  *weakrefEntry
	armed bool // true once a Go runtime finalizer has been installed
}

// getOrCreateWeakrefList returns the referent's per-object weakref
// list, allocating it on first use. The pointer is stable for the
// life of the referent so callers may cache it after the first call.
//
// CPython: Objects/weakrefobject.c:37 GET_WEAKREFS_LISTPTR
func getOrCreateWeakrefList(referent Object) *weakrefList {
	h := referent.Hdr()
	if h.weakrefs == nil {
		h.weakrefs = &weakrefList{}
	}
	return h.weakrefs
}

// Weakref is the weakref.ref Python object. Mirrors PyWeakReference's
// ref variant. The actual list pointers live on the weakrefEntry node
// that stores this Weakref so the linked-list bookkeeping is shared
// with proxy entries; entry is the back-pointer so Clear can detach
// in O(1).
//
// CPython: Include/cpython/weakrefobject.h:8 _PyWeakReference
type Weakref struct {
	Header

	mu       sync.Mutex
	referent Object
	callback Object // nil when no callback was provided
	hash     int64
	hashSet  bool

	entry *weakrefEntry
	kind  weakrefKind
}

type weakrefKind uint8

const (
	weakrefKindRef weakrefKind = iota
	weakrefKindProxy
	weakrefKindCallableProxy
)

// WeakrefType is the type singleton for weakref.ref / ReferenceType.
//
// CPython: Objects/weakrefobject.c:498 _PyWeakref_RefType
var WeakrefType = NewType("weakref", []*Type{objectType})

func init() {
	WeakrefType.Repr = weakrefRepr
	WeakrefType.Str = weakrefRepr
	WeakrefType.Hash = weakrefHash
	WeakrefType.Call = weakrefCall
	WeakrefType.RichCmp = weakrefRichCompare
	// weakref_ref_new: ref(object[, callback])
	// CPython: Objects/weakrefobject.c weakref_ref_new
	WeakrefType.TpNew = func(_ *Type, args []Object, kwargs map[string]Object) (Object, error) {
		var referent, callback Object
		if len(args) >= 1 {
			referent = args[0]
		}
		if v, ok := kwargs["object"]; ok && referent == nil {
			referent = v
		}
		if len(args) >= 2 {
			callback = args[1]
		}
		if v, ok := kwargs["callback"]; ok && callback == nil {
			callback = v
		}
		if referent == nil {
			return nil, errors.New("TypeError: ref() requires an object argument")
		}
		return PyWeakref_NewRef(referent, callback)
	}
}

// GCWeakrefRegisterHook is called from PyWeakref_NewRef whenever a
// new weakref is created, so the cycle collector can clear it when the
// referent becomes unreachable. The gc module sets this at startup to
// avoid an objects -> gc import cycle.
//
// CPython: Objects/weakrefobject.c:271 PyWeakref_NewRef (registers via
// tp_weaklistoffset; CPython's GC walks that list during handle_weakrefs)
var GCWeakrefRegisterHook func(*Weakref)

// GCWeakProxyRegisterHook is the proxy counterpart of GCWeakrefRegisterHook.
var GCWeakProxyRegisterHook func(*WeakProxy)

// NewWeakref builds a fresh weakref pointing at referent. callback
// may be nil or None for no callback, or any callable. The new
// weakref is inserted into referent's per-object list and a runtime
// finalizer is attached so the referent's collection clears the
// weakref even when no Decref path runs it (e.g. cyclic garbage).
//
// CPython: Objects/weakrefobject.c:919 PyWeakref_NewRef
func NewWeakref(referent, callback Object) *Weakref {
	w, _ := PyWeakref_NewRef(referent, callback)
	return w
}

// PyWeakref_NewRef is the public C-API spelling. Returns (nil, err)
// for a nil referent so callers ported straight from C get the
// CPython-shape contract.
//
// CPython: Objects/weakrefobject.c:919 PyWeakref_NewRef
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_NewRef(referent, callback Object) (*Weakref, error) {
	if referent == nil {
		return nil, errors.New("TypeError: cannot create weak reference to None")
	}
	if callback == None() {
		callback = nil
	}
	list := getOrCreateWeakrefList(referent)
	list.mu.Lock()
	if callback == nil {
		if cand := tryReuseBasicRefLocked(list, weakrefKindRef); cand != nil {
			list.mu.Unlock()
			return cand.ref, nil
		}
	}
	w := allocateWeakref(referent, callback, weakrefKindRef)
	insertEntryLocked(list, &weakrefEntry{ref: w}, w, nil)
	list.mu.Unlock()
	armWeakrefFinalizer(referent)
	// Register with the Python-level cycle GC so handle_weakrefs can
	// clear this ref when the referent becomes unreachable during gc.collect().
	//
	// CPython: Objects/weakrefobject.c:271 PyWeakref_NewRef (implicit via
	// tp_weaklistoffset: GC walks the per-object list during handle_weakrefs)
	if h := GCWeakrefRegisterHook; h != nil {
		h(w)
	}
	return w, nil
}

// allocateWeakref builds a bare Weakref. The caller links it into
// the list under the list mutex.
//
// CPython: Objects/weakrefobject.c:400 allocate_weakref
func allocateWeakref(referent, callback Object, kind weakrefKind) *Weakref {
	w := &Weakref{
		referent: referent,
		callback: callback,
		hash:     -1,
		kind:     kind,
	}
	w.init(WeakrefType)
	return w
}

// insertEntryLocked binds w (or p) to the entry and inserts the
// entry in the correct list position. Caller must hold list.mu.
//
// CPython: Objects/weakrefobject.c:375 insert_weakref
func insertEntryLocked(list *weakrefList, e *weakrefEntry, w *Weakref, p *WeakProxy) {
	if w != nil {
		w.entry = e
	}
	if p != nil {
		p.entry = e
	}
	if list.head == nil {
		list.head = e
		return
	}
	// Basic ref always goes at the head. Basic proxy goes right
	// after a basic ref (or at head if there isn't one). Callback-
	// bearing entries go after the basic pair.
	if !e.hasCallback() && e.kindOf() == weakrefKindRef {
		insertHead(e, list)
		return
	}
	if !e.hasCallback() && (e.kindOf() == weakrefKindProxy || e.kindOf() == weakrefKindCallableProxy) {
		// after basic ref if present, else at head
		if list.head.kindOf() == weakrefKindRef && !list.head.hasCallback() {
			insertAfter(e, list.head)
		} else {
			insertHead(e, list)
		}
		return
	}
	// callback-bearing: append after the basic pair.
	anchor := list.head
	if anchor.next != nil && !anchor.next.hasCallback() {
		anchor = anchor.next
	}
	insertAfter(e, anchor)
}

// tryReuseBasicRefLocked returns the existing callback-less entry
// matching kind so ref(x) / proxy(x) called twice returns the same
// object. Caller must hold list.mu.
//
// CPython: Objects/weakrefobject.c:330 try_reuse_basic_ref
func tryReuseBasicRefLocked(list *weakrefList, kind weakrefKind) *weakrefEntry {
	if list.head == nil || list.head.hasCallback() {
		return nil
	}
	switch kind {
	case weakrefKindRef:
		if list.head.kindOf() == weakrefKindRef {
			return list.head
		}
	case weakrefKindProxy, weakrefKindCallableProxy:
		cand := list.head
		if cand.kindOf() == weakrefKindRef {
			cand = cand.next
		}
		if cand != nil && !cand.hasCallback() && (cand.kindOf() == weakrefKindProxy || cand.kindOf() == weakrefKindCallableProxy) {
			return cand
		}
	}
	return nil
}

// insertHead pushes e onto the front of the list.
//
// CPython: Objects/weakrefobject.c:315 insert_head
func insertHead(e *weakrefEntry, list *weakrefList) {
	e.prev = nil
	e.next = list.head
	if list.head != nil {
		list.head.prev = e
	}
	list.head = e
}

// insertAfter splices e in after prev.
//
// CPython: Objects/weakrefobject.c:302 insert_after
func insertAfter(e, prev *weakrefEntry) {
	e.prev = prev
	e.next = prev.next
	if prev.next != nil {
		prev.next.prev = e
	}
	prev.next = e
}

// detachLocked unlinks e from list. Caller must hold list.mu.
//
// CPython: Objects/weakrefobject.c:82 clear_weakref_lock_held (list edit)
func detachLocked(e *weakrefEntry, list *weakrefList) {
	if list.head == e {
		list.head = e.next
	}
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	}
	e.prev = nil
	e.next = nil
}

// Referent returns the live target or None when the weakref has been
// cleared. Concurrent-safe.
//
// CPython: Include/internal/pycore_weakref.h:72 _PyWeakref_GET_REF
func (w *Weakref) Referent() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.referent == nil {
		return None()
	}
	return w.referent
}

// Callback returns the registered callback or None when none was set.
func (w *Weakref) Callback() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.callback == nil {
		return None()
	}
	return w.callback
}

// Clear drops the referent and returns the registered callback so
// the caller can invoke it outside the lock. Returns None when no
// callback was installed. Idempotent: a second Clear returns None.
//
// CPython: Objects/weakrefobject.c:79 clear_weakref_lock_held
func (w *Weakref) Clear() Object {
	w.mu.Lock()
	r := w.referent
	cb := w.callback
	entry := w.entry
	w.referent = nil
	w.callback = nil
	w.entry = nil
	w.mu.Unlock()
	if r != nil && entry != nil {
		if list := r.Hdr().weakrefs; list != nil {
			list.mu.Lock()
			detachLocked(entry, list)
			list.mu.Unlock()
		}
	}
	if cb == nil {
		return None()
	}
	return cb
}

// IsDead reports whether the weakref has been cleared.
//
// CPython: Include/internal/pycore_weakref.h:99 _PyWeakref_IS_DEAD
func (w *Weakref) IsDead() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.referent == nil
}

// PyWeakref_IS_DEAD is the C-API spelling.
//
// CPython: Include/internal/pycore_weakref.h:99 _PyWeakref_IS_DEAD
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_IS_DEAD(w *Weakref) bool { return w.IsDead() }

// PyWeakref_GetObject returns the referent or None when dead. CPython
// returns a borrowed reference; gopy has no borrow distinction so the
// live Object is returned directly.
//
// CPython: Objects/weakrefobject.c:967 PyWeakref_GetObject
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_GetObject(w *Weakref) Object { return w.Referent() }

// PyWeakref_GetRef returns (referent, true) when live, (None, false)
// when dead. Matches the CPython out-param-plus-int contract.
//
// CPython: Objects/weakrefobject.c:949 PyWeakref_GetRef
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_GetRef(w *Weakref) (Object, bool) {
	r := w.Referent()
	if r == None() {
		return None(), false
	}
	return r, true
}

// PyWeakref_NewProxy mirrors the CPython entry point: it picks
// CallableProxyType when the referent has a tp_call, ProxyType
// otherwise, then routes through get_or_create_weakref.
//
// CPython: Objects/weakrefobject.c:925 PyWeakref_NewProxy
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_NewProxy(referent, callback Object) (*WeakProxy, error) {
	if referent == nil {
		return nil, errors.New("TypeError: cannot create weak reference to None")
	}
	callable := referent.Type() != nil && referent.Type().Call != nil
	if callback == None() {
		callback = nil
	}
	list := getOrCreateWeakrefList(referent)
	kind := weakrefKindProxy
	if callable {
		kind = weakrefKindCallableProxy
	}
	list.mu.Lock()
	if callback == nil {
		if cand := tryReuseBasicRefLocked(list, kind); cand != nil {
			list.mu.Unlock()
			return cand.proxy, nil
		}
	}
	p := allocateWeakProxy(referent, callback, callable)
	insertEntryLocked(list, &weakrefEntry{proxy: p}, nil, p)
	list.mu.Unlock()
	armWeakrefFinalizer(referent)
	return p, nil
}

// PyWeakref_GetWeakrefCount returns the number of live weak
// references pointing at obj, including proxies.
//
// CPython: Objects/weakrefobject.c:42 _PyWeakref_GetWeakrefCount
//
//nolint:revive // CPython C-API name kept verbatim for 1:1 port
func PyWeakref_GetWeakrefCount(obj Object) int {
	if obj == nil {
		return 0
	}
	list := obj.Hdr().weakrefs
	if list == nil {
		return 0
	}
	list.mu.Lock()
	defer list.mu.Unlock()
	n := 0
	for cur := list.head; cur != nil; cur = cur.next {
		n++
	}
	return n
}

// GetWeakrefs returns live weakrefs pointing at obj in insertion
// order. Mirrors _weakref.getweakrefs.
//
// CPython: Modules/_weakref.c:75 _weakref_getweakrefs
func GetWeakrefs(obj Object) []Object {
	if obj == nil {
		return nil
	}
	list := obj.Hdr().weakrefs
	if list == nil {
		return nil
	}
	list.mu.Lock()
	defer list.mu.Unlock()
	out := make([]Object, 0)
	for cur := list.head; cur != nil; cur = cur.next {
		out = append(out, cur.asObject())
	}
	return out
}

// ClearWeakRefs is the dealloc-time entry point: detach every
// weakref from obj's list, clear it, and run its callback.
// Callbacks fire outside the list mutex so a callback that
// constructs new weakrefs cannot deadlock.
//
// CPython: Objects/weakrefobject.c:1006 PyObject_ClearWeakRefs
func ClearWeakRefs(obj Object) {
	if obj == nil {
		return
	}
	list := obj.Hdr().weakrefs
	if list == nil {
		return
	}
	clearListAndFire(list)
}

// clearListAndFire walks a weakref list, detaches every node,
// clears the underlying ref/proxy, collects pending callbacks, and
// invokes them after dropping the lock.
//
// CPython: Objects/weakrefobject.c:1006 PyObject_ClearWeakRefs
func clearListAndFire(list *weakrefList) {
	type pending struct {
		w  Object
		cb Object
	}
	var fire []pending
	list.mu.Lock()
	for cur := list.head; cur != nil; {
		next := cur.next
		var cb Object
		switch {
		case cur.ref != nil:
			cur.ref.mu.Lock()
			cb = cur.ref.callback
			cur.ref.referent = nil
			cur.ref.callback = nil
			cur.ref.entry = nil
			cur.ref.mu.Unlock()
			if cb != nil {
				fire = append(fire, pending{w: cur.ref, cb: cb})
			}
		case cur.proxy != nil:
			cur.proxy.mu.Lock()
			cb = cur.proxy.callback
			cur.proxy.referent = nil
			cur.proxy.callback = nil
			cur.proxy.entry = nil
			cur.proxy.mu.Unlock()
			if cb != nil {
				fire = append(fire, pending{w: cur.proxy, cb: cb})
			}
		}
		cur.prev = nil
		cur.next = nil
		cur = next
	}
	list.head = nil
	list.mu.Unlock()

	for _, p := range fire {
		tp := p.cb.Type()
		if tp == nil || tp.Call == nil {
			continue
		}
		// CPython routes weakref callback errors through
		// sys.unraisablehook with the "Exception ignored on calling
		// callback for %R" prefix, then keeps draining the rest of the
		// list.
		//
		// CPython: Objects/weakrefobject.c:97 handle_callback
		_, err := tp.Call(p.cb, []Object{p.w}, nil)
		if err != nil && WriteUnraisableHook != nil {
			s, sErr := Repr(p.w)
			if sErr != nil {
				s = "<weakref>"
			}
			WriteUnraisableHook(p.cb, "Exception ignored on calling callback for "+s, err)
		}
	}
}

// armWeakrefFinalizer attaches a Go GC finalizer to the referent so
// that when Go's runtime reclaims the referent (independently of
// gopy's refcount or cycle-collector path), the weakref list is
// cleared and any callbacks fire. The finalizer is idempotent at the
// gopy layer because clearListAndFire detaches and nils on the first
// call.
//
// Note: as long as any Weakref/WeakProxy holds a strong Go pointer
// to the referent (the default for NewWeakref / NewWeakProxy), this
// finalizer never fires. It is a safety net for paths where the
// strong pointer is dropped explicitly (e.g. a Decref that has
// already cleared all weakrefs).
//
// CPython: Objects/weakrefobject.c:1006 PyObject_ClearWeakRefs (the
// tp_dealloc-driven path that gopy mirrors here for Go-GC reclaim)
func armWeakrefFinalizer(referent Object) {
	list := referent.Hdr().weakrefs
	if list == nil {
		return
	}
	list.mu.Lock()
	if list.armed {
		list.mu.Unlock()
		return
	}
	list.armed = true
	list.mu.Unlock()

	hdr := referent.Hdr()
	runtime.SetFinalizer(hdr, func(h *Header) {
		if h.weakrefs != nil {
			clearListAndFire(h.weakrefs)
		}
	})
}

// weakrefCall implements ref() invocation: returns the referent or
// None when dead.
//
// CPython: Objects/weakrefobject.c:172 weakref_vectorcall
func weakrefCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, errWeakrefArgs
	}
	return o.(*Weakref).Referent(), nil
}

// weakrefRepr formats a weakref using CPython's format string. The
// pointer values are %p hex; a dead weakref omits the type/name.
//
// CPython: Objects/weakrefobject.c:216 weakref_repr
func weakrefRepr(o Object) (string, error) {
	w := o.(*Weakref)
	r := w.Referent()
	if r == None() {
		return fmt.Sprintf("<weakref at %p; dead>", w), nil
	}
	return fmt.Sprintf("<weakref at %p; to '%s' at %p>", w, r.Type().Name, r), nil
}

// weakrefHash caches the referent's hash on first compute. Once the
// referent is cleared, a stale hash from before is still returned
// (matching CPython, which freezes the cached hash); if the hash was
// never computed and the weakref is dead, raise TypeError.
//
// CPython: Objects/weakrefobject.c:190 weakref_hash_lock_held
func weakrefHash(o Object) (int64, error) {
	w := o.(*Weakref)
	w.mu.Lock()
	if w.hashSet {
		h := w.hash
		w.mu.Unlock()
		return h, nil
	}
	r := w.referent
	w.mu.Unlock()
	if r == nil {
		return 0, errWeakrefDeadHash
	}
	h, err := Hash(r)
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	w.hash = h
	w.hashSet = true
	w.mu.Unlock()
	return h, nil
}

// weakrefRichCompare supports only == and !=. Two weakrefs compare
// equal when both referents compare equal; if either is dead, they
// compare equal only when self-identical.
//
// CPython: Objects/weakrefobject.c:245 weakref_richcompare
func weakrefRichCompare(a, b Object, op CompareOp) (Object, error) {
	wa, okA := a.(*Weakref)
	wb, okB := b.(*Weakref)
	if !okA || !okB || (op != CompareEQ && op != CompareNE) {
		return NotImplemented(), nil
	}
	ra := wa.Referent()
	rb := wb.Referent()
	if ra == None() || rb == None() {
		eq := (a == b)
		if op == CompareNE {
			eq = !eq
		}
		return NewBool(eq), nil
	}
	return RichCmp(ra, rb, op)
}

var (
	errWeakrefArgs     = newSimpleError("TypeError: weakref takes no arguments")
	errWeakrefDeadHash = newSimpleError("TypeError: weak object has gone away")
)

// newSimpleError builds an error value local to this file. Other
// objects-package files use the shared err* sentinels declared in
// errors.go; weakref's two cases are unique to it.
func newSimpleError(msg string) error {
	return &simpleError{msg: msg}
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
