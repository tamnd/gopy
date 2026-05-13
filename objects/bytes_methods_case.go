// ASCII case ops and classifier predicates shared by bytes and
// bytearray. Each row mirrors one CPython _Py_bytes_* helper from
// Objects/bytes_methods.c. The classifiers all match CPython's
// "non-empty + every byte passes" pattern; the cased variants
// (islower/isupper/istitle) additionally require at least one cased
// byte.
//
// CPython: Objects/bytes_methods.c

package objects

// Py_ISSPACE recognizes space, tab, LF, VT, FF, CR. The bytes/bytearray
// classifiers use the C locale, not anything locale-aware.
//
// CPython: Include/internal/pycore_ctype.h Py_ISSPACE
//
// (isASCIISpace lives in bytes_methods_wrap.go; do not redefine here.)

// pyIsAlpha returns true for ASCII letters.
//
// CPython: Include/internal/pycore_ctype.h Py_ISALPHA
func pyIsAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// pyIsDigit returns true for the ASCII digits 0..9.
//
// CPython: Include/internal/pycore_ctype.h Py_ISDIGIT
func pyIsDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// pyIsAlnum is the union of pyIsAlpha and pyIsDigit.
//
// CPython: Include/internal/pycore_ctype.h Py_ISALNUM
func pyIsAlnum(c byte) bool {
	return pyIsAlpha(c) || pyIsDigit(c)
}

// pyIsLower returns true for the ASCII lowercase letters.
//
// CPython: Include/internal/pycore_ctype.h Py_ISLOWER
func pyIsLower(c byte) bool {
	return c >= 'a' && c <= 'z'
}

// pyIsUpper returns true for the ASCII uppercase letters.
//
// CPython: Include/internal/pycore_ctype.h Py_ISUPPER
func pyIsUpper(c byte) bool {
	return c >= 'A' && c <= 'Z'
}

// pyToLower maps an ASCII uppercase letter to its lowercase form. Any
// other byte passes through unchanged.
//
// CPython: Include/internal/pycore_ctype.h Py_TOLOWER
func pyToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// pyToUpper is the inverse of pyToLower.
//
// CPython: Include/internal/pycore_ctype.h Py_TOUPPER
func pyToUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// classifyAll walks v and returns true iff len(v) > 0 and pred holds
// for every byte. Empty input matches CPython's `Py_RETURN_FALSE` for
// the empty-string special case.
//
// CPython: Objects/bytes_methods.c _Py_bytes_isspace (and friends)
func classifyAll(v []byte, pred func(byte) bool) bool {
	if len(v) == 0 {
		return false
	}
	for _, c := range v {
		if !pred(c) {
			return false
		}
	}
	return true
}

// IsSpace mirrors bytes.isspace / bytearray.isspace.
//
// CPython: Objects/bytes_methods.c:12 _Py_bytes_isspace
func bytesIsSpace(v []byte) bool {
	return classifyAll(v, isASCIISpace)
}

// IsAlpha mirrors bytes.isalpha.
//
// CPython: Objects/bytes_methods.c:42 _Py_bytes_isalpha
func bytesIsAlpha(v []byte) bool {
	return classifyAll(v, pyIsAlpha)
}

// IsAlnum mirrors bytes.isalnum.
//
// CPython: Objects/bytes_methods.c:72 _Py_bytes_isalnum
func bytesIsAlnum(v []byte) bool {
	return classifyAll(v, pyIsAlnum)
}

// IsDigit mirrors bytes.isdigit.
//
// CPython: Objects/bytes_methods.c:102 _Py_bytes_isdigit
func bytesIsDigit(v []byte) bool {
	return classifyAll(v, pyIsDigit)
}

