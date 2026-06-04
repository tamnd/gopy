// Package unicodetype is a dependency-free port of CPython's Unicode
// character-type helpers (Objects/unicodectype.c) over the packed
// _PyUnicode_TypeRecord tables generated from unicodetype_db.h.
//
// It lives at the module root, importing nothing from the rest of
// gopy, so both the objects package (str predicates / case mapping)
// and the unicodedata module can route through the real CPython data
// without the import cycle that a module/unicodedata dependency would
// create.
//
// CPython: Objects/unicodectype.c
package unicodetype

// Flag bits in _PyUnicode_TypeRecord.flags.
//
// CPython: Objects/unicodectype.c:13 ALPHA_MASK .. EXTENDED_CASE_MASK
const (
	alphaMask         = 0x01
	decimalMask       = 0x02
	digitMask         = 0x04
	lowerMask         = 0x08
	titleMask         = 0x40
	upperMask         = 0x80
	xidStartMask      = 0x100
	xidContinueMask   = 0x200
	printableMask     = 0x400
	numericMask       = 0x800
	caseIgnorableMask = 0x1000
	casedMask         = 0x2000
	extendedCaseMask  = 0x4000
)

// getTypeRecord mirrors gettyperecord: the shift-then-index2 walk over
// index1 / index2 into _PyUnicode_TypeRecords.
//
// CPython: Objects/unicodectype.c:43 gettyperecord
func getTypeRecord(code rune) typeRecord {
	var index int
	if code >= 0x110000 || code < 0 {
		index = 0
	} else {
		index = int(typeIndex1[code>>typeShift])
		index = int(typeIndex2[(index<<typeShift)+(int(code)&((1<<typeShift)-1))])
	}
	return typeRecords[index]
}

// ToTitle returns the titlecase character corresponding to ch, or ch
// itself when no titlecase mapping is known.
//
// CPython: Objects/unicodectype.c:62 _PyUnicode_ToTitlecase
func ToTitle(ch rune) rune {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		return rune(extendedCase[ctype.Title&0xFFFF])
	}
	return ch + ctype.Title
}

// IsTitle reports whether ch has the category 'Lt'.
//
// CPython: Objects/unicodectype.c:74 _PyUnicode_IsTitlecase
func IsTitle(ch rune) bool {
	return getTypeRecord(ch).Flags&titleMask != 0
}

// IsXidStart reports whether ch has the XID_Start property.
//
// CPython: Objects/unicodectype.c:84 _PyUnicode_IsXidStart
func IsXidStart(ch rune) bool {
	return getTypeRecord(ch).Flags&xidStartMask != 0
}

// IsXidContinue reports whether ch has the XID_Continue property.
//
// CPython: Objects/unicodectype.c:94 _PyUnicode_IsXidContinue
func IsXidContinue(ch rune) bool {
	return getTypeRecord(ch).Flags&xidContinueMask != 0
}

// ToDecimalDigit returns the decimal value (0-9) for ch, or -1 when ch
// is not a decimal digit.
//
// CPython: Objects/unicodectype.c:104 _PyUnicode_ToDecimalDigit
func ToDecimalDigit(ch rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&decimalMask != 0 {
		return int(ctype.Decimal)
	}
	return -1
}

// IsDecimalDigit reports whether ch carries the Decimal property.
//
// CPython: Objects/unicodectype.c:111 _PyUnicode_IsDecimalDigit
func IsDecimalDigit(ch rune) bool { return ToDecimalDigit(ch) >= 0 }

// ToDigit returns the digit value (0-9) for ch, or -1 when ch has no
// digit property.
//
// CPython: Objects/unicodectype.c:121 _PyUnicode_ToDigit
func ToDigit(ch rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&digitMask != 0 {
		return int(ctype.Digit)
	}
	return -1
}

// IsDigit reports whether ch carries the Digit property.
//
// CPython: Objects/unicodectype.c:128 _PyUnicode_IsDigit
func IsDigit(ch rune) bool { return ToDigit(ch) >= 0 }

// IsNumeric reports whether ch carries the Numeric property.
//
// CPython: Objects/unicodectype.c:138 _PyUnicode_IsNumeric
func IsNumeric(ch rune) bool {
	return getTypeRecord(ch).Flags&numericMask != 0
}

// ToNumeric returns the numeric value of ch and whether ch has one.
//
// CPython: Objects/unicodetype_db.h:4513 _PyUnicode_ToNumeric
func ToNumeric(ch rune) (float64, bool) {
	v, ok := numericValues[ch]
	return v, ok
}

