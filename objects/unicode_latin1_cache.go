// Latin1 single-character interning. CPython lays 256 pre-built str
// singletons into the static `_PyRuntime.static_objects.strings` table:
// chars 0-127 live in `ascii[]`, chars 128-255 in `latin1[]`. Every
// path that wants a one-character str (`__getitem__`, `chr(n)`,
// `s.encode().decode()`) routes through `_Py_LATIN1_CHR(ch)` instead of
// minting a fresh PyUnicodeObject.
//
// gopy mirrors this with a fixed 256-entry table built at init time.
// Hash is precomputed so the singletons participate in dict / set hot
// paths without paying the on-first-use HashCached() cost. The
// constructor doors (`NewStr`, `unicodeGetItemKind`) consult the cache
// before allocating.
//
// CPython: Objects/unicodeobject.c:1958 get_latin1_char
// CPython: Include/internal/pycore_global_strings.h:834 _Py_LATIN1_CHR
package objects

// latin1Cache holds the 256 single-codepoint str singletons.
// Index i is the str of codepoint i. Built once at init time and
// thereafter immutable.
//
// CPython: Include/internal/pycore_global_strings.h:818 strings.ascii /
// strings.latin1 tables
var latin1Cache [256]*Unicode

func init() {
	for i := range 256 {
		var v string
		if i < 0x80 {
			v = string([]byte{byte(i)})
		} else {
			// UTF-8 encode codepoint i (0x80..0xFF) as two bytes:
			// 110xxxxx 10xxxxxx.
			v = string([]byte{
				byte(0xC0 | (i >> 6)),
				byte(0x80 | (i & 0x3F)),
			})
		}
		u := &Unicode{
			v:      v,
			length: 1,
			kind:   StrKind1Byte,
			ascii:  i < 0x80,
			ready:  true,
			hash:   -1,
		}
		u.init(strType)
		// Pre-compute the hash so the singletons don't race on first
		// HashCached() call. Mirrors CPython's static-string preprocessing
		// in `_PyUnicode_InitStaticStrings`.
		u.hash = HashString(v)
		latin1Cache[i] = u
	}
}

// GetLatin1Char returns the cached singleton for codepoint ch (0..255).
// Out-of-range inputs return nil so the caller can fall back to the
// slow allocator.
//
// CPython: Objects/unicodeobject.c:1958 get_latin1_char
func GetLatin1Char(ch int) *Unicode {
	if ch < 0 || ch >= 256 {
		return nil
	}
	return latin1Cache[ch]
}

// latin1StringHit reports the cached singleton for the UTF-8 string s
// when it encodes exactly one codepoint with ordinal < 256. Returns
// (nil, false) otherwise. Used by NewStr to short-circuit the
// allocation for one-char strings.
//
// CPython: Objects/unicodeobject.c:2086 PyUnicode_FromKindAndData
// (the kind-1 single-char branch routes through get_latin1_char).
func latin1StringHit(s string) (*Unicode, bool) {
	switch len(s) {
	case 1:
		b := s[0]
		if b < 0x80 {
			return latin1Cache[b], true
		}
	case 2:
		b0, b1 := s[0], s[1]
		if (b0 == 0xC2 || b0 == 0xC3) && (b1&0xC0) == 0x80 {
			cp := int(b0&0x1F)<<6 | int(b1&0x3F)
			return latin1Cache[cp], true
		}
	}
	return nil, false
}
