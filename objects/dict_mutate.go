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
// CPython: Objects/dictobject.c:1891 insertdict
func dictInsert(d *Dict, h int64, key, value Object) error {
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
		slot.value = value
		return nil
	}
	if !slot.dummy {
		d.fill++
	}
	*slot = dictEntry{hash: h, key: key, value: value, used: true}
	d.used++
	d.downgradeKindOnInsert(key)
	d.invalidateKeysVersion()
	return nil
}

// dictDelete removes key. The matched slot becomes a dummy so probe
// chains threading through it still resolve; the next resize compacts
// the dummies out.
//
// CPython: Objects/dictobject.c:2790 delitem_common
func dictDelete(d *Dict, key Object) error {
	h, err := Hash(key)
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
	d.entries[idx] = dictEntry{dummy: true}
	d.used--
	d.invalidateKeysVersion()
	return nil
}

// dictResize allocates a new table sized for minNew live entries
// (floored at dictMinSize, rounded up to a power of two) and
// reinserts every active slot. Dummies disappear; the kind flag
// stays put since deletion never promotes General back to Unicode.
// fill resets to used since the rebuilt table has no tombstones.
//
// CPython: Objects/dictobject.c:2065 dictresize
func dictResize(d *Dict, minNew int) error {
	if minNew < d.used {
		minNew = d.used
	}
	old := d.entries
	d.entries = make([]dictEntry, nextDictPow2(minNew))
	d.used = 0
	d.fill = 0
	for i := range old {
		e := old[i]
		if !e.used {
			continue
		}
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
	d.used++
	d.fill++
	return nil
}
