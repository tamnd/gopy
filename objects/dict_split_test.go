package objects

import "testing"

func TestNewSharedKeysCachesHashes(t *testing.T) {
	sk, err := NewSharedKeys([]string{"x", "y", "z"})
	if err != nil {
		t.Fatal(err)
	}
	if sk.Len() != 3 {
		t.Errorf("len = %d, want 3", sk.Len())
	}
	for i, name := range []string{"x", "y", "z"} {
		want, err := Hash(NewStr(name))
		if err != nil {
			t.Fatal(err)
		}
		if sk.hashes[i] != want {
			t.Errorf("hash[%d] = %d, want %d", i, sk.hashes[i], want)
		}
	}
}

func TestSharedKeysHasKey(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x", "y"})
	if !sk.HasKey("x") {
		t.Error("HasKey(x) should be true")
	}
	if sk.HasKey("z") {
		t.Error("HasKey(z) should be false")
	}
}

func TestNewSplitDictPrePopulatesAndCountsRefs(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x", "y"})
	d := NewSplitDict(sk)
	if !d.IsSplit() {
		t.Error("freshly built split dict should report IsSplit=true")
	}
	if sk.refs != 1 {
		t.Errorf("sharedKeys.refs = %d, want 1", sk.refs)
	}
	for _, name := range []string{"x", "y"} {
		ok, err := d.Contains(NewStr(name))
		if err != nil || !ok {
			t.Errorf("split dict missing pre-loaded key %q", name)
		}
	}
	d2 := NewSplitDict(sk)
	if sk.refs != 2 {
		t.Errorf("after second split: refs = %d, want 2", sk.refs)
	}
	_ = d2
}

func TestSplitDictReadsAndWritesShared(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	if err := d.SetItem(NewStr("x"), NewInt(7)); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetItem(NewStr("x"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 7 {
		t.Errorf("split dict x = %d, want 7", v)
	}
}

func TestConvertToCombinedReleasesRef(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	if !d.IsSplit() {
		t.Fatal("expected split before convert")
	}
	d.ConvertToCombined()
	if d.IsSplit() {
		t.Error("IsSplit should be false after ConvertToCombined")
	}
	if sk.refs != 0 {
		t.Errorf("sharedKeys.refs = %d after convert, want 0", sk.refs)
	}
}

func TestConvertToCombinedIdempotent(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	d.ConvertToCombined()
	d.ConvertToCombined() // second call is a no-op
	if sk.refs != 0 {
		t.Errorf("refs = %d after double-convert, want 0", sk.refs)
	}
}