// IsAscii mirrors bytes.isascii. Empty input returns True (the only
// classifier that does, matching CPython 3.14).
//
// CPython: Objects/bytes_methods.c:731 _Py_bytes_isascii
func bytesIsAscii(v []byte) bool {
	for _, c := range v {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// IsLower walks v, fails if any uppercase letter is present, and
// requires at least one lowercase letter to commit to True.
//
// CPython: Objects/bytes_methods.c:132 _Py_bytes_islower
func bytesIsLower(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	cased := false
	for _, c := range v {
		if pyIsUpper(c) {
			return false
		}
		if !cased && pyIsLower(c) {
			cased = true
		}
	}
	return cased
}

// IsUpper is bytesIsLower with the cases swapped.
//
// CPython: Objects/bytes_methods.c:166 _Py_bytes_isupper
func bytesIsUpper(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	cased := false
	for _, c := range v {
		if pyIsLower(c) {
			return false
		}
		if !cased && pyIsUpper(c) {
			cased = true
		}
	}
	return cased
}

// IsTitle returns true when each run of cased bytes starts with an
// uppercase letter and continues with lowercase letters only.
// Single-byte input is title-cased iff the byte is uppercase.
//
// CPython: Objects/bytes_methods.c:202 _Py_bytes_istitle
func bytesIsTitle(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	if len(v) == 1 {
		return pyIsUpper(v[0])
	}
	cased := false
	previousIsCased := false
	for _, c := range v {
		switch {
		case pyIsUpper(c):
			if previousIsCased {
				return false
			}
			previousIsCased = true
			cased = true
		case pyIsLower(c):
			if !previousIsCased {
				return false
			}
			previousIsCased = true
			cased = true
		default:
			previousIsCased = false
		}
	}
	return cased
}

// bytesLower returns a copy of v with each uppercase ASCII letter
// lowercased.
//
// CPython: Objects/bytes_methods.c:251 _Py_bytes_lower
func bytesLower(v []byte) []byte {
	out := make([]byte, len(v))
	for i, c := range v {
		out[i] = pyToLower(c)
	}
	return out
}

// bytesUpper is bytesLower with the cases swapped.
//
// CPython: Objects/bytes_methods.c:267 _Py_bytes_upper
func bytesUpper(v []byte) []byte {
	out := make([]byte, len(v))
	for i, c := range v {
		out[i] = pyToUpper(c)
	}
	return out
}

// bytesTitle titles each ASCII word: the first cased byte after a
// non-cased run becomes uppercase, the rest stay lowercase.
//
// CPython: Objects/bytes_methods.c:284 _Py_bytes_title
func bytesTitle(v []byte) []byte {
	out := make([]byte, len(v))
	previousIsCased := false
	for i, c := range v {
		switch {
		case pyIsLower(c):
			if !previousIsCased {
				c = pyToUpper(c)
			}
			previousIsCased = true
		case pyIsUpper(c):
			if previousIsCased {
				c = pyToLower(c)
			}
			previousIsCased = true
		default:
			previousIsCased = false
		}
		out[i] = c
	}
	return out
}

// bytesCapitalize uppercases the first byte and lowercases the rest.
//
// CPython: Objects/bytes_methods.c:313 _Py_bytes_capitalize
func bytesCapitalize(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	out := make([]byte, len(v))
	out[0] = pyToUpper(v[0])
	for i := 1; i < len(v); i++ {
		out[i] = pyToLower(v[i])
	}
	return out
}

// bytesSwapCase flips the case of every cased byte.
//
// CPython: Objects/bytes_methods.c:329 _Py_bytes_swapcase
func bytesSwapCase(v []byte) []byte {
	out := make([]byte, len(v))
	for i, c := range v {
		switch {
		case pyIsLower(c):
			out[i] = pyToUpper(c)
		case pyIsUpper(c):
			out[i] = pyToLower(c)
		default:
			out[i] = c
		}
	}
	return out
}

// bytesRemovePrefix returns a copy of v with prefix stripped from the
// front when v starts with prefix. Otherwise v is returned unchanged.
//
// CPython: Objects/bytesobject.c:2360 bytes_removeprefix_impl
func bytesRemovePrefix(v, prefix []byte) []byte {
	if len(prefix) == 0 || len(prefix) > len(v) {
		out := make([]byte, len(v))
		copy(out, v)
		return out
	}
	for i, c := range prefix {
		if v[i] != c {
			out := make([]byte, len(v))
			copy(out, v)
			return out
		}
	}
	out := make([]byte, len(v)-len(prefix))
	copy(out, v[len(prefix):])
	return out
}

// bytesRemoveSuffix is bytesRemovePrefix from the other end.
//
// CPython: Objects/bytesobject.c:2386 bytes_removesuffix_impl
func bytesRemoveSuffix(v, suffix []byte) []byte {
	if len(suffix) == 0 || len(suffix) > len(v) {
		out := make([]byte, len(v))
		copy(out, v)
		return out
	}
	off := len(v) - len(suffix)
	for i, c := range suffix {
		if v[off+i] != c {
			out := make([]byte, len(v))
			copy(out, v)
			return out
		}
	}
	out := make([]byte, off)
	copy(out, v[:off])
	return out
}
