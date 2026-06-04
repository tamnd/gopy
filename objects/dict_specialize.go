// Hooks the adaptive specializer reads off the dict.
//
// LOAD_GLOBAL, LOAD_ATTR module variant, STORE_SUBSCR_DICT, and
// CONTAINS_OP_DICT all need a couple of small primitives:
//
//   - "Is this exactly a dict?" (subclasses bail out).
//   - "Are all live keys str?" (the unicode fast path is a hard
//     prerequisite for the cheap specialized arms).
//   - "Look up this str key and tell me its index, or DKIX_EMPTY if
//     it's missing entirely."
//   - "Hand me a stable 32-bit version I can stamp into the inline
//     cache, or 0 if the keys layout already invalidated."
//
// Mutations call invalidateKeysVersion to reset the cached version
// to 0 so the next read gets a fresh value and previously cached
// inline-cache entries no longer match.
//
// CPython: Objects/dictobject.c:7150 _PyDict_GetKeysVersionForCurrentState

package objects

// DictKeyAbsent is the index returned by LookupString when the key
// is not in the table. Mirrors CPython's DKIX_EMPTY (-1).
//
// CPython: Objects/dict-common.h:18 DKIX_EMPTY
const DictKeyAbsent = -1

// IsKeysUnicode reports whether every live key in d is exactly *Unicode.
// The unicode-keys fast path is the precondition for any specialized
// dict access.
//
// CPython: Include/internal/pycore_dict.h DK_IS_UNICODE
func (d *Dict) IsKeysUnicode() bool {
	return d.kind == dictKindUnicode || d.kind == dictKindSplit
}

// LookupString runs the str-keyed lookup variant on d. Returns the
// slot index when the key is present and DictKeyAbsent (-1) when it
// is absent. Returns ok=false on a non-str key or non-unicode dict
// kind so the caller bails out instead of pretending to specialize.
//
// CPython: Objects/dictobject.c:1080 _PyDictKeys_StringLookup
func (d *Dict) LookupString(name *Unicode) (idx int, ok bool) {
	if !d.IsKeysUnicode() {
		return 0, false
	}
	h, err := Hash(name)
	if err != nil {
		return 0, false
	}
	got, found, err := d.lookup(h, name)
	if err != nil {
		return 0, false
	}
	if !found {
		return DictKeyAbsent, true
	}
	return got, true
}

// GetKeysVersion returns the current dk_version, allocating a fresh
// one from the global counter if the slot is still 0. Returns 0
// when the global counter has wrapped (the specializer treats this
// as "give up and never specialize").
//
// Split dicts delegate to the SharedKeys version so every sibling
// instance reports the same dk_version: CPython's dk_version is a
// property of PyDictKeysObject, not of the PyDictObject. When a
// sibling extends the shared keys table via insert_split_key, the
// SharedKeys version bumps and every cache stamped against the
// previous version rejects on the next hit, even though no mutation
// happened on this dict.
//
// CPython: Objects/dictobject.c:7150 _PyDict_GetKeysVersionForCurrentState
func (d *Dict) GetKeysVersion() uint32 {
	if d.sharedKeys != nil {
		return d.sharedKeys.version
	}
	if d.keysVersion != 0 {
		return d.keysVersion
	}
	v := allocDictKeysVersion()
	if v == 0 {
		return 0
	}
	d.keysVersion = v
	return v
}

// invalidateKeysVersion is called from every mutation that changes
// the keys layout (insert or delete). The next GetKeysVersion call
// will allocate a fresh version, so any cached inline-cache copy
// stops matching. Event notification is the caller's responsibility:
// each mutation site picks the right DictEvent* and routes through
// notifyDictEvent (see objects/dict_watcher.go), because the version
// reset is the same for ADDED / MODIFIED / DELETED but the event
// payload is not.
//
// CPython: Objects/dictobject.c dk_version invalidation
func (d *Dict) invalidateKeysVersion() {
	d.keysVersion = 0
}

// Mutations returns the watcher mutation counter for d. The Tier-2
// optimizer caps folding once this exceeds
// MaxAllowedGlobalsModifications.
//
// CPython: Python/optimizer_analysis.c:54-59 get_mutations
func (d *Dict) Mutations() uint32 { return d.mutationCount }

