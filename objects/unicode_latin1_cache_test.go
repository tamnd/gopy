package objects

import "testing"

// TestLatin1CachePopulated locks the shape of the singleton table:
// every entry must exist, classify as kind 1, carry its codepoint as
// the only character, and have its hash pre-computed (no -1 sentinel).
// Catches a future refactor that lazily fills the table or drops the
// hash-precompute pass.
func TestLatin1CachePopulated(t *testing.T) {
	for i := range 256 {
		u := latin1Cache[i]
		if u == nil {
			t.Fatalf("latin1Cache[%d] is nil", i)
		}
		if u.length != 1 {
			t.Errorf("latin1Cache[%d].length=%d, want 1", i, u.length)
		}
		if u.kind != StrKind1Byte {
			t.Errorf("latin1Cache[%d].kind=%d, want StrKind1Byte", i, u.kind)
		}
		wantASCII := i < 0x80
		if u.ascii != wantASCII {
			t.Errorf("latin1Cache[%d].ascii=%v, want %v", i, u.ascii, wantASCII)
		}
		if u.hash == -1 {
			t.Errorf("latin1Cache[%d].hash not precomputed", i)
		}
	}
}

// TestLatin1CacheIdentityFromNewStr verifies that NewStr returns the
// shared singleton for every one-codepoint string < 256. The pointer
// identity is the win that CPython buys with _Py_LATIN1_CHR: dict
// keys, attribute names, and getitem results dedupe without going
// through interning.
//
// CPython: Objects/unicodeobject.c:2086 PyUnicode_FromKindAndData
func TestLatin1CacheIdentityFromNewStr(t *testing.T) {
	for cp := range 256 {
		var s string
		if cp < 0x80 {
			s = string([]byte{byte(cp)})
		} else {
			s = string(rune(cp))
		}
		a, _ := NewStr(s).(*Unicode)
		b, _ := NewStr(s).(*Unicode)
		if a == nil || b == nil {
			t.Fatalf("cp=%d NewStr did not return *Unicode", cp)
		}
		if a != b {
			t.Errorf("cp=%d NewStr returned distinct pointers", cp)
		}
		if a != latin1Cache[cp] {
			t.Errorf("cp=%d NewStr returned non-cache pointer", cp)
		}
	}
}

// TestUnicodeGetItemHitsCache verifies that s[i] on an ASCII string
// returns the cache singleton. Mirrors CPython's `unicode_getitem`
// dispatch into `get_latin1_char` for kind-1 reads.
//
// CPython: Objects/unicodeobject.c:1848 unicode_getitem
func TestUnicodeGetItemHitsCache(t *testing.T) {
	s := NewStr("hello").(*Unicode)
	for i, b := range []byte("hello") {
		got, err := unicodeGetItemKind(s, i)
		if err != nil {
			t.Fatalf("getitem[%d]: %v", i, err)
		}
		if got != latin1Cache[b] {
			t.Errorf("getitem[%d]: not cache singleton", i)
		}
	}
}

// TestGetLatin1CharBounds locks the public accessor's range. Inputs
// outside 0..255 must return nil so the caller can fall back, never a
// silently truncated entry.
func TestGetLatin1CharBounds(t *testing.T) {
	if GetLatin1Char(-1) != nil {
		t.Error("GetLatin1Char(-1) should return nil")
	}
	if GetLatin1Char(256) != nil {
		t.Error("GetLatin1Char(256) should return nil")
	}
	if GetLatin1Char(0) != latin1Cache[0] {
		t.Error("GetLatin1Char(0) should return cache entry")
	}
	if GetLatin1Char(255) != latin1Cache[255] {
		t.Error("GetLatin1Char(255) should return cache entry")
	}
}
