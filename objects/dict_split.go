// Split-keys storage for instance __dict__. CPython lays out the
// keys and hashes in a shared PyDictKeysObject that every instance
// of a class points at; each instance only carries a per-instance
// values[] slice. When an unusual operation happens (a key not in
// the shared set) the dict is "materialized" back to a combined
// layout where keys and values live together.
//
// The real storage win is: ten instances of the same class with the
// same five attributes share one keys table; only the values are
// duplicated. CPython manages it through PyDictValues + PyDictKeysObject;
// gopy mirrors the same split into SharedKeys (the keys table) and
// Dict.splitValues (the per-instance value array).
//
// CPython: Objects/dictobject.c:567 PyDictKeysObject
// CPython: Objects/dictobject.c:897 new_dict_with_shared_keys
// CPython: Objects/dictobject.c:1832 insert_split_key
// CPython: Objects/dictobject.c:6853 make_dict_from_instance_attributes
// CPython: Objects/dictobject.c:7530 _PyDict_DetachFromObject

package objects

// SharedKeys is the shared keys table that every instance of a class
// points at. It owns the same probing layout a combined dict carries
// (entries, order, used, fill, version) but the .value field on each
// entry is unused: per-instance values live in Dict.splitValues
// keyed by the same slot index.
//
// SharedKeys grows monotonically: insert_split_key extends the table
// when a previously-unseen attribute name shows up on any instance.
// Refs counts how many split dicts reference us; ConvertToCombined
// drops the ref and once it hits zero the SharedKeys is unreachable.
//
// CPython: Objects/dictobject.c:567 PyDictKeysObject (DICT_KEYS_SPLIT)
type SharedKeys struct {
	entries []dictEntry // hash + key per slot; .value is unused
	order   []int       // insertion order of slots that hold a key
	used    int         // count of slots in order (== number of shared keys)
	fill    int         // active + dummy slot count, for the load gate
	version uint32      // bumped on every keys-layout change
	refs    int         // number of split-dicts referencing this
}

// NewSharedKeys builds a SharedKeys seeded with the given attribute
// names. Each name is inserted through the same probing path a
// combined dict uses, so probe collisions on a SharedKeys behave the
// same as on a regular dict.
//
// CPython: Objects/dictobject.c:625 new_keys_object (with split flag)
func NewSharedKeys(names []string) (*SharedKeys, error) {
	sk := &SharedKeys{
		entries: make([]dictEntry, nextDictPow2(len(names))),
	}
	for _, name := range names {
		key := NewStr(name).(*Unicode)
		h, err := Hash(key)
		if err != nil {
			return nil, err
		}
		if loadAtCapacity(sk.fill, len(sk.entries)) {
			sk.resize(growthRateShared(sk))
		}
		idx, found := sharedProbe(sk, key, h)
		if found {
			continue
		}
		sk.entries[idx] = dictEntry{hash: h, key: key, used: true}
		sk.order = append(sk.order, idx)
		sk.used++
		sk.fill++
		sk.version = allocDictKeysVersion()
	}
	return sk, nil
}

// growthRateShared mirrors growthRate but reads off SharedKeys.used.
func growthRateShared(sk *SharedKeys) int { return sk.used * 3 }

// NewEmptySharedKeys returns a SharedKeys pre-sized to hold up to
// dictMinSize attribute names without ever needing to reallocate the
// entries slice. The fixed allocation matters: NewSplitDict copies
// sk.entries's slice header into d.entries, so a later resize would
// leave attached dicts pointing at the stale backing array. Callers
// that need a larger table must seed via NewSharedKeys before any
// dict attaches.
func NewEmptySharedKeys() *SharedKeys {
	return &SharedKeys{entries: make([]dictEntry, dictMinSize)}
}

