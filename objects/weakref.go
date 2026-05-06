// Weakref is the weakref.ref type. A weak reference holds a pointer
// at a target object without keeping it alive; once the cycle
// collector or finalizer machinery drops the target, the weakref's
// referent is cleared and any registered callback fires once.
//
// CPython exposes three weakref types: ref, proxy, and CallableProxyType.
// gopy v0.10 implements ref only; the proxy types ride along on the
// same scaffolding in v0.10.x.
//
// CPython: Objects/weakrefobject.c PyWeakReference

package objects

import "sync"

// Weakref is the weakref.ref Python object.
//
// CPython: Objects/weakrefobject.c:25 PyWeakReference
type Weakref struct {
	Header

	mu       sync.Mutex
	referent Object
	callback Object // None if no callback
}

// WeakrefType is the type singleton for weakref.ref.
//
// CPython: Objects/weakrefobject.c:540 _PyWeakref_RefType
var WeakrefType = NewType("weakref", []*Type{objectType})

func init() {
	WeakrefType.Repr = weakrefRepr
	WeakrefType.Str = weakrefRepr
	WeakrefType.Hash = weakrefHash
	WeakrefType.Call = weakrefCall
}

// NewWeakref builds a fresh weakref pointing at referent. callback
// may be None or any callable; gopy fires it when the referent is
// cleared by the collector.
//
// CPython: Objects/weakrefobject.c:271 PyWeakref_NewRef
func NewWeakref(referent, callback Object) *Weakref {
	if callback == nil {
		callback = None()
	}
	w := &Weakref{referent: referent, callback: callback}
	w.init(WeakrefType)
	return w
}

// Referent returns the live target or None if the weakref has been
// cleared. Concurrent-safe.
//
// CPython: Objects/weakrefobject.c:191 PyWeakref_GetObject
func (w *Weakref) Referent() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.referent == nil {
		return None()
	}
	return w.referent
}

// Callback returns the registered callback, or None if none was set.
func (w *Weakref) Callback() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.callback
}

// Clear drops the referent. Returns the callback (or None if there
// is none); the caller is responsible for invoking it. Idempotent;
// repeated calls return None.
//
// CPython: Objects/weakrefobject.c:107 clear_weakref
func (w *Weakref) Clear() Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.referent == nil {
		return None()
	}
	w.referent = nil
	cb := w.callback
	w.callback = None()
	return cb
}

func weakrefCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, errWeakrefArgs
	}
	return o.(*Weakref).Referent(), nil
}

func weakrefRepr(o Object) (string, error) {
	w := o.(*Weakref)
	r := w.Referent()
	if r == None() {
		return "<weakref at ?; dead>", nil
	}
	return "<weakref at ?; to '" + r.Type().Name + "'>", nil
}

func weakrefHash(o Object) (int64, error) {
	w := o.(*Weakref)
	r := w.Referent()
	if r == None() {
		return 0, errWeakrefDeadHash
	}
	return Hash(r)
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
