// Split-keys storage for instance __dict__. CPython lays out the
// keys and hashes in a shared PyDictKeysObject that every instance
// of a class points at; each instance only carries a per-instance
// values[] slice. When an unusual operation happens (a key not in
// the shared set, or any delete) the dict is "materialized" back to
// a combined layout where keys and values live together.
//
// gopy ports the surface but not yet the storage savings. NewSplitDict
// returns a regular combined Dict pre-populated with the shared key
// names mapped to None; the dict carries a back-reference to the
// SharedKeys it was built from so the type machinery in 1672 can
// inspect provenance and so the eventual convert-to-combined path
// has somewhere to land. Real cross-instance sharing arrives once
// the type system actually exercises the path; until then the
// dispatcher's dictKindSplit branch is reachable through this
// constructor for testing parity.
//
// CPython: Objects/dictobject.c:897 new_dict_with_shared_keys
// CPython: Objects/dictobject.c:567 PyDictKeysObject

package objects

// SharedKeys is the shared keys table that every instance of a class
// points at. Holds the unicode key names and their cached hashes; the
// per-instance values[] lives back in the Dict. Building a SharedKeys
// always succeeds (the names are validated as str by the caller), but
// any subsequent insert of a key not in this set converts the dict
// out of split mode.
//
// CPython: Objects/dictobject.c:567 PyDictKeysObject (DICT_KEYS_SPLIT)
type SharedKeys struct {
	keys   []*strStub
	hashes []int64
	refs   int // number of split-dicts referencing this
}

// NewSharedKeys builds a SharedKeys from a list of attribute names.
// Each name's hash is cached so split-mode lookups don't rehash the
// shared key on every probe.
//
// CPython: Objects/dictobject.c:625 new_keys_object (with split flag)
func NewSharedKeys(names []string) (*SharedKeys, error) {
	sk := &SharedKeys{
		keys:   make([]*strStub, len(names)),
		hashes: make([]int64, len(names)),
	}
	for i, name := range names {
		s := NewStr(name).(*strStub)
		h, err := Hash(s)
		if err != nil {
			return nil, err
		}
		sk.keys[i] = s
		sk.hashes[i] = h
	}
	return sk, nil
}

// Len returns the number of shared keys.
func (sk *SharedKeys) Len() int { return len(sk.keys) }

// HasKey reports whether name is one of the shared keys. Used by
// SetItem to decide whether an insert keeps the dict in split mode
// or has to materialize it to combined.
func (sk *SharedKeys) HasKey(name string) bool {
	for _, k := range sk.keys {
		if k.v == name {
			return true
		}
	}
	return false
}

// NewSplitDict builds a fresh dict whose initial keys are the shared
// set, each mapped to None. The returned dict carries a back-reference
// to sk so the type machinery can detect it's in split mode and so
// SharedKeys.refs reflects how many instances point at it.
//
// CPython: Objects/dictobject.c:897 new_dict_with_shared_keys
func NewSplitDict(sk *SharedKeys) *Dict {
	d := NewDict()
	d.kind = dictKindSplit
	d.sharedKeys = sk
	sk.refs++
	for i, key := range sk.keys {
		_ = dictInsert(d, sk.hashes[i], key, None())
	}
	return d
}

// IsSplit reports whether d is still in split-keys mode (i.e. it
// hasn't been converted to combined storage yet).
func (d *Dict) IsSplit() bool { return d.kind == dictKindSplit && d.sharedKeys != nil }

// ConvertToCombined releases the dict's reference to its shared keys
// and flips it to combined storage. Once converted there's no path
// back: the dict owns its keys table outright and SharedKeys.refs
// drops by one, so a deleted instance no longer shows up in the
// type-level shared count.
//
// CPython: Objects/dictobject.c:2125 dictresize (the !DK_IS_SHARED branch
// after a split insert that escaped the shared key set)
func (d *Dict) ConvertToCombined() {
	if d.sharedKeys == nil {
		return
	}
	d.sharedKeys.refs--
	d.sharedKeys = nil
	if d.kind == dictKindSplit {
		d.kind = dictKindUnicode
	}
}
