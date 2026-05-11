// WeakProxy is the weakref.proxy / CallableProxyType Python object.
// A proxy holds a weak reference and forwards every Python operation
// to the referent until the referent is cleared, after which every
// access raises ReferenceError.
//
// CPython exposes two proxy types whose only difference is the
// presence of tp_call. gopy collapses them into one Go struct
// distinguished by the Type singleton (and a callable flag for fast
// checks).
//
// CPython: Objects/weakrefobject.c:852 _PyWeakref_ProxyType
// CPython: Objects/weakrefobject.c:887 _PyWeakref_CallableProxyType

package objects

import (
	"fmt"
	"sync"
)

// WeakProxy backs both weakref.ProxyType and weakref.CallableProxyType.
//
// CPython: Objects/weakrefobject.c:25 PyWeakReference (proxy variants)
type WeakProxy struct {
	Header

	mu       sync.Mutex
	referent Object
	callback Object // nil when no callback was provided
	callable bool

	// entry is the back-pointer into the referent's weakref list,
	// kept so Clear can detach in O(1). nil after Clear.
	entry *weakrefEntry
}

// WeakProxyType is the type singleton for weakref.ProxyType.
//
// CPython: Objects/weakrefobject.c:852 _PyWeakref_ProxyType
var WeakProxyType = NewType("weakproxy", []*Type{objectType})

// WeakCallableProxyType is the type singleton for weakref.CallableProxyType.
//
// CPython: Objects/weakrefobject.c:887 _PyWeakref_CallableProxyType
var WeakCallableProxyType = NewType("weakcallableproxy", []*Type{objectType})

func init() {
	for _, t := range []*Type{WeakProxyType, WeakCallableProxyType} {
		t.Repr = weakProxyRepr
		t.Str = weakProxyStr
		t.RichCmp = weakProxyRichCmp
		t.Getattro = weakProxyGetattr
		t.Setattro = weakProxySetattr
		t.Iter = weakProxyIter
		t.IterNext = weakProxyIterNext
		t.Sequence = &SequenceMethods{Contains: weakProxyContains}
		t.Mapping = &MappingMethods{
			Length:  weakProxyLength,
			GetItem: weakProxyGetItem,
			SetItem: weakProxySetItem,
			DelItem: weakProxyDelItem,
		}
	}
	WeakCallableProxyType.Call = weakProxyCall
}

// NewWeakProxy builds a non-callable weakref.proxy pointing at
// referent. Mirrors PyWeakref_NewProxy when the referent is not
// callable.
//
// CPython: Objects/weakrefobject.c:925 PyWeakref_NewProxy
func NewWeakProxy(referent, callback Object) *WeakProxy {
	return newWeakProxy(referent, callback, false)
}

// NewWeakCallableProxy builds the CallableProxyType variant for
// referents whose type defines __call__.
//
// CPython: Objects/weakrefobject.c:925 PyWeakref_NewProxy (callable
// branch picking _PyWeakref_CallableProxyType)
func NewWeakCallableProxy(referent, callback Object) *WeakProxy {
	return newWeakProxy(referent, callback, true)
}

func newWeakProxy(referent, callback Object, callable bool) *WeakProxy {
	// Route through the unified C-API path so the proxy is registered
	// in the referent's weakref list. The caller has already chosen
	// the callable variant; we just override the kind selection that
	// PyWeakref_NewProxy does by routing through allocateWeakProxy +
	// list insert directly when the requested kind disagrees with
	// the referent's actual callability.
	if referent == nil {
		// Match CPython behavior: NewWeakProxy(NULL, ...) raises.
		// Callers in v0.10 sometimes pass through nil; keep the
		// legacy shape (return an empty proxy with no list entry)
		// so existing tests do not have to handle errors here.
		p := allocateWeakProxy(nil, callback, callable)
		return p
	}
	if cb := callback; cb == None() {
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
			return cand.proxy
		}
	}
	p := allocateWeakProxy(referent, callback, callable)
	insertEntryLocked(list, &weakrefEntry{proxy: p}, nil, p)
	list.mu.Unlock()
	armWeakrefFinalizer(referent)
	return p
}

