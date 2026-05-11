// Tests for WeakValueDictionary. Pin the Lib/weakref.py:92 surface:
// __setitem__/__getitem__/__delitem__/__contains__, iteration over
// live keys, and the _remove callback dropping entries when the value
// dies.

package weakref

import (
	"testing"

	"github.com/tamnd/gopy/module/gc"
	"github.com/tamnd/gopy/objects"
)

func TestWeakValueDictSetGetDelLen(t *testing.T) {
	d, err := NewWeakValueDict(nil)
	if err != nil {
		t.Fatalf("NewWeakValueDict: %v", err)
	}
	k := objects.NewStr("k")
	v := objects.NewList(nil)
	if err := d.SetItem(k, v); err != nil {
		t.Fatalf("SetItem: %v", err)
	}
	if d.Len() != 1 {
		t.Fatalf("Len = %d, want 1", d.Len())
	}
	got, err := d.GetItem(k)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got != v {
		t.Fatalf("GetItem = %v, want %v", got, v)
	}
	if err := d.DelItem(k); err != nil {
		t.Fatalf("DelItem: %v", err)
	}
	if d.Len() != 0 {
		t.Fatalf("Len after DelItem = %d, want 0", d.Len())
	}
}

func TestWeakValueDictContains(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	k := objects.NewStr("k")
	v := objects.NewList(nil)
	if ok, _ := d.Contains(k); ok {
		t.Fatalf("Contains before insert = true")
	}
	_ = d.SetItem(k, v)
	if ok, err := d.Contains(k); err != nil || !ok {
		t.Fatalf("Contains after insert = %v, %v", ok, err)
	}
}

func TestWeakValueDictGetReturnsDefault(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	k := objects.NewStr("missing")
	def := objects.NewStr("default")
	if got := d.Get(k, def); got != def {
		t.Fatalf("Get(missing) = %v, want default", got)
	}
}

func TestWeakValueDictKeysValuesItems(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	k1 := objects.NewStr("a")
	k2 := objects.NewStr("b")
	v1 := objects.NewList(nil)
	v2 := objects.NewList(nil)
	_ = d.SetItem(k1, v1)
	_ = d.SetItem(k2, v2)

	if got := len(d.Keys()); got != 2 {
		t.Fatalf("Keys() len = %d, want 2", got)
	}
	if got := len(d.Values()); got != 2 {
		t.Fatalf("Values() len = %d, want 2", got)
	}
	items := d.Items()
	if len(items) != 2 {
		t.Fatalf("Items() len = %d, want 2", len(items))
	}
	for _, kv := range items {
		if kv[0] != k1 && kv[0] != k2 {
			t.Fatalf("Items() unexpected key %v", kv[0])
		}
	}
}

func TestWeakValueDictUpdateFromDict(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	src := objects.NewDict()
	v1 := objects.NewList(nil)
	v2 := objects.NewList(nil)
	_ = src.SetItem(objects.NewStr("a"), v1)
	_ = src.SetItem(objects.NewStr("b"), v2)

	if err := d.Update(src); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if d.Len() != 2 {
		t.Fatalf("Len = %d, want 2", d.Len())
	}
}

// TestWeakValueDictDropsDeadValueViaCallback exercises the GC path:
// the value is reclaimed in a cycle collection, the weakref's _remove
// callback fires, and the entry leaves the map.
func TestWeakValueDictDropsDeadValueViaCallback(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	k := objects.NewStr("k")
	v := objects.NewList(nil)
	v.Append(v)
	gc.Track(v)
	_ = d.SetItem(k, v)
	if d.Len() != 1 {
		t.Fatalf("pre-collect Len = %d, want 1", d.Len())
	}

	objects.Decref(v)
	if got := gc.Collect(2); got != 1 {
		t.Fatalf("Collect reclaimed %d, want 1", got)
	}
	if d.Len() != 0 {
		t.Fatalf("post-collect Len = %d, want 0", d.Len())
	}
}

func TestWeakValueDictReplaceOverwritesOldRef(t *testing.T) {
	d, _ := NewWeakValueDict(nil)
	k := objects.NewStr("k")
	v1 := objects.NewList(nil)
	v2 := objects.NewList(nil)
	_ = d.SetItem(k, v1)
	_ = d.SetItem(k, v2)
	got, err := d.GetItem(k)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got != v2 {
		t.Fatalf("GetItem = %v, want v2", got)
	}
	if d.Len() != 1 {
		t.Fatalf("Len = %d, want 1", d.Len())
	}
}
