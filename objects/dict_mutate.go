// Mutation helpers for the dict. dict_lookup.go answers "where does
// this key probe to"; this file answers "make the table reflect the
// answer". Insert may resize, delete leaves a dummy in the probe
// chain so other keys hashing through the slot still resolve, and
// resize compacts the dummies out by rebuilding the table.
//
// CPython: Objects/dictobject.c:1891 insertdict
// CPython: Objects/dictobject.c:2790 delitem_common
// CPython: Objects/dictobject.c:2065 dictresize

package objects

// usableFraction is the load ceiling: a table of n slots holds at
// most 2n/3 live keys before the next insert triggers a resize.
//
// CPython: Objects/dictobject.c:543 USABLE_FRACTION
func usableFraction(n int) int { return (n << 1) / 3 }

// growthRate is the target capacity once a dict crosses its load
// ceiling. 3*used: doubles on growth-without-deletes, but compacts
// when deletes have piled up dummies.
//
// CPython: Objects/dictobject.c:590 GROWTH_RATE
func growthRate(d *Dict) int { return d.used * 3 }

// nextDictPow2 rounds n up to the next power of two, with dictMinSize
// as the floor. Tables are sized in powers of two so the modulo-mask
// probing in dictProbe works.
//
// CPython: Objects/dictobject.c:547 calculate_log2_keysize
func nextDictPow2(n int) int {
	if n < dictMinSize {
		return dictMinSize
	}
	p := dictMinSize
	for p < n {
		p <<= 1
	}
	return p
}

// loadAtCapacity reports whether one more live entry would push the
// table past the 2n/3 load mark. Used by SetItem to decide if the
// next insert needs a resize first.
//
// CPython: Objects/dictobject.c:1799 (insert_combined_dict's dk_usable check)
func loadAtCapacity(used, capacity int) bool { return used >= usableFraction(capacity) }

// dictInsert is the shared insert-or-replace path. The probing
// dispatcher returns either the slot the key is already in (replace)
// or the first reusable slot in the probe chain (insert). Resize
// fires before lookup so the slot we land on is in the new table.
//
// The load check counts active entries plus dummies, matching
// CPython's dk_usable which decrements on insert and only resets at
// resize. Without that, a heavily-deleted dict's probe chains stay
// clogged with dummies and the table never compacts.
//
// In split mode the key lookup uses the shared keys table. If the
// key is already in the shared set we route to insertSplit so the
// per-instance value lands in splitValues; otherwise we materialize
// to combined first and insert normally.
//
// CPython: Objects/dictobject.c:1891 insertdict
// CPython: Objects/dictobject.c:1832 insert_split_key
func dictInsert(d *Dict, h int64, key, value Object) error {
	if d.sharedKeys != nil {
		return dictInsertSplit(d, h, key, value)
	}
	if loadAtCapacity(d.fill, len(d.entries)) {
		if err := dictResize(d, growthRate(d)); err != nil {
			return err
		}
	}
	idx, found, err := d.lookup(h, key)
	if err != nil {
		return err
	}
	slot := &d.entries[idx]
	if found {
		// Install the new value and only then release the old one, so a
		// Decref-triggered __del__ that reads back the same key sees the
		// new value rather than a freed slot. CPython:
		// Objects/dictobject.c:1875 sets ep->me_value to value before
		// the matching Py_DECREF on the replaced pointer.
		oldValue := slot.value
		slot.value = value
		Incref(value)
		notifyDictEvent(DictEventModified, d, key, value)
		if oldValue != nil {
			Decref(oldValue)
		}
		return nil
	}
	if !slot.dummy {
		d.fill++
	}
	*slot = dictEntry{hash: h, key: key, value: value, used: true}
	Incref(value)
	d.order = append(d.order, idx)
	d.used++
	d.structVersion++
	d.downgradeKindOnInsert(key)
	d.invalidateKeysVersion()
	// CPython fires ADDED at the same site once the new entry is in
	// the table (dictobject.c:1806/1869).
	notifyDictEvent(DictEventAdded, d, key, value)
	return nil
}

// dictSetDefault is the single-lookup form behind dict.setdefault. It
// probes once: a hit returns the stored value, a miss inserts dflt at
// the reusable slot the same probe already found, so the key's __hash__
// and __eq__ each run exactly once (Issue #13521). A split dict
// materializes to combined first so the insert lands in the local table.
//
// CPython: Objects/dictobject.c:4400 dict_setdefault_ref_lock_held
func dictSetDefault(d *Dict, h int64, key, dflt Object) (Object, error) {
	if d.sharedKeys != nil {
		d.ensureCombined()
	}
	if loadAtCapacity(d.fill, len(d.entries)) {
		if err := dictResize(d, growthRate(d)); err != nil {
			return nil, err
		}
	}
	idx, found, err := d.lookup(h, key)
	if err != nil {
		return nil, err
	}
	slot := &d.entries[idx]
	if found {
		return slot.value, nil
	}
	if !slot.dummy {
		d.fill++
	}
	*slot = dictEntry{hash: h, key: key, value: dflt, used: true}
	Incref(dflt)
	d.order = append(d.order, idx)
	d.used++
	d.structVersion++
	d.downgradeKindOnInsert(key)
	d.invalidateKeysVersion()
	notifyDictEvent(DictEventAdded, d, key, dflt)
	return dflt, nil
}

