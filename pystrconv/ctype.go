// Package pystrconv ports the locale-independent string and number
// conversion utilities from cpython/Python/. v0.4 covers pyctype.c,
// pystrcmp.c, mystrtoul.c, pystrtod.c+dtoa.c, and pystrhex.c.
package pystrconv

// Locale-independent ctype.h-style classification, ported from
// cpython/Python/pyctype.c. CPython carries a 256-entry bitmask table
// plus separate tolower/toupper tables; gopy computes the same values
// directly so the source stays compact and the truth table stays
// auditable from the function bodies.
//
// CPython: Python/pyctype.c overview

// Character classification flags. Values match
// cpython/Include/cpython/pyctype.h so the bitmask layout is
// preserved.
//
// CPython: Include/cpython/pyctype.h:L8 PY_CTF_*
const (
	CtLower  = 0x01
	CtUpper  = 0x02
	CtAlpha  = CtLower | CtUpper
	CtDigit  = 0x04
	CtAlnum  = CtAlpha | CtDigit
	CtSpace  = 0x08
	CtXDigit = 0x10
)

// CharMask coerces a byte to its 0..255 range. CPython's Py_CHARMASK
// exists because C's char may be signed; in Go a byte is always 0..255
// but the function preserves the source-shape for ports that use
// CHARMASK to silence sign warnings.
//
// CPython: Include/cpython/pyport.h:L37 Py_CHARMASK
func CharMask(c byte) byte { return c }

// Flags returns the classification bitmask for c. Mirrors
// `_Py_ctype_table[c]`.
//
// CPython: Python/pyctype.c:L5 _Py_ctype_table
func Flags(c byte) uint32 {
	var f uint32
	switch {
	case c >= '0' && c <= '9':
		f |= CtDigit | CtXDigit
	case c >= 'A' && c <= 'F':
		f |= CtUpper | CtXDigit
	case c >= 'a' && c <= 'f':
		f |= CtLower | CtXDigit
	case c >= 'G' && c <= 'Z':
		f |= CtUpper
	case c >= 'g' && c <= 'z':
		f |= CtLower
	}
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		f |= CtSpace
	}
	return f
}

// IsLower mirrors Py_ISLOWER.
//
// CPython: Include/cpython/pyctype.h:L21 Py_ISLOWER
func IsLower(c byte) bool { return Flags(c)&CtLower != 0 }

// IsUpper mirrors Py_ISUPPER.
//
// CPython: Include/cpython/pyctype.h:L22 Py_ISUPPER
func IsUpper(c byte) bool { return Flags(c)&CtUpper != 0 }

// IsAlpha mirrors Py_ISALPHA.
//
// CPython: Include/cpython/pyctype.h:L23 Py_ISALPHA
func IsAlpha(c byte) bool { return Flags(c)&CtAlpha != 0 }

// IsDigit mirrors Py_ISDIGIT.
//
// CPython: Include/cpython/pyctype.h:L24 Py_ISDIGIT
func IsDigit(c byte) bool { return Flags(c)&CtDigit != 0 }

// IsXDigit mirrors Py_ISXDIGIT.
//
// CPython: Include/cpython/pyctype.h:L25 Py_ISXDIGIT
func IsXDigit(c byte) bool { return Flags(c)&CtXDigit != 0 }

// IsAlnum mirrors Py_ISALNUM.
//
// CPython: Include/cpython/pyctype.h:L26 Py_ISALNUM
func IsAlnum(c byte) bool { return Flags(c)&CtAlnum != 0 }

// IsSpace mirrors Py_ISSPACE.
//
// CPython: Include/cpython/pyctype.h:L27 Py_ISSPACE
func IsSpace(c byte) bool { return Flags(c)&CtSpace != 0 }

// ToLower mirrors Py_TOLOWER. ASCII-only, locale-independent.
//
// CPython: Python/pyctype.c:L145 _Py_ctype_tolower
func ToLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// ToUpper mirrors Py_TOUPPER. ASCII-only, locale-independent.
//
// CPython: Python/pyctype.c:L180 _Py_ctype_toupper
func ToUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}
