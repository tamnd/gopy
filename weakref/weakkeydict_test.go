// Tests for WeakKeyDictionary. Pin the Lib/weakref.py:298 surface and
// the _remove callback path.

package weakref

import (
	"testing"

	"github.com/tamnd/gopy/gc"
	"github.com/tamnd/gopy/objects"
)

func TestWeakKeyDictSetGetDelLen(t *testing.T) {
	d, err := NewWeakKeyDict(nil)
	if err != nil {
		t.Fatalf("NewWeakKeyDict: %v", err)
	}
	k := objects.NewList(nil)
	v := objects.NewStr("v")
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
		t.Fatalf("GetItem = %v, want v", got)
	}
	if err := d.DelItem(k); err != nil {
		t.Fatalf("DelItem: %v", err)
	}
	if d.Len() != 0 {
		t.Fatalf("Len after DelItem = %d, want 0", d.Len())
	}
}

func TestWeakKeyDictReplaceOnSameKey(t *testing.T) {
	d, _ := NewWeakKeyDict(nil)
	k := objects.NewList(nil)
	_ = d.SetItem(k, objects.NewStr("v1"))
	_ = d.SetItem(k, objects.NewStr("v2"))
	if d.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after replace", d.Len())
	}
	got, _ := d.GetItem(k)
	s, err := objects.Str(got)
	if err != nil {
		t.Fatalf("Str: %v", err)
	}
	if s != "v2" {
		t.Fatalf("GetItem = %q, want v2", s)
	}
}

func TestWeakKeyDictContainsAndGet(t *testing.T) {
	d, _ := NewWeakKeyDict(nil)
	k := objects.NewList(nil)
	def := objects.NewStr("default")
	if got := d.Get(k, def); got != def {
		t.Fatalf("Get(missing) = %v, want default", got)
	}
	_ = d.SetItem(k, objects.NewStr("v"))
	if ok, _ := d.Contains(k); !ok {
		t.Fatalf("Contains(k) = false")
	}
}

func TestWeakKeyDictKeysValuesItems(t *testing.T) {
	d, _ := NewWeakKeyDict(nil)
	k1 := objects.NewList(nil)
	k1.Append(objects.NewInt(1))
	k2 := objects.NewList(nil)
	k2.Append(objects.NewInt(2))
	_ = d.SetItem(k1, objects.NewInt(1))
	_ = d.SetItem(k2, objects.NewInt(2))
	if got := len(d.Keys()); got != 2 {
		t.Fatalf("Keys() len = %d, want 2", got)
	}
	if got := len(d.Values()); got != 2 {
		t.Fatalf("Values() len = %d, want 2", got)
	}
	if got := len(d.Items()); got != 2 {
		t.Fatalf("Items() len = %d, want 2", got)
	}
}

func TestWeakKeyDictUpdateFromDict(t *testing.T) {
	d, _ := NewWeakKeyDict(nil)
	src := objects.NewDict()
	k1 := objects.NewStr("a")
	k2 := objects.NewStr("b")
	_ = src.SetItem(k1, objects.NewStr("a"))
	_ = src.SetItem(k2, objects.NewStr("b"))
	if err := d.Update(src); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if d.Len() != 2 {
		t.Fatalf("Len = %d, want 2", d.Len())
	}
}

// TestWeakKeyDictDropsDeadKeyViaCallback exercises the gc weakref-
// callback path for keys.
func TestWeakKeyDictDropsDeadKeyViaCallback(t *testing.T) {
	d, _ := NewWeakKeyDict(nil)
	k := objects.NewList(nil)
	k.Append(k)
	gc.Track(k)
	_ = d.SetItem(k, objects.NewStr("v"))
	if d.Len() != 1 {
		t.Fatalf("pre-collect Len = %d, want 1", d.Len())
	}

	objects.Decref(k)
	if got := gc.Collect(2); got != 1 {
		t.Fatalf("Collect reclaimed %d, want 1", got)
	}
	if d.Len() != 0 {
		t.Fatalf("post-collect Len = %d, want 0", d.Len())
	}
}