// IsPrintable reports whether ch is printable (repr leaves it
// unescaped). Space (U+0020) counts.
//
// CPython: Objects/unicodectype.c:150 _PyUnicode_IsPrintable
func IsPrintable(ch rune) bool {
	return getTypeRecord(ch).Flags&printableMask != 0
}

// IsLower reports whether ch has the category 'Ll'.
//
// CPython: Objects/unicodectype.c:160 _PyUnicode_IsLowercase
func IsLower(ch rune) bool {
	return getTypeRecord(ch).Flags&lowerMask != 0
}

// IsUpper reports whether ch has the category 'Lu'.
//
// CPython: Objects/unicodectype.c:170 _PyUnicode_IsUppercase
func IsUpper(ch rune) bool {
	return getTypeRecord(ch).Flags&upperMask != 0
}

// ToUpper returns the (simple) uppercase character for ch.
//
// CPython: Objects/unicodectype.c:180 _PyUnicode_ToUppercase
func ToUpper(ch rune) rune {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		return rune(extendedCase[ctype.Upper&0xFFFF])
	}
	return ch + ctype.Upper
}

// ToLower returns the (simple) lowercase character for ch.
//
// CPython: Objects/unicodectype.c:192 _PyUnicode_ToLowercase
func ToLower(ch rune) rune {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		return rune(extendedCase[ctype.Lower&0xFFFF])
	}
	return ch + ctype.Lower
}

// ToLowerFull writes the full (1->N) lowercase mapping for ch into res
// and returns the count written.
//
// CPython: Objects/unicodectype.c:201 _PyUnicode_ToLowerFull
func ToLowerFull(ch rune, res []rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		index := ctype.Lower & 0xFFFF
		n := int(ctype.Lower >> 24)
		for i := range n {
			res[i] = rune(extendedCase[int(index)+i])
		}
		return n
	}
	res[0] = ch + ctype.Lower
	return 1
}

// ToTitleFull writes the full (1->N) titlecase mapping for ch into res
// and returns the count written.
//
// CPython: Objects/unicodectype.c:217 _PyUnicode_ToTitleFull
func ToTitleFull(ch rune, res []rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		index := ctype.Title & 0xFFFF
		n := int(ctype.Title >> 24)
		for i := range n {
			res[i] = rune(extendedCase[int(index)+i])
		}
		return n
	}
	res[0] = ch + ctype.Title
	return 1
}

// ToUpperFull writes the full (1->N) uppercase mapping for ch into res
// and returns the count written.
//
// CPython: Objects/unicodectype.c:233 _PyUnicode_ToUpperFull
func ToUpperFull(ch rune, res []rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 {
		index := ctype.Upper & 0xFFFF
		n := int(ctype.Upper >> 24)
		for i := range n {
			res[i] = rune(extendedCase[int(index)+i])
		}
		return n
	}
	res[0] = ch + ctype.Upper
	return 1
}

// ToFoldedFull writes the full case-folded mapping for ch into res and
// returns the count written. Falls back to ToLowerFull when ch has no
// dedicated folding.
//
// CPython: Objects/unicodectype.c:249 _PyUnicode_ToFoldedFull
func ToFoldedFull(ch rune, res []rune) int {
	ctype := getTypeRecord(ch)
	if ctype.Flags&extendedCaseMask != 0 && (ctype.Lower>>20)&7 != 0 {
		index := (ctype.Lower & 0xFFFF) + (ctype.Lower >> 24)
		n := int((ctype.Lower >> 20) & 7)
		for i := range n {
			res[i] = rune(extendedCase[int(index)+i])
		}
		return n
	}
	return ToLowerFull(ch, res)
}

// IsCased reports whether ch is a cased character.
//
// CPython: Objects/unicodectype.c:264 _PyUnicode_IsCased
func IsCased(ch rune) bool {
	return getTypeRecord(ch).Flags&casedMask != 0
}

// IsCaseIgnorable reports whether ch has the Case_Ignorable property.
//
// CPython: Objects/unicodectype.c:271 _PyUnicode_IsCaseIgnorable
func IsCaseIgnorable(ch rune) bool {
	return getTypeRecord(ch).Flags&caseIgnorableMask != 0
}

// IsAlpha reports whether ch has category 'Ll', 'Lu', 'Lt', 'Lo' or
// 'Lm'.
//
// CPython: Objects/unicodectype.c:281 _PyUnicode_IsAlpha
func IsAlpha(ch rune) bool {
	return getTypeRecord(ch).Flags&alphaMask != 0
}