// AddKey inserts name into the shared table without ever resizing.
// Returns true when the name landed (either already present or
// inserted into an empty slot), false when the table is full and the
// caller must fall back to materialize-on-new-key. The no-resize
// invariant keeps sk.entries's backing array stable for any
// NewSplitDict that has already shared the slice header.
//
// CPython: Objects/dictobject.c:1832 insert_split_key (the in-place
// insertion path when dk_usable > 0; gopy adds the explicit cap
// instead of CPython's dk_usable countdown).
func (sk *SharedKeys) AddKey(name string) bool {
	key := NewStr(name).(*Unicode)
	h, err := Hash(key)
	if err != nil {
		return false
	}
	idx, found := sharedProbe(sk, key, h)
	if found {
		return true
	}
	if loadAtCapacity(sk.fill, len(sk.entries)) {
		return false
	}
	sk.entries[idx] = dictEntry{hash: h, key: key, used: true}
	sk.order = append(sk.order, idx)
	sk.used++
	sk.fill++
	sk.version = allocDictKeysVersion()
	return true
}

// sharedProbe is the SharedKeys-flavored dictProbe. It can't call the
// regular probe because that one reads d.entries; here entries live on
// the SharedKeys directly and the equality check is unicode-only since
// SharedKeys only ever holds *Unicode names.
//
// CPython: Objects/dictobject.c:1160 unicodekeys_lookup_split
func sharedProbe(sk *SharedKeys, key *Unicode, hash int64) (int, bool) {
	mask := uint64(len(sk.entries) - 1)
	i := uint64(hash) & mask
	perturb := uint64(hash)
	freeSlot := -1
	for {
		slot := &sk.entries[i]
		if !slot.used {
			if slot.dummy {
				if freeSlot < 0 {
					freeSlot = int(i)
				}
			} else {
				if freeSlot >= 0 {
					return freeSlot, false
				}
				return int(i), false
			}
		} else {
			if stored, ok := slot.key.(*Unicode); ok {
				if stored == key {
					return int(i), true
				}
				if slot.hash == hash && stored.v == key.v {
					return int(i), true
				}
			}
		}
		perturb >>= 5
		i = (5*i + 1 + perturb) & mask
	}
}

// resize grows the SharedKeys entries table to hold at least minNew
// live keys. Existing keys are re-probed into the new table. The
// version bumps so any split dict whose cached state pointed at the
// old layout knows to re-derive.
//
// Resizing requires all split-dicts sharing this SharedKeys to also
// migrate their splitValues arrays. CPython handles that by stamping
// dk_version; gopy makes split-dict callers consult LookupKey before
// reaching into splitValues, so a stale-slot read after a resize
// returns "key not found" and we re-probe.
//
// CPython: Objects/dictobject.c:2065 dictresize
func (sk *SharedKeys) resize(minNew int) {
	if minNew < sk.used {
		minNew = sk.used
	}
	old := sk.entries
	oldOrder := sk.order
	sk.entries = make([]dictEntry, nextDictPow2(minNew))
	sk.order = sk.order[:0]
	sk.used = 0
	sk.fill = 0
	for _, slot := range oldOrder {
		e := old[slot]
		key, _ := e.key.(*Unicode)
		idx, _ := sharedProbe(sk, key, e.hash)
		sk.entries[idx] = dictEntry{hash: e.hash, key: e.key, used: true}
		sk.order = append(sk.order, idx)
		sk.used++
		sk.fill++
	}
	sk.version = allocDictKeysVersion()
}

// Len returns the number of shared keys.
func (sk *SharedKeys) Len() int { return sk.used }

// Version is the current dk_version for the shared table; bumps any
// time the keys layout changes.
//
// CPython: Include/internal/pycore_dict.h dk_version
func (sk *SharedKeys) Version() uint32 { return sk.version }

// HasKey reports whether name is one of the shared keys. Cheap probe
// for callers that only need a yes/no answer.
func (sk *SharedKeys) HasKey(name string) bool {
	key := NewStr(name).(*Unicode)
	h, err := Hash(key)
	if err != nil {
		return false
	}
	_, found := sharedProbe(sk, key, h)
	return found
}

// LookupKey runs the unicode-only probe and returns (slot, found).
// Used by Dict.lookup when in split mode to find the slot index a
// per-instance value lives at.
//
// CPython: Objects/dictobject.c:1160 unicodekeys_lookup_split
func (sk *SharedKeys) LookupKey(key *Unicode, hash int64) (int, bool) {
	return sharedProbe(sk, key, hash)
}

