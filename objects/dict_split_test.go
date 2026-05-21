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
	// Each name should round-trip through the probe back to a hit.
	for _, name := range []string{"x", "y", "z"} {
		key := NewStr(name).(*Unicode)
		h, _ := Hash(key)
		idx, found := sk.LookupKey(key, h)
		if !found {
			t.Errorf("LookupKey(%q) miss", name)
			continue
		}
		stored := sk.entries[idx].key.(*Unicode)
		if stored.v != name {
			t.Errorf("LookupKey(%q) returned slot holding %q", name, stored.v)
		}
		if sk.entries[idx].hash != h {
			t.Errorf("slot for %q has hash %d, want %d", name, sk.entries[idx].hash, h)
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

func TestNewSplitDictStartsEmptyWithSharedKeys(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x", "y"})
	d := NewSplitDict(sk)
	if !d.IsSplit() {
		t.Error("freshly built split dict should report IsSplit=true")
	}
	if sk.refs != 1 {
		t.Errorf("sharedKeys.refs = %d, want 1", sk.refs)
	}
	// A fresh split dict has no values, so Contains is false for every
	// shared name until that instance sets it.
	for _, name := range []string{"x", "y"} {
		ok, err := d.Contains(NewStr(name))
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("fresh split dict should not Contains %q before any store", name)
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
	if !d.IsSplit() {
		t.Error("SetItem of an existing shared key should keep the dict split")
	}
	got, err := d.GetItem(NewStr("x"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 7 {
		t.Errorf("split dict x = %d, want 7", v)
	}
}

func TestSplitDictWriteIsolatedAcrossInstances(t *testing.T) {
	// Two split dicts pointing at the same SharedKeys must not see
	// each other's value writes; that's the whole point of splitValues.
	sk, _ := NewSharedKeys([]string{"x"})
	a := NewSplitDict(sk)
	b := NewSplitDict(sk)
	if err := a.SetItem(NewStr("x"), NewInt(1)); err != nil {
		t.Fatal(err)
	}
	if err := b.SetItem(NewStr("x"), NewInt(2)); err != nil {
		t.Fatal(err)
	}
	av, _ := a.GetItem(NewStr("x"))
	bv, _ := b.GetItem(NewStr("x"))
	avi, _ := av.(*Int).Int64()
	bvi, _ := bv.(*Int).Int64()
	if avi != 1 || bvi != 2 {
		t.Errorf("expected a=1 b=2, got a=%d b=%d", avi, bvi)
	}
}

func TestSplitDictInsertOfNewKeyMaterializes(t *testing.T) {
	// A key not in the shared set forces materialization to combined.
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	if err := d.SetItem(NewStr("y"), NewInt(2)); err != nil {
		t.Fatal(err)
	}
	if d.IsSplit() {
		t.Error("inserting a non-shared key should materialize the dict")
	}
	if sk.refs != 0 {
		t.Errorf("sharedKeys.refs = %d after materialize, want 0", sk.refs)
	}
	got, err := d.GetItem(NewStr("y"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 2 {
		t.Errorf("y = %d, want 2", v)
	}
}

func TestSplitDictDeleteStaysSplit(t *testing.T) {
	// CPython 3.14 still allows split dicts to track per-instance
	// deletes through ma_values[i] = NULL; we mirror that.
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	_ = d.SetItem(NewStr("x"), NewInt(7))
	if err := d.DelItem(NewStr("x")); err != nil {
		t.Fatal(err)
	}
	if !d.IsSplit() {
		t.Error("delete on a still-shared key should keep the dict split")
	}
	if d.Len() != 0 {
		t.Errorf("len = %d after delete, want 0", d.Len())
	}
	if ok, _ := d.Contains(NewStr("x")); ok {
		t.Error("deleted key should not Contains")
	}
}

func TestConvertToCombinedCopiesValuesAndDropsRef(t *testing.T) {
	sk, _ := NewSharedKeys([]string{"x"})
	d := NewSplitDict(sk)
	_ = d.SetItem(NewStr("x"), NewInt(42))
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
	// The value survives the materialization.
	got, err := d.GetItem(NewStr("x"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.(*Int).Int64(); v != 42 {
		t.Errorf("after convert x = %d, want 42", v)
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

func TestSharedKeysAddKeyInPlace(t *testing.T) {
	sk := NewEmptySharedKeys()
	if !sk.AddKey("first") {
		t.Fatal("AddKey(first) should land")
	}
	if !sk.AddKey("second") {
		t.Fatal("AddKey(second) should land")
	}
	// Re-adding an existing name is a hit, not an error.
	if !sk.AddKey("first") {
		t.Error("AddKey of an existing name should report true")
	}
	if sk.Len() != 2 {
		t.Errorf("len after dedup = %d, want 2", sk.Len())
	}
	// Probing should round-trip both names.
	for _, name := range []string{"first", "second"} {
		if !sk.HasKey(name) {
			t.Errorf("HasKey(%q) = false", name)
		}
	}
}

func TestSharedKeysAddKeyStopsAtCapacity(t *testing.T) {
	sk := NewEmptySharedKeys()
	// dictMinSize = 8, usableFraction(8) = 5. Beyond that AddKey must
	// refuse rather than resize (resize would reallocate sk.entries
	// and orphan any attached split dicts).
	added := 0
	for i := range 32 {
		if sk.AddKey(string(rune('a' + i))) {
			added++
		}
	}
	if added > usableFraction(dictMinSize) {
		t.Errorf("AddKey added %d names, expected <= %d", added, usableFraction(dictMinSize))
	}
	if len(sk.entries) != dictMinSize {
		t.Errorf("sk.entries grew to len %d after AddKey loop, expected %d", len(sk.entries), dictMinSize)
	}
}

func TestTypeAddCachedKeyExtendsSharedKeys(t *testing.T) {
	tp := NewType("UserCls", []*Type{objectType})
	tp.HasDict = true
	tp.TpFlags |= TpFlagInlineValues | TpFlagManagedDict

	tp.AddCachedKey("alpha")
	tp.AddCachedKey("beta")

	if tp.SharedKeys() == nil {
		t.Fatal("AddCachedKey should have allocated SharedKeys")
	}
	if !tp.SharedKeys().HasKey("alpha") || !tp.SharedKeys().HasKey("beta") {
		t.Error("SharedKeys missing names AddCachedKey recorded")
	}
}

func TestNewInstanceSharesKeysAcrossSiblings(t *testing.T) {
	tp := NewType("Sibling", []*Type{objectType})
	tp.HasDict = true
	tp.TpFlags |= TpFlagInlineValues | TpFlagManagedDict
	tp.AddCachedKey("x")
	tp.AddCachedKey("y")

	a := NewInstance(tp)
	b := NewInstance(tp)

	if !a.Dict().IsSplit() || !b.Dict().IsSplit() {
		t.Fatalf("both instance dicts should be split: a=%v b=%v",
			a.Dict().IsSplit(), b.Dict().IsSplit())
	}
	if a.Dict().sharedKeys != b.Dict().sharedKeys {
		t.Error("sibling instances should point at the same SharedKeys")
	}
	if a.Dict().sharedKeys.refs != 2 {
		t.Errorf("after two instances refs = %d, want 2", a.Dict().sharedKeys.refs)
	}
	// Writes through GenericSetAttr must stay isolated even though
	// the keys table is shared.
	if err := a.Dict().SetItem(NewStr("x"), NewInt(11)); err != nil {
		t.Fatal(err)
	}
	if err := b.Dict().SetItem(NewStr("x"), NewInt(22)); err != nil {
		t.Fatal(err)
	}
	av, _ := a.Dict().GetItem(NewStr("x"))
	bv, _ := b.Dict().GetItem(NewStr("x"))
	avi, _ := av.(*Int).Int64()
	bvi, _ := bv.(*Int).Int64()
	if avi != 11 || bvi != 22 {
		t.Errorf("expected a.x=11 b.x=22, got a.x=%d b.x=%d", avi, bvi)
	}
}

func TestSplitDictKeysVersionShared(t *testing.T) {
	// Two split dicts pointing at the same SharedKeys must report the
	// same dk_version. CPython's dk_version lives on PyDictKeysObject,
	// not on PyDictObject; an inline cache stamped on one sibling has
	// to invalidate when another sibling extends the shared table.
	sk, _ := NewSharedKeys([]string{"x"})
	a := NewSplitDict(sk)
	b := NewSplitDict(sk)
	va := a.GetKeysVersion()
	vb := b.GetKeysVersion()
	if va == 0 || vb == 0 {
		t.Fatalf("split dicts must report non-zero keys version, got a=%d b=%d", va, vb)
	}
	if va != vb {
		t.Errorf("split siblings should share dk_version, got a=%d b=%d", va, vb)
	}
	if va != sk.Version() {
		t.Errorf("split dict version %d should equal sharedKeys.Version() %d", va, sk.Version())
	}
	// Extending the shared table must bump the version that every
	// sibling sees, so inline caches stamped against the old version
	// reject.
	before := va
	sk.AddKey("z")
	if a.GetKeysVersion() == before {
		t.Error("AddKey on sharedKeys should bump the version siblings see")
	}
	if a.GetKeysVersion() != b.GetKeysVersion() {
		t.Error("siblings must still agree on the version after AddKey")
	}
}

func TestSplitDictReadsThroughEntryAt(t *testing.T) {
	// The LOAD_ATTR_INSTANCE_VALUE fast arm reads through Dict.EntryAt;
	// EntryAt has to route through the split-aware slotValue accessor
	// so a split dict's value array shows up. Without this the fast
	// arm would read d.entries[slot].value, which is always nil for
	// split dicts (values live on splitValues, not entries).
	sk, _ := NewSharedKeys([]string{"x", "y"})
	d := NewSplitDict(sk)
	if err := d.SetItem(NewStr("x"), NewInt(42)); err != nil {
		t.Fatal(err)
	}
	idx, ok := sk.LookupKey(NewStr("x").(*Unicode), mustHash(t, NewStr("x")))
	if !ok {
		t.Fatal("LookupKey(x) miss")
	}
	key, value, found := d.EntryAt(idx)
	if !found {
		t.Fatal("EntryAt should report live for the set slot")
	}
	if u, ok := key.(*Unicode); !ok || u.v != "x" {
		t.Errorf("EntryAt key = %v, want x", key)
	}
	if v, _ := value.(*Int).Int64(); v != 42 {
		t.Errorf("EntryAt value = %v, want 42", v)
	}
	// The "y" slot is shared but this instance never set it; EntryAt
	// must report not-found so the fast arm bails to the slow path.
	idxY, _ := sk.LookupKey(NewStr("y").(*Unicode), mustHash(t, NewStr("y")))
	if _, _, found := d.EntryAt(idxY); found {
		t.Error("EntryAt on an unset split slot must report not-found")
	}
}

func TestSplitDictWritesThroughStoreEntryAtName(t *testing.T) {
	// STORE_ATTR_INSTANCE_VALUE / STORE_ATTR_WITH_HINT route through
	// Dict.StoreEntryAtName; the write must land in splitValues, not
	// in entries[slot].value (which the split dict shares with every
	// sibling).
	sk, _ := NewSharedKeys([]string{"x"})
	a := NewSplitDict(sk)
	b := NewSplitDict(sk)
	idx, _ := sk.LookupKey(NewStr("x").(*Unicode), mustHash(t, NewStr("x")))
	if !a.StoreEntryAtName(idx, "x", NewInt(7)) {
		t.Fatal("StoreEntryAtName on a should land")
	}
	if !b.StoreEntryAtName(idx, "x", NewInt(8)) {
		t.Fatal("StoreEntryAtName on b should land")
	}
	_, av, _ := a.EntryAt(idx)
	_, bv, _ := b.EntryAt(idx)
	avi, _ := av.(*Int).Int64()
	bvi, _ := bv.(*Int).Int64()
	if avi != 7 || bvi != 8 {
		t.Errorf("StoreEntryAtName isolation broken: a=%d b=%d", avi, bvi)
	}
}

func TestNewInstanceFirstInstanceStillCombined(t *testing.T) {
	// Before any AddCachedKey has run, NewInstance has nothing to seed
	// the SharedKeys with, so the dict has to start combined. The first
	// SetAttr will later populate cachedKeys, and the *next* instance of
	// the same type picks up the split shape. This matches CPython's
	// observation that the very first object materializes the
	// SharedKeys for subsequent siblings.
	tp := NewType("FreshCls", []*Type{objectType})
	tp.HasDict = true
	tp.TpFlags |= TpFlagInlineValues | TpFlagManagedDict

	a := NewInstance(tp)
	if a.Dict().IsSplit() {
		t.Error("first instance with no cached keys should not be split")
	}
}