// dictInsertSplit handles inserts on a split dict. Three cases:
//
//   - key already in shared set AND already has a value for this
//     instance (splitValues[idx] != nil): replace; fire MODIFIED.
//   - key already in shared set but this instance hasn't set it yet
//     (splitValues[idx] == nil): land the new value in splitValues,
//     append to d.order, bump d.used; fire ADDED.
//   - key not in the shared set OR not a *Unicode (split dicts only
//     hold unicode keys): materialize the dict to combined and route
//     through the regular insert path.
//
// CPython: Objects/dictobject.c:1832 insert_split_key
func dictInsertSplit(d *Dict, h int64, key, value Object) error {
	// insertdict only takes the split fast path for an exact str
	// (PyUnicode_CheckExact). A str subclass such as MyStr matches an
	// existing shared key under __eq__ but must be stored as the subclass
	// instance, so it materializes to combined and inserts there (gh-143189).
	//
	// CPython: Objects/dictobject.c:1898 insertdict
	u, isUnicode := key.(*Unicode)
	if !isUnicode || key.Type() != StrType() {
		d.ensureCombined()
		return dictInsert(d, h, key, value)
	}
	idx, found := d.sharedKeys.LookupKey(u, h)
	if !found {
		// Key not in the shared set. Materialize and insert through
		// the combined path; CPython's insert_split_key would extend
		// the shared keys when refs allow, but a conservative
		// materialize keeps correctness without the multi-instance
		// invalidation dance. The shared keys remain intact for any
		// sibling instances still using them.
		d.ensureCombined()
		return dictInsert(d, h, key, value)
	}
	if d.splitValues[idx] != nil {
		// Install before Decref so a re-entrant lookup from __del__
		// observes the new value, matching the dictInsert replace path.
		oldValue := d.splitValues[idx]
		d.splitValues[idx] = value
		Incref(value)
		notifyDictEvent(DictEventModified, d, key, value)
		Decref(oldValue)
		return nil
	}
	Incref(value)
	d.splitValues[idx] = value
	d.order = append(d.order, idx)
	d.used++
	d.structVersion++
	d.invalidateKeysVersion()
	notifyDictEvent(DictEventAdded, d, key, value)
	return nil
}

// dictDelete removes key. In combined mode the matched slot becomes a
// dummy so probe chains threading through it still resolve; the next
// resize compacts the dummies out. In split mode we just clear
// splitValues[idx]; the shared keys entry stays put so other instances
// pointing at the same SharedKeys still find their values.
//
// CPython: Objects/dictobject.c:2790 delitem_common
func dictDelete(d *Dict, key Object) error {
	h, err := dictKeyHash(key)
	if err != nil {
		return err
	}
	idx, found, err := d.lookup(h, key)
	if err != nil {
		return err
	}
	if !found {
		return errKeyNotFound
	}
	// Clear the entry and update bookkeeping BEFORE running any Decref
	// that may trigger __del__ re-entrancy through Finalize. CPython's
	// delitem_common does the same dance: tombstone the slot, decrement
	// ma_used, bump the version tag, and only then release the old key
	// and value so a re-entrant lookup sees a consistent dict.
	//
	// CPython: Objects/dictobject.c:2853 delitem_common
	var oldKey, oldValue Object
	if d.sharedKeys != nil {
		oldValue = d.splitValues[idx]
		d.splitValues[idx] = nil
	} else {
		oldKey = d.entries[idx].key
		oldValue = d.entries[idx].value
		d.entries[idx] = dictEntry{dummy: true}
	}
	for i, slot := range d.order {
		if slot == idx {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	d.used--
	d.structVersion++
	d.invalidateKeysVersion()
	// CPython fires DELETED from delitem_common (dictobject.c:2872).
	// Pass nil for value (the old value isn't part of the contract).
	notifyDictEvent(DictEventDeleted, d, key, nil)
	if oldKey != nil {
		Decref(oldKey)
	}
	if oldValue != nil {
		Decref(oldValue)
	}
	return nil
}

// dictResize allocates a new table sized for minNew live entries
// (floored at dictMinSize, rounded up to a power of two) and
// reinserts every active slot. Dummies disappear; the kind flag
// stays put since deletion never promotes General back to Unicode.
// fill resets to used since the rebuilt table has no tombstones.
//
// A split dict materializes first: its entries[] borrows the shared
// keys table, and allocating a private table is exactly what
// _PyDict_DetachFromObject does anyway.
//
// CPython: Objects/dictobject.c:2065 dictresize
func dictResize(d *Dict, minNew int) error {
	if d.sharedKeys != nil {
		d.ensureCombined()
	}
	if minNew < d.used {
		minNew = d.used
	}
	oldEntries := d.entries
	oldOrder := d.order
	d.entries = make([]dictEntry, nextDictPow2(minNew))
	d.order = make([]int, 0, len(oldOrder))
	d.used = 0
	d.fill = 0
	for _, slot := range oldOrder {
		e := oldEntries[slot]
		if err := dictReinsert(d, e.hash, e.key, e.value); err != nil {
			return err
		}
	}
	return nil
}

// dictReinsert is the resize-time fast path: the new table is empty
// and was sized for the live count, so no nested resize and no
// replace-vs-insert branch. fill grows in lockstep with used since
// every reinsert lands in a fresh empty slot.
//
// CPython: Objects/dictobject.c:2010 build_indices_generic /
// Objects/dictobject.c:2025 build_indices_unicode
func dictReinsert(d *Dict, h int64, key, value Object) error {
	idx, _, err := d.lookup(h, key)
	if err != nil {
		return err
	}
	d.entries[idx] = dictEntry{hash: h, key: key, value: value, used: true}
	d.order = append(d.order, idx)
	d.used++
	d.fill++
	return nil
}
