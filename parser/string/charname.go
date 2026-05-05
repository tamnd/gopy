// CPython: Objects/unicodeobject.c _PyUnicode_DecodeUnicodeEscape
// path for the \N{NAME} escape. The full lookup goes through
// unicodedata.lookup(), which expands to the one-million-name UCD
// table. gopy ships a small builtin for the common names so the
// codepath compiles and a few smoke tests pass; the heavy table
// generator lands alongside the unicodedata port.

package string

import "fmt"

// CharByName returns the rune for one of the common Unicode names
// the test suite exercises. Names are upper-case, hyphens and
// spaces in the canonical form.
//
// CPython: Modules/_unicodedata.c ucd_lookup
func CharByName(name string) (rune, error) {
	if r, ok := commonCharNames[name]; ok {
		return r, nil
	}
	return 0, fmt.Errorf("unknown Unicode character name %q", name)
}

// commonCharNames is the smoke-test table. A real port replaces
// this with a generated map driven by Modules/unicodedata_db.h.
var commonCharNames = map[string]rune{
	"NULL":                            0x0000,
	"START OF HEADING":                0x0001,
	"BELL":                            0x0007,
	"BACKSPACE":                       0x0008,
	"HORIZONTAL TABULATION":           0x0009,
	"LINE FEED":                       0x000a,
	"CARRIAGE RETURN":                 0x000d,
	"SPACE":                           0x0020,
	"EXCLAMATION MARK":                0x0021,
	"QUOTATION MARK":                  0x0022,
	"NUMBER SIGN":                     0x0023,
	"DOLLAR SIGN":                     0x0024,
	"PERCENT SIGN":                    0x0025,
	"AMPERSAND":                       0x0026,
	"APOSTROPHE":                      0x0027,
	"LEFT PARENTHESIS":                0x0028,
	"RIGHT PARENTHESIS":               0x0029,
	"ASTERISK":                        0x002a,
	"PLUS SIGN":                       0x002b,
	"COMMA":                           0x002c,
	"HYPHEN-MINUS":                    0x002d,
	"FULL STOP":                       0x002e,
	"SOLIDUS":                         0x002f,
	"COLON":                           0x003a,
	"SEMICOLON":                       0x003b,
	"LESS-THAN SIGN":                  0x003c,
	"EQUALS SIGN":                     0x003d,
	"GREATER-THAN SIGN":               0x003e,
	"QUESTION MARK":                   0x003f,
	"COMMERCIAL AT":                   0x0040,
	"LATIN SMALL LETTER A":            0x0061,
	"LATIN CAPITAL LETTER A":          0x0041,
	"LATIN SMALL LETTER E WITH ACUTE": 0x00e9,
	"LATIN SMALL LETTER N WITH TILDE": 0x00f1,
	"GREEK SMALL LETTER ALPHA":        0x03b1,
	"GREEK CAPITAL LETTER ALPHA":      0x0391,
	"GREEK SMALL LETTER PI":           0x03c0,
	"BULLET":                          0x2022,
	"HORIZONTAL ELLIPSIS":             0x2026,
	"COPYRIGHT SIGN":                  0x00a9,
	"REGISTERED SIGN":                 0x00ae,
	"DEGREE SIGN":                     0x00b0,
	"PILCROW SIGN":                    0x00b6,
	"INFINITY":                        0x221e,
	"BLACK STAR":                      0x2605,
	"WHITE STAR":                      0x2606,
	"SNOWMAN":                         0x2603,
	"WHITE SMILING FACE":              0x263a,
	"BLACK SMILING FACE":              0x263b,
	"GRINNING FACE":                   0x1f600,
	"FACE WITH TEARS OF JOY":          0x1f602,
}
