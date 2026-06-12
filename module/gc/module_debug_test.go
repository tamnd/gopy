// Coverage for the gc module entries that landed alongside the
// callbacks invocation: set_debug / get_debug, get_stats, is_finalized,
// and the debug constants stamped on the module dict.

package gc

import (
	"testing"

	"github.com/tamnd/gopy/objects"
)

func TestBuildModuleExportsDebugSurface(t *testing.T) {
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	want := []string{
		"set_debug", "get_debug", "get_stats", "is_finalized",
		"DEBUG_STATS", "DEBUG_COLLECTABLE", "DEBUG_UNCOLLECTABLE",
		"DEBUG_SAVEALL", "DEBUG_LEAK", "callbacks",
	}
	for _, name := range want {
		v, err := mod.Dict().GetItem(objects.NewStr(name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v == nil {
			t.Fatalf("%s: nil entry", name)
		}
	}
}

func TestSetDebugGetDebugRoundTrip(t *testing.T) {
	prev := GetDebug()
	defer SetDebug(prev)

	if _, err := gcSetDebug([]objects.Object{objects.NewInt(int64(DebugLeak))}, nil); err != nil {
		t.Fatalf("set_debug: %v", err)
	}
	out, err := gcGetDebug(nil, nil)
	if err != nil {
		t.Fatalf("get_debug: %v", err)
	}
	x, ok := out.(*objects.Int)
	if !ok {
		t.Fatalf("get_debug returned %T, want *Int", out)
	}
	v, _ := x.Int64()
	if int(v) != DebugLeak {
		t.Fatalf("get_debug = %d, want %d", v, DebugLeak)
	}
}

func TestSetDebugRequiresInt(t *testing.T) {
	if _, err := gcSetDebug([]objects.Object{objects.NewStr("oops")}, nil); err == nil {
		t.Fatal("set_debug('oops'): want TypeError, got nil")
	}
}

func TestGetStatsShape(t *testing.T) {
	out, err := gcGetStats(nil, nil)
	if err != nil {
		t.Fatalf("get_stats: %v", err)
	}
	lst, ok := out.(*objects.List)
	if !ok {
		t.Fatalf("get_stats returned %T, want *List", out)
	}
	if lst.Len() != NumGenerations {
		t.Fatalf("get_stats len = %d, want %d", lst.Len(), NumGenerations)
	}
	for i := 0; i < lst.Len(); i++ {
		d, ok := lst.Item(i).(*objects.Dict)
		if !ok {
			t.Fatalf("entry %d is %T, want *Dict", i, lst.Item(i))
		}
		for _, key := range []string{"collections", "collected", "uncollectable"} {
			v, err := d.GetItem(objects.NewStr(key))
			if err != nil {
				t.Fatalf("entry %d missing %q: %v", i, key, err)
			}
			if _, ok := v.(*objects.Int); !ok {
				t.Fatalf("entry %d %q is %T, want *Int", i, key, v)
			}
		}
	}
}

func TestGetStatsTracksCollections(t *testing.T) {
	defer untrackAll()
	before := GetStats()
	Collect(0)
	after := GetStats()
	if after[0].collections != before[0].collections+1 {
		t.Fatalf("collections did not advance: before=%d after=%d",
			before[0].collections, after[0].collections)
	}
}

func TestIsFinalizedReportsFinalizerRun(t *testing.T) {
	defer untrackAll()
	a := objects.NewList(nil)
	Track(a)
	RegisterFinalizer(a, func(objects.Object) {})

	out, err := gcIsFinalized([]objects.Object{a}, nil)
	if err != nil {
		t.Fatalf("is_finalized: %v", err)
	}
	if out != objects.NewBool(false) {
		t.Fatalf("is_finalized before collect = %v, want False", out)
	}

	Collect(2)

	out, err = gcIsFinalized([]objects.Object{a}, nil)
	if err != nil {
		t.Fatalf("is_finalized: %v", err)
	}
	// After Collect, the rooted object survived but its gcHead state
	// depends on whether the finalizer fired. Since a is externally
	// rooted, finalizeGarbage never ran; is_finalized stays False.
	if out != objects.NewBool(false) {
		t.Fatalf("is_finalized after rooted collect = %v, want False", out)
	}
}

func TestIsFinalizedAfterUnreachable(t *testing.T) {
	// Build a cycle that becomes unreachable, run Collect, then check
	// is_finalized. The finalized map is the source of truth after the
	// gcHead has been dropped.
	defer untrackAll()
	a := objects.NewList(nil)
	b := objects.NewList(nil)
	a.Append(b)
	b.Append(a)
	Track(a)
	Track(b)
	objects.Decref(a) // simulate del a, b: Append now owns the cross refs
	objects.Decref(b)

	fired := 0
	RegisterFinalizer(a, func(objects.Object) { fired++ })

	Collect(2)

	if fired != 1 {
		t.Fatalf("finalizer fired %d times, want 1", fired)
	}
	out, err := gcIsFinalized([]objects.Object{a}, nil)
	if err != nil {
		t.Fatalf("is_finalized: %v", err)
	}
	// reclaimUnreachable drops the entry from finalized so a fresh
	// allocation at the same address would not show as finalized.
	if out != objects.NewBool(false) {
		t.Fatalf("is_finalized after reclaim = %v, want False", out)
	}
}

func TestCallbacksFireStartAndStop(t *testing.T) {
	defer untrackAll()
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	cbsObj, err := mod.Dict().GetItem(objects.NewStr("callbacks"))
	if err != nil {
		t.Fatalf("callbacks: %v", err)
	}
	cbs := cbsObj.(*objects.List)

	var phases []string
	cb := objects.NewBuiltinFunction("rec", func(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
		if len(args) != 2 {
			t.Fatalf("callback got %d args, want 2", len(args))
		}
		s, _ := objects.Str(args[0])
		phases = append(phases, s)
		if _, ok := args[1].(*objects.Dict); !ok {
			t.Fatalf("callback info arg = %T, want *Dict", args[1])
		}
		return objects.None(), nil
	})
	cbs.Append(cb)
	defer SetCallbacks(nil)

	Collect(0)

	if len(phases) != 2 || phases[0] != "start" || phases[1] != "stop" {
		t.Fatalf("phases = %v, want [start stop]", phases)
	}
}

func TestCallbacksErrorIgnored(t *testing.T) {
	defer untrackAll()
	mod, err := buildModule()
	if err != nil {
		t.Fatalf("buildModule: %v", err)
	}
	cbsObj, err := mod.Dict().GetItem(objects.NewStr("callbacks"))
	if err != nil {
		t.Fatalf("callbacks: %v", err)
	}
	cbs := cbsObj.(*objects.List)
	defer SetCallbacks(nil)

	bad := objects.NewBuiltinFunction("boom", func([]objects.Object, map[string]objects.Object) (objects.Object, error) {
		return nil, errBoom
	})
	cbs.Append(bad)

	if got := Collect(0); got != 0 {
		t.Fatalf("Collect = %d, want 0", got)
	}
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }
