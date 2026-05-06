// Lookup variants for the dict. CPython's _Py_dict_lookup picks one of
// four inner closures based on the dict's key-kind flag and the type
// of the lookup key. gopy mirrors the same dispatch over its simpler
// open-addressed table: the probing loop is shared, the per-slot
// equality check is what varies.
//
// CPython: Objects/dictobject.c:1247 _Py_dict_lookup

package objects

import "fmt"

// dictKind labels which lookup variant the dict's keys are eligible
// for. Mirrors CPython's DictKeysKind: General means at least one
// non-str key has been inserted, Unicode means every live key is str
// (so the unicode-only fast paths apply), Split is the instance-dict
// shared-keys layout that gopy hasn't ported yet but the dispatch
// still has to recognize.
//
// CPython: Objects/dict-common.h:23 DictKeysKind
type dictKind uint8

const (
	dictKindUnicode dictKind = iota // all live keys are *strStub
	dictKindGeneral                 // at least one non-str key
	dictKindSplit                   // split-keys (instance dict); not yet implemented
)

// dictCompare is the per-slot equality check the probing loop calls.
// Returns (matched, error). The probing driver halts on a match or on
// an empty slot.
type dictCompare func(d *Dict, slot *dictEntry, key Object, hash int64) (bool, error)

// compareUnicodeUnicode is the fast path: both stored and lookup keys
// are str, so identity or string-equality decides without invoking
// RichCompare. CPython doesn't even compare hashes here because the
// unicode_eq check is cheap once you've narrowed the type.
//
// CPython: Objects/dictobject.c:1080 compare_unicode_unicode
func compareUnicodeUnicode(_ *Dict, slot *dictEntry, key Object, hash int64) (bool, error) {
	stored := slot.key.(*strStub)
	lookup := key.(*strStub)
	if stored == lookup {
		return true, nil
	}
	if slot.hash != hash {
		return false, nil
	}
	return stored.v == lookup.v, nil
}

// compareUnicodeGeneric is the path used when the table holds only
// str keys but the lookup key is something else. Hash filter first,
// then a generic RichCompare since the lookup key may have a custom
// __eq__ that compares equal to a string (e.g. an int subclass that
// overrides equality).
//
// CPython: Objects/dictobject.c:1045 compare_unicode_generic
func compareUnicodeGeneric(_ *Dict, slot *dictEntry, key Object, hash int64) (bool, error) {
	if slot.hash != hash {
		return false, nil
	}
	return RichCmpBool(slot.key, key, CompareEQ)
}

// compareGeneric is the catch-all the dispatcher reaches when the
// table has any non-str key. Identity short-circuit first (CPython
// does this as an optimization for interned strings, mutable keys
// keyed by id, etc.), then hash filter, then RichCompare.
//
// CPython: Objects/dictobject.c:1101 compare_generic
func compareGeneric(_ *Dict, slot *dictEntry, key Object, hash int64) (bool, error) {
	if slot.key == key {
		return true, nil
	}
	if slot.hash != hash {
		return false, nil
	}
	return RichCmpBool(slot.key, key, CompareEQ)
}

// compareSplit is the slot check for split-keys tables. The split
// layout stores per-instance values in a parallel array while
// sharing the keys with the type's dict; lookups still match on
// stored str keys, so the comparator is the same shape as
// compareUnicodeUnicode. The dispatcher only reaches it once the
// split-table machinery exists; for now it forwards to the unicode
// fast path so the four-variant surface is still callable from
// tests and the rest of the codebase.
//
// CPython: Objects/dictobject.c:1160 unicodekeys_lookup_split
func compareSplit(d *Dict, slot *dictEntry, key Object, hash int64) (bool, error) {
	return compareUnicodeUnicode(d, slot, key, hash)
}

// dictProbe is the shared open-addressed driver. perturb mixes the
// upper hash bits into the probe sequence so colliding low bits
// don't form long chains. cmp is the variant-specific equality
// check; the loop returns the slot index on hit or the first empty
// slot on miss, matching the (idx, found) shape callers expect.
//
// CPython: Objects/dictobject.c:1001 do_lookup
func dictProbe(d *Dict, key Object, hash int64, cmp dictCompare) (int, bool, error) {
	mask := uint64(len(d.entries) - 1)
	i := uint64(hash) & mask
	perturb := uint64(hash)
	for {
		slot := &d.entries[i]
		if !slot.used {
			return int(i), false, nil
		}
		eq, err := cmp(d, slot, key, hash)
		if err != nil {
			return 0, false, err
		}
		if eq {
			return int(i), true, nil
		}
		perturb >>= 5
		i = (5*i + 1 + perturb) & mask
	}
}

// lookupUnicodeUnicode dispatches with the unicode/unicode comparator.
// Callers must have already confirmed both that the dict's kind is
// not General and that the lookup key is *strStub; otherwise the
// type-asserts in compareUnicodeUnicode panic.
//
// CPython: Objects/dictobject.c:1095 unicodekeys_lookup_unicode
func lookupUnicodeUnicode(d *Dict, key Object, hash int64) (int, bool, error) {
	return dictProbe(d, key, hash, compareUnicodeUnicode)
}

// lookupUnicodeGeneric dispatches with the unicode/generic comparator
// for the case where stored keys are all str but the lookup key is
// not.
//
// CPython: Objects/dictobject.c:1074 unicodekeys_lookup_generic
func lookupUnicodeGeneric(d *Dict, key Object, hash int64) (int, bool, error) {
	return dictProbe(d, key, hash, compareUnicodeGeneric)
}

// lookupGeneric is the fully generic fallback used once the table has
// even one non-str key.
//
// CPython: Objects/dictobject.c:1129 dictkeys_generic_lookup
func lookupGeneric(d *Dict, key Object, hash int64) (int, bool, error) {
	return dictProbe(d, key, hash, compareGeneric)
}

// lookupSplit is the split-keys variant. gopy doesn't ship split
// tables yet, so the dispatcher won't reach this in practice; the
// function still exists so all four CPython variants have a Go
// counterpart and the dispatcher table is complete.
//
// CPython: Objects/dictobject.c:1160 unicodekeys_lookup_split
func lookupSplit(d *Dict, key Object, hash int64) (int, bool, error) {
	if _, ok := key.(*strStub); !ok {
		return 0, false, fmt.Errorf("SystemError: lookupSplit called with non-str key of type %s", key.Type().Name)
	}
	return dictProbe(d, key, hash, compareSplit)
}

// dispatchLookup picks the variant the way _Py_dict_lookup does:
// non-General kind plus str lookup key takes the unicode/unicode
// fast path; non-General with non-str key takes unicode/generic;
// everything else falls through to the generic compare.
//
// CPython: Objects/dictobject.c:1247 _Py_dict_lookup
func dispatchLookup(d *Dict, key Object, hash int64) (int, bool, error) {
	switch d.kind {
	case dictKindUnicode:
		if _, ok := key.(*strStub); ok {
			return lookupUnicodeUnicode(d, key, hash)
		}
		return lookupUnicodeGeneric(d, key, hash)
	case dictKindSplit:
		if _, ok := key.(*strStub); ok {
			return lookupSplit(d, key, hash)
		}
		return lookupUnicodeGeneric(d, key, hash)
	default:
		return lookupGeneric(d, key, hash)
	}
}

// downgradeKindOnInsert is the bookkeeping SetItem does after every
// insert: the moment a non-str key joins the table the dict can no
// longer use the unicode-only fast paths. Promotion the other way
// (general → unicode after deletes) doesn't happen in CPython
// either since the dict-keys layout is type-specialized at creation.
//
// CPython: Objects/dictobject.c:1635 insertdict (the
// DICT_KEYS_GENERAL transition near the keys_unicode check)
func (d *Dict) downgradeKindOnInsert(key Object) {
	if d.kind != dictKindUnicode {
		return
	}
	if _, ok := key.(*strStub); !ok {
		d.kind = dictKindGeneral
	}
}