// NewSplitDict builds a fresh dict whose key table is borrowed from
// sk. The dict has no values yet (splitValues is allocated to match
// sk's entry count, all entries nil). d.entries shares its backing
// array with sk.entries so probing through d.entries returns the
// same slot indices the shared table sees.
//
// CPython: Objects/dictobject.c:897 new_dict_with_shared_keys
func NewSplitDict(sk *SharedKeys) *Dict {
	d := &Dict{
		entries:     sk.entries,
		kind:        dictKindSplit,
		sharedKeys:  sk,
		splitValues: make([]Object, len(sk.entries)),
	}
	d.init(DictType)
	sk.refs++
	return d
}

// IsSplit reports whether d is still in split-keys mode (i.e. it
// hasn't been converted to combined storage yet).
func (d *Dict) IsSplit() bool { return d.kind == dictKindSplit && d.sharedKeys != nil }

// ensureCombined materializes a split dict into combined form. After
// this returns the dict owns its own entries[] and the SharedKeys'
// refs has dropped by one. Callers invoke this before any mutation
// that the split shape can't represent (insert of a new key, delete,
// resize, etc.).
//
// CPython: Objects/dictobject.c:7530 _PyDict_DetachFromObject
func (d *Dict) ensureCombined() {
	if d.sharedKeys == nil {
		return
	}
	sk := d.sharedKeys
	// Copy keys out of the shared table into a private entries[] for
	// d. Only slots whose splitValues are non-nil are actually live
	// for this instance; the rest become empty.
	newEntries := make([]dictEntry, len(sk.entries))
	newOrder := make([]int, 0, d.used)
	used := 0
	fill := 0
	for _, slot := range sk.order {
		v := d.splitValues[slot]
		if v == nil {
			continue
		}
		newEntries[slot] = dictEntry{
			hash:  sk.entries[slot].hash,
			key:   sk.entries[slot].key,
			value: v,
			used:  true,
		}
		newOrder = append(newOrder, slot)
		used++
		fill++
	}
	d.entries = newEntries
	d.order = newOrder
	d.used = used
	d.fill = fill
	d.kind = dictKindUnicode
	d.splitValues = nil
	sk.refs--
	d.sharedKeys = nil
}

// ConvertToCombined is the public form of ensureCombined. Used by
// tests and by external code paths (e.g. _PyDict_DetachFromObject)
// that need to force materialization.
//
// CPython: Objects/dictobject.c:7530 _PyDict_DetachFromObject
func (d *Dict) ConvertToCombined() { d.ensureCombined() }

// slotValue returns the live value stored at slot idx, or nil when
// the slot is unset. In combined mode that's d.entries[idx].value;
// in split mode it's d.splitValues[idx]. Returns nil when idx is
// out of range so callers can use the same nil-check for both
// "slot empty" and "out of bounds".
func (d *Dict) slotValue(idx int) Object {
	if d.sharedKeys != nil {
		if idx < 0 || idx >= len(d.splitValues) {
			return nil
		}
		return d.splitValues[idx]
	}
	if idx < 0 || idx >= len(d.entries) {
		return nil
	}
	return d.entries[idx].value
}

// slotIsLive reports whether slot idx carries a live value for this
// dict. In combined mode that's entries[idx].used. In split mode it's
// splitValues[idx] != nil (the shared keys table may have the key,
// but this instance hasn't set it).
func (d *Dict) slotIsLive(idx int) bool {
	if d.sharedKeys != nil {
		return idx >= 0 && idx < len(d.splitValues) && d.splitValues[idx] != nil
	}
	return idx >= 0 && idx < len(d.entries) && d.entries[idx].used
}

// slotKey returns the key stored at slot idx. Same in both modes
// since d.entries shares its backing array with sharedKeys.entries
// while split.
func (d *Dict) slotKey(idx int) Object {
	if idx < 0 || idx >= len(d.entries) {
		return nil
	}
	return d.entries[idx].key
}
