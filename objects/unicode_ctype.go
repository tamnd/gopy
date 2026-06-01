// Codepoint classification predicates. CPython generates a packed
// _PyUnicode_TypeRecord table from UnicodeData.txt and routes
// _PyUnicode_IsLowercase / IsUppercase / IsAlpha / etc. through
// bit-test macros over that table.
//
// These wrappers route through the unicodetype leaf package, the
// dependency-free port of Objects/unicodectype.c over the real
// CPython tables, so str predicates and case mapping match CPython
// bit-for-bit (Greek final-sigma, titlecase fold pairs, the exact set
// of Cf/Cn classifications) rather than Go's stdlib unicode database.
//
// CPython: Objects/unicodectype.c:160 _PyUnicode_IsLowercase
// CPython: Objects/unicodectype.c:175 _PyUnicode_IsUppercase
// CPython: Objects/unicodectype.c:201 _PyUnicode_IsAlpha
// CPython: Objects/unicodectype.c:226 _PyUnicode_IsDigit

package objects

import "github.com/tamnd/gopy/unicodetype"

// IsLowerRune reports whether r has the Lowercase property.
//
// CPython: Objects/unicodectype.c:160 _PyUnicode_IsLowercase
func IsLowerRune(r rune) bool { return unicodetype.IsLower(r) }

// IsUpperRune reports whether r has the Uppercase property.
//
// CPython: Objects/unicodectype.c:175 _PyUnicode_IsUppercase
func IsUpperRune(r rune) bool { return unicodetype.IsUpper(r) }

// IsTitleRune reports whether r has the Titlecase property.
//
// CPython: Objects/unicodectype.c:190 _PyUnicode_IsTitlecase
func IsTitleRune(r rune) bool { return unicodetype.IsTitle(r) }

// IsAlphaRune reports whether r is a letter.
//
// CPython: Objects/unicodectype.c:201 _PyUnicode_IsAlpha
func IsAlphaRune(r rune) bool { return unicodetype.IsAlpha(r) }

// IsDigitRune reports whether r has the Digit property.
//
// CPython: Objects/unicodectype.c:226 _PyUnicode_IsDigit
func IsDigitRune(r rune) bool { return unicodetype.IsDigit(r) }

// IsDecimalRune reports whether r has the Decimal_Digit property.
//
// CPython: Objects/unicodectype.c:213 _PyUnicode_IsDecimalDigit
func IsDecimalRune(r rune) bool { return unicodetype.IsDecimalDigit(r) }

// IsNumericRune reports whether r carries the Numeric property.
//
// CPython: Objects/unicodectype.c:239 _PyUnicode_IsNumeric
func IsNumericRune(r rune) bool { return unicodetype.IsNumeric(r) }

// IsSpaceRune reports whether r is whitespace. Routes through
// isPyWhitespaceRune (the bit-for-bit port of the _PyUnicode_IsWhitespace
// table) instead of Go's unicode.IsSpace, which misses 0x1C-0x1F
// (FS/GS/RS/US) that CPython treats as whitespace.
//
// CPython: Objects/unicodectype.c:252 _PyUnicode_IsWhitespace
func IsSpaceRune(r rune) bool { return isPyWhitespaceRune(r) }

// IsPrintableRune reports whether r is printable.
//
// CPython: Objects/unicodectype.c:269 _PyUnicode_IsPrintable
func IsPrintableRune(r rune) bool { return unicodetype.IsPrintable(r) }

// IsXIDStartRune reports whether r can begin an identifier. CPython's
// tokenizer treats '_' as a valid leading character separately from
// the XID_Start property, so the wrapper folds it in here.
//
// CPython: Objects/unicodectype.c:283 _PyUnicode_IsXidStart
func IsXIDStartRune(r rune) bool { return r == '_' || unicodetype.IsXidStart(r) }

// IsXIDContinueRune reports whether r can continue an identifier.
//
// CPython: Objects/unicodectype.c:294 _PyUnicode_IsXidContinue
func IsXIDContinueRune(r rune) bool { return r == '_' || unicodetype.IsXidContinue(r) }

// IsCasedRune reports whether r is a cased character.
//
// CPython: Objects/unicodectype.c:142 _PyUnicode_IsCased
func IsCasedRune(r rune) bool { return unicodetype.IsCased(r) }

// IsCaseIgnorableRune reports whether r has the Case_Ignorable property.
//
// CPython: Objects/unicodectype.c:271 _PyUnicode_IsCaseIgnorable
func IsCaseIgnorableRune(r rune) bool { return unicodetype.IsCaseIgnorable(r) }

// ToLowerRune folds r to (simple) lowercase.
//
// CPython: Objects/unicodectype.c:65 _PyUnicode_ToLowercase
func ToLowerRune(r rune) rune { return unicodetype.ToLower(r) }

// ToUpperRune folds r to (simple) uppercase.
//
// CPython: Objects/unicodectype.c:91 _PyUnicode_ToUppercase
func ToUpperRune(r rune) rune { return unicodetype.ToUpper(r) }

// ToTitleRune folds r to (simple) titlecase.
//
// CPython: Objects/unicodectype.c:117 _PyUnicode_ToTitlecase
func ToTitleRune(r rune) rune { return unicodetype.ToTitle(r) }