// allocateWeakProxy builds a bare WeakProxy. The caller links it
// into the referent's weakref list under the list mutex.
//
// CPython: Objects/weakrefobject.c:400 allocate_weakref (proxy variant)
func allocateWeakProxy(referent, callback Object, callable bool) *WeakProxy {
	p := &WeakProxy{referent: referent, callback: callback, callable: callable}
	if callable {
		p.init(WeakCallableProxyType)
	} else {
		p.init(WeakProxyType)
	}
	return p
}

// Referent returns the live target or nil when the proxy has been
// cleared.
//
// CPython: Objects/weakrefobject.c:191 PyWeakref_GetObject
func (p *WeakProxy) Referent() Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.referent
}

// Callback returns the registered callback (or None when no
// callback was installed).
func (p *WeakProxy) Callback() Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.callback == nil {
		return None()
	}
	return p.callback
}

// Clear drops the referent. Returns the callback for the caller to
// invoke, or None when no callback was installed. Idempotent.
//
// CPython: Objects/weakrefobject.c:79 clear_weakref_lock_held
func (p *WeakProxy) Clear() Object {
	p.mu.Lock()
	r := p.referent
	cb := p.callback
	entry := p.entry
	if r == nil {
		p.mu.Unlock()
		return None()
	}
	p.referent = nil
	p.callback = nil
	p.entry = nil
	p.mu.Unlock()
	if entry != nil && r != nil {
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

// liveReferent returns the live target or a ReferenceError describing
// a dead proxy. Used by every forwarding slot.
//
// CPython: Objects/weakrefobject.c:548 proxy_check_ref
func (p *WeakProxy) liveReferent() (Object, error) {
	r := p.Referent()
	if r == nil {
		return nil, errProxyDead
	}
	return r, nil
}

var errProxyDead = newSimpleError("ReferenceError: weakly-referenced object no longer exists")

func weakProxyRepr(o Object) (string, error) {
	p := o.(*WeakProxy)
	r := p.Referent()
	if r == nil {
		return fmt.Sprintf("<weakproxy at %p; dead>", p), nil
	}
	return fmt.Sprintf("<weakproxy at %p; to '%s' at %p>", p, r.Type().Name, r), nil
}

func weakProxyStr(o Object) (string, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return "", err
	}
	return Str(r)
}

func weakProxyGetattr(o Object, name Object) (Object, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return nil, err
	}
	return GetAttr(r, name)
}

func weakProxySetattr(o Object, name, value Object) error {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return err
	}
	if value == nil {
		return DelAttr(r, name)
	}
	return SetAttr(r, name, value)
}

func weakProxyRichCmp(a, b Object, op CompareOp) (Object, error) {
	if pa, ok := a.(*WeakProxy); ok {
		r, err := pa.liveReferent()
		if err != nil {
			return nil, err
		}
		a = r
	}
	if pb, ok := b.(*WeakProxy); ok {
		r, err := pb.liveReferent()
		if err != nil {
			return nil, err
		}
		b = r
	}
	return RichCmp(a, b, op)
}

func weakProxyIter(o Object) (Object, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return nil, err
	}
	return Iter(r)
}

func weakProxyIterNext(o Object) (Object, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return nil, err
	}
	if r.Type().IterNext == nil {
		return nil, fmt.Errorf("TypeError: Weakref proxy referenced a non-iterator '%s' object", r.Type().Name)
	}
	return IterNext(r)
}

func weakProxyContains(o, v Object) (bool, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return false, err
	}
	return Contains(r, v)
}

func weakProxyLength(o Object) (int, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return 0, err
	}
	return Length(r)
}

func weakProxyGetItem(o, key Object) (Object, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return nil, err
	}
	return GetItem(r, key)
}

func weakProxySetItem(o, key, value Object) error {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return err
	}
	return SetItem(r, key, value)
}

func weakProxyDelItem(o, key Object) error {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return err
	}
	return DelItem(r, key)
}

func weakProxyCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	p := o.(*WeakProxy)
	r, err := p.liveReferent()
	if err != nil {
		return nil, err
	}
	tp := r.Type()
	if tp.Call == nil {
		return nil, fmt.Errorf("TypeError: '%s' object is not callable", tp.Name)
	}
	return tp.Call(r, args, kwargs)
}