// IncrementMutations bumps the watcher mutation counter. The Tier-2
// optimizer's globals watcher callback runs this after invalidating
// dependent executors so the next folding pass sees the bumped count.
//
// CPython: Python/optimizer_analysis.c:62-66 increment_mutations
func (d *Dict) IncrementMutations() { d.mutationCount++ }

// Entries iterates active str-keyed entries, yielding (slotIndex, key,
// value) for each used slot. The Tier-2 globals folder uses this to
// resolve a precomputed slot index back to the live value without
// importing dict internals. ok=false when slotIndex is out of range or
// names a dead slot. The unicode-keys precondition matches the dict
// kind the inline-cache stamp relies on.
//
// CPython: Objects/dictobject.c DK_UNICODE_ENTRIES dictionary entry
// access
func (d *Dict) EntryAt(slot int) (key, value Object, ok bool) {
	if slot < 0 || slot >= len(d.entries) {
		return nil, nil, false
	}
	if !d.slotIsLive(slot) {
		return nil, nil, false
	}
	return d.slotKey(slot), d.slotValue(slot), true
}

// StoreEntryAtName overwrites the value at slot when the slot still
// holds a *Unicode key whose contents equal expectName. Returns false
// when the slot is out of range, holds a non-unicode key, or names a
// different attribute (a delete + re-insert into the same bucket can
// leave the cached index pointing at a stale name). The STORE_ATTR
// inline cache only stamps type_version, so this runtime key check is
// the same safety net CPython relies on inside _STORE_ATTR_WITH_HINT.
//
// Liveness rules differ across modes:
//
//   - Combined mode: the slot must already be live, since combined
//     entries only carry a key when the dict has a value for that key
//     (a fresh slot has no key to compare against). Matches CPython's
//     _STORE_ATTR_WITH_HINT precondition.
//   - Split mode: the shared key is always live in the shared table,
//     but splitValues[slot] may be nil if this instance never set the
//     attribute. The fast store still has to land: a sibling having
//     populated the slot doesn't mean this instance has. Mirrors the
//     "shared key, first write on this instance" branch of
//     dictInsertSplit: bump d.used, append to d.order, fire ADDED.
//
// CPython: Python/bytecodes.c:2583 _STORE_ATTR_WITH_HINT key check
// CPython: Objects/dictobject.c:1832 insert_split_key (split-mode
//
//	first-write bookkeeping)
func (d *Dict) StoreEntryAtName(slot int, expectName string, value Object) bool {
	if slot < 0 || slot >= len(d.entries) {
		return false
	}
	if d.sharedKeys == nil {
		// Combined mode: slot must be live, otherwise we have no key
		// to compare expectName against.
		if !d.slotIsLive(slot) {
			return false
		}
		storedKey := d.slotKey(slot)
		k, ok := storedKey.(*Unicode)
		if !ok {
			return false
		}
		if k.Value() != expectName {
			return false
		}
		old := d.entries[slot].value
		d.entries[slot].value = value
		notifyDictEvent(DictEventModified, d, storedKey, value)
		if old != nil {
			Decref(old)
		}
		return true
	}
	// Split mode: the shared key is always live in d.entries[slot]
	// (shared table); only splitValues[slot] tracks per-instance
	// liveness.
	storedKey := d.slotKey(slot)
	k, ok := storedKey.(*Unicode)
	if !ok {
		return false
	}
	if k.Value() != expectName {
		return false
	}
	prev := d.splitValues[slot]
	d.splitValues[slot] = value
	if prev == nil {
		d.order = append(d.order, slot)
		d.used++
		notifyDictEvent(DictEventAdded, d, storedKey, value)
	} else {
		notifyDictEvent(DictEventModified, d, storedKey, value)
		Decref(prev)
	}
	return true
}

// IsExactDict reports whether o is exactly *Dict (not a subclass).
// The specializer narrows on this before reading dict internals.
//
// CPython: Include/cpython/dictobject.h PyDict_CheckExact
func IsExactDict(o Object) bool {
	d, ok := o.(*Dict)
	if !ok {
		return false
	}
	return d.Type() == DictType
}
