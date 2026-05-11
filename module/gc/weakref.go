// handle_weakrefs from CPython gc.c. Once move_unreachable has
// settled the candidate set, this pass walks every unreachable
// object's weakref list and:
//
//   1. clears the weakref's referent (so future ref() calls return
//      None), and
//   2. queues the registered callback (if any) to fire after the
//      collection completes.
//
// CPython runs callbacks on a clean Python frame outside the
// collector lock; gopy mirrors that by collecting the callbacks
// during the unreachable walk and invoking them after state.mu has
// been released.
//
// CPython: Python/gc.c:879 handle_weakrefs

package gc

import (
	"github.com/tamnd/gopy/objects"
)

// RegisterWeakref records w against its referent so a future
// collection can clear it. Caller-side: objects.NewWeakref builds
// the weakref; gopy code that wants the GC to clear the weakref
// when the referent dies must call this.
//
// CPython: Objects/weakrefobject.c:271 PyWeakref_NewRef registers
// via the referent's tp_weaklistoffset slot.
func RegisterWeakref(w *objects.Weakref) {
	if w == nil {
		return
	}
	target := w.Referent()
	if target == objects.None() {
		return
	}
	state.mu.Lock()
	state.weakrefs[target] = append(state.weakrefs[target], w)
	state.mu.Unlock()
}

// RegisterWeakProxy records p against its referent. The collector
// clears the proxy and queues its callback exactly the way it does
// for ref-style weakrefs.
//
// CPython: Objects/weakrefobject.c:925 PyWeakref_NewProxy registers
// via the same tp_weaklistoffset slot used by PyWeakref_NewRef.
func RegisterWeakProxy(p *objects.WeakProxy) {
	if p == nil {
		return
	}
	target := p.Referent()
	if target == nil {
		return
	}
	state.mu.Lock()
	state.weakProxies[target] = append(state.weakProxies[target], p)
	state.mu.Unlock()
}

// pendingCallback pairs a weakref or proxy with its registered
// callback so the caller can invoke callback(weakref) after the
// collector lock is released. CPython delivers the weakref as the
// callback's sole argument (see _PyWeakref_ClearRef +
// handle_weakrefs); the proxy variants share the path.
type pendingCallback struct {
	weakref  objects.Object
	callback objects.Object
}

// handleWeakrefs clears every weakref whose referent appears on
// unreachable. The (weakref, callback) pairs are returned to the
// caller, which invokes them after dropping state.mu.
//
// CPython: Python/gc.c:879 handle_weakrefs
func handleWeakrefs(unreachable *gcHead, weakrefs map[objects.Object][]*objects.Weakref, proxies map[objects.Object][]*objects.WeakProxy) []pendingCallback {
	var pending []pendingCallback
	for g := unreachable.next; g != unreachable; g = g.next {
		if wrs, ok := weakrefs[g.obj]; ok {
			delete(weakrefs, g.obj)
			for _, w := range wrs {
				cb := w.Clear()
				if cb != nil && cb != objects.None() {
					pending = append(pending, pendingCallback{weakref: w, callback: cb})
				}
			}
		}
		if prs, ok := proxies[g.obj]; ok {
			delete(proxies, g.obj)
			for _, p := range prs {
				cb := p.Clear()
				if cb != nil && cb != objects.None() {
					pending = append(pending, pendingCallback{weakref: p, callback: cb})
				}
			}
		}
	}
	return pending
}

// invokeWeakrefCallbacks fires each pending callback with its weakref
// as the sole argument. Errors are swallowed: CPython prints them
// through PyErr_WriteUnraisable and continues; gopy has no equivalent
// surface yet, so we drop them on the floor for v0.10 and revisit
// once a sys.unraisablehook port lands.
func invokeWeakrefCallbacks(pending []pendingCallback) {
	for _, p := range pending {
		_, _ = p.callback.Type().Call(p.callback, []objects.Object{p.weakref}, nil)
	}
}
