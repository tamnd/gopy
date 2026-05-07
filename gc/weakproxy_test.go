package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func clearProxies() {
	state.mu.Lock()
	state.weakProxies = make(map[objects.Object][]*objects.WeakProxy)
	state.mu.Unlock()
}

func TestRegisterWeakProxySkipsNilAndDead(t *testing.T) {
	defer clearProxies()
	RegisterWeakProxy(nil)

	dead := objects.NewWeakProxy(objects.NewList(nil), nil)
	dead.Clear()
	RegisterWeakProxy(dead)

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.weakProxies) != 0 {
		t.Fatalf("weakProxies = %v, want empty", state.weakProxies)
	}
}

func TestCollectClearsWeakProxyForCycle(t *testing.T) {
	defer untrackAll()
	defer clearProxies()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	p := objects.NewWeakProxy(a, nil)
	RegisterWeakProxy(p)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if got := p.Referent(); got != nil {
		t.Fatalf("Referent after Collect = %v, want nil", got)
	}
}

func TestWeakProxyCallbackFiresOnce(t *testing.T) {
	defer untrackAll()
	defer clearProxies()

	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)

	calls := 0
	var seen objects.Object
	cb := objects.NewBuiltinFunction("cb", func(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
		calls++
		if len(args) == 1 {
			seen = args[0]
		}
		return objects.None(), nil
	})

	p := objects.NewWeakProxy(a, cb)
	RegisterWeakProxy(p)

	if Collect(2) != 2 {
		t.Fatalf("cycle did not reclaim")
	}
	if calls != 1 {
		t.Fatalf("callback fired %d times, want 1", calls)
	}
	if seen != objects.Object(p) {
		t.Fatalf("callback arg = %v, want the proxy", seen)
	}
}
