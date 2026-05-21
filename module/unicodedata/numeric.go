// decimal / digit / numeric. Each one looks up the
// _PyUnicode_TypeRecord for the character (gettyperecord, the
// unicodetype_db.h walk) and returns the matching field, falling
// back to the caller's default (or ValueError) when the character
// does not carry that property.
//
// CPython: Objects/unicodectype.c:43 gettyperecord
// CPython: Objects/unicodectype.c:104 _PyUnicode_ToDecimalDigit
// CPython: Objects/unicodectype.c:121 _PyUnicode_ToDigit
// CPython: Objects/unicodetype_db.h:4513 _PyUnicode_ToNumeric

package unicodedata

import (
	"fmt"

	"github.com/tamnd/gopy/objects"
)

// CPython: Objects/unicodectype.c:13-25 flag-bit definitions
const (
	typeFlagAlpha         = 0x0001
	typeFlagDecimal       = 0x0002
	typeFlagDigit         = 0x0004
	typeFlagLower         = 0x0008
	typeFlagTitle         = 0x0040
	typeFlagUpper         = 0x0080
	typeFlagXIDStart      = 0x0100
	typeFlagXIDContinue   = 0x0200
	typeFlagPrintable     = 0x0400
	typeFlagNumeric       = 0x0800
	typeFlagCaseIgnorable = 0x1000
	typeFlagCased         = 0x2000
	typeFlagExtendedCase  = 0x4000
)

// getTypeRecord mirrors gettyperecord: the shift-then-index2 walk
// over typeIndex1 / typeIndex2 into typeRecords.
//
// CPython: Objects/unicodectype.c:43 gettyperecord
func getTypeRecord(code rune) typeRecord {
	if code < 0 || code >= 0x110000 {
		return typeRecords[0]
	}
	idx := int(typeIndex1[code>>typeShift])
	idx = int(typeIndex2[(idx<<typeShift)+(int(code)&((1<<typeShift)-1))])
	return typeRecords[idx]
}

// CPython: Modules/unicodedata.c:132 unicodedata_UCD_decimal_impl
func decimalBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("decimal", args)
	if err != nil {
		return nil, err
	}
	rec := getTypeRecord(r)
	if rec.Flags&typeFlagDecimal == 0 {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a decimal")
	}
	return objects.NewInt(int64(rec.Decimal)), nil
}

// CPython: Modules/unicodedata.c:184 unicodedata_UCD_digit_impl
func digitBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("digit", args)
	if err != nil {
		return nil, err
	}
	rec := getTypeRecord(r)
	if rec.Flags&typeFlagDigit == 0 {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a digit")
	}
	return objects.NewInt(int64(rec.Digit)), nil
}

// CPython: Modules/unicodedata.c:218 unicodedata_UCD_numeric_impl
// CPython: Objects/unicodetype_db.h:4513 _PyUnicode_ToNumeric
func numericBuiltin(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	r, def, err := argCharWithDefault("numeric", args)
	if err != nil {
		return nil, err
	}
	v, ok := numericValues[r]
	if !ok {
		if def != nil {
			return def, nil
		}
		return nil, fmt.Errorf("ValueError: not a numeric character")
	}
	return objects.NewFloat(v), nil
}
