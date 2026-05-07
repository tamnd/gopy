// Codepoint classification predicates. CPython generates a packed
// _PyUnicode_TypeRecord table from UnicodeData.txt and routes
// _PyUnicode_IsLowercase / IsUppercase / IsAlpha / etc. through
// bit-test macros over that table. gopy currently delegates to Go's
// `unicode` package, which carries its own version of the same
// Unicode database.
//
// The two libraries agree for the vast majority of inputs. Where they
// differ (Greek final-sigma in lowercasing, some titlecase fold
// pairs, the exact set of Cf/Cn classifications), a follow-up under
// 1677-D-tables will swap these wrappers out for a generated
// PEP 393 table that mirrors CPython's bit-for-bit. Until then this
// file is the single seam where that swap will land, so callers
// across the package go through these wrappers rather than the
// stdlib unicode package directly.
//
// CPython: Objects/unicodectype.c:160 _PyUnicode_IsLowercase
// CPython: Objects/unicodectype.c:175 _PyUnicode_IsUppercase
// CPython: Objects/unicodectype.c:201 _PyUnicode_IsAlpha
// CPython: Objects/unicodectype.c:226 _PyUnicode_IsDigit

package objects

import "unicode"

// IsLowerRune reports whether r has the Lowercase property.
//
// CPython: Objects/unicodectype.c:160 _PyUnicode_IsLowercase
func IsLowerRune(r rune) bool { return unicode.IsLower(r) }

// IsUpperRune reports whether r has the Uppercase property.
//
// CPython: Objects/unicodectype.c:175 _PyUnicode_IsUppercase
func IsUpperRune(r rune) bool { return unicode.IsUpper(r) }

// IsTitleRune reports whether r has the Titlecase property.
//
// CPython: Objects/unicodectype.c:190 _PyUnicode_IsTitlecase
func IsTitleRune(r rune) bool { return unicode.IsTitle(r) }

// IsAlphaRune reports whether r is a letter.
//
// CPython: Objects/unicodectype.c:201 _PyUnicode_IsAlpha
func IsAlphaRune(r rune) bool { return unicode.IsLetter(r) }

// IsDigitRune reports whether r has the Digit property (Nd / No / Nl).
//
// CPython: Objects/unicodectype.c:226 _PyUnicode_IsDigit
func IsDigitRune(r rune) bool { return unicode.IsDigit(r) || unicode.IsNumber(r) }

// IsDecimalRune reports whether r has the Decimal_Digit property
// (the Nd category only).
//
// CPython: Objects/unicodectype.c:213 _PyUnicode_IsDecimalDigit
func IsDecimalRune(r rune) bool { return unicode.IsDigit(r) }

// IsNumericRune reports whether r is in any Number category.
//
// CPython: Objects/unicodectype.c:239 _PyUnicode_IsNumeric
func IsNumericRune(r rune) bool { return unicode.IsNumber(r) || unicode.IsDigit(r) }

// IsSpaceRune reports whether r is whitespace.
//
// CPython: Objects/unicodectype.c:252 _PyUnicode_IsWhitespace
func IsSpaceRune(r rune) bool { return unicode.IsSpace(r) }

// IsPrintableRune reports whether r is printable. Matches CPython's
// definition: any rune that is not a control, format, surrogate,
// private-use, or unassigned code point. Space (U+0020) counts.
//
// CPython: Objects/unicodectype.c:269 _PyUnicode_IsPrintable
func IsPrintableRune(r rune) bool { return unicode.IsPrint(r) || r == ' ' }

// IsXIDStartRune reports whether r can begin an identifier.
//
// CPython: Objects/unicodectype.c:283 _PyUnicode_IsXidStart
func IsXIDStartRune(r rune) bool { return unicode.IsLetter(r) || r == '_' }

// IsXIDContinueRune reports whether r can continue an identifier.
//
// CPython: Objects/unicodectype.c:294 _PyUnicode_IsXidContinue
func IsXIDContinueRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// IsCasedRune reports whether r has any of upper/lower/title case.
//
// CPython: Objects/unicodectype.c:142 _PyUnicode_IsCased
func IsCasedRune(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r)
}

// ToLowerRune folds r to lowercase.
//
// CPython: Objects/unicodectype.c:65 _PyUnicode_ToLowercase
func ToLowerRune(r rune) rune { return unicode.ToLower(r) }

// ToUpperRune folds r to uppercase.
//
// CPython: Objects/unicodectype.c:91 _PyUnicode_ToUppercase
func ToUpperRune(r rune) rune { return unicode.ToUpper(r) }

// ToTitleRune folds r to titlecase.
//
// CPython: Objects/unicodectype.c:117 _PyUnicode_ToTitlecase
func ToTitleRune(r rune) rune { return unicode.ToTitle(r) }
