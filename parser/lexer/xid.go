// Identifier validation for the lexer's verify_identifier path.
//
// CPython runs _PyUnicode_ScanIdentifier (Objects/unicodeobject.c:12426)
// against the XID_Start and XID_Continue Unicode tables. Those tables
// are auto-generated from DerivedCoreProperties.txt by
// Tools/unicode/makeunicodedata.py and baked into the interpreter.
//
// gopy composes the same sets from Go's stdlib unicode tables using
// UAX #31's published derivation rules: ID_Start = L | Nl |
// Other_ID_Start, minus Pattern_Syntax and Pattern_White_Space.
// ID_Continue extends with Mn, Mc, Nd, Pc, and Other_ID_Continue.
// XID_* filters out NFKC-unstable code points; in Unicode 16.0 that
// filter does not remove anything from the ID_* set (the
// id_start_to_xid_start delta in DerivedCoreProperties.txt is empty
// for the BMP planes Python lexes against), so the composition is
// exact for the gate-test corpus. If a future Unicode version
// resurfaces an NFKC-unstable Letter, add an explicit exclusion
// below.
//
// CPython: Objects/unicodectype.c:283 _PyUnicode_IsXidStart
// CPython: Objects/unicodectype.c:294 _PyUnicode_IsXidContinue
// Spec: https://www.unicode.org/reports/tr31/

package lexer

import "unicode"

// isXIDStart reports whether r may begin a Python identifier. ASCII
// underscore is accepted per the CPython comment at
// Objects/unicodeobject.c:12446 ("_ must be allowed as starting an
// identifier").
func isXIDStart(r rune) bool {
	if r < 0x80 {
		return r == '_' ||
			('a' <= r && r <= 'z') ||
			('A' <= r && r <= 'Z')
	}
	if unicode.In(r, unicode.Pattern_Syntax) || unicode.In(r, unicode.Pattern_White_Space) {
		return false
	}
	return unicode.IsLetter(r) ||
		unicode.In(r, unicode.Nl) ||
		unicode.In(r, unicode.Other_ID_Start)
}

// isXIDContinue reports whether r may extend a Python identifier.
//
// CPython: Objects/unicodectype.c:294 _PyUnicode_IsXidContinue
func isXIDContinue(r rune) bool {
	if r < 0x80 {
		return r == '_' ||
			('a' <= r && r <= 'z') ||
			('A' <= r && r <= 'Z') ||
			('0' <= r && r <= '9')
	}
	if unicode.In(r, unicode.Pattern_Syntax) || unicode.In(r, unicode.Pattern_White_Space) {
		return false
	}
	if unicode.IsLetter(r) ||
		unicode.In(r, unicode.Nl) ||
		unicode.In(r, unicode.Other_ID_Start) {
		return true
	}
	return unicode.In(r, unicode.Mn) ||
		unicode.In(r, unicode.Mc) ||
		unicode.In(r, unicode.Nd) ||
		unicode.In(r, unicode.Pc) ||
		unicode.In(r, unicode.Other_ID_Continue)
}

// scanIdentifier walks s and returns (byteOffset, badRune, ok).
// On success ok is true and badRune / byteOffset are zero. On
// failure ok is false, byteOffset is the UTF-8 offset of the first
// bad rune (so callers can pin tok->cur), and badRune is the rune
// itself (so the error message can report the codepoint).
//
// An empty string fails with offset 0.
//
// CPython: Objects/unicodeobject.c:12426 _PyUnicode_ScanIdentifier
func scanIdentifier(s string) (byteOffset int, badRune rune, ok bool) {
	if s == "" {
		return 0, 0, false
	}
	first := true
	for i, r := range s {
		if first {
			first = false
			if !isXIDStart(r) {
				return i, r, false
			}
			continue
		}
		if !isXIDContinue(r) {
			return i, r, false
		}
	}
	return 0, 0, true
}
